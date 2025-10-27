package mockfs_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/balinomad/go-mockfs"
)

func TestNewStatsRecorder_NilInitial(t *testing.T) {
	t.Parallel()
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
}

func TestNewStatsRecorder_FromExisting(t *testing.T) {
	t.Parallel()
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
}

// TestStatsRecorder_Record_Panic tests panic on invalid operation.
func TestStatsRecorder_Record_Panic(t *testing.T) {
	s := mockfs.NewStatsRecorder(nil)
	assertPanic(t, func() { s.Record(mockfs.Operation(-1), 0, nil) }, "invalid op -1")
	assertPanic(t, func() { s.Record(mockfs.NumOperations, 0, nil) }, "invalid op NumOperations")
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
		assertPanic(t, func() { s.Set(mockfs.Operation(-1), 1, 0) }, "invalid op")
		assertPanic(t, func() { s.Set(mockfs.NumOperations, 1, 0) }, "out of range op")
		assertPanic(t, func() { s.Set(mockfs.OpStat, 5, -1) }, "negative failures")
		assertPanic(t, func() { s.Set(mockfs.OpStat, 5, 6) }, "failures > total")
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
		assertPanic(t, func() { s.SetBytes(-1, 0) }, "negative read")
		assertPanic(t, func() { s.SetBytes(0, -1) }, "negative written")
		assertPanic(t, func() { s.SetBytes(-1, -1) }, "both negative")
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

// TestStatsRecorder_Delta verifies that statsRecorder.Delta correctly delegates to Snapshot().Delta
// and computes differences between various StatsRecorder and Stats combinations.
func TestStatsRecorder_Delta(t *testing.T) {
	t.Parallel()

	makeRec := func(readBytes, writeBytes int, ops map[mockfs.Operation][2]int) mockfs.StatsRecorder {
		r := mockfs.NewStatsRecorder(nil)
		r.SetBytes(readBytes, writeBytes)
		for op, vals := range ops {
			r.Set(op, vals[0], vals[1])
		}
		return r
	}

	t.Run("same", func(t *testing.T) {
		t.Parallel()
		left := makeRec(100, 200, map[mockfs.Operation][2]int{
			mockfs.OpRead:  {1, 0},
			mockfs.OpWrite: {2, 1},
		})
		right := makeRec(100, 200, map[mockfs.Operation][2]int{
			mockfs.OpRead:  {1, 0},
			mockfs.OpWrite: {2, 1},
		})
		delta := left.Delta(right)
		if delta.Operations() != 0 || delta.BytesRead() != 0 || delta.BytesWritten() != 0 {
			t.Fatalf("expected no delta, got ops=%d bytesRead=%d bytesWritten=%d",
				delta.Operations(), delta.BytesRead(), delta.BytesWritten())
		}
	})

	t.Run("increased", func(t *testing.T) {
		t.Parallel()
		left := makeRec(500, 1000, map[mockfs.Operation][2]int{
			mockfs.OpRead: {5, 1},
			mockfs.OpStat: {10, 2},
		})
		right := makeRec(200, 400, map[mockfs.Operation][2]int{
			mockfs.OpRead: {4, 0},
			mockfs.OpStat: {7, 1},
		}).Snapshot()

		delta := left.Delta(right)
		if delta.Count(mockfs.OpRead) != 1 ||
			delta.CountFailure(mockfs.OpRead) != 1 ||
			delta.Count(mockfs.OpStat) != 3 ||
			delta.CountFailure(mockfs.OpStat) != 1 ||
			delta.BytesRead() != 300 ||
			delta.BytesWritten() != 600 {
			t.Fatalf("unexpected delta: %+v", delta)
		}
	})

	t.Run("negative-after-reset", func(t *testing.T) {
		t.Parallel()
		left := makeRec(1000, 2000, map[mockfs.Operation][2]int{
			mockfs.OpRead: {10, 2},
		})
		right := left.Snapshot()

		// Reset left so Delta should produce negative differences
		left.Reset()
		delta := left.Delta(right)

		if !(delta.Count(mockfs.OpRead) < 0 && delta.BytesRead() < 0) {
			t.Fatalf("expected negative delta, got ops=%d bytesRead=%d",
				delta.Count(mockfs.OpRead), delta.BytesRead())
		}
	})
}

// TestStatsRecorder_Equal verifies statsRecorder.Equal delegates to Snapshot().Equal
// and correctly reports equality with both StatsRecorder and Stats (snapshot) arguments.
func TestStatsRecorder_Equal(t *testing.T) {
	t.Parallel()

	base := mockfs.NewStatsRecorder(nil)
	base.Record(mockfs.OpRead, 100, nil)
	base.Set(mockfs.OpStat, 5, 1)

	identical := mockfs.NewStatsRecorder(nil)
	identical.Record(mockfs.OpRead, 100, nil)
	identical.Set(mockfs.OpStat, 5, 1)

	different := mockfs.NewStatsRecorder(nil)
	different.Record(mockfs.OpRead, 100, nil)
	different.Record(mockfs.OpWrite, 1, nil) // extra op makes it different

	tests := []struct {
		name string
		a    mockfs.StatsRecorder
		b    mockfs.Stats
		want bool
	}{
		{"equal-recorder", base, identical.Snapshot(), true}, // compare recorder to snapshot
		{"equal-snapshot", base, identical, true},            // compare recorder to recorder
		{"different", base, different.Snapshot(), false},     // different snapshot
		{"different-recorder", base, different, false},       // different recorder
		{"equal-empty", mockfs.NewStatsRecorder(nil), mockfs.NewStatsRecorder(nil), true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.a.Equal(tt.b)
			if got != tt.want {
				t.Fatalf("Equal() = %v, want %v (test %q)", got, tt.want, tt.name)
			}
		})
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

	assertPanic(t, func() { s.Count(mockfs.Operation(-1)) }, "Count invalid op")
	assertPanic(t, func() { s.CountSuccess(mockfs.Operation(-1)) }, "CountSuccess invalid op")
	assertPanic(t, func() { s.CountFailure(mockfs.Operation(-1)) }, "CountFailure invalid op")
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

	t.Run("delta between empty snapshots", func(t *testing.T) {
		s1 := mockfs.NewStatsRecorder(nil)
		s2 := mockfs.NewStatsRecorder(nil)
		delta := s2.Snapshot().Delta(s1.Snapshot())

		if delta.Operations() != 0 {
			t.Errorf("delta Operations = %d, want 0", delta.Operations())
		}
		if delta.BytesRead() != 0 || delta.BytesWritten() != 0 {
			t.Error("delta bytes should be zero")
		}
	})
}

// TestStats_Equal verifies equality comparison.
func TestStats_Equal(t *testing.T) {
	t.Parallel()

	newBase := func() mockfs.StatsRecorder {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 100, nil)
		s.Set(mockfs.OpStat, 5, 1)
		return s
	}

	t.Run("identical", func(t *testing.T) {
		t.Parallel()
		a := newBase()
		b := newBase()
		if !a.Snapshot().Equal(b.Snapshot()) {
			t.Error("identical stats not equal")
		}
	})

	t.Run("extra-on-right", func(t *testing.T) {
		t.Parallel()
		a := newBase()
		b := newBase()
		b.Record(mockfs.OpWrite, 1, nil) // extra op on right
		if a.Snapshot().Equal(b.Snapshot()) {
			t.Error("different stats (extra op on right) reported equal")
		}
	})

	t.Run("extra-on-left", func(t *testing.T) {
		t.Parallel()
		// left has an op that right lacks; this triggers the comparison of
		// s.ops[i].total vs other.Count(op) / s.ops[i].failure vs other.CountFailure(op).
		left := newBase()
		left.Record(mockfs.OpWrite, 1, nil) // extra op on left
		right := newBase()
		if left.Snapshot().Equal(right.Snapshot()) {
			t.Error("stats with extra op on left reported equal")
		}
	})

	t.Run("different-failure-count", func(t *testing.T) {
		t.Parallel()
		a := newBase()
		b := mockfs.NewStatsRecorder(nil)
		b.Record(mockfs.OpRead, 100, nil)
		b.Set(mockfs.OpStat, 5, 2) // same totals but different failure count
		if a.Snapshot().Equal(b.Snapshot()) {
			t.Error("stats with differing failure count reported equal")
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		empty1 := mockfs.NewStatsRecorder(nil)
		empty2 := mockfs.NewStatsRecorder(nil)
		if !empty1.Snapshot().Equal(empty2.Snapshot()) {
			t.Error("empty stats not equal")
		}
	})
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
	if !strings.Contains(str, "12") { // Total ops
		t.Error("String() missing total operations")
	}
	if !strings.Contains(str, "3") { // Total failures
		t.Error("String() missing failure count")
	}
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
			performConcurrentWrites(s)
		}()
	}

	// Concurrent readers (unchanged)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = s.Snapshot()
				_ = s.Count(mockfs.OpRead)
				_ = s.HasFailures()
			}
		}()
	}

	wg.Wait()
}

func performConcurrentWrites(s mockfs.StatsRecorder) {
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
func TestStats_SnapshotMethods(t *testing.T) {
	t.Parallel()

	t.Run("CountSuccess on snapshot", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Set(mockfs.OpRead, 10, 3)
		snap := s.Snapshot()
		if snap.CountSuccess(mockfs.OpRead) != 7 {
			t.Errorf("CountSuccess = %d, want 7", snap.CountSuccess(mockfs.OpRead))
		}
	})

	t.Run("HasFailures on snapshot", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		snap1 := s.Snapshot()
		if snap1.HasFailures() {
			t.Error("empty snapshot HasFailures = true")
		}

		s.Record(mockfs.OpRead, 0, errors.New("fail"))
		snap2 := s.Snapshot()
		if !snap2.HasFailures() {
			t.Error("snapshot with failures HasFailures = false")
		}
	})

	t.Run("Failures on snapshot", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Set(mockfs.OpOpen, 5, 2)
		s.Set(mockfs.OpRead, 10, 1)
		snap := s.Snapshot()

		failed := snap.Failures()
		if len(failed) != 2 {
			t.Errorf("Failures len = %d, want 2", len(failed))
		}
	})

	t.Run("Delta on snapshot", func(t *testing.T) {
		s1 := mockfs.NewStatsRecorder(nil)
		s1.Record(mockfs.OpRead, 100, nil)
		snap1 := s1.Snapshot()

		s2 := mockfs.NewStatsRecorder(nil)
		s2.Record(mockfs.OpRead, 150, nil)
		snap2 := s2.Snapshot()

		delta := snap2.Delta(snap1)
		if delta.BytesRead() != 50 {
			t.Errorf("delta BytesRead = %d, want 50", delta.BytesRead())
		}
	})

	t.Run("Equal on snapshot", func(t *testing.T) {
		s1 := mockfs.NewStatsRecorder(nil)
		s1.Record(mockfs.OpRead, 100, nil)
		snap1 := s1.Snapshot()

		s2 := mockfs.NewStatsRecorder(nil)
		s2.Record(mockfs.OpRead, 100, nil)
		snap2 := s2.Snapshot()

		if !snap1.Equal(snap2) {
			t.Error("equal snapshots reported not equal")
		}

		s2.Record(mockfs.OpWrite, 1, nil)
		snap3 := s2.Snapshot()
		if snap1.Equal(snap3) {
			t.Error("different snapshots reported equal")
		}
	})

	t.Run("Count panic on snapshot", func(t *testing.T) {
		snap := mockfs.NewStatsRecorder(nil).Snapshot()
		assertPanic(t, func() { snap.Count(mockfs.Operation(-1)) }, "Count invalid op")
	})

	t.Run("CountSuccess panic on snapshot", func(t *testing.T) {
		snap := mockfs.NewStatsRecorder(nil).Snapshot()
		assertPanic(t, func() { snap.CountSuccess(mockfs.NumOperations) }, "CountSuccess invalid op")
	})

	t.Run("CountFailure panic on snapshot", func(t *testing.T) {
		snap := mockfs.NewStatsRecorder(nil).Snapshot()
		assertPanic(t, func() { snap.CountFailure(mockfs.Operation(-1)) }, "CountFailure invalid op")
	})
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
