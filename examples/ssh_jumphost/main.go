package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	_ "github.com/netmiko-go/netmiko/pkg/drivers"
	"github.com/netmiko-go/netmiko/pkg/netmiko"
	"golang.org/x/crypto/ssh"
)

// JumpTransport is the interactive target session, plus both SSH clients that
// must stay alive for the duration of the session.
type jumpTransport struct {
	jumpClient   *ssh.Client
	targetClient *ssh.Client
	session      *ssh.Session
	stdin        io.WriteCloser
	stdout       io.Reader
}

func (t *jumpTransport) Read(p []byte) (int, error)  { return t.stdout.Read(p) }
func (t *jumpTransport) Write(p []byte) (int, error) { return t.stdin.Write(p) }

func (t *jumpTransport) Close() error {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.session != nil {
		_ = t.session.Close()
	}
	if t.targetClient != nil {
		_ = t.targetClient.Close()
	}
	if t.jumpClient != nil {
		_ = t.jumpClient.Close()
	}
	return nil
}

func requireEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s must be set", name)
	}
	return value
}

func portEnv(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		log.Fatalf("%s must be a valid TCP port, got %q", name, value)
	}
	return port
}

func boolEnv(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		log.Fatalf("%s must be true or false, got %q", name, value)
	}
	return parsed
}

func passwordConfig(user, password string, timeout time.Duration, strict bool, knownHostsFile string) (*ssh.ClientConfig, error) {
	hostKeyCallback, err := netmiko.NewHostKeyCallback(strict, knownHostsFile)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: hostKeyCallback,
		Timeout:         timeout,
	}, nil
}

func openJumpTransport(jumpHost string, jumpPort int, jumpUser, jumpPassword string, targetHost string, targetPort int, targetUser, targetPassword string, timeout time.Duration, strict bool, knownHostsFile string) (*jumpTransport, error) {
	jumpAddr := net.JoinHostPort(jumpHost, strconv.Itoa(jumpPort))
	jumpConfig, err := passwordConfig(jumpUser, jumpPassword, timeout, strict, knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("configure jumphost host-key verification: %w", err)
	}
	jumpClient, err := ssh.Dial("tcp", jumpAddr, jumpConfig)
	if err != nil {
		return nil, fmt.Errorf("connect to jumphost %s: %w", jumpAddr, err)
	}

	closeJump := func() {
		_ = jumpClient.Close()
	}

	targetAddr := net.JoinHostPort(targetHost, strconv.Itoa(targetPort))
	proxiedConn, err := jumpClient.Dial("tcp", targetAddr)
	if err != nil {
		closeJump()
		return nil, fmt.Errorf("dial target %s through jumphost: %w", targetAddr, err)
	}

	targetConfig, err := passwordConfig(targetUser, targetPassword, timeout, strict, knownHostsFile)
	if err != nil {
		_ = proxiedConn.Close()
		closeJump()
		return nil, fmt.Errorf("configure target host-key verification: %w", err)
	}

	targetConn, targetChannels, targetRequests, err := ssh.NewClientConn(proxiedConn, targetAddr, targetConfig)
	if err != nil {
		_ = proxiedConn.Close()
		closeJump()
		return nil, fmt.Errorf("authenticate to target %s through jumphost: %w", targetAddr, err)
	}
	targetClient := ssh.NewClient(targetConn, targetChannels, targetRequests)

	closeTarget := func() {
		_ = targetClient.Close()
		closeJump()
	}

	session, err := targetClient.NewSession()
	if err != nil {
		closeTarget()
		return nil, fmt.Errorf("create target SSH session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty("vt100", 120, 40, modes); err != nil {
		_ = session.Close()
		closeTarget()
		return nil, fmt.Errorf("request target PTY: %w", err)
	}

	stdin, err := session.StdinPipe()
	if err != nil {
		_ = session.Close()
		closeTarget()
		return nil, fmt.Errorf("create target stdin pipe: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		_ = session.Close()
		closeTarget()
		return nil, fmt.Errorf("create target stdout pipe: %w", err)
	}

	if err := session.Shell(); err != nil {
		_ = stdin.Close()
		_ = session.Close()
		closeTarget()
		return nil, fmt.Errorf("start target shell: %w", err)
	}

	return &jumpTransport{
		jumpClient:   jumpClient,
		targetClient: targetClient,
		session:      session,
		stdin:        stdin,
		stdout:       stdout,
	}, nil
}

func main() {
	const timeout = 30 * time.Second

	jumpHost := requireEnv("JUMP_HOST")
	jumpUser := requireEnv("JUMP_USER")
	jumpPassword := requireEnv("JUMP_PASSWORD")
	targetHost := requireEnv("TARGET_HOST")
	targetUser := requireEnv("TARGET_USER")
	targetPassword := requireEnv("TARGET_PASSWORD")
	sshStrict := boolEnv("SSH_STRICT", false)
	knownHostsFile := os.Getenv("SSH_KNOWN_HOSTS_FILE")

	targetPort := portEnv("TARGET_PORT", 22)
	jump, err := openJumpTransport(
		jumpHost,
		portEnv("JUMP_PORT", 22),
		jumpUser,
		jumpPassword,
		targetHost,
		targetPort,
		targetUser,
		targetPassword,
		timeout,
		sshStrict,
		knownHostsFile,
	)
	if err != nil {
		log.Fatal(err)
	}

	device := netmiko.Device{
		DeviceType:               "cisco_ios",
		Host:                     targetHost,
		Port:                     targetPort,
		Username:                 targetUser,
		Password:                 targetPassword,
		Timeout:                  timeout,
		SSHStrictHostKeyChecking: sshStrict,
		SSHKnownHostsFile:        knownHostsFile,
	}
	conn, err := netmiko.ConnectHandler(device)
	if err != nil {
		_ = jump.Close()
		log.Fatalf("create Cisco IOS connection: %v", err)
	}

	setter, ok := conn.(interface{ SetTransport(netmiko.Transport) })
	if !ok {
		_ = jump.Close()
		log.Fatal("Cisco IOS driver does not support transport injection")
	}
	setter.SetTransport(jump)
	defer conn.Disconnect()

	if err := conn.Connect(context.Background()); err != nil {
		log.Fatalf("connect to Cisco IOS through jumphost: %v", err)
	}
	if err := conn.Enable(); err != nil {
		log.Fatalf("enter enable mode: %v", err)
	}

	output, err := conn.SendCommand("show version")
	if err != nil {
		log.Fatalf("run show version: %v", err)
	}
	fmt.Println(output)
}
