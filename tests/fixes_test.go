package tests

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/netmiko-go/netmiko/pkg/drivers"
	"github.com/netmiko-go/netmiko/pkg/drivers/cisco"
	"github.com/netmiko-go/netmiko/pkg/drivers/juniper"
	"github.com/netmiko-go/netmiko/pkg/drivers/linux"
	"github.com/netmiko-go/netmiko/pkg/mock"
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

// stallTransport behaves like a device that has gone silent: Read blocks until Close.
type stallTransport struct {
	closed chan struct{}
	once   sync.Once
}

func newStallTransport() *stallTransport {
	return &stallTransport{closed: make(chan struct{})}
}

func (s *stallTransport) Read(p []byte) (int, error) {
	<-s.closed
	return 0, io.EOF
}

func (s *stallTransport) Write(p []byte) (int, error) {
	return len(p), nil
}

func (s *stallTransport) Close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func TestReadUntilPatternTimesOutOnSilentDevice(t *testing.T) {
	st := newStallTransport()
	defer st.Close()

	bc := netmiko.NewBaseConnection(netmiko.Device{Timeout: 5 * time.Second})
	bc.SetTransport(st)

	start := time.Now()
	_, err := bc.ReadUntilPattern(regexp.MustCompile("never"), 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("ReadUntilPattern took %v; expected about 300ms", elapsed)
	}
}

func TestSendCommandTimingReturnsWhenDeviceIsSilent(t *testing.T) {
	st := newStallTransport()
	defer st.Close()

	bc := netmiko.NewBaseConnection(netmiko.Device{Timeout: 500 * time.Millisecond})
	bc.SetTransport(st)

	start := time.Now()
	out, err := bc.SendCommandTiming("show version", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("SendCommandTiming returned error: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output from a silent device, got %q", out)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("SendCommandTiming took %v; expected about 500ms", elapsed)
	}
}

func TestFindPromptLinuxPromptIsFast(t *testing.T) {
	term := mock.NewMockTerminalWithPrompt("ubuntu@srv:~$ ")
	bc := netmiko.NewBaseConnection(netmiko.Device{Timeout: 5 * time.Second})
	bc.SetTransport(term)

	start := time.Now()
	prompt, err := bc.FindPrompt()
	if err != nil {
		t.Fatalf("FindPrompt failed: %v", err)
	}
	if prompt != "ubuntu@srv:~$" {
		t.Errorf("expected prompt %q, got %q", "ubuntu@srv:~$", prompt)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("FindPrompt took %v; a matching prompt should not wait for the timeout", elapsed)
	}
}

func TestFindPromptJunosPrompt(t *testing.T) {
	term := mock.NewMockJunosTerminal("user@junos-router")
	bc := netmiko.NewBaseConnection(netmiko.Device{Timeout: 5 * time.Second})
	bc.SetTransport(term)

	prompt, err := bc.FindPrompt()
	if err != nil {
		t.Fatalf("FindPrompt failed: %v", err)
	}
	if prompt != "user@junos-router>" {
		t.Errorf("expected %q, got %q", "user@junos-router>", prompt)
	}
}

func TestJuniperSendConfigSet(t *testing.T) {
	term := mock.NewMockJunosTerminal("user@junos-router")
	dev := netmiko.Device{DeviceType: "juniper_junos", Host: "10.2.2.1", Username: "user", Timeout: 5 * time.Second}
	drv := juniper.NewJuniperJunos(dev)
	drv.SetTransport(term)

	if err := drv.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer func() { _ = drv.Disconnect() }()

	out, err := drv.SendConfigSet([]string{"set system host-name edge1", "set system time-zone UTC"})
	if err != nil {
		t.Fatalf("SendConfigSet failed: %v", err)
	}
	if !strings.Contains(out, "set system host-name edge1") {
		t.Errorf("expected the first statement in the output, got %q", out)
	}

	inConfig, err := drv.CheckConfigMode()
	if err != nil || inConfig {
		t.Errorf("expected to be back in operational mode, inConfig=%v err=%v", inConfig, err)
	}
}

func TestLinuxSendConfigSetNotSupported(t *testing.T) {
	drv := linux.NewLinuxSSH(netmiko.Device{DeviceType: "linux", Host: "10.3.3.1"})
	_, err := drv.SendConfigSet([]string{"echo hi"})
	if !errors.Is(err, netmiko.ErrNotSupported) {
		t.Fatalf("expected ErrNotSupported, got %v", err)
	}
}

func TestExecuteConcurrentlyReportsEveryDeviceWhenCancelled(t *testing.T) {
	netmiko.RegisterDriver("fix_cancel_mock", func(d netmiko.Device) netmiko.Connection {
		bc := netmiko.NewBaseConnection(d)
		bc.SetTransport(mock.NewMockCiscoTerminal("sw"))
		return bc
	})

	devices := []netmiko.Device{
		{DeviceType: "fix_cancel_mock", Host: "10.9.0.1"},
		{DeviceType: "fix_cancel_mock", Host: "10.9.0.2"},
		{DeviceType: "fix_cancel_mock", Host: "10.9.0.3"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := netmiko.ExecuteConcurrently(ctx, devices, 2, func(conn netmiko.Connection) (string, error) {
		return conn.SendCommand("show ip int br")
	})

	if len(results) != len(devices) {
		t.Fatalf("expected %d results, got %d", len(devices), len(results))
	}
	for key, res := range results {
		if res.Err == nil {
			t.Errorf("%s: expected an error after cancellation", key)
		}
	}
}

func TestExecuteConcurrentlyKeysByHostAndPort(t *testing.T) {
	netmiko.RegisterDriver("fix_port_mock", func(d netmiko.Device) netmiko.Connection {
		bc := netmiko.NewBaseConnection(d)
		bc.SetTransport(mock.NewMockCiscoTerminal("sw"))
		return bc
	})

	devices := []netmiko.Device{
		{DeviceType: "fix_port_mock", Host: "10.8.0.1", Port: 22},
		{DeviceType: "fix_port_mock", Host: "10.8.0.1", Port: 2222},
	}
	results := netmiko.ExecuteConcurrently(context.Background(), devices, 0, func(conn netmiko.Connection) (string, error) {
		return "ok", nil
	})
	if len(results) != 2 {
		t.Fatalf("devices sharing a host but not a port should both be kept, got %d results", len(results))
	}
	if _, ok := results["10.8.0.1:2222"]; !ok {
		t.Errorf("expected key 10.8.0.1:2222, got keys %v", keysOf(results))
	}
}

func keysOf(m map[string]netmiko.DeviceResult) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestConnectRequiresHostKeyConfiguration(t *testing.T) {
	dev := netmiko.Device{
		DeviceType:     "cisco_ios",
		Host:           "192.0.2.1",
		Port:           22,
		Username:       "u",
		Password:       "p",
		Timeout:        time.Second,
		KnownHostsPath: filepath.Join(t.TempDir(), "missing_known_hosts"),
	}
	conn, err := netmiko.ConnectHandler(dev)
	if err != nil {
		t.Fatalf("ConnectHandler failed: %v", err)
	}
	err = conn.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("expected a host key verification error, got %v", err)
	}
}

func TestEnableDoesNotWriteSecretToSessionLog(t *testing.T) {
	const secret = "s3cr3t-pass"
	var logBuf bytes.Buffer
	term := mock.NewMockCiscoTerminal("sw1")
	dev := netmiko.Device{
		DeviceType: "cisco_ios",
		Host:       "192.0.2.2",
		Username:   "u",
		Secret:     secret,
		Timeout:    5 * time.Second,
		SessionLog: &logBuf,
	}
	drv := cisco.NewCiscoIOS(dev)
	drv.SetTransport(term)

	if err := drv.Connect(context.Background()); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer func() { _ = drv.Disconnect() }()

	if err := drv.Enable(); err != nil {
		t.Fatalf("Enable failed: %v", err)
	}
	if strings.Contains(logBuf.String(), secret) {
		t.Fatal("the enable secret was written to the session log")
	}
}

func TestFindNTCTemplateUsesIndexAbbreviations(t *testing.T) {
	dir := t.TempDir()
	index := "# NTC index\n" +
		"Template, Hostname, Platform, Command\n" +
		"cisco_ios_show_ip_int_brief.textfsm, .*, cisco_ios, sh[[ow]] ip int[[erface]] br[[ief]]\n"
	if err := os.WriteFile(filepath.Join(dir, "index"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cisco_ios_show_ip_int_brief.textfsm"), []byte("Value X (\\S+)\n\nStart\n  ^${X} -> Record\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NET_TEXTFSM", dir)

	for _, cmd := range []string{"show ip interface brief", "sh ip int br"} {
		path, err := netmiko.FindNTCTemplate("cisco_ios", cmd)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", cmd, err)
			continue
		}
		if filepath.Base(path) != "cisco_ios_show_ip_int_brief.textfsm" {
			t.Errorf("%q: got template %s", cmd, path)
		}
	}

	if _, err := netmiko.FindNTCTemplate("juniper", "show version"); err == nil {
		t.Error("expected no template for juniper show version")
	}
}
