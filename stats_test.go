package mockfs_test

import (
	"errors"
	"reflect"
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

// TestNewStats verifies that NewStats initializes all counters to zero.
func TestNewStats(t *testing.T) {
	s := mockfs.NewStats()
	if s == nil {
		t.Fatal("NewStats returned nil")
	}

	snapshot := s.Snapshot()
	if snapshot.BytesRead != 0 {
		t.Errorf("NewStats: BytesRead = %d, want 0", snapshot.BytesRead)
	}
	if snapshot.BytesWritten != 0 {
		t.Errorf("NewStats: BytesWritten = %d, want 0", snapshot.BytesWritten)
	}

	if len(snapshot.Operations) != int(mockfs.NumOperations) {
		t.Fatalf("NewStats: snapshot has %d operations, want %d", len(snapshot.Operations), mockfs.NumOperations)
	}

	for i, opCount := range snapshot.Operations {
		if opCount.Total != 0 || opCount.Failure != 0 {
			op := mockfs.Operation(i)
			t.Errorf("NewStats: operation %s (%d) has non-zero counts: Total=%d, Failure=%d",
				op.String(), i, opCount.Total, opCount.Failure)
		}
	}
}

// TestStats_Record exercises the Record method.
func TestStats_Record(t *testing.T) {
	someErr := errors.New("test error")

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
			name:        "OpRead success with bytes",
			op:          mockfs.OpRead,
			bytes:       100,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    100,
			wantWritten: 0,
		},
		{
			name:        "OpRead failure with bytes (partial read)",
			op:          mockfs.OpRead,
			bytes:       50,
			err:         someErr,
			wantTotal:   1,
			wantFailure: 1,
			wantRead:    50,
			wantWritten: 0,
		},
		{
			name:        "OpRead failure no bytes",
			op:          mockfs.OpRead,
			bytes:       0,
			err:         someErr,
			wantTotal:   1,
			wantFailure: 1,
			wantRead:    0,
			wantWritten: 0,
		},
		{
			name:        "OpWrite success with bytes",
			op:          mockfs.OpWrite,
			bytes:       200,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    0,
			wantWritten: 200,
		},
		{
			name:        "OpWrite failure with bytes (partial write)",
			op:          mockfs.OpWrite,
			bytes:       75,
			err:         someErr,
			wantTotal:   1,
			wantFailure: 1,
			wantRead:    0,
			wantWritten: 75,
		},
		{
			name:        "OpWrite success no bytes",
			op:          mockfs.OpWrite,
			bytes:       0,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    0,
			wantWritten: 0,
		},
		{
			name:        "OpStat success (no bytes)",
			op:          mockfs.OpStat,
			bytes:       0,
			err:         nil,
			wantTotal:   1,
			wantFailure: 0,
			wantRead:    0,
			wantWritten: 0,
		},
		{
			name:        "OpStat failure (bytes ignored)",
			op:          mockfs.OpStat,
			bytes:       123, // Bytes are ignored for non-read/write ops
			err:         someErr,
			wantTotal:   1,
			wantFailure: 1,
			wantRead:    0,
			wantWritten: 0,
		},
		{
			name:        "OpRead with negative bytes (ignored)",
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
			s := mockfs.NewStats()
			s.Record(tt.op, tt.bytes, tt.err)

			if got := s.Count(tt.op); got != tt.wantTotal {
				t.Errorf("Count(%s) = %d, want %d", tt.op, got, tt.wantTotal)
			}
			if got := s.CountFailure(tt.op); got != tt.wantFailure {
				t.Errorf("CountFailure(%s) = %d, want %d", tt.op, got, tt.wantFailure)
			}
			if got := s.GetBytesRead(); got != tt.wantRead {
				t.Errorf("GetBytesRead() = %d, want %d", got, tt.wantRead)
			}
			if got := s.GetBytesWritten(); got != tt.wantWritten {
				t.Errorf("GetBytesWritten() = %d, want %d", got, tt.wantWritten)
			}
		})
	}

	t.Run("cumulative", func(t *testing.T) {
		s := mockfs.NewStats()
		s.Record(mockfs.OpRead, 100, nil)
		s.Record(mockfs.OpRead, 50, someErr)
		s.Record(mockfs.OpWrite, 200, nil)
		s.Record(mockfs.OpStat, 0, someErr)

		if got, want := s.Count(mockfs.OpRead), 2; got != want {
			t.Errorf("Cumulative Count(OpRead) = %d, want %d", got, want)
		}
		if got, want := s.CountFailure(mockfs.OpRead), 1; got != want {
			t.Errorf("Cumulative CountFailure(OpRead) = %d, want %d", got, want)
		}
		if got, want := s.Count(mockfs.OpWrite), 1; got != want {
			t.Errorf("Cumulative Count(OpWrite) = %d, want %d", got, want)
		}
		if got, want := s.Count(mockfs.OpStat), 1; got != want {
			t.Errorf("Cumulative Count(OpStat) = %d, want %d", got, want)
		}
		if got, want := s.CountFailure(mockfs.OpStat), 1; got != want {
			t.Errorf("Cumulative CountFailure(OpStat) = %d, want %d", got, want)
		}
		if got, want := s.GetBytesRead(), 150; got != want {
			t.Errorf("Cumulative GetBytesRead() = %d, want %d", got, want)
		}
		if got, want := s.GetBytesWritten(), 200; got != want {
			t.Errorf("Cumulative GetBytesWritten() = %d, want %d", got, want)
		}
	})
}

// TestStats_Record_Panic tests the panic condition for Record.
func TestStats_Record_Panic(t *testing.T) {
	s := mockfs.NewStats()
	assertPanics(t, func() {
		s.Record(mockfs.Operation(-1), 0, nil)
	}, "Record(invalid op -1)")
	assertPanics(t, func() {
		s.Record(mockfs.NumOperations, 0, nil)
	}, "Record(invalid op NumOperations)")
}

// TestStats_Counters exercises Count, CountSuccess, and CountFailure.
func TestStats_Counters(t *testing.T) {
	s := mockfs.NewStats()
	s.Set(mockfs.OpStat, 10, 3) // 10 total, 3 failures, 7 success
	s.Set(mockfs.OpOpen, 5, 0)  // 5 total, 0 failures, 5 success
	s.Set(mockfs.OpClose, 2, 2) // 2 total, 2 failures, 0 success

	tests := []struct {
		name        string
		op          mockfs.Operation
		wantTotal   int
		wantSuccess int
		wantFailure int
	}{
		{"OpStat", mockfs.OpStat, 10, 7, 3},
		{"OpOpen", mockfs.OpOpen, 5, 5, 0},
		{"OpClose", mockfs.OpClose, 2, 0, 2},
		{"OpRead (unset)", mockfs.OpRead, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.Count(tt.op); got != tt.wantTotal {
				t.Errorf("Count() = %d, want %d", got, tt.wantTotal)
			}
			if got := s.CountSuccess(tt.op); got != tt.wantSuccess {
				t.Errorf("CountSuccess() = %d, want %d", got, tt.wantSuccess)
			}
			if got := s.CountFailure(tt.op); got != tt.wantFailure {
				t.Errorf("CountFailure() = %d, want %d", got, tt.wantFailure)
			}
		})
	}
}

// TestStats_Counters_Panic tests the panic conditions for counter methods.
func TestStats_Counters_Panic(t *testing.T) {
	s := mockfs.NewStats()
	invalidOp := mockfs.Operation(-1)
	invalidOp2 := mockfs.NumOperations

	assertPanics(t, func() { s.Count(invalidOp) }, "Count(invalid -1)")
	assertPanics(t, func() { s.Count(invalidOp2) }, "Count(invalid NumOperations)")

	assertPanics(t, func() { s.CountSuccess(invalidOp) }, "CountSuccess(invalid -1)")
	assertPanics(t, func() { s.CountSuccess(invalidOp2) }, "CountSuccess(invalid NumOperations)")

	assertPanics(t, func() { s.CountFailure(invalidOp) }, "CountFailure(invalid -1)")
	assertPanics(t, func() { s.CountFailure(invalidOp2) }, "CountFailure(invalid NumOperations)")
}

// TestStats_ByteCounters tests GetBytesRead and GetBytesWritten.
func TestStats_ByteCounters(t *testing.T) {
	s := mockfs.NewStats()

	if got := s.GetBytesRead(); got != 0 {
		t.Errorf("Initial GetBytesRead() = %d, want 0", got)
	}
	if got := s.GetBytesWritten(); got != 0 {
		t.Errorf("Initial GetBytesWritten() = %d, want 0", got)
	}

	s.Record(mockfs.OpRead, 100, nil)
	s.Record(mockfs.OpWrite, 200, nil)
	s.Record(mockfs.OpRead, 50, errors.New("err")) // Bytes recorded even on error
	s.Record(mockfs.OpStat, 1000, nil)             // Should be ignored
	s.Record(mockfs.OpWrite, 25, errors.New("err"))

	if got := s.GetBytesRead(); got != 150 {
		t.Errorf("After records GetBytesRead() = %d, want 150", got)
	}
	if got := s.GetBytesWritten(); got != 225 {
		t.Errorf("After records GetBytesWritten() = %d, want 225", got)
	}
}

// TestStats_Snapshot verifies that Snapshot returns a correct copy.
func TestStats_Snapshot(t *testing.T) {
	s := mockfs.NewStats()
	s.Record(mockfs.OpRead, 120, nil)
	s.Record(mockfs.OpWrite, 240, errors.New("err"))
	s.Set(mockfs.OpStat, 5, 2)

	want := mockfs.Snapshot{
		BytesRead:    120,
		BytesWritten: 240,
		Operations:   [mockfs.NumOperations]mockfs.OpCount{},
	}
	want.Operations[mockfs.OpRead] = mockfs.OpCount{Total: 1, Failure: 0}
	want.Operations[mockfs.OpWrite] = mockfs.OpCount{Total: 1, Failure: 1}
	want.Operations[mockfs.OpStat] = mockfs.OpCount{Total: 5, Failure: 2}

	got := s.Snapshot()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Snapshot() mismatch:\ngot:  %#v\nwant: %#v", got, want)
	}

	// Test that snapshot is a copy
	s.Record(mockfs.OpRead, 10, nil)
	s.Set(mockfs.OpOpen, 1, 0)

	if got.BytesRead == s.GetBytesRead() {
		t.Error("Snapshot BytesRead was modified after Stats changed; not a copy")
	}
	if got.Operations[mockfs.OpOpen].Total == s.Count(mockfs.OpOpen) {
		t.Error("Snapshot Operations was modified after Stats changed; not a copy")
	}
}

// TestStats_Set verifies the Set method, including its panic conditions.
func TestStats_Set(t *testing.T) {
	t.Run("valid set", func(t *testing.T) {
		s := mockfs.NewStats()
		s.Set(mockfs.OpStat, 10, 4)

		if got := s.Count(mockfs.OpStat); got != 10 {
			t.Errorf("Count(OpStat) = %d, want 10", got)
		}
		if got := s.CountFailure(mockfs.OpStat); got != 4 {
			t.Errorf("CountFailure(OpStat) = %d, want 4", got)
		}
		if got := s.CountSuccess(mockfs.OpStat); got != 6 {
			t.Errorf("CountSuccess(OpStat) = %d, want 6", got)
		}

		// Set to zero
		s.Set(mockfs.OpStat, 0, 0)
		if got := s.Count(mockfs.OpStat); got != 0 {
			t.Errorf("Count(OpStat) after reset = %d, want 0", got)
		}
		if got := s.CountFailure(mockfs.OpStat); got != 0 {
			t.Errorf("CountFailure(OpStat) after reset = %d, want 0", got)
		}
	})

	t.Run("panics", func(t *testing.T) {
		s := mockfs.NewStats()
		assertPanics(t, func() { s.Set(mockfs.Operation(-1), 1, 0) }, "Set(invalid op -1)")
		assertPanics(t, func() { s.Set(mockfs.NumOperations, 1, 0) }, "Set(invalid op NumOperations)")
		assertPanics(t, func() { s.Set(mockfs.OpStat, 5, -1) }, "Set(failures < 0)")
		assertPanics(t, func() { s.Set(mockfs.OpStat, 5, 6) }, "Set(failures > total)")
	})
}

// TestStats_Reset verifies that Reset resets all counters to zero.
func TestStats_Reset(t *testing.T) {
	t.Run("empty stats", func(t *testing.T) {
		s := mockfs.NewStats()
		s.Reset()
		if !s.Equal(mockfs.NewStats()) {
			t.Error("Reset() on empty stats did not result in empty stats")
		}
	})

	t.Run("populated stats", func(t *testing.T) {
		s := mockfs.NewStats()
		s.Record(mockfs.OpRead, 100, nil)
		s.Record(mockfs.OpWrite, 200, errors.New("err"))
		s.Set(mockfs.OpStat, 10, 2)

		s.Reset()

		want := mockfs.NewStats()
		if !s.Equal(want) {
			t.Errorf("Reset() stats not equal to new stats.\ngot:  %#v\nwant: %#v", s.Snapshot(), want.Snapshot())
		}
	})
}

// TestStats_Clone verifies that Clone returns a deep copy.
func TestStats_Clone(t *testing.T) {
	s := mockfs.NewStats()
	s.Record(mockfs.OpRead, 123, nil)
	s.Record(mockfs.OpWrite, 456, errors.New("err"))
	s.Set(mockfs.OpStat, 10, 2)

	clone := s.Clone()

	if clone == s {
		t.Fatal("Clone() returned the same pointer")
	}

	if !clone.Equal(s) {
		t.Errorf("Clone() is not equal to original.\noriginal: %#v\nclone:    %#v", s.Snapshot(), clone.Snapshot())
	}

	// Modify original and check clone is unaffected
	s.Record(mockfs.OpRead, 1, nil)
	s.Set(mockfs.OpOpen, 1, 0)

	// Create the snapshot we expect the clone to still have
	wantCloneSnap := mockfs.Snapshot{
		BytesRead:    123,
		BytesWritten: 456,
		Operations:   [mockfs.NumOperations]mockfs.OpCount{},
	}
	wantCloneSnap.Operations[mockfs.OpRead] = mockfs.OpCount{Total: 1, Failure: 0}
	wantCloneSnap.Operations[mockfs.OpWrite] = mockfs.OpCount{Total: 1, Failure: 1}
	wantCloneSnap.Operations[mockfs.OpStat] = mockfs.OpCount{Total: 10, Failure: 2}

	if !reflect.DeepEqual(clone.Snapshot(), wantCloneSnap) {
		t.Errorf("Clone was modified after original changed.\ngot:  %#v\nwant: %#v", clone.Snapshot(), wantCloneSnap)
	}

	// Modify clone and check original is unaffected
	clone.Set(mockfs.OpRemove, 5, 0)
	if s.Count(mockfs.OpRemove) != 0 {
		t.Error("Original was modified after clone changed")
	}
}

// TestStats_Equal verifies the Equal method.
func TestStats_Equal(t *testing.T) {
	s1 := mockfs.NewStats()
	s1.Record(mockfs.OpRead, 100, nil)
	s1.Set(mockfs.OpStat, 5, 1)

	s2 := mockfs.NewStats()
	s2.Record(mockfs.OpRead, 100, nil)
	s2.Set(mockfs.OpStat, 5, 1)

	s3_diff_failure := mockfs.NewStats()
	s3_diff_failure.Record(mockfs.OpRead, 100, nil)
	s3_diff_failure.Set(mockfs.OpStat, 5, 2) // Different failure

	s4_diff_bytes := mockfs.NewStats()
	s4_diff_bytes.Record(mockfs.OpRead, 101, nil) // Different bytes
	s4_diff_bytes.Set(mockfs.OpStat, 5, 1)

	s5_diff_op := mockfs.NewStats()
	s5_diff_op.Record(mockfs.OpRead, 100, nil)
	s5_diff_op.Set(mockfs.OpStat, 5, 1)
	s5_diff_op.Set(mockfs.OpOpen, 1, 0) // Different op count

	tests := []struct {
		name string
		s1   *mockfs.Stats
		s2   *mockfs.Stats
		want bool
	}{
		{"two empty stats", mockfs.NewStats(), mockfs.NewStats(), true},
		{"identical populated stats", s1, s2, true},
		{"same instance", s1, s1, true},
		{"different failures", s1, s3_diff_failure, false},
		{"different bytesRead", s1, s4_diff_bytes, false},
		{"different op count", s1, s5_diff_op, false},
		{"one empty, one populated", s1, mockfs.NewStats(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s1.Equal(tt.s2); got != tt.want {
				t.Errorf("s1.Equal(s2) = %v, want %v", got, tt.want)
			}
			// Test commutativity
			if got := tt.s2.Equal(tt.s1); got != tt.want {
				t.Errorf("s2.Equal(s1) = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestStats_ConcurrentAccess tests for race conditions.
// Run this test with the -race flag: go test -race
func TestStats_ConcurrentAccess(t *testing.T) {
	s := mockfs.NewStats()
	var wg sync.WaitGroup

	numGoroutines := 50
	numOpsPerG := 100

	// Hammer writers (Record, Set, Reset)
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOpsPerG; j++ {
				op := mockfs.Operation((j % (int(mockfs.NumOperations) - 2)) + 1) // Cycle through valid ops
				var err error
				if j%10 == 0 {
					err = errors.New("err")
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

	// Hammer readers (Count*, GetBytes*, Snapshot, Clone)
	for i := 0; i < numGoroutines/5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOpsPerG; j++ {
				_ = s.Snapshot()
				_ = s.Count(mockfs.OpRead)
				_ = s.CountSuccess(mockfs.OpWrite)
				_ = s.CountFailure(mockfs.OpStat)
				_ = s.GetBytesRead()
				_ = s.GetBytesWritten()
				_ = s.Clone()
				_ = s.Equal(s)
			}
		}()
	}

	wg.Wait()

	// The primary goal is to ensure no races are detected by `go test -race`.
	// We can't assert final counts due to the intentional races with Set/Reset.
	if s == nil {
		t.Error("Stats is nil after concurrent access")
	}
	t.Log("Concurrent access test finished. Run with -race flag to detect races.")
}
