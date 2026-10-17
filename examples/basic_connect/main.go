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
	// Define target network device
	device := netmiko.Device{
		DeviceType:        "cisco_ios",
		Host:              "192.168.1.1",
		Port:              22,
		Username:          "admin",
		Password:          "cisco123",
		Secret:            "enableSecret",
		Timeout:           30 * time.Second,
		GlobalDelayFactor: 1.0,
	}

	// Connect using ConnectHandler dispatcher
	netConnect, err := netmiko.ConnectHandler(device)
	if err != nil {
		log.Fatalf("ConnectHandler error: %v", err)
	}

	ctx := context.Background()
	if err := netConnect.Connect(ctx); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer netConnect.Disconnect()

	// Find and print base prompt
	prompt, err := netConnect.FindPrompt()
	if err != nil {
		log.Fatalf("FindPrompt error: %v", err)
	}
	fmt.Printf("Connected to device. Current prompt: %s\n", prompt)

	// Execute command
	output, err := netConnect.SendCommand("show ip interface brief")
	if err != nil {
		log.Fatalf("SendCommand error: %v", err)
	}

	fmt.Println("--- Command Output ---")
	fmt.Println(output)
}
