package mockfs_test

import (
	"math/rand"
	"testing"

	"github.com/balinomad/go-mockfs"
)

// newCountersWithCounts returns a new Counters instance with the given operation counts.
// The function takes an even number of arguments, where each pair represents an operation and its count.
func newCountersWithCounts[T interface{ mockfs.Operation | int }](opsAndCounts ...T) *mockfs.Counters {
	c := &mockfs.Counters{}
	for i := 0; i < len(opsAndCounts); i += 2 {
		c.Set(mockfs.Operation(opsAndCounts[i]), int(opsAndCounts[i+1]))
	}

	return c
}

// TestNewCounters verifies that NewCounters initializes the
// operation counters correctly. It checks that the returned Counters instance
// is not nil and that the calls array is initialized to zero.
func TestNewCounters(t *testing.T) {
	c := mockfs.NewCounters()
	if c == nil {
		t.Error("NewCounters returned nil")
	}

	snapshot := c.Snapshot()
	if len(snapshot) != int(mockfs.NumOperations) {
		t.Error("NewCounters did not initialize calls array")
	}

	iErr := 0
	for i, count := range snapshot {
		if count != 0 {
			if iErr == 0 {
				t.Errorf("NewCounters did not initialize calls array to zero for the following operations:")
			}
			iErr++
			op := mockfs.Operation(i)
			t.Errorf("%s (%d) count = %d", op.String(), i, count)
		}
	}
}

// TestCounters_Count exercises the Count method on Counters.
func TestCounters_Count(t *testing.T) {
	opRand := mockfs.Operation(rand.Intn(int(mockfs.NumOperations)-1) + 1)
	populateCounters := func() *mockfs.Counters {
		c := mockfs.NewCounters()
		for i := mockfs.Operation(-10); i < mockfs.NumOperations+10; i++ {
			c.Set(i, 42)
		}
		return c
	}

	tests := []struct {
		name      string
		counts    *mockfs.Counters
		operation mockfs.Operation
		want      int
	}{
		{
			name:      "empty counters check lower bound",
			counts:    mockfs.NewCounters(),
			operation: mockfs.Operation(0),
			want:      0,
		},
		{
			name:      "empty counters check upper bound",
			counts:    mockfs.NewCounters(),
			operation: mockfs.NumOperations - 1,
			want:      0,
		},
		{
			name:      "empty counters check random operation",
			counts:    mockfs.NewCounters(),
			operation: opRand,
			want:      0,
		},
		{
			name:      "empty counters check invalid negative operation",
			counts:    mockfs.NewCounters(),
			operation: -100,
			want:      0,
		},
		{
			name:      "empty counters check invalid large operation",
			counts:    mockfs.NewCounters(),
			operation: 1000,
			want:      0,
		},
		{
			name:      "check non-zero lower bound",
			counts:    newCountersWithCounts(0, 42),
			operation: mockfs.Operation(0),
			want:      42,
		},
		{
			name:      "check non-zero upper bound",
			counts:    newCountersWithCounts(mockfs.NumOperations-1, 42),
			operation: mockfs.NumOperations - 1,
			want:      42,
		},
		{
			name:      "check non-zero random operation",
			counts:    newCountersWithCounts(opRand, 42),
			operation: opRand,
			want:      42,
		},
		{
			name:      "check invalid negative operation on non-zero counters",
			counts:    populateCounters(),
			operation: -5,
			want:      0,
		},
		{
			name:      "check invalid large operation on non-zero counters",
			counts:    populateCounters(),
			operation: mockfs.NumOperations + 5,
			want:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count := tt.counts.Count(tt.operation)
			if count != tt.want {
				t.Errorf("Counters.Count() = %d, want %d", count, tt.want)
			}
		})
	}
}

// TestCounters_Snapshot verifies that Snapshot returns a correct copy of all
// operation counters with their respective counts. It checks that the
// returned Counters instance is not nil and that the calls array is initialized
// to the correct values.
func TestCounters_Snapshot(t *testing.T) {
	tests := []struct {
		name   string
		counts *mockfs.Counters
		want   [mockfs.NumOperations]int
	}{
		{
			name:   "empty counters",
			counts: mockfs.NewCounters(),
			want:   [mockfs.NumOperations]int{},
		},
		{
			name: "non-empty counters",
			counts: func() *mockfs.Counters {
				c := mockfs.NewCounters()
				c.Set(mockfs.OpStat, 42)
				c.Set(mockfs.OpMkdir, 1)
				c.Set(mockfs.OpRename, 6)
				return c
			}(),
			want: [mockfs.NumOperations]int{
				mockfs.OpStat:   42,
				mockfs.OpMkdir:  1,
				mockfs.OpRename: 6,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := tt.counts.Snapshot()
			if snapshot != tt.want {
				t.Errorf("Counters.Snapshot() = %v, want %v", snapshot, tt.want)
			}
		})
	}
}

// TestCounters_Set verifies that Set correctly updates the operation counters
// in the given Counters instance.
func TestCounters_Set(t *testing.T) {
	counts := mockfs.NewCounters()

	tests := []struct {
		name      string
		operation mockfs.Operation
		count     int
	}{
		{
			name:      "update zero count of OpStat",
			operation: mockfs.OpStat,
			count:     10,
		},
		{
			name:      "update zero count of OpOpen",
			operation: mockfs.OpOpen,
			count:     1,
		},
		{
			name:      "update zero count of OpWrite",
			operation: mockfs.OpWrite,
			count:     42,
		},
		{
			name:      "update zero count of OpRename",
			operation: mockfs.OpRename,
			count:     2,
		},
		{
			name:      "update existing count of OpStat",
			operation: mockfs.OpStat,
			count:     20,
		},
		{
			name:      "update existing count of OpOpen",
			operation: mockfs.OpOpen,
			count:     3,
		},
		{
			name:      "update existing count of OpWrite",
			operation: mockfs.OpWrite,
			count:     10,
		},
		{
			name:      "update existing count of OpRename",
			operation: mockfs.OpRename,
			count:     0,
		},
		{
			name:      "non-existent operation",
			operation: mockfs.NumOperations + 100,
			count:     200,
		},
		{
			name:      "negative operation",
			operation: -100,
			count:     22,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts.Set(tt.operation, tt.count)
			want := tt.count
			if tt.operation >= mockfs.NumOperations || tt.operation < 0 {
				want = 0
			}
			if got := counts.Count(tt.operation); got != want {
				t.Errorf("count after Counters.Set() = %d, want %d", got, want)
			}
		})
	}
}

// TestCounters_ResetAll verifies that ResetAll correctly resets all operation counters to zero.
func TestCounters_ResetAll(t *testing.T) {
	tests := []struct {
		name   string
		counts *mockfs.Counters
		want   *mockfs.Counters
	}{
		{
			name:   "empty counters",
			counts: mockfs.NewCounters(),
			want:   mockfs.NewCounters(),
		},
		{
			name:   "non-empty counters",
			counts: newCountersWithCounts(mockfs.OpStat, 1, mockfs.OpOpen, 1),
			want:   mockfs.NewCounters(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.counts.ResetAll()
			if !tt.counts.Equal(tt.want) {
				t.Errorf("snapshot after Counters.ResetAll() = %v, want %v", tt.counts.Snapshot(), tt.want.Snapshot())
			}
		})
	}
}

// TestCounters_Clone verifies that Clone returns a deep copy of the Counters instance.
func TestCounters_Clone(t *testing.T) {
	tests := []struct {
		name   string
		counts *mockfs.Counters
	}{
		{
			name:   "empty counters",
			counts: mockfs.NewCounters(),
		},
		{
			name:   "non-empty counters",
			counts: newCountersWithCounts(1, 10, 2, 20, 3, 30, 4, 40),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clone := tt.counts.Clone()

			if clone == tt.counts {
				t.Error("Clone() returned the same pointer")
			}

			if !clone.Equal(tt.counts) {
				t.Error("Clone() returned a Counters instance with different operation counts")
			}

			clone.Set(mockfs.Operation(5), 50)
			if tt.counts.Count(mockfs.Operation(5)) == clone.Count(mockfs.Operation(5)) {
				t.Errorf("Clone() did not create a separate instance")
			}
		})
	}
}

func TestCounters_Equal(t *testing.T) {
	same := newCountersWithCounts(1, 10, 2, 20, 3, 30, 4, 40)

	tests := []struct {
		name    string
		counts1 *mockfs.Counters
		counts2 *mockfs.Counters
		want    bool
	}{
		{
			name:    "empty counters",
			counts1: mockfs.NewCounters(),
			counts2: mockfs.NewCounters(),
			want:    true,
		},
		{
			name:    "identical counters",
			counts1: newCountersWithCounts(1, 10, 2, 20, 3, 30, 4, 40),
			counts2: newCountersWithCounts(1, 10, 2, 20, 3, 30, 4, 40),
			want:    true,
		},
		{
			name:    "identical counters with different Set order",
			counts1: newCountersWithCounts(1, 10, 2, 20, 3, 30, 4, 40),
			counts2: newCountersWithCounts(3, 30, 4, 40, 1, 10, 2, 20),
			want:    true,
		},
		{
			name:    "different counters",
			counts1: newCountersWithCounts(1, 10, 2, 20, 3, 30),
			counts2: newCountersWithCounts(1, 10, 2, 20, 3, 30, 4, 40),
			want:    false,
		},
		{
			name:    "same counters instance",
			counts1: same,
			counts2: same,
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.counts1.Equal(tt.counts2) != tt.want {
				t.Errorf("Equal() = %v, want %v", tt.counts1.Equal(tt.counts2), tt.want)
			}
		})
	}
}
