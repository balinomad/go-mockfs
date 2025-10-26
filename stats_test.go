package mockfs_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/balinomad/go-mockfs"
)

// assertPanics is a helper function to verify that a function call panics.
func assertPanics(t *testing.T, fn func(), name string) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s did not panic as expected", name)
		}
	}()
	fn()
}

// TestNewStatsRecorder verifies that NewStatsRecorder initializes correctly.
func TestNewStatsRecorder(t *testing.T) {
	t.Run("nil initial", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		if s == nil {
			t.Fatal("returned nil")
		}

		if s.BytesRead() != 0 {
			t.Errorf("BytesRead = %d, want 0", s.BytesRead())
		}
		if s.BytesWritten() != 0 {
			t.Errorf("BytesWritten = %d, want 0", s.BytesWritten())
		}

		for op := mockfs.Operation(0); op < mockfs.NumOperations; op++ {
			if !op.IsValid() {
				continue
			}
			if s.Count(op) != 0 || s.CountFailure(op) != 0 {
				t.Errorf("%s has non-zero counts: total=%d, failure=%d",
					op, s.Count(op), s.CountFailure(op))
			}
		}
	})

	t.Run("from existing stats", func(t *testing.T) {
		initial := mockfs.NewStatsRecorder(nil)
		initial.Record(mockfs.OpRead, 100, nil)
		initial.Record(mockfs.OpWrite, 200, errors.New("fail"))
		initial.Set(mockfs.OpStat, 5, 2)

		s := mockfs.NewStatsRecorder(initial.Snapshot())

		if s.Count(mockfs.OpRead) != 1 {
			t.Errorf("OpRead count = %d, want 1", s.Count(mockfs.OpRead))
		}
		if s.Count(mockfs.OpWrite) != 1 {
			t.Errorf("OpWrite count = %d, want 1", s.Count(mockfs.OpWrite))
		}
		if s.CountFailure(mockfs.OpWrite) != 1 {
			t.Errorf("OpWrite failures = %d, want 1", s.CountFailure(mockfs.OpWrite))
		}
		if s.Count(mockfs.OpStat) != 5 {
			t.Errorf("OpStat count = %d, want 5", s.Count(mockfs.OpStat))
		}
		if s.CountFailure(mockfs.OpStat) != 2 {
			t.Errorf("OpStat failures = %d, want 2", s.CountFailure(mockfs.OpStat))
		}
		if s.BytesRead() != 100 {
			t.Errorf("BytesRead = %d, want 100", s.BytesRead())
		}
		if s.BytesWritten() != 200 {
			t.Errorf("BytesWritten = %d, want 200", s.BytesWritten())
		}
	})
}

// TestStatsRecorder_Record exercises the Record method.
func TestStatsRecorder_Record(t *testing.T) {
	testErr := errors.New("test error")

	tests := []struct {
		name        string
		op          mockfs.Operation
		bytes       int
		err         error
		wantTotal   int
		wantFailure int
		wantRead    int
		wantWritten int
	}{
		{
			name:        "read success with bytes",
			op:          mockfs.OpRead,
			bytes:       100,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    100,
			wantWritten: 0,
		},
		{
			name:        "read failure with partial bytes",
			op:          mockfs.OpRead,
			bytes:       50,
			err:         testErr,
			wantTotal:   1,
			wantFailure: 1,
			wantRead:    50,
			wantWritten: 0,
		},
		{
			name:        "read failure no bytes",
			op:          mockfs.OpRead,
			bytes:       0,
			err:         testErr,
			wantTotal:   1,
			wantFailure: 1,
			wantRead:    0,
			wantWritten: 0,
		},
		{
			name:        "write success with bytes",
			op:          mockfs.OpWrite,
			bytes:       200,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    0,
			wantWritten: 200,
		},
		{
			name:        "write failure with partial bytes",
			op:          mockfs.OpWrite,
			bytes:       75,
			err:         testErr,
			wantTotal:   1,
			wantFailure: 1,
			wantRead:    0,
			wantWritten: 75,
		},
		{
			name:        "write success no bytes",
			op:          mockfs.OpWrite,
			bytes:       0,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    0,
			wantWritten: 0,
		},
		{
			name:        "stat success",
			op:          mockfs.OpStat,
			bytes:       0,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    0,
			wantWritten: 0,
		},
		{
			name:        "stat failure",
			op:          mockfs.OpStat,
			bytes:       123, // Bytes are ignored for non-read/write ops
			err:         testErr,
			wantTotal:   1,
			wantFailure: 1,
			wantRead:    0,
			wantWritten: 0,
		},
		{
			name:        "read negative bytes ignored",
			op:          mockfs.OpRead,
			bytes:       -10,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    0,
			wantWritten: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mockfs.NewStatsRecorder(nil)
			s.Record(tt.op, tt.bytes, tt.err)

			if got := s.Count(tt.op); got != tt.wantTotal {
				t.Errorf("Count = %d, want %d", got, tt.wantTotal)
			}
			if got := s.CountFailure(tt.op); got != tt.wantFailure {
				t.Errorf("CountFailure = %d, want %d", got, tt.wantFailure)
			}
			if got := s.BytesRead(); got != tt.wantRead {
				t.Errorf("BytesRead = %d, want %d", got, tt.wantRead)
			}
			if got := s.BytesWritten(); got != tt.wantWritten {
				t.Errorf("BytesWritten = %d, want %d", got, tt.wantWritten)
			}
		})
	}

	t.Run("cumulative", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 100, nil)
		s.Record(mockfs.OpRead, 50, testErr)
		s.Record(mockfs.OpWrite, 200, nil)
		s.Record(mockfs.OpStat, 0, testErr)

		if got := s.Count(mockfs.OpRead); got != 2 {
			t.Errorf("OpRead count = %d, want 2", got)
		}
		if got := s.CountFailure(mockfs.OpRead); got != 1 {
			t.Errorf("OpRead failures = %d, want 1", got)
		}
		if got := s.BytesRead(); got != 150 {
			t.Errorf("BytesRead = %d, want 150", got)
		}
		if got := s.BytesWritten(); got != 200 {
			t.Errorf("BytesWritten = %d, want 200", got)
		}
	})
}

// TestStatsRecorder_Record_Panic tests panic on invalid operation.
func TestStatsRecorder_Record_Panic(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	assertPanics(t, func() { s.Record(mockfs.Operation(-1), 0, nil) }, "invalid op -1")
	assertPanics(t, func() { s.Record(mockfs.NumOperations, 0, nil) }, "invalid op NumOperations")
}

// TestStatsRecorder_Set verifies the Set method.
func TestStatsRecorder_Set(t *testing.T) {
	t.Run("valid set", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Set(mockfs.OpStat, 10, 4)

		if s.Count(mockfs.OpStat) != 10 {
			t.Errorf("Count = %d, want 10", s.Count(mockfs.OpStat))
		}
		if s.CountFailure(mockfs.OpStat) != 4 {
			t.Errorf("CountFailure = %d, want 4", s.CountFailure(mockfs.OpStat))
		}
		if s.CountSuccess(mockfs.OpStat) != 6 {
			t.Errorf("CountSuccess = %d, want 6", s.CountSuccess(mockfs.OpStat))
		}

		// Set to zero
		s.Set(mockfs.OpStat, 0, 0)
		if s.Count(mockfs.OpStat) != 0 {
			t.Errorf("after reset Count = %d, want 0", s.Count(mockfs.OpStat))
		}
	})

	t.Run("panics", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		assertPanics(t, func() { s.Set(mockfs.Operation(-1), 1, 0) }, "invalid op")
		assertPanics(t, func() { s.Set(mockfs.NumOperations, 1, 0) }, "out of range op")
		assertPanics(t, func() { s.Set(mockfs.OpStat, 5, -1) }, "negative failures")
		assertPanics(t, func() { s.Set(mockfs.OpStat, 5, 6) }, "failures > total")
	})
}

// TestStatsRecorder_SetBytes verifies the SetBytes method.
func TestStatsRecorder_SetBytes(t *testing.T) {
	t.Run("valid values", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)

		s.SetBytes(123, 456)
		if s.BytesRead() != 123 {
			t.Errorf("BytesRead = %d, want 123", s.BytesRead())
		}
		if s.BytesWritten() != 456 {
			t.Errorf("BytesWritten = %d, want 456", s.BytesWritten())
		}

		s.SetBytes(0, 0)
		if s.BytesRead() != 0 || s.BytesWritten() != 0 {
			t.Error("SetBytes(0, 0) did not reset")
		}
	})
	t.Run("panics on negative", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		assertPanics(t, func() { s.SetBytes(-1, 0) }, "negative read")
		assertPanics(t, func() { s.SetBytes(0, -1) }, "negative written")
		assertPanics(t, func() { s.SetBytes(-1, -1) }, "both negative")
	})
}

// TestStatsRecorder_Reset verifies that Reset clears all counters.
func TestStatsRecorder_Reset(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	s.Record(mockfs.OpRead, 100, nil)
	s.Record(mockfs.OpWrite, 200, errors.New("fail"))
	s.Set(mockfs.OpStat, 10, 2)

	s.Reset()

	empty := mockfs.NewStatsRecorder(nil)
	if !s.Snapshot().Equal(empty.Snapshot()) {
		t.Error("Reset did not clear all counters")
	}
}

// TestStatsRecorder_Snapshot verifies snapshot independence.
func TestStatsRecorder_Snapshot(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	s.Record(mockfs.OpRead, 100, nil)

	snap1 := s.Snapshot()
	s.Record(mockfs.OpRead, 50, nil)
	snap2 := s.Snapshot()

	if snap1.Count(mockfs.OpRead) != 1 {
		t.Error("snap1 was modified after new recording")
	}
	if snap2.Count(mockfs.OpRead) != 2 {
		t.Error("snap2 does not reflect new recording")
	}
}

// TestStats_Count verifies Count methods with valid and invalid operations.
func TestStats_Count(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	s.Set(mockfs.OpStat, 10, 3)

	if s.Count(mockfs.OpStat) != 10 {
		t.Errorf("Count = %d, want 10", s.Count(mockfs.OpStat))
	}
	if s.CountSuccess(mockfs.OpStat) != 7 {
		t.Errorf("CountSuccess = %d, want 7", s.CountSuccess(mockfs.OpStat))
	}
	if s.CountFailure(mockfs.OpStat) != 3 {
		t.Errorf("CountFailure = %d, want 3", s.CountFailure(mockfs.OpStat))
	}

	assertPanics(t, func() { s.Count(mockfs.Operation(-1)) }, "Count invalid op")
	assertPanics(t, func() { s.CountSuccess(mockfs.Operation(-1)) }, "CountSuccess invalid op")
	assertPanics(t, func() { s.CountFailure(mockfs.Operation(-1)) }, "CountFailure invalid op")
}

// TestStats_HasFailures verifies failure detection.
func TestStats_HasFailures(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	if s.HasFailures() {
		t.Error("empty stats HasFailures = true")
	}

	s.Record(mockfs.OpRead, 0, nil)
	if s.HasFailures() {
		t.Error("success only HasFailures = true")
	}

	s.Record(mockfs.OpWrite, 0, errors.New("fail"))
	if !s.HasFailures() {
		t.Error("with failure HasFailures = false")
	}
}

// TestStats_Operations verifies total operation count.
func TestStats_Operations(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	if s.Operations() != 0 {
		t.Error("empty stats Operations != 0")
	}

	s.Record(mockfs.OpRead, 0, nil)
	s.Record(mockfs.OpWrite, 0, nil)
	s.Set(mockfs.OpStat, 5, 0)

	if s.Operations() != 7 {
		t.Errorf("Operations = %d, want 7", s.Operations())
	}
}

// TestStats_Failures verifies failed operations list.
func TestStats_Failures(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	if len(s.Failures()) != 0 {
		t.Error("empty stats has failures")
	}

	s.Set(mockfs.OpOpen, 5, 2)
	s.Set(mockfs.OpWrite, 3, 1)
	s.Set(mockfs.OpRead, 10, 0)

	failed := s.Failures()
	if len(failed) != 2 {
		t.Fatalf("Failures len = %d, want 2", len(failed))
	}

	hasOp := func(ops []mockfs.Operation, op mockfs.Operation) bool {
		for _, o := range ops {
			if o == op {
				return true
			}
		}
		return false
	}

	if !hasOp(failed, mockfs.OpOpen) {
		t.Error("OpOpen not in failures")
	}
	if !hasOp(failed, mockfs.OpWrite) {
		t.Error("OpWrite not in failures")
	}
	if hasOp(failed, mockfs.OpRead) {
		t.Error("OpRead in failures but has none")
	}
}

// TestStats_Delta verifies delta calculation including negative values.
func TestStats_Delta(t *testing.T) {
	s1 := mockfs.NewStatsRecorder(nil)
	s1.Record(mockfs.OpRead, 100, nil)
	s1.Set(mockfs.OpStat, 10, 2)

	s2 := mockfs.NewStatsRecorder(nil)
	s2.Record(mockfs.OpRead, 150, nil)
	s2.Record(mockfs.OpRead, 0, errors.New("fail"))
	s2.Set(mockfs.OpStat, 15, 3)

	delta := s2.Snapshot().Delta(s1.Snapshot())

	if delta.Count(mockfs.OpRead) != 1 {
		t.Errorf("delta OpRead count = %d, want 1", delta.Count(mockfs.OpRead))
	}
	if delta.CountFailure(mockfs.OpRead) != 1 {
		t.Errorf("delta OpRead failures = %d, want 1", delta.CountFailure(mockfs.OpRead))
	}
	if delta.Count(mockfs.OpStat) != 5 {
		t.Errorf("delta OpStat count = %d, want 5", delta.Count(mockfs.OpStat))
	}
	if delta.BytesRead() != 50 {
		t.Errorf("delta BytesRead = %d, want 50", delta.BytesRead())
	}

	t.Run("negative delta after reset", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 100, nil)
		before := s.Snapshot()

		s.Reset()
		after := s.Snapshot()

		delta := after.Delta(before)
		if delta.Count(mockfs.OpRead) != -1 {
			t.Errorf("delta count = %d, want -1", delta.Count(mockfs.OpRead))
		}
		if delta.BytesRead() != -100 {
			t.Errorf("delta BytesRead = %d, want -100", delta.BytesRead())
		}
	})
}

// TestStats_Equal verifies equality comparison.
func TestStats_Equal(t *testing.T) {
	s1 := mockfs.NewStatsRecorder(nil)
	s1.Record(mockfs.OpRead, 100, nil)
	s1.Set(mockfs.OpStat, 5, 1)

	s2 := mockfs.NewStatsRecorder(nil)
	s2.Record(mockfs.OpRead, 100, nil)
	s2.Set(mockfs.OpStat, 5, 1)

	if !s1.Snapshot().Equal(s2.Snapshot()) {
		t.Error("identical stats not equal")
	}

	s2.Record(mockfs.OpWrite, 1, nil)
	if s1.Snapshot().Equal(s2.Snapshot()) {
		t.Error("different stats reported equal")
	}

	empty1 := mockfs.NewStatsRecorder(nil)
	empty2 := mockfs.NewStatsRecorder(nil)
	if !empty1.Snapshot().Equal(empty2.Snapshot()) {
		t.Error("empty stats not equal")
	}
}

// TestStats_String verifies human-readable output.
func TestStats_String(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	s.Record(mockfs.OpRead, 100, nil)
	s.Record(mockfs.OpWrite, 200, errors.New("fail"))
	s.Set(mockfs.OpStat, 10, 2)

	str := s.String()
	if str == "" {
		t.Error("String() returned empty")
	}

	// Should contain operation count and failure count
	if !contains(str, "12") { // Total ops
		t.Error("String() missing total operations")
	}
	if !contains(str, "3") { // Total failures
		t.Error("String() missing failure count")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

// TestStats_Concurrent tests concurrent access to stats.
func TestStats_Concurrent(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	var wg sync.WaitGroup

	// Concurrent writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				op := mockfs.Operation((j % (int(mockfs.NumOperations) - 2)) + 1)
				var err error
				if j%10 == 0 {
					err = errors.New("fail")
				}
				s.Record(op, 1, err)

				if j%20 == 0 {
					s.Set(mockfs.OpStat, 5, 1)
				}
				if j%100 == 0 {
					s.Reset()
				}
			}
		}()
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = s.Snapshot()
				_ = s.Count(mockfs.OpRead)
				_ = s.CountSuccess(mockfs.OpWrite)
				_ = s.CountFailure(mockfs.OpStat)
				_ = s.BytesRead()
				_ = s.BytesWritten()
				_ = s.HasFailures()
				_ = s.Operations()
				_ = s.Failures()
			}
		}()
	}

	wg.Wait()
}

// TestStats_SnapshotImmutability verifies snapshots are immutable values.
func TestStats_SnapshotImmutability(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	s.Record(mockfs.OpRead, 100, nil)

	snap := s.Snapshot()
	before := snap.Count(mockfs.OpRead)

	s.Record(mockfs.OpRead, 50, nil)

	after := snap.Count(mockfs.OpRead)
	if before != after {
		t.Error("snapshot was mutated after recording")
	}
}

// TestStatsRecorder_ByteCounters verifies byte counting for read/write operations.
func TestStatsRecorder_ByteCounters(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)

	s.Record(mockfs.OpRead, 100, nil)
	s.Record(mockfs.OpWrite, 200, nil)
	s.Record(mockfs.OpRead, 50, errors.New("fail"))
	s.Record(mockfs.OpStat, 1000, nil) // Should be ignored
	s.Record(mockfs.OpWrite, 25, errors.New("fail"))

	if s.BytesRead() != 150 {
		t.Errorf("BytesRead = %d, want 150", s.BytesRead())
	}
	if s.BytesWritten() != 225 {
		t.Errorf("BytesWritten = %d, want 225", s.BytesWritten())
	}
}

// Benchmark tests
func BenchmarkStatsRecorder_Record(b *testing.B) {
	s := mockfs.NewStatsRecorder(nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Record(mockfs.OpRead, 100, nil)
	}
}

func BenchmarkStatsRecorder_Snapshot(b *testing.B) {
	s := mockfs.NewStatsRecorder(nil)
	s.Record(mockfs.OpRead, 100, nil)
	s.Record(mockfs.OpWrite, 200, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.Snapshot()
	}
}

func BenchmarkStats_Count(b *testing.B) {
	s := mockfs.NewStatsRecorder(nil)
	s.Record(mockfs.OpRead, 100, nil)
	snap := s.Snapshot()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = snap.Count(mockfs.OpRead)
	}
}

func BenchmarkStats_Delta(b *testing.B) {
	s1 := mockfs.NewStatsRecorder(nil)
	s1.Record(mockfs.OpRead, 100, nil)
	snap1 := s1.Snapshot()

	s2 := mockfs.NewStatsRecorder(nil)
	s2.Record(mockfs.OpRead, 150, nil)
	snap2 := s2.Snapshot()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = snap2.Delta(snap1)
	}
}
