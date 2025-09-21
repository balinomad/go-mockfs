package mockfs

import (
	"sync"
)

// Counters records operation counts for verification in tests.
// It is safe for concurrent use.
type Counters struct {
	calls [NumOperations]int
	mu    sync.RWMutex
}

// NewCounters returns a pointer to a new Counters instance with all operation counters initialized to zero.
func NewCounters() *Counters {
	return &Counters{}
}

// Count reports the current count for the given operation.
func (c *Counters) Count(op Operation) int {
	if op >= NumOperations || op < 0 {
		return 0
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.calls[op]
}

// Snapshot returns a copy of all operation counters with their respective counts.
func (c *Counters) Snapshot() [NumOperations]int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.calls
}

// Set sets the operation counter for the given operation to the given count.
// This is useful for tests that need to set up specific operation counts before verification.
func (c *Counters) Set(op Operation, count int) {
	if op >= NumOperations || op < 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls[op] = count
}

// ResetAll resets all operation counters to zero.
func (c *Counters) ResetAll() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls = [NumOperations]int{}
}

// Clone returns a deep copy of the Counters instance.
func (c *Counters) Clone() *Counters {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := &Counters{}
	clone.calls = c.calls

	return clone
}

// Equal reports whether the given Counters instance has the same operation counts as the receiver.
func (c *Counters) Equal(other *Counters) bool {
	// Fast path when pointers are equal
	if c == other {
		return true
	}

	a := c.Snapshot()
	b := other.Snapshot()

	return a == b
}

// inc increments the counter for the given operation.
func (c *Counters) inc(op Operation) {
	if op >= NumOperations || op < 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.calls[op]++
}
