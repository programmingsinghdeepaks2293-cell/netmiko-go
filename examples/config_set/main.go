package main

import (
	"context"
	"fmt"
	"log"
	"time"

	_ "github.com/netmiko-go/netmiko/pkg/drivers"
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

	if err := netConnect.Connect(context.Background()); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer netConnect.Disconnect()

	// Ensure privileged EXEC mode
	if err := netConnect.Enable(); err != nil {
		log.Fatalf("Enable failed: %v", err)
	}

	// Send configuration commands
	configCommands := []string{
		"interface Loopback100",
		"description Configured by netmiko-go",
		"ip address 10.100.100.1 255.255.255.255",
		"no shutdown",
	}

	output, err := netConnect.SendConfigSet(configCommands)
	if err != nil {
		log.Fatalf("SendConfigSet failed: %v", err)
	}

	fmt.Println("--- Configuration Results ---")
	fmt.Println(output)
}
