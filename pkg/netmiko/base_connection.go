package netmiko

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Transport abstracts the interactive byte stream (SSH channel or mock).
type Transport interface {
	io.ReadWriteCloser
}

// SSHTransport wraps a real crypto/ssh Client and Session.
type SSHTransport struct {
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
}

func (s *SSHTransport) Read(p []byte) (int, error) {
	return s.stdout.Read(p)
}

func (s *SSHTransport) Write(p []byte) (int, error) {
	return s.stdin.Write(p)
}

func (s *SSHTransport) Close() error {
	var errs []error
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.session != nil {
		if err := s.session.Close(); err != nil && !errors.Is(err, io.EOF) {
			errs = append(errs, err)
		}
	}
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// BaseConnection provides standard terminal interaction logic.
type BaseConnection struct {
	Device            Device
	Transport         Transport
	BasePrompt        string
	DefaultPromptTerm string // e.g. "#", ">", "$"
	mu                sync.Mutex
	readBuf           bytes.Buffer
}

// NewBaseConnection creates a BaseConnection initialized with Device options.
func NewBaseConnection(dev Device) *BaseConnection {
	if dev.Port == 0 {
		dev.Port = 22
	}
	if dev.Timeout == 0 {
		dev.Timeout = 30 * time.Second
	}
	if dev.GlobalDelayFactor == 0 {
		dev.GlobalDelayFactor = 1.0
	}
	return &BaseConnection{
		Device:            dev,
		DefaultPromptTerm: "#",
	}
}

// SetTransport allows injecting a mock or pre-configured transport (useful for unit tests).
func (bc *BaseConnection) SetTransport(t Transport) {
	bc.Transport = t
}

func (bc *BaseConnection) GetDevice() Device {
	return bc.Device
}

func (bc *BaseConnection) GetBasePrompt() string {
	return bc.BasePrompt
}

// Connect dials SSH, requests a PTY, and opens an interactive shell.
func (bc *BaseConnection) Connect(ctx context.Context) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.Transport != nil {
		return nil // Already connected or mock transport set
	}

	authMethods := []ssh.AuthMethod{}
	if bc.Device.Password != "" {
		authMethods = append(authMethods, ssh.Password(bc.Device.Password))
		// Support keyboard-interactive fallback
		authMethods = append(authMethods, ssh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = bc.Device.Password
			}
			return answers, nil
		}))
	}

	if bc.Device.SSHKeyPath != "" {
		keyBytes, err := os.ReadFile(bc.Device.SSHKeyPath)
		if err != nil {
			return fmt.Errorf("failed to read ssh key file %q: %w", bc.Device.SSHKeyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			return fmt.Errorf("failed to parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	sshConfig := &ssh.ClientConfig{
		User:            bc.Device.Username,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Standard for network devices
		Timeout:         bc.Device.Timeout,
	}

	addr := net.JoinHostPort(bc.Device.Host, fmt.Sprintf("%d", bc.Device.Port))
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("ssh dial failed for %s: %w", addr, err)
	}

	session, err := client.NewSession()
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("failed to create ssh session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("vt100", 80, 24, modes); err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("request for pty failed: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	if err := session.Shell(); err != nil {
		_ = session.Close()
		_ = client.Close()
		return fmt.Errorf("failed to start shell: %w", err)
	}

	bc.Transport = &SSHTransport{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
	}

	return nil
}

// Disconnect terminates the session and transport.
func (bc *BaseConnection) Disconnect() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.Transport == nil {
		return nil
	}
	err := bc.Transport.Close()
	bc.Transport = nil
	return err
}

// WriteChannel writes raw data string to the transport.
func (bc *BaseConnection) WriteChannel(data string) error {
	if bc.Transport == nil {
		return errors.New("connection is not established")
	}
	b := []byte(data)
	if bc.Device.SessionLog != nil {
		_, _ = bc.Device.SessionLog.Write(b)
	}
	_, err := bc.Transport.Write(b)
	return err
}

// ReadChannel reads available data into an in-memory buffer.
func (bc *BaseConnection) ReadChannel() (string, error) {
	if bc.Transport == nil {
		return "", errors.New("connection is not established")
	}
	buf := make([]byte, 4096)
	n, err := bc.Transport.Read(buf)
	if n > 0 {
		received := buf[:n]
		if bc.Device.SessionLog != nil {
			_, _ = bc.Device.SessionLog.Write(received)
		}
		bc.readBuf.Write(received)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	out := bc.readBuf.String()
	bc.readBuf.Reset()
	return out, nil
}

// ReadUntilPattern repeatedly reads the channel until pattern matches or timeout is reached.
func (bc *BaseConnection) ReadUntilPattern(pattern *regexp.Regexp, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = bc.Device.Timeout
	}
	deadline := time.Now().Add(timeout)
	var output bytes.Buffer

	// Flush whatever was buffered
	if bc.readBuf.Len() > 0 {
		output.Write(bc.readBuf.Bytes())
		bc.readBuf.Reset()
		if pattern.Match(output.Bytes()) {
			return output.String(), nil
		}
	}

	temp := make([]byte, 2048)
	for time.Now().Before(deadline) {
		// Attempt non-blocking / short-timeout read
		n, err := bc.Transport.Read(temp)
		if n > 0 {
			chunk := temp[:n]
			if bc.Device.SessionLog != nil {
				_, _ = bc.Device.SessionLog.Write(chunk)
			}
			output.Write(chunk)
			if pattern.Match(output.Bytes()) {
				return output.String(), nil
			}
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return output.String(), err
		}
		time.Sleep(50 * time.Millisecond)
	}

	return output.String(), fmt.Errorf("timed out after %v waiting for pattern %q (output: %q)", timeout, pattern.String(), output.String())
}

// ReadUntilPrompt reads until the base prompt or standard termination pattern is seen.
func (bc *BaseConnection) ReadUntilPrompt(timeout time.Duration) (string, error) {
	var p string
	if bc.BasePrompt == "" {
		p = `[#>$%]\s*$`
	} else {
		p = regexp.QuoteMeta(bc.BasePrompt) + `.*[#>$%]\s*$`
	}
	re := regexp.MustCompile(p)
	return bc.ReadUntilPattern(re, timeout)
}

// FindPrompt sends a carriage return and discovers the current terminal prompt.
func (bc *BaseConnection) FindPrompt() (string, error) {
	// Send newline to awaken prompt
	if err := bc.WriteChannel("\n"); err != nil {
		return "", err
	}
	promptRegex := regexp.MustCompile(`(?:^|[\r\n]+)\s*([a-zA-Z0-9_\-\.\:\(\)]+[#>$%])\s*$`)
	out, err := bc.ReadUntilPattern(promptRegex, 5*time.Second)
	if err != nil {
		// Fallback: strip line breaks and return last line
		lines := strings.Split(NormalizeLinebreaks(StripAnsi(out)), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			if line != "" {
				return line, nil
			}
		}
		return "", fmt.Errorf("unable to determine prompt: %w", err)
	}

	matches := promptRegex.FindStringSubmatch(out)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1]), nil
	}
	return strings.TrimSpace(out), nil
}

// SetBasePrompt sets and records the clean base prompt without privilege indicators.
func (bc *BaseConnection) SetBasePrompt() (string, error) {
	prompt, err := bc.FindPrompt()
	if err != nil {
		return "", err
	}
	// Strip trailing privilege markers: #, >, $
	clean := strings.TrimRight(prompt, "#>$% \t")
	bc.BasePrompt = clean
	return clean, nil
}

// SendCommand sends a command and returns the output up to the next prompt.
func (bc *BaseConnection) SendCommand(command string, opts ...CommandOption) (string, error) {
	options := CommandOptions{
		ReadTimeout:  bc.Device.Timeout,
		DelayFactor:  bc.Device.GlobalDelayFactor,
		StripPrompt:  true,
		StripCommand: true,
	}
	for _, opt := range opts {
		opt(&options)
	}

	// Prepare expect pattern
	pattern := options.ExpectPattern
	if pattern == nil {
		if bc.BasePrompt == "" {
			_, _ = bc.SetBasePrompt()
		}
		pattern = regexp.MustCompile(regexp.QuoteMeta(bc.BasePrompt) + `.*[#>$%]\s*$`)
	}

	// Send command
	if err := bc.WriteChannel(command + "\n"); err != nil {
		return "", err
	}

	rawOut, err := bc.ReadUntilPattern(pattern, options.ReadTimeout)
	if err != nil {
		return rawOut, err
	}

	return CleanOutput(command, rawOut, options.StripCommand, options.StripPrompt), nil
}

// SendCommandTiming writes a command and reads until the channel is idle for idleTimeout.
func (bc *BaseConnection) SendCommandTiming(command string, idleTimeout time.Duration, opts ...CommandOption) (string, error) {
	if idleTimeout <= 0 {
		idleTimeout = time.Duration(float64(2*time.Second) * bc.Device.GlobalDelayFactor)
	}

	if err := bc.WriteChannel(command + "\n"); err != nil {
		return "", err
	}

	var output bytes.Buffer
	deadline := time.Now().Add(bc.Device.Timeout)
	lastDataTime := time.Now()
	temp := make([]byte, 2048)

	for time.Now().Before(deadline) {
		n, err := bc.Transport.Read(temp)
		if n > 0 {
			chunk := temp[:n]
			output.Write(chunk)
			if bc.Device.SessionLog != nil {
				_, _ = bc.Device.SessionLog.Write(chunk)
			}
			lastDataTime = time.Now()
		}
		if time.Since(lastDataTime) > idleTimeout && output.Len() > 0 {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return CleanOutput(command, output.String(), true, true), nil
}

// SendConfigSet enters config mode, executes commands, and exits config mode.
func (bc *BaseConnection) SendConfigSet(commands []string, opts ...ConfigOption) (string, error) {
	options := ConfigOptions{
		ConfigModeCommand: "configure terminal",
		ExitConfigCommand: "end",
		ReadTimeout:       bc.Device.Timeout,
		StripPrompt:       true,
	}
	for _, opt := range opts {
		opt(&options)
	}

	var cumulativeOutput strings.Builder

	// Enter config mode if needed
	if err := bc.WriteChannel(options.ConfigModeCommand + "\n"); err != nil {
		return "", err
	}
	out, err := bc.ReadUntilPattern(regexp.MustCompile(`\(config.*\)#\s*$`), 10*time.Second)
	if err == nil {
		cumulativeOutput.WriteString(out)
	}

	for _, cmd := range commands {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if err := bc.WriteChannel(cmd + "\n"); err != nil {
			return cumulativeOutput.String(), err
		}
		res, err := bc.ReadUntilPattern(regexp.MustCompile(`[#>$%]\s*$`), options.ReadTimeout)
		if err != nil {
			return cumulativeOutput.String(), err
		}
		cumulativeOutput.WriteString(res)
	}

	// Exit config mode
	if err := bc.WriteChannel(options.ExitConfigCommand + "\n"); err != nil {
		return cumulativeOutput.String(), err
	}
	exitOut, _ := bc.ReadUntilPrompt(10 * time.Second)
	cumulativeOutput.WriteString(exitOut)

	return CleanOutput("", cumulativeOutput.String(), false, options.StripPrompt), nil
}

// Enable default fallback: checks if already in enable mode, otherwise errors or sends enable.
func (bc *BaseConnection) Enable() error {
	inEnable, err := bc.CheckEnableMode()
	if err == nil && inEnable {
		return nil
	}
	return errors.New("enable() not implemented for base driver, override in vendor driver")
}

func (bc *BaseConnection) ExitEnableMode() error {
	return nil
}

func (bc *BaseConnection) CheckEnableMode() (bool, error) {
	prompt, err := bc.FindPrompt()
	if err != nil {
		return false, err
	}
	return strings.HasSuffix(prompt, "#"), nil
}

func (bc *BaseConnection) ConfigMode() error {
	return errors.New("config_mode() not implemented for base driver")
}

func (bc *BaseConnection) ExitConfigMode() error {
	return errors.New("exit_config_mode() not implemented for base driver")
}

func (bc *BaseConnection) CheckConfigMode() (bool, error) {
	prompt, err := bc.FindPrompt()
	if err != nil {
		return false, err
	}
	return strings.Contains(prompt, "(config"), nil
}
