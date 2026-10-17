package netmiko

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// DeviceResult captures the execution output and timing for a specific device.
type DeviceResult struct {
	Device   Device
	Output   string
	Err      error
	Duration time.Duration
}

// DeviceTaskFunc defines the operation to perform on a connected device.
type DeviceTaskFunc func(conn Connection) (string, error)

// ExecuteConcurrently runs a task across multiple network devices in parallel using a goroutine worker pool.
//
// maxConcurrency controls how many devices are automated simultaneously. If <= 0, defaults to len(devices).
//
// Results are keyed by "host:port" (a "#<index>" suffix is added if two devices share an address).
// Every device gets an entry. Devices that were not started because ctx was cancelled have Err set to ctx's error.
func ExecuteConcurrently(
	ctx context.Context,
	devices []Device,
	maxConcurrency int,
	task DeviceTaskFunc,
) map[string]DeviceResult {
	results := make(map[string]DeviceResult, len(devices))
	if len(devices) == 0 {
		return results
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if maxConcurrency <= 0 || maxConcurrency > len(devices) {
		maxConcurrency = len(devices)
	}

	keys := resultKeys(devices)

	jobs := make(chan int, len(devices))
	for i := range devices {
		jobs <- i
	}
	close(jobs)

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for w := 0; w < maxConcurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				if ctx.Err() != nil {
					continue // cancelled: filled in with ctx's error below
				}
				dev := devices[idx]
				start := time.Now()
				out, err := executeSingleDevice(ctx, dev, task)
				res := DeviceResult{
					Device:   dev,
					Output:   out,
					Err:      err,
					Duration: time.Since(start),
				}
				mu.Lock()
				results[keys[idx]] = res
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	for i, dev := range devices {
		if _, ok := results[keys[i]]; ok {
			continue
		}
		err := ctx.Err()
		if err == nil {
			err = errors.New("device was not executed")
		}
		results[keys[i]] = DeviceResult{Device: dev, Err: err}
	}
	return results
}

// resultKeys returns a unique "host:port" key for each device, in input order.
func resultKeys(devices []Device) []string {
	keys := make([]string, len(devices))
	seen := make(map[string]bool, len(devices))
	for i, d := range devices {
		host := d.Host
		if host == "" {
			host = d.DeviceType
		}
		port := d.Port
		if port == 0 {
			port = 22
		}
		key := net.JoinHostPort(host, strconv.Itoa(port))
		if seen[key] {
			key = fmt.Sprintf("%s#%d", key, i)
		}
		seen[key] = true
		keys[i] = key
	}
	return keys
}

func executeSingleDevice(ctx context.Context, dev Device, task DeviceTaskFunc) (out string, err error) {
	conn, err := ConnectHandler(dev)
	if err != nil {
		return "", err
	}

	if err := conn.Connect(ctx); err != nil {
		return "", err
	}
	defer func() { _ = conn.Disconnect() }()
	// A panic in one device's task should fail that device, not the whole run.
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("task panicked: %v", r)
		}
	}()

	return task(conn)
}
