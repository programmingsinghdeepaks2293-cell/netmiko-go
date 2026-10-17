package netmiko

import (
	"context"
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
func ExecuteConcurrently(
	ctx context.Context,
	devices []Device,
	maxConcurrency int,
	task DeviceTaskFunc,
) map[string]DeviceResult {
	if maxConcurrency <= 0 || maxConcurrency > len(devices) {
		maxConcurrency = len(devices)
	}
	if maxConcurrency == 0 {
		return make(map[string]DeviceResult)
	}

	deviceChan := make(chan Device, len(devices))
	resultChan := make(chan DeviceResult, len(devices))

	// Enqueue all devices
	for _, dev := range devices {
		deviceChan <- dev
	}
	close(deviceChan)

	var wg sync.WaitGroup

	// Spawn worker goroutines
	for i := 0; i < maxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case dev, ok := <-deviceChan:
					if !ok {
						return
					}
					start := time.Now()
					out, err := executeSingleDevice(ctx, dev, task)
					resultChan <- DeviceResult{
						Device:   dev,
						Output:   out,
						Err:      err,
						Duration: time.Since(start),
					}
				}
			}
		}()
	}

	// Closer goroutine: waits for all workers to finish then closes resultChan
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	results := make(map[string]DeviceResult)
	for res := range resultChan {
		key := res.Device.Host
		if key == "" {
			key = res.Device.DeviceType
		}
		results[key] = res
	}

	return results
}

func executeSingleDevice(ctx context.Context, dev Device, task DeviceTaskFunc) (string, error) {
	conn, err := ConnectHandler(dev)
	if err != nil {
		return "", err
	}

	if err := conn.Connect(ctx); err != nil {
		return "", err
	}
	defer func() { _ = conn.Disconnect() }()

	return task(conn)
}
