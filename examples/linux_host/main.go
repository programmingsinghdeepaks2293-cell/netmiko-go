package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/netmiko-go/netmiko/pkg/drivers" // Register platform drivers
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

func main() {
	password := os.Getenv("NETMIKO_PASSWORD")
	if password == "" {
		log.Fatal("NETMIKO_PASSWORD is not set")
	}

	device := netmiko.Device{
		DeviceType: "linux",
		Host:       os.Getenv("TARGET_HOST"),
		Port:       22,
		Username:   "t_auto",
		Password:   password,
		Timeout:    30 * time.Second,
	}
	if os.Getenv("NETMIKO_DEBUG") != "" {
		device.SessionLog = os.Stderr
	}

	fmt.Printf("Connecting to %s@%s:%d...\n", device.Username, device.Host, device.Port)
	conn, err := netmiko.ConnectHandler(device)
	if err != nil {
		log.Fatalf("ConnectHandler error: %v", err)
	}

	if err := conn.Connect(context.Background()); err != nil {
		log.Fatalf("Connection failed: %v", err)
	}
	defer conn.Disconnect()
	fmt.Println("Connected; running pwd...")

	// Print PWD directly because the remote account's pwd command returns no
	// value, while the shell variable is available.
	output, err := conn.SendCommand(`echo "$PWD"`)
	if err != nil {
		log.Fatalf("SendCommand error: %v", err)
	}
	fmt.Println("--- pwd output ---")
	fmt.Printf("%q\n", output)
}
