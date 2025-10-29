package mockfs_test

import (
	"io/fs"
	"path"
	"testing"
	"time"

	"github.com/balinomad/go-mockfs"
)

func TestNewFileInfo(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		path    string
		size    int64
		mode    fs.FileMode
		modTime time.Time
	}{
		{
			name:    "normal file",
			path:    "file.txt",
			size:    3,
			mode:    0644,
			modTime: time.Now(),
		},
		{
			name:    "zero time",
			path:    "file.txt",
			size:    3,
			mode:    0644,
			modTime: time.Time{},
		},
		{
			name:    "normal path",
			path:    "path/to/file.txt",
			size:    3,
			mode:    0644,
			modTime: time.Now(),
		},
		{
			name:    "normal dir",
			path:    "dir",
			size:    0,
			mode:    fs.ModeDir | 0755,
			modTime: time.Now(),
		},
		{
			name:    "invalid file mode",
			path:    "file.txt",
			size:    3,
			mode:    fs.ModeSocket | fs.ModeNamedPipe,
			modTime: time.Now(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileName := path.Base(tt.path)
			got := mockfs.NewFileInfo(tt.path, tt.size, tt.mode, tt.modTime)
			if got.Name() != fileName {
				t.Errorf("Name() = %v, want %v", got.Name(), fileName)
			}
			if got.IsDir() != (tt.mode&fs.ModeDir != 0) {
				t.Errorf("IsDir() = %v, want %v", got.IsDir(), tt.mode&fs.ModeDir != 0)
			}
			if !got.IsDir() && got.Size() != tt.size {
				t.Errorf("Size() = %v, want %v", got.Size(), tt.size)
			}
			if got.IsDir() && got.Size() != 0 {
				t.Errorf("Size() = %v, want 0", got.Size())
			}
			if got.Mode() != tt.mode {
				t.Errorf("Mode() = %v, want %v", got.Mode(), tt.mode)
			}
			assertFileInfoTime(t, got.ModTime(), tt.modTime)
			if got.Type() != tt.mode.Type() {
				t.Errorf("Type() = %v, want %v", got.Type(), tt.mode.Type())
			}
			if got.Sys() != nil {
				t.Errorf("Sys() = %v, want nil", got.Sys())
			}
			gotFileInfo, gotErr := got.Info()
			if gotErr != nil {
				t.Errorf("Info() failed: %v", gotErr)
			}
			if !got.Equal(gotFileInfo) {
				t.Errorf("Info() = %v, want %v", gotFileInfo, got)
			}
		})
	}
}

func TestNewFileInfo_Panic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		mode fs.FileMode
		size int64
	}{
		{
			name: "empty",
			in:   "",
			mode: 0,
			size: 0,
		},
		{
			name: "absolute",
			in:   "/abs",
			mode: 0,
			size: 0,
		},
		{
			name: "parent segment",
			in:   "a/../b",
			mode: 0,
			size: 0,
		},
		{
			name: "trailing slash",
			in:   "a/",
			mode: 0,
			size: 0,
		},
		{
			name: "dir with size",
			in:   "dir",
			mode: fs.ModeDir | 0744,
			size: 3,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			fileName := tt.in
			fileSize := tt.size
			fileMode := tt.mode
			t.Parallel()
			requirePanic(t, func() {
				_ = mockfs.NewFileInfo(fileName, fileSize, fileMode, time.Now())
			}, "NewFileInfo")
		})
	}
}

// assertFileInfoTime verifies ModTime() matches expected value.
func assertFileInfoTime(t *testing.T, got time.Time, want time.Time) {
	t.Helper()
	if !want.IsZero() && !got.Equal(want) {
		t.Errorf("ModTime() = %v, want %v", got, want)
	}
	if want.IsZero() && (got.Before(time.Now().Add(-time.Second)) || got.After(time.Now())) {
		t.Errorf("ModTime() = zero, want close to now")
	}
}
