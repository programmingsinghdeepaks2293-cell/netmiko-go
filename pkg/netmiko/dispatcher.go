package netmiko

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// DriverFactory is a function that creates a Connection for a given Device configuration.
type DriverFactory func(dev Device) Connection

var (
	driverRegistryMu sync.RWMutex
	driverRegistry   = make(map[string]DriverFactory)
)

// RegisterDriver registers a DriverFactory for a given device_type identifier.
func RegisterDriver(deviceType string, factory DriverFactory) {
	driverRegistryMu.Lock()
	defer driverRegistryMu.Unlock()
	driverRegistry[strings.ToLower(deviceType)] = factory
}

// ConnectHandler is the primary Netmiko factory function that instantiates the appropriate
// vendor connection driver matching dev.DeviceType.
func ConnectHandler(dev Device) (Connection, error) {
	driverRegistryMu.RLock()
	defer driverRegistryMu.RUnlock()

	normType := strings.ToLower(strings.TrimSpace(dev.DeviceType))
	factory, exists := driverRegistry[normType]
	if !exists {
		return nil, fmt.Errorf("unsupported device_type %q; supported platforms: %v", dev.DeviceType, Platforms())
	}

	return factory(dev), nil
}

// Platforms returns a sorted slice of all currently registered platform device_types.
func Platforms() []string {
	driverRegistryMu.RLock()
	defer driverRegistryMu.RUnlock()

	keys := make([]string, 0, len(driverRegistry))
	for k := range driverRegistry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
