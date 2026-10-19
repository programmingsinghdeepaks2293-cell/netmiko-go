package tests

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/netmiko-go/netmiko/pkg/drivers" // Register all drivers
	"github.com/netmiko-go/netmiko/pkg/drivers/arista"
	"github.com/netmiko-go/netmiko/pkg/drivers/cisco"
	"github.com/netmiko-go/netmiko/pkg/drivers/juniper"
	"github.com/netmiko-go/netmiko/pkg/drivers/linux"
	"github.com/netmiko-go/netmiko/pkg/mock"
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

func TestCiscoIOSDriverWorkflow(t *testing.T) {
	mockTerm := mock.NewMockCiscoTerminal("switch1")

	dev := netmiko.Device{
		DeviceType:        "cisco_ios",
		Host:              "192.168.1.1",
		Username:          "admin",
		Password:          "cisco",
		Secret:            "secret",
		Timeout:           5 * time.Second,
		GlobalDelayFactor: 1.0,
	}

	driver := cisco.NewCiscoIOS(dev)
	driver.SetTransport(mockTerm)

	ctx := context.Background()
	if err := driver.Connect(ctx); err != nil {
		t.Fatalf("driver.Connect failed: %v", err)
	}
	defer func() { _ = driver.Disconnect() }()

	// 1. Test FindPrompt (initially unprivileged '>')
	prompt, err := driver.FindPrompt()
	if err != nil {
		t.Fatalf("FindPrompt failed: %v", err)
	}
	if !strings.HasSuffix(prompt, ">") {
		t.Errorf("expected prompt ending with '>', got %q", prompt)
	}

	// 2. Test Enable Mode
	if err := driver.Enable(); err != nil {
		t.Fatalf("Enable failed: %v", err)
	}
	inEnable, err := driver.CheckEnableMode()
	if err != nil || !inEnable {
		t.Errorf("expected to be in enable mode, check returned inEnable=%v, err=%v", inEnable, err)
	}

	// 3. Test SendCommand
	out, err := driver.SendCommand("show ip int br")
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}
	if !strings.Contains(out, "GigabitEthernet0/0") {
		t.Errorf("expected GigabitEthernet0/0 in output, got: %s", out)
	}
	if strings.Contains(out, "switch1#") {
		t.Errorf("output should have trailing prompt stripped, got: %s", out)
	}

	versionOut, err := driver.SendCommand("show version")
	if err != nil {
		t.Fatalf("SendCommand for show version failed: %v", err)
	}
	if !strings.Contains(versionOut, "Cisco IOS Software") {
		t.Errorf("expected 'Cisco IOS Software' in version output, got: %s", versionOut)
	}

	// 4. Test SendCommandTiming
	timedOut, err := driver.SendCommandTiming("show ip int br", 200*time.Millisecond)
	if err != nil {
		t.Fatalf("SendCommandTiming failed: %v", err)
	}
	if !strings.Contains(timedOut, "GigabitEthernet0/0") {
		t.Errorf("expected GigabitEthernet0/0 in SendCommandTiming output, got: %s", timedOut)
	}

	// 5. Test Config Mode
	if err := driver.ConfigMode(); err != nil {
		t.Fatalf("ConfigMode failed: %v", err)
	}
	inConfig, err := driver.CheckConfigMode()
	if err != nil || !inConfig {
		t.Errorf("expected to be in config mode, check returned inConfig=%v, err=%v", inConfig, err)
	}

	// 6. Test Exit Config Mode
	if err := driver.ExitConfigMode(); err != nil {
		t.Fatalf("ExitConfigMode failed: %v", err)
	}
	inConfig, err = driver.CheckConfigMode()
	if err != nil || inConfig {
		t.Errorf("expected to have exited config mode, check returned inConfig=%v, err=%v", inConfig, err)
	}

	// 7. Test SendConfigSet
	configCmds := []string{
		"vlan 10",
		"name SALES",
	}
	cfgOut, err := driver.SendConfigSet(configCmds)
	if err != nil {
		t.Fatalf("SendConfigSet failed: %v", err)
	}
	if !strings.Contains(cfgOut, "vlan 10") {
		t.Errorf("expected 'vlan 10' in config output, got: %s", cfgOut)
	}
}

func TestAristaEOSDriver(t *testing.T) {
	mockTerm := mock.NewMockCiscoTerminal("arista-spine1")
	dev := netmiko.Device{
		DeviceType: "arista_eos",
		Host:       "10.1.1.1",
		Username:   "admin",
	}
	driver := arista.NewAristaEOS(dev)
	driver.SetTransport(mockTerm)

	if err := driver.Connect(context.Background()); err != nil {
		t.Fatalf("Arista Connect failed: %v", err)
	}
	defer func() { _ = driver.Disconnect() }()

	prompt, err := driver.FindPrompt()
	if err != nil {
		t.Fatalf("Arista FindPrompt failed: %v", err)
	}
	if !strings.Contains(prompt, "arista-spine1") {
		t.Errorf("expected prompt containing 'arista-spine1', got %q", prompt)
	}
}

func TestJuniperJunosDriver(t *testing.T) {
	mockTerm := mock.NewMockCiscoTerminal("junos-router")
	dev := netmiko.Device{
		DeviceType: "juniper_junos",
		Host:       "10.2.2.1",
		Username:   "root",
	}
	driver := juniper.NewJuniperJunos(dev)
	driver.SetTransport(mockTerm)

	if err := driver.Connect(context.Background()); err != nil {
		t.Fatalf("Juniper Connect failed: %v", err)
	}
	defer func() { _ = driver.Disconnect() }()

	prompt, err := driver.FindPrompt()
	if err != nil {
		t.Fatalf("Juniper FindPrompt failed: %v", err)
	}
	if !strings.Contains(prompt, "junos-router") {
		t.Errorf("expected prompt containing 'junos-router', got %q", prompt)
	}
}

func TestLinuxDriver(t *testing.T) {
	mockTerm := mock.NewMockLinuxTerminal("ubuntu", "ubuntu-srv")
	dev := netmiko.Device{
		DeviceType: "linux",
		Host:       "10.3.3.1",
		Username:   "ubuntu",
	}
	driver := linux.NewLinuxSSH(dev)
	driver.SetTransport(mockTerm)

	if err := driver.Connect(context.Background()); err != nil {
		t.Fatalf("Linux Connect failed: %v", err)
	}
	defer func() { _ = driver.Disconnect() }()

	prompt, err := driver.FindPrompt()
	if err != nil {
		t.Fatalf("Linux FindPrompt failed: %v", err)
	}
	if !strings.Contains(prompt, "ubuntu@ubuntu-srv:~$") {
		t.Errorf("expected prompt containing 'ubuntu@ubuntu-srv', got %q", prompt)
	}

	out, err := driver.SendCommand("pwd")
	if err != nil {
		t.Fatalf("SendCommand failed: %v", err)
	}
	if !strings.Contains(out, "/home/ubuntu") {
		t.Errorf("expected '/home/ubuntu' in output, got: %s", out)
	}
	if strings.Contains(out, "ubuntu@ubuntu-srv:~$") {
		t.Errorf("output should have trailing prompt stripped, got: %s", out)
	}
}

func TestUtilities(t *testing.T) {
	// 1. ANSI escape stripping
	coloredText := "\x1b[31;1mError: connection refused\x1b[0m"
	stripped := netmiko.StripAnsi(coloredText)
	if stripped != "Error: connection refused" {
		t.Errorf("StripAnsi failed, got: %q", stripped)
	}

	// 2. Linebreak normalization
	crlfText := "line1\r\nline2\rline3\n"
	norm := netmiko.NormalizeLinebreaks(crlfText)
	if norm != "line1\nline2\nline3\n" {
		t.Errorf("NormalizeLinebreaks failed, got: %q", norm)
	}

	// 3. Command echo stripping
	cmdOutput := "show ip int br\r\nInterface  IP-Address  Status\r\nrouter#"
	cleaned := netmiko.CleanOutput("show ip int br", cmdOutput, true, true)
	if cleaned != "Interface  IP-Address  Status" {
		t.Errorf("CleanOutput failed, got: %q", cleaned)
	}
}

func TestHostkeyCallbackPolicy(t *testing.T) {
	if callback, err := netmiko.NewHostKeyCallback(false, filepath.Join(t.TempDir(), "missing_known_hosts")); err != nil || callback == nil {
		t.Fatalf("NewHostKeyCallback failed for missing known_hosts: %v", err)
	}
	missingFile := filepath.Join(t.TempDir(), "missing_known_hosts")
	if callback, err := netmiko.NewHostKeyCallback(true, missingFile); err != nil || callback == nil {
		t.Fatalf("NewHostKeyCallback failed for missing known_hosts with strict checking: %v", err)
	}
}

func TestDispatcherConnectHandler(t *testing.T) {
	dev := netmiko.Device{
		DeviceType: "cisco_ios",
		Host:       "10.0.0.1",
	}

	conn, err := netmiko.ConnectHandler(dev)
	if err != nil {
		t.Fatalf("ConnectHandler failed for cisco_ios: %v", err)
	}
	if conn == nil {
		t.Fatal("ConnectHandler returned nil connection")
	}

	platforms := netmiko.Platforms()
	expected := []string{"arista_eos", "cisco_ios", "cisco_nxos", "cisco_xe", "cisco_xr", "juniper", "juniper_junos", "linux"}
	for _, exp := range expected {
		found := false
		for _, p := range platforms {
			if p == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected platform %q to be registered, registered: %v", exp, platforms)
		}
	}
}
