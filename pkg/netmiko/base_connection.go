package netmiko

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

var (
	errNotConnected = errors.New("connection is not established")
	errReadTimeout  = errors.New("read timeout")

	// genericPromptRe matches output whose last line ends in a prompt character.
	genericPromptRe = regexp.MustCompile(`(?:^|\n)[^\r\n]*[#>$%]\s*$`)
	// promptTailRe matches text whose last line ends in a prompt character.
	promptTailRe = regexp.MustCompile(`[^\r\n]*[#>$%]\s*$`)
	// configPromptRe matches an IOS-style configuration prompt such as "R1(config-if)#".
	configPromptRe = regexp.MustCompile(`\(config[^)\r\n]*\)#\s*$`)
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

// readChunk is one result of a background read from the transport.
type readChunk struct {
	data []byte
	err  error
}

// BaseConnection provides standard terminal interaction logic.
//
// A BaseConnection is not safe for concurrent use: issue one operation at a time,
// and give each goroutine its own connection.
type BaseConnection struct {
	Device            Device
	Transport         Transport
	BasePrompt        string
	DefaultPromptTerm string // e.g. "#", ">", "$"

	mu  sync.Mutex
	ctx context.Context

	// Reads happen on a background goroutine (see ensureReader) so that every
	// read can be bounded by a deadline and by ctx, even when the transport
	// blocks indefinitely.
	inbound  chan readChunk
	readDone chan struct{}
	readErr  error
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
	bc.stopReader()
	bc.Transport = t
}

func (bc *BaseConnection) GetDevice() Device {
	return bc.Device
}

func (bc *BaseConnection) GetBasePrompt() string {
	return bc.BasePrompt
}

// sessionCtx returns the context given to Connect, or Background if none.
func (bc *BaseConnection) sessionCtx() context.Context {
	if bc.ctx == nil {
		return context.Background()
	}
	return bc.ctx
}

// Connect dials SSH, requests a PTY, and opens an interactive shell.
//
// ctx bounds the whole session: once it is cancelled, the dial or handshake is
// aborted and every later operation on this connection returns ctx.Err().
func (bc *BaseConnection) Connect(ctx context.Context) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}
	bc.ctx = ctx
	if bc.Transport != nil {
		return nil // already connected, or a mock/preconfigured transport
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	authMethods, err := bc.authMethods()
	if err != nil {
		return err
	}
	hostKeyCB, err := hostKeyCallback(bc.Device)
	if err != nil {
		return err
	}

	sshConfig := &ssh.ClientConfig{
		User:            bc.Device.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCB,
		Timeout:         bc.Device.Timeout,
	}

	addr := net.JoinHostPort(bc.Device.Host, fmt.Sprintf("%d", bc.Device.Port))
	dialer := net.Dialer{Timeout: bc.Device.Timeout}
	raw, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("ssh dial failed for %s: %w", addr, err)
	}

	// Abort the handshake if ctx is cancelled before it completes.
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	sshConn, chans, reqs, err := ssh.NewClientConn(raw, addr, sshConfig)
	if err != nil {
		stop()
		_ = raw.Close()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return fmt.Errorf("ssh handshake with %s failed: %w", addr, err)
	}
	if !stop() {
		// ctx fired during the handshake and closed the socket.
		_ = sshConn.Close()
		return ctx.Err()
	}
	client := ssh.NewClient(sshConn, chans, reqs)

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

	bc.stopReader()
	bc.Transport = &SSHTransport{
		client:  client,
		session: session,
		stdin:   stdin,
		stdout:  stdout,
	}
	return nil
}

func (bc *BaseConnection) authMethods() ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod
	if bc.Device.Password != "" {
		password := bc.Device.Password
		methods = append(methods, ssh.Password(password))
		// Some devices use keyboard-interactive for the same password.
		methods = append(methods, ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = password
			}
			return answers, nil
		}))
	}
	if bc.Device.SSHKeyPath != "" {
		keyBytes, err := os.ReadFile(bc.Device.SSHKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read ssh key file %q: %w", bc.Device.SSHKeyPath, err)
		}
		signer, err := ssh.ParsePrivateKey(keyBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}
	return methods, nil
}

// hostKeyCallback builds the host key check for a device. Verification is on by
// default and uses the known_hosts file at Device.KnownHostsPath (or
// ~/.ssh/known_hosts). Skipping it requires Device.InsecureIgnoreHostKey.
func hostKeyCallback(dev Device) (ssh.HostKeyCallback, error) {
	if dev.InsecureIgnoreHostKey {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	path := dev.KnownHostsPath
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("host key verification: no known_hosts path set and no home directory: %w (set Device.KnownHostsPath or Device.InsecureIgnoreHostKey)", err)
		}
		path = filepath.Join(home, ".ssh", "known_hosts")
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("host key verification: cannot load known_hosts %q: %w (set Device.KnownHostsPath, or Device.InsecureIgnoreHostKey to skip verification)", path, err)
	}
	return cb, nil
}

// Disconnect terminates the session and transport.
func (bc *BaseConnection) Disconnect() error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.stopReader()
	if bc.Transport == nil {
		return nil
	}
	err := bc.Transport.Close()
	bc.Transport = nil
	return err
}

// ensureReader starts the background reader goroutine if it is not running.
// The goroutine only touches its own local variables, so it never races with
// the connection's fields.
func (bc *BaseConnection) ensureReader() {
	if bc.inbound != nil || bc.Transport == nil {
		return
	}
	t := bc.Transport
	in := make(chan readChunk, 16)
	done := make(chan struct{})
	bc.inbound = in
	bc.readDone = done

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := t.Read(buf)
			if n > 0 {
				data := make([]byte, n)
				copy(data, buf[:n])
				select {
				case in <- readChunk{data: data}:
				case <-done:
					return
				}
			}
			if err != nil {
				select {
				case in <- readChunk{err: err}:
				case <-done:
				}
				return
			}
		}
	}()
}

// stopReader stops the background reader and discards any pending state.
func (bc *BaseConnection) stopReader() {
	if bc.readDone != nil {
		close(bc.readDone)
		bc.readDone = nil
	}
	bc.inbound = nil
	bc.readErr = nil
}

// absorb logs a received chunk and converts it into data or an error.
func (bc *BaseConnection) absorb(c readChunk) ([]byte, error) {
	if c.err != nil {
		bc.readErr = c.err
		return nil, c.err
	}
	if bc.Device.SessionLog != nil {
		_, _ = bc.Device.SessionLog.Write(c.data)
	}
	return c.data, nil
}

// recv returns the next chunk of data received from the device. It returns
// errReadTimeout if nothing arrives before deadline, io.EOF (or the transport's
// error) if the session has ended, or ctx's error if the session was cancelled.
func (bc *BaseConnection) recv(deadline time.Time) ([]byte, error) {
	if bc.Transport == nil {
		return nil, errNotConnected
	}
	if bc.readErr != nil {
		return nil, bc.readErr
	}
	ctx := bc.sessionCtx()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	bc.ensureReader()

	wait := time.Until(deadline)
	if wait <= 0 {
		// Deadline already passed: take anything that is already waiting, but don't block.
		select {
		case c := <-bc.inbound:
			return bc.absorb(c)
		default:
			return nil, errReadTimeout
		}
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case c := <-bc.inbound:
		return bc.absorb(c)
	case <-timer.C:
		return nil, errReadTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// drainIdle discards output until the device has been quiet for the given duration.
func (bc *BaseConnection) drainIdle(quiet time.Duration) {
	for {
		if _, err := bc.recv(time.Now().Add(quiet)); err != nil {
			return
		}
	}
}

// WriteChannel writes raw data to the transport and logs it to the session log.
func (bc *BaseConnection) WriteChannel(data string) error {
	if bc.Device.SessionLog != nil {
		_, _ = bc.Device.SessionLog.Write([]byte(data))
	}
	return bc.send(data)
}

// WriteChannelSensitive writes data without logging it. Use it for secrets such as
// the enable password so they never reach Device.SessionLog.
func (bc *BaseConnection) WriteChannelSensitive(data string) error {
	return bc.send(data)
}

func (bc *BaseConnection) send(data string) error {
	if bc.Transport == nil {
		return errNotConnected
	}
	_, err := bc.Transport.Write([]byte(data))
	return err
}

// ReadChannel returns whatever output is available now, without blocking.
func (bc *BaseConnection) ReadChannel() (string, error) {
	if bc.Transport == nil {
		return "", errNotConnected
	}
	if bc.readErr != nil {
		return "", bc.readErr
	}
	bc.ensureReader()

	var out bytes.Buffer
	for {
		select {
		case c := <-bc.inbound:
			data, err := bc.absorb(c)
			out.Write(data)
			if err != nil {
				if out.Len() == 0 {
					return "", err
				}
				return out.String(), nil
			}
		default:
			return out.String(), nil
		}
	}
}

// ReadUntilPattern reads output until pattern matches it or timeout elapses.
// The returned string is everything read so far.
func (bc *BaseConnection) ReadUntilPattern(pattern *regexp.Regexp, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = bc.Device.Timeout
	}
	deadline := time.Now().Add(timeout)

	var output bytes.Buffer
	for {
		if pattern.Match(output.Bytes()) {
			return output.String(), nil
		}
		chunk, err := bc.recv(deadline)
		output.Write(chunk)
		if err == nil {
			continue
		}
		if errors.Is(err, errReadTimeout) {
			return output.String(), fmt.Errorf("timed out after %v waiting for pattern %q (output: %q)", timeout, pattern.String(), output.String())
		}
		return output.String(), err
	}
}

// promptPattern matches a prompt line for the base prompt, or any prompt if the base is unknown.
func (bc *BaseConnection) promptPattern() *regexp.Regexp {
	if bc.BasePrompt == "" {
		return genericPromptRe
	}
	return regexp.MustCompile(`(?:^|\n)` + regexp.QuoteMeta(bc.BasePrompt) + `[^\r\n]*[#>$%]\s*$`)
}

// ReadUntilPrompt reads until the base prompt (or any prompt, if unknown) appears.
func (bc *BaseConnection) ReadUntilPrompt(timeout time.Duration) (string, error) {
	return bc.ReadUntilPattern(bc.promptPattern(), timeout)
}

// FindPrompt sends a newline and returns the prompt the device displays, including
// any privilege or configuration marker (e.g. "R1#", "R1(config)#", "user@host>").
func (bc *BaseConnection) FindPrompt() (string, error) {
	if err := bc.WriteChannel("\n"); err != nil {
		return "", err
	}
	out, err := bc.ReadUntilPattern(promptTailRe, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("unable to determine prompt: %w", err)
	}
	// The first chunk may end before the device finished printing. Drain the rest
	// so it does not get mistaken for output of the next command.
	bc.drainIdle(50 * time.Millisecond)

	lines := strings.Split(NormalizeLinebreaks(StripAnsi(out)), "\n")
	idx := lastNonEmptyLine(lines)
	if idx < 0 {
		return "", errors.New("unable to determine prompt: empty output")
	}
	return strings.TrimSpace(lines[idx]), nil
}

// SetBasePrompt sets and records the clean base prompt, without privilege or
// configuration markers.
func (bc *BaseConnection) SetBasePrompt() (string, error) {
	prompt, err := bc.FindPrompt()
	if err != nil {
		return "", err
	}
	clean := prompt
	if i := strings.Index(clean, "("); i > 0 {
		clean = clean[:i] // drop "(config)" and similar
	}
	clean = strings.TrimRight(clean, "#>$% \t")
	bc.BasePrompt = clean
	return clean, nil
}

// SendCommand sends a command and returns the output up to the next prompt.
func (bc *BaseConnection) SendCommand(command string, opts ...CommandOption) (string, error) {
	options := CommandOptions{
		ReadTimeout:  bc.Device.Timeout,
		StripPrompt:  true,
		StripCommand: true,
	}
	for _, opt := range opts {
		opt(&options)
	}

	pattern := options.ExpectPattern
	if pattern == nil {
		if bc.BasePrompt == "" {
			_, _ = bc.SetBasePrompt()
		}
		pattern = bc.promptPattern()
	}

	if err := bc.WriteChannel(command + "\n"); err != nil {
		return "", err
	}

	raw, err := bc.ReadUntilPattern(pattern, options.ReadTimeout)
	if err != nil {
		return raw, err
	}
	return cleanWithPattern(command, raw, options.StripCommand, options.StripPrompt, pattern), nil
}

// SendCommandTiming writes a command and reads until the device has been idle for
// idleTimeout, or until Device.Timeout elapses.
func (bc *BaseConnection) SendCommandTiming(command string, idleTimeout time.Duration, opts ...CommandOption) (string, error) {
	if idleTimeout <= 0 {
		idleTimeout = time.Duration(float64(2*time.Second) * bc.Device.GlobalDelayFactor)
	}

	if err := bc.WriteChannel(command + "\n"); err != nil {
		return "", err
	}

	var output bytes.Buffer
	deadline := time.Now().Add(bc.Device.Timeout)
	lastData := time.Now()

	for {
		now := time.Now()
		limit := deadline
		if output.Len() > 0 {
			idleDeadline := lastData.Add(idleTimeout)
			if !now.Before(idleDeadline) {
				break // quiet long enough
			}
			if idleDeadline.Before(limit) {
				limit = idleDeadline
			}
		}
		if !now.Before(limit) {
			break
		}

		chunk, err := bc.recv(limit)
		if len(chunk) > 0 {
			output.Write(chunk)
			lastData = time.Now()
		}
		if err == nil || errors.Is(err, errReadTimeout) {
			continue
		}
		if errors.Is(err, io.EOF) {
			break
		}
		return CleanOutput(command, output.String(), true, true), err
	}

	return CleanOutput(command, output.String(), true, true), nil
}

// SendConfigSet enters config mode, executes commands, and exits config mode.
// Each command must return to the configuration prompt before the next is sent.
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

	var out strings.Builder

	if err := bc.WriteChannel(options.ConfigModeCommand + "\n"); err != nil {
		return "", err
	}
	res, err := bc.ReadUntilPattern(configPromptRe, 10*time.Second)
	out.WriteString(res)
	if err != nil {
		return CleanOutput("", out.String(), false, options.StripPrompt), fmt.Errorf("failed to enter config mode: %w", err)
	}

	for _, cmd := range commands {
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		if err := bc.WriteChannel(cmd + "\n"); err != nil {
			return CleanOutput("", out.String(), false, options.StripPrompt), err
		}
		res, err := bc.ReadUntilPattern(configPromptRe, options.ReadTimeout)
		out.WriteString(res)
		if err != nil {
			return CleanOutput("", out.String(), false, options.StripPrompt), fmt.Errorf("config command %q failed: %w", cmd, err)
		}
	}

	if err := bc.WriteChannel(options.ExitConfigCommand + "\n"); err != nil {
		return CleanOutput("", out.String(), false, options.StripPrompt), err
	}
	res, err = bc.ReadUntilPrompt(10 * time.Second)
	out.WriteString(res)
	if err != nil {
		return CleanOutput("", out.String(), false, options.StripPrompt), fmt.Errorf("failed to exit config mode: %w", err)
	}

	return CleanOutput("", out.String(), false, options.StripPrompt), nil
}

// Enable is the generic implementation; vendor drivers override it.
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

// Commit is not supported by default; drivers with a commit model override it.
func (bc *BaseConnection) Commit() (string, error) {
	return "", ErrNotSupported
}

// SaveConfig is not supported by default; vendor drivers override it.
func (bc *BaseConnection) SaveConfig() (string, error) {
	return "", ErrNotSupported
}
