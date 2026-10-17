# Netmiko Go (`netmiko-go`)

A concurrent Go port of Kirk Byers' [Netmiko](https://github.com/ktbyers/netmiko) library for multi-vendor
network device automation over interactive SSH channels.

---

## Supported Network Platforms

| Driver Identifier | Vendor / Platform | Supported Features |
|---|---|---|
| `cisco_ios` | Cisco IOS | CLI, Enable, Config Mode, Paging Disabling, SaveConfig |
| `cisco_xe` | Cisco IOS-XE | Same as `cisco_ios` |
| `cisco_nxos` | Cisco Nexus (NX-OS) | CLI, Enable, Config Mode, Terminal Length |
| `cisco_xr` | Cisco IOS-XR | CLI, Enable, Config Mode, Commit |
| `arista_eos` | Arista EOS | CLI, Enable, Config Mode, High Terminal Width |
| `juniper_junos` | Juniper JunOS | CLI, Operational & Config Mode, SendConfigSet, Commit |
| `juniper` | Juniper JunOS alias | Same as `juniper_junos` |
| `linux` | Linux / Unix servers | Interactive shell, PTY disabling. SendConfigSet returns `ErrNotSupported` |

---

## Installation

```bash
go get github.com/netmiko-go/netmiko
```

---

## Security: host key verification

Host keys are verified by default against an OpenSSH `known_hosts` file. Set
`Device.KnownHostsPath`, or leave it empty to use `~/.ssh/known_hosts`. If the file can't be loaded, `Connect`
fails. To skip verification on a network you trust, set `Device.InsecureIgnoreHostKey: true`.

Passwords and the enable secret are never written to `Device.SessionLog`.

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
		DeviceType:     "cisco_ios",
		Host:           "192.168.1.1",
		Port:           22,
		Username:       "admin",
		Password:       "cisco123",
		Secret:         "enableSecret",
		Timeout:        30 * time.Second,
		KnownHostsPath: "/home/me/.ssh/known_hosts",
	}

	netConnect, err := netmiko.ConnectHandler(device)
	if err != nil {
		log.Fatalf("ConnectHandler error: %v", err)
	}

	// ctx bounds the whole session: cancelling it aborts the connection and later calls fail.
	ctx := context.Background()
	if err := netConnect.Connect(ctx); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer netConnect.Disconnect()

	if err := netConnect.Enable(); err != nil {
		log.Fatalf("Enable failed: %v", err)
	}

	output, err := netConnect.SendCommand("show ip interface brief")
	if err != nil {
		log.Fatalf("SendCommand error: %v", err)
	}
	fmt.Println(output)
}
```

---

### 2. Pushing Configuration Changes (`SendConfigSet`)

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

On JunOS, `SendConfigSet` takes `set`/`delete` statements, stages them, and returns to operational mode. Call
`Commit()` afterwards to apply them.

---

### 3. Concurrent Multi-Device Automation (`ExecuteConcurrently`)

```go
switches := []netmiko.Device{
	{DeviceType: "cisco_ios", Host: "10.0.0.1", Username: "admin", Password: "pwd", KnownHostsPath: kh},
	{DeviceType: "cisco_ios", Host: "10.0.0.2", Username: "admin", Password: "pwd", KnownHostsPath: kh},
	{DeviceType: "arista_eos", Host: "10.0.0.3", Username: "admin", Password: "pwd", KnownHostsPath: kh},
}

ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
defer cancel()

// Up to 50 devices at a time
results := netmiko.ExecuteConcurrently(ctx, switches, 50, func(conn netmiko.Connection) (string, error) {
	return conn.SendCommand("show version")
})

for key, res := range results { // key is "host:port"
	if res.Err != nil {
		log.Printf("%s failed: %v", key, res.Err)
		continue
	}
	fmt.Printf("%s took %v\n", key, res.Duration)
}
```

Results are keyed by `host:port`, and every device gets an entry. A device that was never started because `ctx`
was cancelled has `Err` set to the context error. A panic in one device's task is reported as that device's error.

---

### 4. Structured Parsing with TextFSM & NTC Templates

```go
records, err := conn.SendCommandStructured("show ip interface brief", template)
if err != nil {
	log.Fatal(err)
}
for _, row := range records {
	fmt.Printf("Interface %s has IP %s (Status: %s)\n", row["Interface"], row["IPAddress"], row["Status"])
}
```

Without an explicit template, `SendCommandStructured` looks up the NTC template in the directory given by the
`NET_TEXTFSM` environment variable (for example `NET_TEXTFSM=/path/to/ntc-templates/templates`). The lookup uses
the NTC `index` file, so abbreviated commands such as `show ip interface brief` resolve correctly.

---

### 5. Session Logging

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

The log contains device output and the commands you send. Secrets (the password and the enable secret) are not
logged. Commands are logged as written, so avoid sending secrets through `SendCommand`.

---

## Concurrency

A connection is not safe for concurrent use. Give each goroutine its own connection, or use `ExecuteConcurrently`,
which does this for you.

---

## Running Tests

The tests use an in-memory terminal emulator for Cisco IOS, Arista, and JunOS-style sessions, plus a silent
transport for timeout tests. No live devices are required.

```bash
go test -v ./...
```
