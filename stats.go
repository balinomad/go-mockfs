package mockfs

import (
	"fmt"
	"sync"
)

// TestReporter is a minimal interface for reporting test failures.
// Both [*testing.T] and [*testing.B] satisfy this interface.
type TestReporter interface {
	// Errorf reports a test failure.
	Errorf(format string, args ...any)

	// Helper marks the calling function as a test helper function.
	Helper()
}

// Stats is the read-only statistics interface returned by MockFS.Stats() and MockFile.Stats().
// It represents an immutable snapshot of operation statistics at a point in time.
//
// All Stats instances are safe for concurrent reads and can be safely stored/compared
// across goroutines. Treat Stats as a value type even if type-asserting to concrete
// implementations.
type Stats interface {
	// Operation counters

	// Count reports the total number of times the given operation was called.
	Count(Operation) int

	// CountSuccess reports the number of times the given operation succeeded.
	CountSuccess(Operation) int

	// CountFailure reports the number of times the given operation failed.
	CountFailure(Operation) int

	// Operations reports the total number of operations across all types.
	Operations() int

	// Byte counters

	// BytesRead reports the total number of bytes read.
	BytesRead() int

	// BytesWritten reports the total number of bytes written.
	BytesWritten() int

	// Assessment queries (derived state)

	// HasFailures reports whether any operation has failed.
	HasFailures() bool

	// Failures reports operations that have at least one failure.
	FailedOperations() []Operation

	// Empty reports whether no operations have been recorded.
	Empty() bool

	// Comparison

	// Delta returns the difference between this and other stats.
	// Negative values indicate the counter decreased (e.g., after Reset()).
	Delta(other Stats) Stats

	// Equal reports whether this Stats has the same values as other.
	Equal(other Stats) bool

	// String returns a human-readable summary.
	String() string

	// Testing integration

	// Expect returns a StatsAssertion for fluent verification in tests.
	//
	// Usage:
	//
	//	stats.Expect().
	//		Count(OpRead, 3).
	//		Count(OpWrite, 1).
	//		NoFailures().
	//		Assert(t)
	Expect() StatsAssertion
}

// StatsAssertion is a fluent assertion interface for Stats.
type StatsAssertion interface {
	// Count asserts the total number of times the given operation was called.
	Count(op Operation, expected int) StatsAssertion

	// Success asserts the total number of times the given operation succeeded.
	Success(op Operation, expected int) StatsAssertion

	// Failure asserts the total number of times the given operation failed.
	Failure(op Operation, expected int) StatsAssertion

	// NoFailures asserts that no operations have failed.
	NoFailures() StatsAssertion

	// BytesRead asserts the total number of bytes read.
	BytesRead(expected int) StatsAssertion

	// BytesWritten asserts the total number of bytes written.
	BytesWritten(expected int) StatsAssertion

	// Assert runs the assertions.
	Assert(TestReporter)
}

// StatsRecorder is the mutable statistics interface used internally by MockFS and MockFile.
// It extends Stats with mutation methods for recording operations and manipulating counters.
//
// This interface is exported to allow custom filesystem implementations to use the same
// statistics mechanism. Users of MockFS/MockFile typically interact only with the read-only
// Stats interface.
type StatsRecorder interface {
	Stats

	// Record logs an operation result and bytes transferred (if applicable).
	// Panics if the operation is invalid.
	Record(op Operation, bytes int, err error)

	// Set directly sets the total and failure counts for an operation.
	// Panics if the operation is invalid, failures is negative, or failures > total.
	Set(op Operation, total int, failures int)

	// SetBytes sets the byte counters directly.
	// This is useful for initialization or restoration from storage.
	SetBytes(read int, written int)

	// Reset resets all counters to zero.
	Reset()

	// Snapshot returns an immutable Stats view of the current state.
	Snapshot() Stats
}

// --- StatsRecorder Implementation ---

// statsRecorder is the internal mutable implementation.
type statsRecorder struct {
	bytesRead    uint64
	bytesWritten uint64
	ops          [NumOperations]struct {
		total   uint64
		failure uint64
	}
	mu sync.RWMutex
}

// Ensure interface implementations.
var _ StatsRecorder = (*statsRecorder)(nil)

// NewStatsRecorder creates a mutable StatsRecorder.
// If initial is nil, returns a recorder with all counters at zero.
// If initial is provided, the recorder is initialized with those values.
func NewStatsRecorder(initial Stats) StatsRecorder {
	r := &statsRecorder{}

	if initial != nil {
		for op := Operation(0); op < NumOperations; op++ {
			if !op.IsValid() {
				continue
			}
			total := initial.Count(op)
			failures := initial.CountFailure(op)
			if total > 0 || failures > 0 {
				r.Set(op, total, failures)
			}
		}
		r.SetBytes(initial.BytesRead(), initial.BytesWritten())
	}

	return r
}

// Record logs an operation result and bytes transferred.
func (r *statsRecorder) Record(op Operation, bytes int, err error) {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: StatsRecorder.Record called with invalid operation: %d", op))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.ops[op].total++
	if err != nil {
		r.ops[op].failure++
	}

	// Always record bytes, even on partial read/write or other errors
	if bytes > 0 {
		switch op {
		case OpRead:
			r.bytesRead += uint64(bytes)
		case OpWrite:
			r.bytesWritten += uint64(bytes)
		}
	}
}

// Set sets the total and failure counts for an operation.
func (r *statsRecorder) Set(op Operation, total int, failures int) {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: StatsRecorder.Set called with invalid operation: %d", op))
	}
	if failures < 0 {
		panic(fmt.Sprintf("mockfs: StatsRecorder.Set: failures (%d) cannot be negative", failures))
	}
	if failures > total {
		panic(fmt.Sprintf("mockfs: StatsRecorder.Set: failures (%d) exceeds total (%d)", failures, total))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.ops[op].total = uint64(total)
	r.ops[op].failure = uint64(failures)
}

// SetBytes sets the byte counters directly.
func (r *statsRecorder) SetBytes(read int, written int) {
	if read < 0 {
		panic(fmt.Sprintf("mockfs: StatsRecorder.SetBytes: read (%d) cannot be negative", read))
	}
	if written < 0 {
		panic(fmt.Sprintf("mockfs: StatsRecorder.SetBytes: written (%d) cannot be negative", written))
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.bytesRead = uint64(read)
	r.bytesWritten = uint64(written)
}

// Reset resets all counters to zero.
func (r *statsRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.bytesRead = 0
	r.bytesWritten = 0
	for i := 0; i < int(NumOperations); i++ {
		r.ops[i].total = 0
		r.ops[i].failure = 0
	}
}

// Snapshot returns an immutable Stats view of the current state.
func (r *statsRecorder) Snapshot() Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snap := statsSnapshot{
		bytesRead:    int(r.bytesRead),
		bytesWritten: int(r.bytesWritten),
	}
	for i := 0; i < int(NumOperations); i++ {
		snap.ops[i].total = int(r.ops[i].total)
		snap.ops[i].failure = int(r.ops[i].failure)
	}

	return snap
}

// Count reports the total number of times the given operation was called.
// It panics if the operation is invalid.
func (r *statsRecorder) Count(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.Count called with invalid operation: %d", op))
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return int(r.ops[op].total)
}

// CountSuccess reports the number of times the given operation succeeded.
// It panics if the operation is invalid.
func (r *statsRecorder) CountSuccess(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.CountSuccess called with invalid operation: %d", op))
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return int(r.ops[op].total - r.ops[op].failure)
}

// CountFailure reports the number of times the given operation failed.
// It panics if the operation is invalid.
func (r *statsRecorder) CountFailure(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.CountFailure called with invalid operation: %d", op))
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return int(r.ops[op].failure)
}

// Operations reports the total number of operations across all types.
func (r *statsRecorder) Operations() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	sum := 0
	for i := 0; i < int(NumOperations); i++ {
		sum += int(r.ops[i].total)
	}

	return sum
}

// BytesRead reports the total number of bytes read.
func (r *statsRecorder) BytesRead() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return int(r.bytesRead)
}

// BytesWritten reports the total number of bytes written.
func (r *statsRecorder) BytesWritten() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return int(r.bytesWritten)
}

// HasFailures reports whether any operation has failed.
func (r *statsRecorder) HasFailures() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for i := 0; i < int(NumOperations); i++ {
		if r.ops[i].failure > 0 {
			return true
		}
	}

	return false
}

// FailedOperations reports operations that have at least one failure.
func (r *statsRecorder) FailedOperations() []Operation {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var failed []Operation
	for i := 0; i < int(NumOperations); i++ {
		if r.ops[i].failure > 0 {
			failed = append(failed, Operation(i))
		}
	}

	return failed
}

// Empty reports whether any operations have been recorded.
func (r *statsRecorder) Empty() bool {
	return r.Operations() == 0
}

// Delta returns the difference between this and other stats.
func (r *statsRecorder) Delta(other Stats) Stats {
	return r.Snapshot().Delta(other)
}

// Equal reports whether this Stats has the same values as other.
func (r *statsRecorder) Equal(other Stats) bool {
	return r.Snapshot().Equal(other)
}

// String returns a human-readable summary.
func (r *statsRecorder) String() string {
	return r.Snapshot().String()
}

// Expect returns a fluent assertion for the current state.
func (s *statsRecorder) Expect() StatsAssertion {
	return s.Snapshot().Expect()
}

// --- Stats Snapshot Implementation ---

// statsSnapshot is the internal immutable implementation.
type statsSnapshot struct {
	bytesRead    int
	bytesWritten int
	ops          [NumOperations]struct {
		total   int
		failure int
	}
}

// Ensure interface implementation.
var _ Stats = (*statsSnapshot)(nil)

// Count reports the total number of times the given operation was called.
func (s statsSnapshot) Count(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.Count called with invalid operation: %d", op))
	}

	return s.ops[op].total
}

// CountSuccess reports the number of times the given operation succeeded.
func (s statsSnapshot) CountSuccess(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.CountSuccess called with invalid operation: %d", op))
	}

	return s.ops[op].total - s.ops[op].failure
}

// CountFailure reports the number of times the given operation failed.
func (s statsSnapshot) CountFailure(op Operation) int {
	if !op.IsValid() {
		panic(fmt.Sprintf("mockfs: Stats.CountFailure called with invalid operation: %d", op))
	}

	return s.ops[op].failure
}

// Operations reports the total number of operations across all types.
func (s statsSnapshot) Operations() int {
	sum := 0
	for i := 0; i < int(NumOperations); i++ {
		sum += s.ops[i].total
	}

	return sum
}

// BytesRead reports the total number of bytes read.
func (s statsSnapshot) BytesRead() int {
	return s.bytesRead
}

// BytesWritten reports the total number of bytes written.
func (s statsSnapshot) BytesWritten() int {
	return s.bytesWritten
}

// HasFailures reports whether any operation has failed.
func (s statsSnapshot) HasFailures() bool {
	for i := 0; i < int(NumOperations); i++ {
		if s.ops[i].failure > 0 {
			return true
		}
	}

	return false
}

// FailedOperations returns operations that have at least one failure.
func (s statsSnapshot) FailedOperations() []Operation {
	var failed []Operation
	for i := 0; i < int(NumOperations); i++ {
		if s.ops[i].failure > 0 {
			failed = append(failed, Operation(i))
		}
	}
	return failed
}

// Empty reports whether any operations have been recorded.
func (s statsSnapshot) Empty() bool {
	return s.Operations() == 0
}

// Delta returns the difference between this and other stats.
func (s statsSnapshot) Delta(other Stats) Stats {
	delta := statsSnapshot{
		bytesRead:    s.bytesRead - other.BytesRead(),
		bytesWritten: s.bytesWritten - other.BytesWritten(),
	}
	for i := 0; i < int(NumOperations); i++ {
		op := Operation(i)
		if !op.IsValid() {
			continue
		}
		delta.ops[i].total = s.ops[i].total - other.Count(op)
		delta.ops[i].failure = s.ops[i].failure - other.CountFailure(op)
	}

	return delta
}

// Equal reports whether this Stats has the same values as other.
func (s statsSnapshot) Equal(other Stats) bool {
	if s.bytesRead != other.BytesRead() || s.bytesWritten != other.BytesWritten() {
		return false
	}

	for i := 0; i < int(NumOperations); i++ {
		op := Operation(i)
		if !op.IsValid() {
			continue
		}
		if s.ops[i].total != other.Count(op) || s.ops[i].failure != other.CountFailure(op) {
			return false
		}
	}

	return true
}

// String returns a human-readable summary.
func (s statsSnapshot) String() string {
	totalOps := s.Operations()
	failCount := 0

	for i := 0; i < int(NumOperations); i++ {
		failCount += s.ops[i].failure
	}

	return fmt.Sprintf("Stats{Ops: %d (%d failures), Bytes: %d read, %d written}",
		totalOps, failCount, s.bytesRead, s.bytesWritten)
}

// Expect returns a fluent assertion for the current state.
func (s statsSnapshot) Expect() StatsAssertion {
	return &statsAssertion{stats: s}
}

// --- StatsAssertion Implementation ---

// statsAssertion is a fluent assertion interface implementation for Stats.
type statsAssertion struct {
	stats  Stats
	checks []func(TestReporter) // Accumulate checks for deferred execution
}

// Ensure interface implementation.
var _ StatsAssertion = (*statsAssertion)(nil)

// Count asserts the total number of times the given operation was called.
func (sa *statsAssertion) Count(op Operation, expected int) StatsAssertion {
	sa.checks = append(sa.checks, func(t TestReporter) {
		if got := sa.stats.Count(op); got != expected {
			t.Helper()
			t.Errorf("Count(%s) = %d, want %d", op, got, expected)
		}
	})
	return sa
}

// Success asserts the total number of times the given operation succeeded.
func (sa *statsAssertion) Success(op Operation, expected int) StatsAssertion {
	sa.checks = append(sa.checks, func(t TestReporter) {
		if got := sa.stats.CountSuccess(op); got != expected {
			t.Helper()
			t.Errorf("CountSuccess(%s) = %d, want %d", op, got, expected)
		}
	})
	return sa
}

// Failure asserts the total number of times the given operation failed.
func (sa *statsAssertion) Failure(op Operation, expected int) StatsAssertion {
	sa.checks = append(sa.checks, func(t TestReporter) {
		if got := sa.stats.CountFailure(op); got != expected {
			t.Helper()
			t.Errorf("CountFailure(%s) = %d, want %d", op, got, expected)
		}
	})
	return sa
}

// NoFailures asserts that no operations have failed.
func (sa *statsAssertion) NoFailures() StatsAssertion {
	sa.checks = append(sa.checks, func(t TestReporter) {
		if sa.stats.HasFailures() {
			t.Helper()
			failed := sa.stats.FailedOperations()
			t.Errorf("expected no failures, but found failures in: %v", failed)
		}
	})
	return sa
}

// BytesRead asserts the total number of bytes read.
func (sa *statsAssertion) BytesRead(expected int) StatsAssertion {
	sa.checks = append(sa.checks, func(t TestReporter) {
		if got := sa.stats.BytesRead(); got != expected {
			t.Helper()
			t.Errorf("BytesRead() = %d, want %d", got, expected)
		}
	})
	return sa
}

// BytesWritten asserts the total number of bytes written.
func (sa *statsAssertion) BytesWritten(expected int) StatsAssertion {
	sa.checks = append(sa.checks, func(t TestReporter) {
		if got := sa.stats.BytesWritten(); got != expected {
			t.Helper()
			t.Errorf("BytesWritten() = %d, want %d", got, expected)
		}
	})
	return sa
}

// Assert runs the assertions.
func (sa *statsAssertion) Assert(t TestReporter) {
	t.Helper()
	for _, check := range sa.checks {
		check(t)
	}
}
