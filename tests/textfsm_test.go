package tests

import (
	"testing"

	"github.com/netmiko-go/netmiko/pkg/mock"
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

func TestParseTextFSM(t *testing.T) {
	template := `Value Required Interface (\S+)
Value IPAddress (\S+)
Value OK (\S+)
Value Method (\S+)
Value Status (up|down|administratively down)
Value Protocol (up|down)

Start
  ^${Interface}\s+${IPAddress}\s+${OK}\s+${Method}\s+${Status}\s+${Protocol} -> Record
`

	cliOutput := `
Interface              IP-Address      OK? Method Status                Protocol
GigabitEthernet0/0     192.168.1.1     YES manual up                    up
GigabitEthernet0/1     unassigned      YES unset  administratively down down
`

	records, err := netmiko.ParseTextFSM(template, cliOutput)
	if err != nil {
		t.Fatalf("ParseTextFSM failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Verify first record
	r0 := records[0]
	if r0["Interface"] != "GigabitEthernet0/0" {
		t.Errorf("expected GigabitEthernet0/0, got %v", r0["Interface"])
	}
	if r0["IPAddress"] != "192.168.1.1" {
		t.Errorf("expected 192.168.1.1, got %v", r0["IPAddress"])
	}
	if r0["Status"] != "up" {
		t.Errorf("expected up, got %v", r0["Status"])
	}
	if r0["Protocol"] != "up" {
		t.Errorf("expected up, got %v", r0["Protocol"])
	}

	// Verify second record
	r1 := records[1]
	if r1["Interface"] != "GigabitEthernet0/1" {
		t.Errorf("expected GigabitEthernet0/1, got %v", r1["Interface"])
	}
	if r1["Status"] != "administratively down" {
		t.Errorf("expected administratively down, got %v", r1["Status"])
	}
}

func TestSendCommandStructured(t *testing.T) {
	mockTerm := mock.NewMockCiscoTerminal("switch1")
	dev := netmiko.Device{
		DeviceType: "cisco_ios",
		Host:       "10.0.0.1",
	}

	bc := netmiko.NewBaseConnection(dev)
	bc.SetTransport(mockTerm)

	template := `Value Required Interface (\S+)
Value IPAddress (\S+)
Value Status (up|down|administratively down)
Value Protocol (up|down)

Start
  ^${Interface}\s+${IPAddress}\s+\S+\s+\S+\s+${Status}\s+${Protocol} -> Record
`

	records, err := bc.SendCommandStructured("show ip int br", template)
	if err != nil {
		t.Fatalf("SendCommandStructured failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 parsed records, got %d", len(records))
	}
	if records[0]["Interface"] != "GigabitEthernet0/0" {
		t.Errorf("expected GigabitEthernet0/0, got %v", records[0]["Interface"])
	}
}
