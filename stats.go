package mockfs

import (
	"fmt"
	"sync"
)

// Stats records operation statistics for verification in tests.
// It is safe for concurrent use.
type Stats struct {
	bytesRead    uint64
	bytesWritten uint64
	ops          [NumOperations]struct {
		total   uint64
		failure uint64
	} // Operation counters
	mu sync.RWMutex // Protects all fields
}

// StatsSnapshot represents a read-only point-in-time copy of Stats.
type StatsSnapshot struct {
	BytesRead    int
	BytesWritten int
	Operations   [NumOperations]OpCount
}

// OpCount contains the total and failure counts for a single operation.
type OpCount struct {
	Total   int
	Failure int
}

// NewStats returns a pointer to a new Stats instance with all counters initialized to zero.
func NewStats() *Stats {
	return &Stats{}
}

// Record logs an operation, its result, and bytes (if applicable).
// This method is intended for internal use by MockFS and MockFile.
// It panics if the operation is invalid, as this indicates an internal library bug.
func (s *Stats) Record(op Operation, bytes int, err error) {
	if op >= NumOperations || op < 0 {
		panic(fmt.Sprintf("mockfs: Stats.Record called with invalid operation: %d", op))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Increment total and failure counters
	s.ops[op].total++
	if err != nil {
		s.ops[op].failure++
	}

	// Always record bytes, even on partial read/write or other errors
	if bytes > 0 {
		switch op {
		case OpRead:
			s.bytesRead += uint64(bytes)
		case OpWrite:
			s.bytesWritten += uint64(bytes)
		}
	}
}

// Count reports the total number of times the given operation was called.
// It panics if the operation is invalid.
func (s *Stats) Count(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.Count called with invalid operation: %d", op))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return int(s.ops[op].total)
}

// CountSuccess reports the number of times the given operation was called and returned no error.
// It panics if the operation is invalid.
func (s *Stats) CountSuccess(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.CountSuccess called with invalid operation: %d", op))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return int(s.ops[op].total - s.ops[op].failure)
}

// CountFailure reports the number of times the given operation was called and returned an error.
// It panics if the operation is invalid.
func (s *Stats) CountFailure(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.CountFailure called with invalid operation: %d", op))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	return int(s.ops[op].failure)
}

// GetBytesRead reports the total number of bytes read successfully or partially.
func (s *Stats) GetBytesRead() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return int(s.bytesRead)
}

// GetBytesWritten reports the total number of bytes written successfully or partially.
func (s *Stats) GetBytesWritten() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return int(s.bytesWritten)
}

// Snapshot returns a point-in-time copy of all counters.
// The snapshot is consistent: all operation counts reflect the same moment in time.
func (s *Stats) Snapshot() StatsSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var snap StatsSnapshot
	snap.BytesRead = int(s.bytesRead)
	snap.BytesWritten = int(s.bytesWritten)
	for i := 0; i < int(NumOperations); i++ {
		snap.Operations[i].Total = int(s.ops[i].total)
		snap.Operations[i].Failure = int(s.ops[i].failure)
	}

	return snap
}

// Set sets the total and failure counts for the given operation.
// It panics if the operation is invalid or if failures is negative or exceeds total.
func (s *Stats) Set(op Operation, total int, failures int) {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.Set called with invalid operation: %d", op))
	}
	if failures < 0 {
		panic(fmt.Sprintf("mockfs: Stats.Set: failures (%d) cannot be negative", failures))
	}
	if failures > total {
		panic(fmt.Sprintf("mockfs: Stats.Set: failures (%d) exceeds total (%d)", failures, total))
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.ops[op].total = uint64(total)
	s.ops[op].failure = uint64(failures)
}

// Reset resets all operation counters to zero.
func (s *Stats) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bytesRead = 0
	s.bytesWritten = 0
	for i := 0; i < int(NumOperations); i++ {
		s.ops[i].total = 0
		s.ops[i].failure = 0
	}
}

// Clone returns a deep copy of the Stats instance.
func (s *Stats) Clone() *Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	clone := NewStats()
	clone.bytesRead = s.bytesRead
	clone.bytesWritten = s.bytesWritten
	for i := 0; i < int(NumOperations); i++ {
		clone.ops[i].total = s.ops[i].total
		clone.ops[i].failure = s.ops[i].failure
	}

	return clone
}

// Equal reports whether the given Stats instance has the same counts as the receiver.
func (s *Stats) Equal(other *Stats) bool {
	if s == other {
		return true
	}

	a := s.Snapshot()
	b := other.Snapshot()

	return a == b
}
