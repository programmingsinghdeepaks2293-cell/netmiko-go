package main

import (
	"context"
	"fmt"
	"time"

	_ "github.com/netmiko-go/netmiko/pkg/drivers"
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

func main() {
	// Define a fleet of target switches
	switches := []netmiko.Device{
		{DeviceType: "cisco_ios", Host: "10.0.0.1", Username: "admin", Password: "pwd"},
		{DeviceType: "cisco_ios", Host: "10.0.0.2", Username: "admin", Password: "pwd"},
		{DeviceType: "cisco_ios", Host: "10.0.0.3", Username: "admin", Password: "pwd"},
		{DeviceType: "arista_eos", Host: "10.0.0.4", Username: "admin", Password: "pwd"},
		{DeviceType: "juniper_junos", Host: "10.0.0.5", Username: "root", Password: "pwd"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("Starting concurrent execution across %d devices using goroutines...\n", len(switches))

	// ExecuteConcurrently uses worker goroutines to process devices in parallel
	results := netmiko.ExecuteConcurrently(ctx, switches, 10, func(conn netmiko.Connection) (string, error) {
		// This closure runs inside its own worker goroutine
		prompt, _ := conn.FindPrompt()
		out, err := conn.SendCommand("show version")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("[%s]: %s", prompt, out), nil
	})

	// Process aggregated results
	for host, res := range results {
		if res.Err != nil {
			fmt.Printf("[-] %s: Failed (%v)\n", host, res.Err)
			continue
		}
		fmt.Printf("[+] %s: Succeeded in %v\n", host, res.Duration)
	}
}
