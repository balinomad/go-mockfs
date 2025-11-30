package mockfs_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/balinomad/go-mockfs/v2"
)

// --- Helpers ---

// assertOpCount verifies operation count and failures.
func assertOpCount(t *testing.T, s mockfs.Stats, op mockfs.Operation, wantTotal int, wantFailures int) {
	t.Helper()
	if got := s.Count(op); got != wantTotal {
		t.Errorf("%s count = %d, want %d", op, got, wantTotal)
	}
	if got := s.CountFailure(op); got != wantFailures {
		t.Errorf("%s failures = %d, want %d", op, got, wantFailures)
	}
	if got := s.CountSuccess(op); got != wantTotal-wantFailures {
		t.Errorf("%s successes = %d, want %d", op, got, wantTotal-wantFailures)
	}
}

func assertBytes(t *testing.T, s mockfs.Stats, wantRead int, wantWritten int) {
	t.Helper()
	if got := s.BytesRead(); got != wantRead {
		t.Errorf("bytes read = %d, want %d", got, wantRead)
	}
	if got := s.BytesWritten(); got != wantWritten {
		t.Errorf("bytes written = %d, want %d", got, wantWritten)
	}
}

// mockReporter implements TestReporter for testing assertions.
type mockReporter struct {
	errors []string
}

func (m *mockReporter) Errorf(format string, args ...any) {
	m.errors = append(m.errors, fmt.Sprintf(format, args...))
}

func (m *mockReporter) Helper() {}

// --- Tests ---

// TestNewStatsRecorder verifies recorder initialization.
func TestNewStatsRecorder(t *testing.T) {
	t.Parallel()

	t.Run("nil initial", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		if s == nil {
			t.Fatal("returned nil")
		}
		assertBytes(t, s, 0, 0)
		for op := mockfs.Operation(0); op < mockfs.NumOperations; op++ {
			if !op.IsValid() {
				continue
			}
			assertOpCount(t, s, op, 0, 0)
		}
	})

	t.Run("from existing", func(t *testing.T) {
		initial := mockfs.NewStatsRecorder(nil)
		initial.Record(mockfs.OpRead, 100, nil)
		initial.Record(mockfs.OpWrite, 200, errors.New("fail"))
		initial.Set(mockfs.OpStat, 5, 2)

		s := mockfs.NewStatsRecorder(initial.Snapshot())
		assertOpCount(t, s, mockfs.OpRead, 1, 0)
		assertOpCount(t, s, mockfs.OpWrite, 1, 1)
		assertOpCount(t, s, mockfs.OpStat, 5, 2)
		assertBytes(t, s, 100, 200)
	})
}

// TestStatsRecorder_Panics verifies panic conditions for mutation methods.
func TestStatsRecorder_Panics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		fn   func(mockfs.StatsRecorder)
	}{
		{"record invalid op -1", func(s mockfs.StatsRecorder) { s.Record(mockfs.Operation(-1), 0, nil) }},
		{"record invalid op NumOps", func(s mockfs.StatsRecorder) { s.Record(mockfs.NumOperations, 0, nil) }},
		{"set invalid op -1", func(s mockfs.StatsRecorder) { s.Set(mockfs.Operation(-1), 1, 0) }},
		{"set invalid op NumOps", func(s mockfs.StatsRecorder) { s.Set(mockfs.NumOperations, 1, 0) }},
		{"set negative failures", func(s mockfs.StatsRecorder) { s.Set(mockfs.OpStat, 5, -1) }},
		{"set failures exceed total", func(s mockfs.StatsRecorder) { s.Set(mockfs.OpStat, 5, 6) }},
		{"setbytes negative read", func(s mockfs.StatsRecorder) { s.SetBytes(-1, 0) }},
		{"setbytes negative written", func(s mockfs.StatsRecorder) { s.SetBytes(0, -1) }},
		{"setbytes both negative", func(s mockfs.StatsRecorder) { s.SetBytes(-1, -1) }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := mockfs.NewStatsRecorder(nil)
			assertPanic(t, func() { tt.fn(s) }, tt.name)
		})
	}
}

// TestStatsRecorder_Set verifies Set method.
func TestStatsRecorder_Set(t *testing.T) {
	t.Parallel()

	s := mockfs.NewStatsRecorder(nil)
	s.Set(mockfs.OpStat, 10, 4)
	assertOpCount(t, s, mockfs.OpStat, 10, 4)

	s.Set(mockfs.OpStat, 0, 0)
	if s.Count(mockfs.OpStat) != 0 {
		t.Errorf("after reset Count = %d, want 0", s.Count(mockfs.OpStat))
	}
}

// TestStatsRecorder_SetBytes verifies SetBytes method.
func TestStatsRecorder_SetBytes(t *testing.T) {
	t.Parallel()

	s := mockfs.NewStatsRecorder(nil)
	s.SetBytes(123, 456)
	assertBytes(t, s, 123, 456)

	s.SetBytes(0, 0)
	if s.BytesRead() != 0 || s.BytesWritten() != 0 {
		t.Error("SetBytes(0, 0) did not reset")
	}
}

// TestStatsRecorder_Reset verifies Reset clears all counters.
func TestStatsRecorder_Reset(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

// TestStatsRecorder_Delta verifies Delta computation.
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

	tests := []struct {
		name             string
		left             mockfs.StatsRecorder
		right            mockfs.Stats
		wantOps          int
		wantBytesRead    int
		wantBytesWritten int
		wantReadCount    int
		wantReadFailures int
		wantStatCount    int
		wantStatFailures int
		expectNegative   bool
	}{
		{
			name: "same",
			left: makeRec(100, 200, map[mockfs.Operation][2]int{
				mockfs.OpRead:  {1, 0},
				mockfs.OpWrite: {2, 1},
			}),
			right: makeRec(100, 200, map[mockfs.Operation][2]int{
				mockfs.OpRead:  {1, 0},
				mockfs.OpWrite: {2, 1},
			}),
			wantOps:          0,
			wantBytesRead:    0,
			wantBytesWritten: 0,
		},
		{
			name: "increased",
			left: makeRec(500, 1000, map[mockfs.Operation][2]int{
				mockfs.OpRead: {5, 1},
				mockfs.OpStat: {10, 2},
			}),
			right: makeRec(200, 400, map[mockfs.Operation][2]int{
				mockfs.OpRead: {4, 0},
				mockfs.OpStat: {7, 1},
			}).Snapshot(),
			wantReadCount:    1,
			wantReadFailures: 1,
			wantStatCount:    3,
			wantStatFailures: 1,
			wantBytesRead:    300,
			wantBytesWritten: 600,
		},
		{
			name: "negative after reset",
			left: func() mockfs.StatsRecorder {
				r := makeRec(1000, 2000, map[mockfs.Operation][2]int{mockfs.OpRead: {10, 2}})
				r.Reset()
				return r
			}(),
			right: makeRec(1000, 2000, map[mockfs.Operation][2]int{
				mockfs.OpRead: {10, 2},
			}).Snapshot(),
			expectNegative: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delta := tt.left.Delta(tt.right)

			if tt.expectNegative {
				if delta.Count(mockfs.OpRead) >= 0 || delta.BytesRead() >= 0 {
					t.Errorf("expected negative delta, got ops=%d bytesRead=%d",
						delta.Count(mockfs.OpRead), delta.BytesRead())
				}
				return
			}

			if tt.name == "same" {
				if delta.Operations() != 0 || delta.BytesRead() != 0 || delta.BytesWritten() != 0 {
					t.Errorf("expected no delta, got ops=%d bytesRead=%d bytesWritten=%d",
						delta.Operations(), delta.BytesRead(), delta.BytesWritten())
				}
				return
			}

			if tt.name == "increased" {
				assertOpCount(t, delta, mockfs.OpRead, tt.wantReadCount, tt.wantReadFailures)
				assertOpCount(t, delta, mockfs.OpStat, tt.wantStatCount, tt.wantStatFailures)
				assertBytes(t, delta, tt.wantBytesRead, tt.wantBytesWritten)
			}
		})
	}
}

// TestStatsRecorder_Equal verifies Equal comparison.
func TestStatsRecorder_Equal(t *testing.T) {
	t.Parallel()

	base := func() mockfs.StatsRecorder {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 100, nil)
		s.Set(mockfs.OpStat, 5, 1)
		return s
	}

	tests := []struct {
		name string
		a    mockfs.StatsRecorder
		b    mockfs.Stats
		want bool
	}{
		{
			name: "equal recorder",
			a:    base(),
			b:    base().Snapshot(),
			want: true,
		},
		{
			name: "equal snapshot",
			a:    base(),
			b:    base(),
			want: true,
		},
		{
			name: "different op",
			a:    base(),
			b: func() mockfs.Stats {
				s := base()
				s.Record(mockfs.OpWrite, 1, nil)
				return s.Snapshot()
			}(),
			want: false,
		},
		{
			name: "different failures",
			a:    base(),
			b: func() mockfs.Stats {
				s := mockfs.NewStatsRecorder(nil)
				s.Record(mockfs.OpRead, 100, nil)
				s.Set(mockfs.OpStat, 5, 2)
				return s.Snapshot()
			}(),
			want: false,
		},
		{
			name: "equal empty",
			a:    mockfs.NewStatsRecorder(nil),
			b:    mockfs.NewStatsRecorder(nil),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestStats_Count verifies Count methods with valid and invalid operations.
func TestStats_Count(t *testing.T) {
	t.Parallel()

	s := mockfs.NewStatsRecorder(nil)
	s.Set(mockfs.OpStat, 10, 3)
	assertOpCount(t, s, mockfs.OpStat, 10, 3)

	assertPanic(t, func() { s.Count(mockfs.Operation(-1)) }, "Count invalid op")
	assertPanic(t, func() { s.CountSuccess(mockfs.Operation(-1)) }, "CountSuccess invalid op")
	assertPanic(t, func() { s.CountFailure(mockfs.Operation(-1)) }, "CountFailure invalid op")
}

// TestStats_HasFailures verifies failure detection.
func TestStats_HasFailures(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

// TestStats_FailedOperations verifies failed operations list.
func TestStats_FailedOperations(t *testing.T) {
	t.Parallel()

	s := mockfs.NewStatsRecorder(nil)
	if len(s.FailedOperations()) != 0 {
		t.Error("empty stats has failures")
	}

	s.Set(mockfs.OpOpen, 5, 2)
	s.Set(mockfs.OpWrite, 3, 1)
	s.Set(mockfs.OpRead, 10, 0)

	failed := s.FailedOperations()
	if len(failed) != 2 {
		t.Fatalf("FailedOperations len = %d, want 2", len(failed))
	}

	hasOp := func(ops []mockfs.Operation, op mockfs.Operation) bool {
		for _, o := range ops {
			if o == op {
				return true
			}
		}
		return false
	}

	if !hasOp(failed, mockfs.OpOpen) || !hasOp(failed, mockfs.OpWrite) {
		t.Error("missing expected failed operations")
	}
	if hasOp(failed, mockfs.OpRead) {
		t.Error("OpRead in failures but has none")
	}
}

// TestStats_Delta verifies delta calculation including negative values.
func TestStats_Delta(t *testing.T) {
	t.Parallel()

	s1 := mockfs.NewStatsRecorder(nil)
	s1.Record(mockfs.OpRead, 100, nil)
	s1.Set(mockfs.OpStat, 10, 2)

	s2 := mockfs.NewStatsRecorder(nil)
	s2.Record(mockfs.OpRead, 150, nil)
	s2.Record(mockfs.OpRead, 0, errors.New("fail"))
	s2.Set(mockfs.OpStat, 15, 3)

	delta := s2.Snapshot().Delta(s1.Snapshot())

	assertOpCount(t, delta, mockfs.OpRead, 1, 1)
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
			t.Errorf("delta bytes read = %d, want -100", delta.BytesRead())
		}
	})

	t.Run("delta between empty snapshots", func(t *testing.T) {
		s1 := mockfs.NewStatsRecorder(nil)
		s2 := mockfs.NewStatsRecorder(nil)
		delta := s2.Snapshot().Delta(s1.Snapshot())

		if delta.Operations() != 0 || delta.BytesRead() != 0 || delta.BytesWritten() != 0 {
			t.Error("delta of empty should be zero")
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

	tests := []struct {
		name string
		a    func() mockfs.StatsRecorder
		b    func() mockfs.Stats
		want bool
	}{
		{
			name: "identical",
			a:    newBase,
			b:    func() mockfs.Stats { return newBase().Snapshot() },
			want: true,
		},
		{
			name: "extra on right",
			a:    newBase,
			b: func() mockfs.Stats {
				b := newBase()
				b.Record(mockfs.OpWrite, 1, nil)
				return b.Snapshot()
			},
			want: false,
		},
		{
			name: "extra on left",
			a: func() mockfs.StatsRecorder {
				a := newBase()
				a.Record(mockfs.OpWrite, 1, nil)
				return a
			},
			b:    func() mockfs.Stats { return newBase().Snapshot() },
			want: false,
		},
		{
			name: "different failures",
			a:    newBase,
			b: func() mockfs.Stats {
				b := mockfs.NewStatsRecorder(nil)
				b.Record(mockfs.OpRead, 100, nil)
				b.Set(mockfs.OpStat, 5, 2)
				return b.Snapshot()
			},
			want: false,
		},
		{
			name: "empty",
			a:    func() mockfs.StatsRecorder { return mockfs.NewStatsRecorder(nil) },
			b:    func() mockfs.Stats { return mockfs.NewStatsRecorder(nil).Snapshot() },
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a().Snapshot().Equal(tt.b()); got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestStats_String verifies human-readable output.
func TestStats_String(t *testing.T) {
	t.Parallel()

	s := mockfs.NewStatsRecorder(nil)
	s.Record(mockfs.OpRead, 100, nil)
	s.Record(mockfs.OpWrite, 200, errors.New("fail"))
	s.Set(mockfs.OpStat, 10, 2)

	str := s.String()
	if str == "" {
		t.Error("String() returned empty")
	}

	if !strings.Contains(str, "12") {
		t.Error("String() missing total operations")
	}
	if !strings.Contains(str, "3") {
		t.Error("String() missing failure count")
	}
}

// TestStats_Empty verifies Empty() detection.
func TestStats_Empty(t *testing.T) {
	t.Parallel()

	t.Run("recorder empty", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		if !s.Empty() {
			t.Error("new recorder should be empty")
		}

		s.Record(mockfs.OpRead, 0, nil)
		if s.Empty() {
			t.Error("recorder with operations should not be empty")
		}
	})

	t.Run("snapshot empty", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		snap := s.Snapshot()
		if !snap.Empty() {
			t.Error("snapshot of empty recorder should be empty")
		}

		s.Record(mockfs.OpRead, 0, nil)
		snap = s.Snapshot()
		if snap.Empty() {
			t.Error("snapshot with operations should not be empty")
		}
	})
}

// TestStats_SnapshotMethods verifies snapshot interface methods.
func TestStats_SnapshotMethods(t *testing.T) {
	t.Parallel()

	t.Run("count success", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Set(mockfs.OpRead, 10, 3)
		snap := s.Snapshot()
		assertOpCount(t, snap, mockfs.OpRead, 10, 3)
	})

	t.Run("has failures", func(t *testing.T) {
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

	t.Run("failed operations", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Set(mockfs.OpOpen, 5, 2)
		s.Set(mockfs.OpRead, 10, 1)
		snap := s.Snapshot()

		failed := snap.FailedOperations()
		if len(failed) != 2 {
			t.Errorf("FailedOperations len = %d, want 2", len(failed))
		}
	})

	t.Run("delta", func(t *testing.T) {
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

	t.Run("equal", func(t *testing.T) {
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

	t.Run("panics", func(t *testing.T) {
		snap := mockfs.NewStatsRecorder(nil).Snapshot()
		assertPanic(t, func() { snap.Count(mockfs.Operation(-1)) }, "Count invalid op")
		assertPanic(t, func() { snap.CountSuccess(mockfs.NumOperations) }, "CountSuccess invalid op")
		assertPanic(t, func() { snap.CountFailure(mockfs.Operation(-1)) }, "CountFailure invalid op")
	})
}

// TestStatsAssertion verifies fluent assertion API.
func TestStatsAssertion(t *testing.T) {
	t.Parallel()

	t.Run("all assertions pass", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 100, nil)
		s.Record(mockfs.OpWrite, 50, nil)

		s.Expect().
			Count(mockfs.OpRead, 1).
			Success(mockfs.OpRead, 1).
			BytesRead(100).
			BytesWritten(50).
			NoFailures().
			Assert(t)
	})

	t.Run("count mismatch fails", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 0, nil)

		mt := &mockReporter{}
		s.Expect().Count(mockfs.OpRead, 2).Assert(mt)

		if len(mt.errors) != 1 || !strings.Contains(mt.errors[0], "Count(Read) = 1, want 2") {
			t.Errorf("unexpected errors: %v", mt.errors)
		}
	})

	t.Run("failure detection", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 0, errors.New("fail"))

		mt := &mockReporter{}
		s.Expect().NoFailures().Assert(mt)

		if len(mt.errors) != 1 || !strings.Contains(mt.errors[0], "expected no failures") {
			t.Errorf("unexpected errors: %v", mt.errors)
		}
	})

	t.Run("chain multiple assertions", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 100, nil)
		s.Record(mockfs.OpRead, 50, errors.New("fail"))
		s.Record(mockfs.OpWrite, 200, nil)

		s.Expect().
			Count(mockfs.OpRead, 2).
			Success(mockfs.OpRead, 1).
			Failure(mockfs.OpRead, 1).
			Count(mockfs.OpWrite, 1).
			BytesRead(150).
			BytesWritten(200).
			Assert(t)
	})

	t.Run("multiple failures", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpRead, 0, nil)

		mt := &mockReporter{}
		s.Expect().
			Count(mockfs.OpRead, 2). // Fails
			BytesRead(100).          // Fails
			Assert(mt)

		if len(mt.errors) != 2 {
			t.Errorf("expected 2 errors, got %d: %v", len(mt.errors), mt.errors)
		}
	})

	t.Run("success assertion", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Set(mockfs.OpRead, 10, 3)

		s.Expect().Success(mockfs.OpRead, 7).Assert(t)

		mt := &mockReporter{}
		s.Expect().Success(mockfs.OpRead, 5).Assert(mt)
		if len(mt.errors) != 1 {
			t.Errorf("expected 1 error, got %d", len(mt.errors))
		}
	})

	t.Run("failure assertion", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Set(mockfs.OpWrite, 10, 4)

		s.Expect().Failure(mockfs.OpWrite, 4).Assert(t)

		mt := &mockReporter{}
		s.Expect().Failure(mockfs.OpWrite, 2).Assert(mt)
		if len(mt.errors) != 1 {
			t.Errorf("expected 1 error, got %d", len(mt.errors))
		}
	})

	t.Run("bytes written assertion", func(t *testing.T) {
		s := mockfs.NewStatsRecorder(nil)
		s.Record(mockfs.OpWrite, 100, nil)

		s.Expect().BytesWritten(100).Assert(t)

		mt := &mockReporter{}
		s.Expect().BytesWritten(50).Assert(mt)
		if len(mt.errors) != 1 {
			t.Errorf("expected 1 error, got %d", len(mt.errors))
		}
	})
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

// TestStats_SnapshotImmutability verifies snapshots are immutable values.
func TestStats_SnapshotImmutability(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	s := mockfs.NewStatsRecorder(nil)

	s.Record(mockfs.OpRead, 100, nil)
	s.Record(mockfs.OpWrite, 200, nil)
	s.Record(mockfs.OpRead, 50, errors.New("fail"))
	s.Record(mockfs.OpStat, 1000, nil) // Should be ignored
	s.Record(mockfs.OpWrite, 25, errors.New("fail"))

	assertBytes(t, s, 150, 225)
}

// --- Benchmarks ---

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
