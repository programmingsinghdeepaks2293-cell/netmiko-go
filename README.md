# Netmiko Go (`netmiko-go`)

[![Go Reference](https://pkg.go.dev/badge/github.com/netmiko-go/netmiko.svg)](https://pkg.go.dev/github.com/netmiko-go/netmiko)
[![Go Report Card](https://goreportcard.com/badge/github.com/netmiko-go/netmiko)](https://goreportcard.com/report/github.com/netmiko-go/netmiko)

A fast, idiomatic, concurrent Golang port of Kirk Byers' popular **[Netmiko](https://github.com/ktbyers/netmiko)** library for multi-vendor network device automation over interactive SSH channels.

---

## Overview

Netmiko simplifies terminal-based screen-scraping and CLI automation across heterogeneous network hardware (switches, routers, firewalls, and Unix systems).

`netmiko-go` brings the familiar API and battle-tested CLI interaction workflows of Python Netmiko into Go, offering:
- **Zero-Dependency Native Binaries**: Compile your network automation scripts into single, lightweight binaries.
- **High Concurrency**: Run thousands of simultaneous device connections efficiently using Go goroutines without Python GIL or multi-processing overhead.
- **Type Safety**: Strongly typed device parameters and command option builders.
- **In-Memory Testing**: Includes terminal emulator mocks for deterministic unit testing without live network switches.

---

## Supported Network Platforms

| Driver Identifier | Vendor / Platform | Supported Features |
|---|---|---|
| `cisco_ios` | Cisco IOS | CLI, Enable, Config Mode, Paging Disabling |
| `cisco_xe` | Cisco IOS-XE | CLI, Enable, Config Mode, Paging Disabling |
| `cisco_nxos` | Cisco Nexus (NX-OS) | CLI, Enable, Config Mode, Terminal Length |
| `cisco_xr` | Cisco IOS-XR | CLI, Enable, Config Mode, Commit |
| `arista_eos` | Arista EOS | CLI, Enable, Config Mode, High Terminal Width |
| `juniper_junos` | Juniper JunOS | CLI, Operational & Config Mode, Commit |
| `juniper` | Juniper JunOS Alias | CLI, Operational & Config Mode, Commit |
| `linux` | Linux / Unix Servers | Interactive Bash/Shell, PTY Disabling |

---

## Installation

```bash
go get github.com/netmiko-go/netmiko
```

---

## Quick Start

### 1. Connecting and Running Commands

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	_ "github.com/netmiko-go/netmiko/pkg/drivers" // Register vendor drivers
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

func main() {
	device := netmiko.Device{
		DeviceType: "cisco_ios",
		Host:       "192.168.1.1",
		Port:       22,
		Username:   "admin",
		Password:   "cisco123",
		Secret:     "enableSecret",
		Timeout:    30 * time.Second,
	}

	netConnect, err := netmiko.ConnectHandler(device)
	if err != nil {
		log.Fatalf("ConnectHandler error: %v", err)
	}

	ctx := context.Background()
	if err := netConnect.Connect(ctx); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer netConnect.Disconnect()

	// Ensure Privileged EXEC mode (#)
	if err := netConnect.Enable(); err != nil {
		log.Fatalf("Enable failed: %v", err)
	}

	// Send show command
	output, err := netConnect.SendCommand("show ip interface brief")
	if err != nil {
		log.Fatalf("SendCommand error: %v", err)
	}

	fmt.Println(output)
}
```

---

### 2. Connecting Through an SSH Jumphost[cite: 9]

The [`examples/ssh_jumphost`](examples/ssh_jumphost) example connects to a bastion, opens a second SSH connection to a Cisco IOS device through it, and runs `show version`. It uses the existing interactive Cisco driver and does not store credentials in source control[cite: 9].

Set these environment variables before running it[cite: 9]:

```bash
export JUMP_HOST=jump.example.com
export JUMP_PORT=22                            # optional; defaults to 22
export JUMP_USER=jump-user
export JUMP_PASSWORD='jump-password'
export TARGET_HOST=router.example.com
export TARGET_PORT=22                          # optional; defaults to 22
export TARGET_USER=router-user
export TARGET_PASSWORD='router-password'
export SSH_STRICT=true                         # optional; defaults to false
export SSH_KNOWN_HOSTS_FILE="$HOME/.ssh/known_hosts" # optional; default shown
go run ./examples/ssh_jumphost
```

The example uses relaxed host-key behavior by default, matching Python Netmiko's default. Set `SSH_STRICT=true` to reject unknown or changed host keys. Strict mode reads `SSH_KNOWN_HOSTS_FILE`, or `~/.ssh/known_hosts` when that variable is omitted. The same known-hosts policy is applied to both the jumphost and Cisco target connections.

---
### 3. Pushing Configuration Changes (`SendConfigSet`)

```go
commands := []string{
	"interface Loopback100",
	"description Managed by Netmiko Go",
	"ip address 10.100.100.1 255.255.255.255",
	"no shutdown",
}

output, err := netConnect.SendConfigSet(commands)
if err != nil {
	log.Fatalf("SendConfigSet failed: %v", err)
}

fmt.Println("Config output:\n", output)
```

---

### 4. Concurrent Multi-Device Automation with Goroutines (`ExecuteConcurrently`)

Automate hundreds of network switches concurrently using a built-in goroutine worker pool:

```go
switches := []netmiko.Device{
    {DeviceType: "cisco_ios", Host: "10.0.0.1", Username: "admin", Password: "pwd"},
    {DeviceType: "cisco_ios", Host: "10.0.0.2", Username: "admin", Password: "pwd"},
    {DeviceType: "arista_eos", Host: "10.0.0.3", Username: "admin", Password: "pwd"},
}

ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

// Run with up to 50 concurrent goroutines
results := netmiko.ExecuteConcurrently(ctx, switches, 50, func(conn netmiko.Connection) (string, error) {
    return conn.SendCommand("show version")
})

for host, res := range results {
    if res.Err != nil {
        log.Printf("Host %s failed: %v", host, res.Err)
        continue
    }
    fmt.Printf("Host %s took %v\n", host, res.Duration)
}
```

---

### 5. Structured Data Parsing with TextFSM & NTC Templates

Convert unstructured CLI text into strongly typed slices of maps (`[]map[string]any`) using TextFSM templates:

```go
template := `Value Required Interface (\S+)
Value IPAddress (\S+)
Value Status (up|down)
Value Protocol (up|down)

Start
  ^${Interface}\s+${IPAddress}\s+\S+\s+\S+\s+${Status}\s+${Protocol} -> Record
`

// Parse command output directly with template
records, err := conn.SendCommandStructured("show ip int br", template)
if err != nil {
    log.Fatal(err)
}

for _, row := range records {
    fmt.Printf("Interface %s has IP %s (Status: %s)\n", row["Interface"], row["IPAddress"], row["Status"])
}
```

You can also point to an external NTC-templates directory by setting the `NET_TEXTFSM` environment variable (e.g., `NET_TEXTFSM=/path/to/ntc-templates/templates`).

---

### 6. Session Logging

You can capture all raw input and output streams for auditing or debugging by setting `SessionLog`:

```go
logFile, _ := os.Create("session.log")
defer logFile.Close()

device := netmiko.Device{
	DeviceType: "cisco_ios",
	Host:       "10.0.0.1",
	Username:   "admin",
	Password:   "password",
	SessionLog: logFile,
}
```

---

## Running Tests

Netmiko Go includes an interactive in-memory terminal emulator that simulates realistic device prompt negotiation, enable password challenges, and configuration commands:

```bash
go test -v ./...
```
