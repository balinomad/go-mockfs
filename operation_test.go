package mockfs_test

import (
	"testing"

	"github.com/balinomad/go-mockfs"
)

func TestOperationString(t *testing.T) {
	tests := []struct {
		name string
		op   mockfs.Operation
		want string
	}{
		{
			name: "stat",
			op:   mockfs.OpStat,
			want: "Stat",
		},
		{
			name: "open",
			op:   mockfs.OpOpen,
			want: "Open",
		},
		{
			name: "read",
			op:   mockfs.OpRead,
			want: "Read",
		},
		{
			name: "mkdir",
			op:   mockfs.OpMkdir,
			want: "Mkdir",
		},
		{
			name: "rename",
			op:   mockfs.OpRename,
			want: "Rename",
		},
		{
			name: "out of range",
			op:   mockfs.NumOperations,
			want: "",
		},
		{
			name: "invalid",
			op:   mockfs.InvalidOperation,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.op.String()
			if got != tt.want {
				t.Errorf("String() = %s, wanted %s", got, tt.want)
			}
		})
	}
}

func TestStringToOperation(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want mockfs.Operation
	}{
		{
			name: "stat",
			s:    "Stat",
			want: mockfs.OpStat,
		},
		{
			name: "close",
			s:    "Close",
			want: mockfs.OpClose,
		},
		{
			name: "write",
			s:    "Write",
			want: mockfs.OpWrite,
		},
		{
			name: "mkdirall",
			s:    "MkdirAll",
			want: mockfs.OpMkdirAll,
		},
		{
			name: "rename",
			s:    "Rename",
			want: mockfs.OpRename,
		},
		{
			name: "invalid",
			s:    "invalid",
			want: mockfs.InvalidOperation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mockfs.StringToOperation(tt.s)
			if got != tt.want {
				t.Errorf("StringToOperation() = %v, want %v", got, tt.want)
			}
		})
	}
}
