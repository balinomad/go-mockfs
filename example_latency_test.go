package mockfs_test

import (
	"fmt"
	"time"

	"github.com/balinomad/go-mockfs"
)

// ExampleWithLatency demonstrates global latency simulation.
func ExampleWithLatency() {
	start := time.Now()

	mfs := mockfs.NewMockFS(
		map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("data"), Mode: 0644, ModTime: time.Now()},
		},
		mockfs.WithLatency(50*time.Millisecond),
	)

	// Each operation adds 50ms delay
	mfs.Stat("file.txt")
	mfs.Open("file.txt")

	elapsed := time.Since(start)
	fmt.Printf("Operations took >= 100ms: %v\n", elapsed >= 100*time.Millisecond)
	// Output: Operations took >= 100ms: true
}

// ExampleWithPerOperationLatency demonstrates per-operation latency.
func ExampleWithPerOperationLatency() {
	mfs := mockfs.NewMockFS(
		map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("data"), Mode: 0644, ModTime: time.Now()},
		},
		mockfs.WithPerOperationLatency(map[mockfs.Operation]time.Duration{
			mockfs.OpStat: 10 * time.Millisecond,
			mockfs.OpOpen: 50 * time.Millisecond,
			mockfs.OpRead: 100 * time.Millisecond,
		}),
	)

	start := time.Now()
	mfs.Stat("file.txt") // 10ms
	elapsed := time.Since(start)
	fmt.Printf("Stat took >= 10ms: %v\n", elapsed >= 10*time.Millisecond)

	start = time.Now()
	file, _ := mfs.Open("file.txt") // 50ms
	elapsed = time.Since(start)
	fmt.Printf("Open took >= 50ms: %v\n", elapsed >= 50*time.Millisecond)

	start = time.Now()
	buf := make([]byte, 10)
	file.Read(buf) // 100ms
	elapsed = time.Since(start)
	fmt.Printf("Read took >= 100ms: %v\n", elapsed >= 100*time.Millisecond)
	// Output:
	// Stat took >= 10ms: true
	// Open took >= 50ms: true
	// Read took >= 100ms: true
}

// ExampleLatencySimulator_Simulate_once demonstrates once mode.
func ExampleLatencySimulator_Simulate_once() {
	sim := mockfs.NewLatencySimulator(50 * time.Millisecond)

	// First call has latency
	start := time.Now()
	sim.Simulate(mockfs.OpRead, mockfs.Once())
	first := time.Since(start)

	// Second call has no latency
	start = time.Now()
	sim.Simulate(mockfs.OpRead, mockfs.Once())
	second := time.Since(start)

	fmt.Printf("First >= 50ms: %v\n", first >= 50*time.Millisecond)
	fmt.Printf("Second < 10ms: %v\n", second < 10*time.Millisecond)
	// Output:
	// First >= 50ms: true
	// Second < 10ms: true
}

// ExampleLatencySimulator_Simulate_async demonstrates async mode.
func ExampleLatencySimulator_Simulate_async() {
	sim := mockfs.NewLatencySimulator(30 * time.Millisecond)

	start := time.Now()

	// Async mode: both operations can run concurrently
	done := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		go func() {
			sim.Simulate(mockfs.OpRead, mockfs.Async())
			done <- true
		}()
	}

	<-done
	<-done
	elapsed := time.Since(start)

	// Both complete in ~30ms (not 60ms) due to async
	fmt.Printf("Completed in < 50ms: %v\n", elapsed < 50*time.Millisecond)
	// Output: Completed in < 50ms: true
}
