package mockfs_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/balinomad/go-mockfs"
)

const (
	testDuration     = 50 * time.Millisecond
	testDurationLong = 100 * time.Millisecond
	tolerance        = 15 * time.Millisecond // Timing tolerance for test flakiness
)

// assertDuration checks if elapsed time is within expected range
func assertDuration(t *testing.T, start time.Time, expected time.Duration, name string) {
	t.Helper()
	elapsed := time.Since(start)
	if elapsed < expected-tolerance || elapsed > expected+tolerance {
		t.Errorf("%s: expected ~%v, got %v", name, expected, elapsed)
	}
}

// assertNoDuration checks that operation completed quickly (no sleep)
func assertNoDuration(t *testing.T, start time.Time, name string) {
	t.Helper()
	elapsed := time.Since(start)
	if elapsed > tolerance {
		t.Errorf("%s: expected no sleep, but took %v", name, elapsed)
	}
}

func TestNewLatencySimulator(t *testing.T) {
	t.Run("zero duration", func(t *testing.T) {
		ls := mockfs.NewLatencySimulator(0)
		if ls == nil {
			t.Fatal("expected non-nil simulator")
		}

		start := time.Now()
		ls.Simulate(mockfs.OpRead)
		assertNoDuration(t, start, "zero duration")
	})

	t.Run("positive duration", func(t *testing.T) {
		ls := mockfs.NewLatencySimulator(testDuration)

		start := time.Now()
		ls.Simulate(mockfs.OpRead)
		assertDuration(t, start, testDuration, "positive duration")
	})

	t.Run("negative duration panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for negative duration")
			}
		}()
		mockfs.NewLatencySimulator(-1 * time.Millisecond)
	})
}

func TestNewLatencySimulatorPerOp(t *testing.T) {
	t.Run("empty map", func(t *testing.T) {
		ls := mockfs.NewLatencySimulatorPerOp(map[mockfs.Operation]time.Duration{})

		start := time.Now()
		ls.Simulate(mockfs.OpRead)
		assertNoDuration(t, start, "empty map")
	})

	t.Run("per-operation durations", func(t *testing.T) {
		ls := mockfs.NewLatencySimulatorPerOp(map[mockfs.Operation]time.Duration{
			mockfs.OpRead:  testDuration,
			mockfs.OpWrite: testDurationLong,
		})

		start := time.Now()
		ls.Simulate(mockfs.OpRead)
		assertDuration(t, start, testDuration, "OpRead")

		start = time.Now()
		ls.Simulate(mockfs.OpWrite)
		assertDuration(t, start, testDurationLong, "OpWrite")
	})

	t.Run("fallback to OpUnknown", func(t *testing.T) {
		ls := mockfs.NewLatencySimulatorPerOp(map[mockfs.Operation]time.Duration{
			mockfs.OpUnknown: testDuration,
		})

		start := time.Now()
		ls.Simulate(mockfs.OpRead) // Not in map, should use OpUnknown
		assertDuration(t, start, testDuration, "fallback to OpUnknown")
	})

	t.Run("operation overrides OpUnknown", func(t *testing.T) {
		ls := mockfs.NewLatencySimulatorPerOp(map[mockfs.Operation]time.Duration{
			mockfs.OpUnknown: testDuration,
			mockfs.OpRead:    testDurationLong,
		})

		start := time.Now()
		ls.Simulate(mockfs.OpRead)
		assertDuration(t, start, testDurationLong, "operation overrides OpUnknown")
	})

	t.Run("negative duration panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for negative duration")
			}
		}()
		mockfs.NewLatencySimulatorPerOp(map[mockfs.Operation]time.Duration{
			mockfs.OpRead: -1 * time.Millisecond,
		})
	})
}

func TestNewNoopLatencySimulator(t *testing.T) {
	ls := mockfs.NewNoopLatencySimulator()
	if ls == nil {
		t.Fatal("expected non-nil simulator")
	}

	start := time.Now()
	ls.Simulate(mockfs.OpRead)
	ls.Simulate(mockfs.OpWrite)
	assertNoDuration(t, start, "noop simulator")
}

func TestSimulate_InvalidOperation(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	start := time.Now()
	ls.Simulate(mockfs.InvalidOperation)
	assertDuration(t, start, testDuration, "invalid operation uses OpUnknown")
}

func TestSimulate_OutOfRangeOperation(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	start := time.Now()
	ls.Simulate(mockfs.Operation(999)) // Out of range
	assertDuration(t, start, testDuration, "out of range operation uses OpUnknown")
}

func TestSimulate_DefaultSerialized(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	var wg sync.WaitGroup
	startTimes := make([]time.Time, 3)
	endTimes := make([]time.Time, 3)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startTimes[idx] = time.Now()
			ls.Simulate(mockfs.OpRead)
			endTimes[idx] = time.Now()
		}(i)
	}

	wg.Wait()

	// In serialized mode, operations should not overlap significantly
	// Each should take ~testDuration, total should be ~3*testDuration
	totalElapsed := endTimes[2].Sub(startTimes[0])
	if totalElapsed < 2*testDuration {
		t.Errorf("serialized mode: operations overlapped too much, total: %v", totalElapsed)
	}
}

func TestSimulate_Async(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ls.Simulate(mockfs.OpRead, mockfs.Async())
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// In async mode, all 3 should run concurrently, total time ~testDuration
	if elapsed > testDuration+tolerance*2 {
		t.Errorf("async mode: expected concurrent execution ~%v, got %v", testDuration, elapsed)
	}
}

func TestSimulate_Once(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	start := time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	firstElapsed := time.Since(start)

	// First call should sleep
	assertDuration(t, start, testDuration, "once first call")

	// Second call should not sleep
	start = time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	assertNoDuration(t, start, "once second call")

	// Different operation should still sleep
	start = time.Now()
	ls.Simulate(mockfs.OpWrite, mockfs.Once())
	assertDuration(t, start, testDuration, "once different operation")

	_ = firstElapsed // Use the variable
}

func TestSimulate_OnceAsync(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	var wg sync.WaitGroup
	callCount := int32(0)
	completedCount := int32(0)

	start := time.Now()

	// Launch 10 concurrent calls with OnceAsync
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			atomic.AddInt32(&callCount, 1)
			ls.Simulate(mockfs.OpRead, mockfs.OnceAsync())
			atomic.AddInt32(&completedCount, 1)
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Should complete in ~testDuration (all run async, but only one actually sleeps)
	if elapsed > testDuration+tolerance*2 {
		t.Errorf("onceAsync: expected ~%v, got %v", testDuration, elapsed)
	}

	if callCount != 10 {
		t.Errorf("expected 10 calls, got %d", callCount)
	}
	if completedCount != 10 {
		t.Errorf("expected 10 completions, got %d", completedCount)
	}
}

func TestSimulate_OnceSerialized(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	var wg sync.WaitGroup
	completed := int32(0)

	start := time.Now()

	// Launch multiple goroutines with Once (serialized)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ls.Simulate(mockfs.OpRead, mockfs.Once())
			atomic.AddInt32(&completed, 1)
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Only first goroutine should sleep, others skip immediately
	// Total time should be ~testDuration, not 5*testDuration
	if elapsed > testDuration+tolerance*2 {
		t.Errorf("once serialized: expected ~%v, got %v", testDuration, elapsed)
	}

	if completed != 5 {
		t.Errorf("expected 5 completions, got %d", completed)
	}
}

func TestSimulate_MixedOptions(t *testing.T) {
	t.Run("once then non-once", func(t *testing.T) {
		ls := mockfs.NewLatencySimulator(testDuration)

		// First with Once
		start := time.Now()
		ls.Simulate(mockfs.OpRead, mockfs.Once())
		assertDuration(t, start, testDuration, "once first")

		// Then without Once (should still sleep)
		start = time.Now()
		ls.Simulate(mockfs.OpRead)
		assertDuration(t, start, testDuration, "non-once after once")
	})

	t.Run("async then serialized", func(t *testing.T) {
		ls := mockfs.NewLatencySimulator(testDuration)

		// Async call
		start := time.Now()
		ls.Simulate(mockfs.OpRead, mockfs.Async())
		assertDuration(t, start, testDuration, "async")

		// Serialized call
		start = time.Now()
		ls.Simulate(mockfs.OpRead)
		assertDuration(t, start, testDuration, "serialized after async")
	})
}

func TestReset(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	// First call with Once
	start := time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	assertDuration(t, start, testDuration, "before reset")

	// Second call should be skipped
	start = time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	assertNoDuration(t, start, "before reset - second call")

	// Reset
	ls.Reset()

	// After reset, should sleep again
	start = time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	assertDuration(t, start, testDuration, "after reset")
}

func TestReset_MultipleOperations(t *testing.T) {
	ls := mockfs.NewLatencySimulator(testDuration)

	// Mark multiple operations as seen
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	ls.Simulate(mockfs.OpWrite, mockfs.Once())
	ls.Simulate(mockfs.OpOpen, mockfs.Once())

	// Verify they're skipped
	start := time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	ls.Simulate(mockfs.OpWrite, mockfs.Once())
	ls.Simulate(mockfs.OpOpen, mockfs.Once())
	assertNoDuration(t, start, "all operations seen")

	// Reset
	ls.Reset()

	// All should work again
	start = time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	assertDuration(t, start, testDuration, "OpRead after reset")

	start = time.Now()
	ls.Simulate(mockfs.OpWrite, mockfs.Once())
	assertDuration(t, start, testDuration, "OpWrite after reset")
}

func TestSimulate_ConcurrentReset(t *testing.T) {
	// This test verifies Reset is safe when called after operations complete
	ls := mockfs.NewLatencySimulator(testDuration)

	var wg sync.WaitGroup

	// Run multiple operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ls.Simulate(mockfs.OpRead, mockfs.Once())
		}()
	}

	wg.Wait()

	// Now reset (safe because all operations completed)
	ls.Reset()

	// Verify reset worked
	start := time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	assertDuration(t, start, testDuration, "after concurrent reset")
}

func TestSimulate_MultipleOperationTypes(t *testing.T) {
	durations := map[mockfs.Operation]time.Duration{
		mockfs.OpRead:  testDuration,
		mockfs.OpWrite: testDurationLong,
		mockfs.OpOpen:  testDuration / 2,
	}
	ls := mockfs.NewLatencySimulatorPerOp(durations)

	start := time.Now()
	ls.Simulate(mockfs.OpRead)
	assertDuration(t, start, testDuration, "OpRead")

	start = time.Now()
	ls.Simulate(mockfs.OpWrite)
	assertDuration(t, start, testDurationLong, "OpWrite")

	start = time.Now()
	ls.Simulate(mockfs.OpOpen)
	assertDuration(t, start, testDuration/2, "OpOpen")
}

func TestSimulate_ZeroDurationOperation(t *testing.T) {
	ls := mockfs.NewLatencySimulatorPerOp(map[mockfs.Operation]time.Duration{
		mockfs.OpRead:  testDuration,
		mockfs.OpWrite: 0, // Explicit zero
	})

	start := time.Now()
	ls.Simulate(mockfs.OpWrite)
	assertNoDuration(t, start, "zero duration operation")

	start = time.Now()
	ls.Simulate(mockfs.OpRead)
	assertDuration(t, start, testDuration, "non-zero duration operation")
}

func TestSimOpt_Once(t *testing.T) {
	// Test Once() behavior: first call sleeps, second doesn't
	ls := mockfs.NewLatencySimulator(testDuration)

	start := time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	assertDuration(t, start, testDuration, "Once first call should sleep")

	start = time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once())
	assertNoDuration(t, start, "Once second call should not sleep")
}

func TestSimOpt_Async(t *testing.T) {
	// Test Async() behavior: multiple concurrent calls complete in ~testDuration
	ls := mockfs.NewLatencySimulator(testDuration)

	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ls.Simulate(mockfs.OpRead, mockfs.Async())
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// All 3 should run concurrently (not serialized)
	if elapsed > testDuration+tolerance*2 {
		t.Errorf("Async should allow concurrent execution, expected ~%v, got %v", testDuration, elapsed)
	}
}

func TestSimOpt_OnceAsync(t *testing.T) {
	// Test OnceAsync() behavior: combines both Once and Async
	ls := mockfs.NewLatencySimulator(testDuration)

	start := time.Now()
	var wg sync.WaitGroup

	// First call should sleep
	wg.Add(1)
	go func() {
		defer wg.Done()
		ls.Simulate(mockfs.OpRead, mockfs.OnceAsync())
	}()
	wg.Wait()
	firstElapsed := time.Since(start)
	assertDuration(t, start, testDuration, "OnceAsync first call should sleep")

	// Second call should not sleep (Once behavior)
	start = time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.OnceAsync())
	assertNoDuration(t, start, "OnceAsync second call should not sleep")

	_ = firstElapsed // Use the variable
}

func TestSimOpt_MultipleOptions(t *testing.T) {
	// Test applying both Once() and Async() separately
	ls := mockfs.NewLatencySimulator(testDuration)

	start := time.Now()
	var wg sync.WaitGroup

	// Launch concurrent calls with both options
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ls.Simulate(mockfs.OpRead, mockfs.Once(), mockfs.Async())
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Should complete quickly (async) and only one should actually sleep (once)
	if elapsed > testDuration+tolerance*2 {
		t.Errorf("Multiple options should work together, expected ~%v, got %v", testDuration, elapsed)
	}

	// Second call should not sleep (once was applied)
	start = time.Now()
	ls.Simulate(mockfs.OpRead, mockfs.Once(), mockfs.Async())
	assertNoDuration(t, start, "Multiple options second call should not sleep")
}

// Benchmark tests
func BenchmarkSimulate_NoLatency(b *testing.B) {
	ls := mockfs.NewLatencySimulator(0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ls.Simulate(mockfs.OpRead)
	}
}

func BenchmarkSimulate_WithLatency(b *testing.B) {
	ls := mockfs.NewLatencySimulator(time.Microsecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ls.Simulate(mockfs.OpRead)
	}
}

func BenchmarkSimulate_Once(b *testing.B) {
	ls := mockfs.NewLatencySimulator(time.Microsecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ls.Simulate(mockfs.OpRead, mockfs.Once())
	}
}

func BenchmarkSimulate_OnceAsync(b *testing.B) {
	ls := mockfs.NewLatencySimulator(time.Microsecond)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ls.Simulate(mockfs.OpRead, mockfs.OnceAsync())
	}
}

func BenchmarkSimulate_Parallel(b *testing.B) {
	ls := mockfs.NewLatencySimulator(0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			ls.Simulate(mockfs.OpRead)
		}
	})
}
