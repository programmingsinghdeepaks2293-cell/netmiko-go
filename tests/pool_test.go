package tests

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/netmiko-go/netmiko/pkg/drivers"
	"github.com/netmiko-go/netmiko/pkg/mock"
	"github.com/netmiko-go/netmiko/pkg/netmiko"
)

func TestExecuteConcurrentlyWithGoroutines(t *testing.T) {
	deviceCount := 5
	devices := make([]netmiko.Device, deviceCount)

	// Register custom driver factory for mocked concurrent testing
	for i := 0; i < deviceCount; i++ {
		host := fmt.Sprintf("192.168.1.%d", i+1)
		devType := fmt.Sprintf("mock_cisco_%d", i)

		devices[i] = netmiko.Device{
			DeviceType: devType,
			Host:       host,
		}

		mockTerm := mock.NewMockCiscoTerminal(fmt.Sprintf("switch-%d", i+1))
		netmiko.RegisterDriver(devType, func(d netmiko.Device) netmiko.Connection {
			bc := netmiko.NewBaseConnection(d)
			bc.SetTransport(mockTerm)
			return bc
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Execute across all devices using 3 concurrent goroutine workers
	results := netmiko.ExecuteConcurrently(ctx, devices, 3, func(conn netmiko.Connection) (string, error) {
		return conn.SendCommand("show ip int br")
	})

	if len(results) != deviceCount {
		t.Fatalf("expected %d results, got %d", deviceCount, len(results))
	}

	for host, res := range results {
		if res.Err != nil {
			t.Errorf("host %s returned error: %v", host, res.Err)
		}
		if !strings.Contains(res.Output, "GigabitEthernet0/0") {
			t.Errorf("host %s expected command output, got: %s", host, res.Output)
		}
	}
}
