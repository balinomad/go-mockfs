package mockfs

import (
	"fmt"
	"sync"
	"time"
)

// LatencySimulator simulates latency for operations and is thread-safe.
// It should be configured at creation time and is immutable thereafter.
//
// The simulator maintains a "seen" state for Once() mode. Call Reset() to clear
// this state when reusing the simulator (e.g., after closing and reopening a file).
type LatencySimulator interface {
	// Simulate simulates latency for an operation. It is thread-safe.
	//
	// By default, Simulate serializes access (holds lock during sleep) to model
	// blocking I/O. Use Async() to release the lock before sleeping.
	//
	// Use Once() to ensure an operation's latency is simulated at most once.
	Simulate(op Operation, opts ...SimOpt)

	// Reset clears the internal "seen" state for all operations.
	// Must be called when no other goroutines are calling Simulate().
	Reset()

	// Clone returns a copy of the simulator with reset state.
	Clone() LatencySimulator
}

type simOptions struct {
	once  bool
	async bool
}

type SimOpt func(*simOptions)

// Once makes Simulate apply latency at most once for the operation type.
// The first call simulates latency; subsequent calls for the same operation return immediately.
func Once() SimOpt { return func(o *simOptions) { o.once = true } }

// Async makes Simulate release the lock before sleeping (non-serialized).
// Use this when operations should not block each other.
func Async() SimOpt { return func(o *simOptions) { o.async = true } }

// OnceAsync is a convenience function that applies Once and Async.
func OnceAsync() SimOpt { return func(o *simOptions) { o.once = true; o.async = true } }

// latencySimulator implements LatencySimulator.
type latencySimulator struct {
	durations [NumOperations]time.Duration // Duration for each operation, OpUnknown contains global duration.
	seen      [NumOperations]bool          // Tracks whether an operation latency has been simulated.
	mu        sync.Mutex                   // Mutex for concurrent access.
}

// NewLatencySimulator returns a LatencySimulator with a global duration for all operations.
// If duration is 0, no latency is simulated.
// Panics if duration is negative.
func NewLatencySimulator(duration time.Duration) LatencySimulator {
	if duration < 0 {
		panic(fmt.Sprintf("mockfs: negative duration not allowed: %v", duration))
	}

	ls := &latencySimulator{}
	ls.durations[OpUnknown] = duration

	return ls
}

// NewLatencySimulatorPerOp creates a simulator that uses per-operation durations.
// If an operation is missing from the map, it falls back to OpUnknown's duration,
// then to zero (no sleep) if OpUnknown is also not specified.
// Panics if any duration is negative.
func NewLatencySimulatorPerOp(durations map[Operation]time.Duration) LatencySimulator {
	ls := &latencySimulator{}
	for op, dur := range durations {
		if dur < 0 {
			panic(fmt.Sprintf("mockfs: negative duration not allowed for %v: %v", op, dur))
		}
		ls.durations[op] = dur
	}

	return ls
}

// NewNoopLatencySimulator returns a LatencySimulator that does nothing (useful for tests).
func NewNoopLatencySimulator() LatencySimulator {
	return &latencySimulator{}
}

// Simulate simulates latency for an operation. It is thread-safe.
//
// Parameters:
//   - op - the operation to simulate.
//   - opts - optional simulation options. See Once(), Async(), OnceAsync().
func (ls *latencySimulator) Simulate(op Operation, opts ...SimOpt) {
	// Parse options
	var so simOptions
	for _, o := range opts {
		o(&so)
	}

	// Normalize operation
	if !op.IsValid() {
		op = OpUnknown
	}

	// Resolve duration with fallback
	dur := ls.durations[op]
	if dur == 0 && op != OpUnknown {
		dur = ls.durations[OpUnknown]
	}

	// Early exit if no latency
	if dur == 0 {
		return
	}

	// Handle Once mode
	if so.once {
		ls.mu.Lock()
		if ls.seen[op] {
			ls.mu.Unlock()

			return
		}
		ls.seen[op] = true

		if so.async {
			ls.mu.Unlock()
			time.Sleep(dur)

			return
		}

		// Serialized once: hold lock while sleeping
		time.Sleep(dur)
		ls.mu.Unlock()

		return
	}

	// Non-once mode
	if !so.async {
		// Serialized: hold lock during sleep
		ls.mu.Lock()
		time.Sleep(dur)
		ls.mu.Unlock()

		return
	}

	// Async: sleep without lock
	time.Sleep(dur)
}

// Reset clears the internal "seen" state for all operations.
// Must be called when no other goroutines are calling Simulate().
func (ls *latencySimulator) Reset() {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	ls.seen = [NumOperations]bool{}
}

// Clone returns a copy of the simulator with reset state.
// The returned simulator has the same duration configuration but
// fresh Once() tracking state.
func (ls *latencySimulator) Clone() LatencySimulator {
	ls.mu.Lock()
	defer ls.mu.Unlock()

	clone := &latencySimulator{
		durations: ls.durations,
		// seen is zero-initialized
	}

	return clone
}
