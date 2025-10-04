package mockfs_test

import (
	"errors"
	"io"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/balinomad/go-mockfs"
)

// Helper functions

func checkStats(t *testing.T, m *mockfs.MockFS, expected [mockfs.NumOperations]int, context string) {
	t.Helper()
	actual := m.GetStats().Snapshot()
	if actual != expected {
		t.Errorf("%s: snapshot = %+v, want %+v", context, actual, expected)
	}
}

func expectError(t *testing.T, got, want error) {
	t.Helper()
	if errors.Is(got, want) {
		return
	}
	var pathErr *fs.PathError
	if errors.As(got, &pathErr) && errors.Is(pathErr.Err, want) {
		return
	}
	t.Errorf("expected error %v, got: %v", want, got)
}

// Basic operations tests

func TestNewMockFS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		initial     map[string]*mockfs.MapFile
		opts        []mockfs.Option
		validateErr bool
	}{
		{
			name:    "empty filesystem",
			initial: nil,
		},
		{
			name: "with files",
			initial: map[string]*mockfs.MapFile{
				"test.txt": {Data: []byte("content")},
			},
		},
		{
			name: "with latency option",
			initial: map[string]*mockfs.MapFile{
				"test.txt": {Data: []byte("content")},
			},
			opts: []mockfs.Option{mockfs.WithLatency(10 * time.Millisecond)},
		},
		{
			name: "with writes enabled",
			initial: map[string]*mockfs.MapFile{
				"test.txt": {Data: []byte("content")},
			},
			opts: []mockfs.Option{mockfs.WithWritesEnabled(nil)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := mockfs.NewMockFS(tt.initial, tt.opts...)
			if mockFS == nil {
				t.Fatal("NewMockFS returned nil")
			}
		})
	}
}

func TestStatOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		wantErr   error
		checkInfo func(*testing.T, fs.FileInfo)
	}{
		{
			name: "stat file",
			path: "test.txt",
			checkInfo: func(t *testing.T, info fs.FileInfo) {
				if info.Name() != "test.txt" || info.Size() != 12 || info.IsDir() {
					t.Errorf("unexpected file info: %+v", info)
				}
			},
		},
		{
			name: "stat directory",
			path: "dir",
			checkInfo: func(t *testing.T, info fs.FileInfo) {
				if !info.IsDir() {
					t.Error("expected directory")
				}
			},
		},
		{
			name:    "stat nonexistent",
			path:    "nonexistent.txt",
			wantErr: fs.ErrNotExist,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
				"test.txt":     {Data: []byte("test content")},
				"dir":          {Mode: fs.ModeDir},
				"dir/file.txt": {Data: []byte("in dir")},
			})

			info, err := mockFS.Stat(tt.path)

			if tt.wantErr != nil {
				expectError(t, err, tt.wantErr)
				return
			}

			if err != nil {
				t.Fatalf("Stat failed: %v", err)
			}

			if tt.checkInfo != nil {
				tt.checkInfo(t, info)
			}
		})
	}
}

func TestOpenReadClose(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt":     {Data: []byte("test content")},
		"dir/file.txt": {Data: []byte("nested content")},
		"dir":          {Mode: fs.ModeDir},
	})

	tests := []struct {
		name            string
		path            string
		expectedContent string
	}{
		{"root file", "test.txt", "test content"},
		{"nested file", "dir/file.txt", "nested content"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := mockFS.Open(tt.path)
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}

			content, err := io.ReadAll(file)
			if err != nil {
				t.Fatalf("ReadAll failed: %v", err)
			}

			if string(content) != tt.expectedContent {
				t.Errorf("expected %q, got %q", tt.expectedContent, string(content))
			}

			if err := file.Close(); err != nil {
				t.Fatalf("Close failed: %v", err)
			}
		})
	}
}

func TestReadFile(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("test content")},
	})

	content, err := mockFS.ReadFile("test.txt")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}

	if string(content) != "test content" {
		t.Errorf("expected %q, got %q", "test content", string(content))
	}
}

func TestReadDir(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"dir":           {Mode: fs.ModeDir},
		"dir/file1.txt": {Data: []byte("content1")},
		"dir/file2.txt": {Data: []byte("content2")},
	})

	entries, err := mockFS.ReadDir("dir")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

// Error injection tests

func TestErrorInjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setupErr  func(*mockfs.MockFS)
		operation func(*mockfs.MockFS) error
		wantErr   error
	}{
		{
			name: "stat error",
			setupErr: func(m *mockfs.MockFS) {
				m.AddStatError("test.txt", fs.ErrPermission)
			},
			operation: func(m *mockfs.MockFS) error {
				_, err := m.Stat("test.txt")
				return err
			},
			wantErr: fs.ErrPermission,
		},
		{
			name: "open error",
			setupErr: func(m *mockfs.MockFS) {
				m.AddOpenError("test.txt", mockfs.ErrTimeout)
			},
			operation: func(m *mockfs.MockFS) error {
				_, err := m.Open("test.txt")
				return err
			},
			wantErr: mockfs.ErrTimeout,
		},
		{
			name: "read error",
			setupErr: func(m *mockfs.MockFS) {
				m.AddReadError("test.txt", mockfs.ErrCorrupted)
			},
			operation: func(m *mockfs.MockFS) error {
				f, err := m.Open("test.txt")
				if err != nil {
					return err
				}
				defer f.Close()
				_, err = f.Read(make([]byte, 1))
				return err
			},
			wantErr: mockfs.ErrCorrupted,
		},
		{
			name: "close error",
			setupErr: func(m *mockfs.MockFS) {
				m.AddCloseError("test.txt", mockfs.ErrTimeout)
			},
			operation: func(m *mockfs.MockFS) error {
				f, err := m.Open("test.txt")
				if err != nil {
					return err
				}
				return f.Close()
			},
			wantErr: mockfs.ErrTimeout,
		},
		{
			name: "readdir error",
			setupErr: func(m *mockfs.MockFS) {
				m.AddReadDirError("dir", mockfs.ErrNotExist)
			},
			operation: func(m *mockfs.MockFS) error {
				_, err := m.ReadDir("dir")
				return err
			},
			wantErr: mockfs.ErrNotExist,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
				"test.txt": {Data: []byte("content")},
				"dir":      {Mode: fs.ModeDir},
			})

			tt.setupErr(mockFS)
			err := tt.operation(mockFS)
			expectError(t, err, tt.wantErr)
		})
	}
}

func TestErrorModes(t *testing.T) {
	t.Parallel()

	t.Run("error mode once", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"test.txt": {Data: []byte("content")},
		})

		mockFS.AddOpenErrorOnce("test.txt", fs.ErrPermission)

		_, err := mockFS.Open("test.txt")
		expectError(t, err, fs.ErrPermission)

		file, err := mockFS.Open("test.txt")
		if err != nil {
			t.Errorf("second open should succeed, got: %v", err)
		}
		if file != nil {
			file.Close()
		}
	})

	t.Run("error mode after successes", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"test.txt": {Data: []byte("abcde")},
		})

		mockFS.AddReadErrorAfterN("test.txt", mockfs.ErrCorrupted, 2)

		file, err := mockFS.Open("test.txt")
		if err != nil {
			t.Fatalf("open failed: %v", err)
		}
		defer file.Close()

		buf := make([]byte, 1)

		for i := 0; i < 2; i++ {
			_, err = file.Read(buf)
			if err != nil {
				t.Errorf("read %d should succeed, got: %v", i+1, err)
			}
		}

		_, err = file.Read(buf)
		expectError(t, err, mockfs.ErrCorrupted)
	})
}

func TestErrorPattern(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"config.yaml": {Data: []byte("settings")},
		"data.txt":    {Data: []byte("info")},
		"log.txt":     {Data: []byte("trace")},
	})

	err := mockFS.GetInjector().AddPattern(mockfs.OpOpen, `\.txt$`, fs.ErrPermission, mockfs.ErrorModeAlways, 0)
	if err != nil {
		t.Fatalf("AddPattern failed: %v", err)
	}

	_, err = mockFS.Open("config.yaml")
	if err != nil {
		t.Errorf("non-matching file should succeed, got: %v", err)
	}

	_, err = mockFS.Open("data.txt")
	expectError(t, err, fs.ErrPermission)

	_, err = mockFS.Open("log.txt")
	expectError(t, err, fs.ErrPermission)
}

func TestClearErrors(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("content")},
	})

	mockFS.AddOpenError("test.txt", fs.ErrPermission)

	_, err := mockFS.Open("test.txt")
	expectError(t, err, fs.ErrPermission)

	mockFS.ClearErrors()

	_, err = mockFS.Open("test.txt")
	if err != nil {
		t.Errorf("after ClearErrors should succeed, got: %v", err)
	}
}

func TestMarkNonExistent(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"keep.txt":   {Data: []byte("keep")},
		"delete.txt": {Data: []byte("delete")},
	})

	mockFS.MarkNonExistent("delete.txt")

	_, err := mockFS.Stat("delete.txt")
	expectError(t, err, fs.ErrNotExist)

	_, err = mockFS.Open("delete.txt")
	expectError(t, err, fs.ErrNotExist)

	_, err = mockFS.Stat("keep.txt")
	if err != nil {
		t.Errorf("other file should be accessible, got: %v", err)
	}
}

// Statistics tests

func TestStatistics(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("hello")},
		"dir":      {Mode: fs.ModeDir},
	})

	mockFS.ResetStats()

	_, _ = mockFS.Stat("test.txt")
	_, _ = mockFS.Stat("dir")

	file, _ := mockFS.Open("test.txt")
	if file != nil {
		_, _ = file.Read(make([]byte, 1))
		_, _ = file.Read(make([]byte, 10))
		_ = file.Close()
	}

	_, _ = mockFS.ReadDir("dir")

	stats := mockFS.GetStats()
	if stats.Count(mockfs.OpStat) != 2 {
		t.Errorf("expected 2 stats, got %d", stats.Count(mockfs.OpStat))
	}
	if stats.Count(mockfs.OpOpen) != 1 {
		t.Errorf("expected 1 open, got %d", stats.Count(mockfs.OpOpen))
	}
	if stats.Count(mockfs.OpRead) != 2 {
		t.Errorf("expected 2 reads, got %d", stats.Count(mockfs.OpRead))
	}
	if stats.Count(mockfs.OpClose) != 1 {
		t.Errorf("expected 1 close, got %d", stats.Count(mockfs.OpClose))
	}
	if stats.Count(mockfs.OpReadDir) != 1 {
		t.Errorf("expected 1 readdir, got %d", stats.Count(mockfs.OpReadDir))
	}

	mockFS.ResetStats()
	stats = mockFS.GetStats()
	if stats.Count(mockfs.OpStat) != 0 {
		t.Errorf("after reset, expected 0 stats, got %d", stats.Count(mockfs.OpStat))
	}
}

func TestCountersClone(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("content")},
	})

	_, _ = mockFS.Open("test.txt")
	stats1 := mockFS.GetStats()
	stats2 := mockFS.GetStats()

	if !stats1.Equal(stats2) {
		t.Error("cloned stats should be equal")
	}

	_, _ = mockFS.Open("test.txt")
	if stats1.Equal(mockFS.GetStats()) {
		t.Error("original clone should not reflect new operations")
	}
}

// Latency simulation tests

func TestLatencySimulation(t *testing.T) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("hello")},
	}, mockfs.WithLatency(20*time.Millisecond))

	minDuration := 15 * time.Millisecond  // Allow some tolerance lower
	maxDuration := 100 * time.Millisecond // Allow generous upper bound

	operations := []struct {
		name string
		exec func() error
	}{
		{"stat", func() error { _, err := mockFS.Stat("test.txt"); return err }},
		{"open", func() error { _, err := mockFS.Open("test.txt"); return err }},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			start := time.Now()
			err := op.exec()
			duration := time.Since(start)

			if err != nil {
				t.Fatalf("%s failed: %v", op.name, err)
			}

			if duration < minDuration || duration > maxDuration {
				t.Errorf("%s duration (%v) outside expected range [%v, %v]",
					op.name, duration, minDuration, maxDuration)
			}
		})
	}
}

// SubFS tests

func TestSubFS(t *testing.T) {
	t.Parallel()

	parentFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"dir":                  {Mode: fs.ModeDir | 0755},
		"dir/file1.txt":        {Data: []byte("file1")},
		"dir/subdir":           {Mode: fs.ModeDir | 0755},
		"dir/subdir/file2.txt": {Data: []byte("file2")},
		"outside.txt":          {Data: []byte("outside")},
	})

	t.Run("create subfs", func(t *testing.T) {
		subFS, err := fs.Sub(parentFS, "dir")
		if err != nil {
			t.Fatalf("Sub failed: %v", err)
		}

		content, err := fs.ReadFile(subFS, "file1.txt")
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(content) != "file1" {
			t.Errorf("expected %q, got %q", "file1", string(content))
		}
	})

	t.Run("stat root of subfs", func(t *testing.T) {
		subFS, err := fs.Sub(parentFS, "dir")
		if err != nil {
			t.Fatalf("Sub failed: %v", err)
		}

		info, err := fs.Stat(subFS, ".")
		if err != nil {
			t.Fatalf("Stat(.) failed: %v", err)
		}
		if !info.IsDir() || info.Name() != "." {
			t.Errorf("expected directory named '.', got: %+v", info)
		}
	})

	t.Run("subfs error propagation", func(t *testing.T) {
		parentFS.AddStatError("dir/file1.txt", fs.ErrPermission)
		subFS, err := fs.Sub(parentFS, "dir")
		if err != nil {
			t.Fatalf("Sub failed: %v", err)
		}

		_, err = fs.Stat(subFS, "file1.txt")
		expectError(t, err, fs.ErrPermission)
	})

	t.Run("nested subfs", func(t *testing.T) {
		subFS, err := fs.Sub(parentFS, "dir")
		if err != nil {
			t.Fatalf("Sub(dir) failed: %v", err)
		}

		subSubFS, err := fs.Sub(subFS, "subdir")
		if err != nil {
			t.Fatalf("Sub(subdir) failed: %v", err)
		}

		content, err := fs.ReadFile(subSubFS, "file2.txt")
		if err != nil {
			t.Fatalf("ReadFile failed: %v", err)
		}
		if string(content) != "file2" {
			t.Errorf("expected %q, got %q", "file2", string(content))
		}
	})

	t.Run("sub on file", func(t *testing.T) {
		_, err := fs.Sub(parentFS, "dir/file1.txt")
		if err == nil {
			t.Error("Sub on file should fail")
		}
	})

	t.Run("sub on nonexistent", func(t *testing.T) {
		_, err := fs.Sub(parentFS, "nonexistent")
		expectError(t, err, fs.ErrNotExist)
	})
}

// File management tests

func TestFileManagement(t *testing.T) {
	t.Parallel()

	t.Run("add file string", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(nil)
		mockFS.AddFileString("test.txt", "content", 0644)

		info, err := mockFS.Stat("test.txt")
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if info.Size() != 7 || info.Mode().Perm() != 0644 {
			t.Errorf("unexpected file info: %+v", info)
		}
	})

	t.Run("add file bytes", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(nil)
		mockFS.AddFileBytes("data.bin", []byte{1, 2, 3}, 0600)

		info, err := mockFS.Stat("data.bin")
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if info.Size() != 3 || info.Mode().Perm() != 0600 {
			t.Errorf("unexpected file info: %+v", info)
		}
	})

	t.Run("add directory", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(nil)
		mockFS.AddDirectory("dir", 0755)

		info, err := mockFS.Stat("dir")
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if !info.IsDir() || info.Mode().Perm() != 0755 {
			t.Errorf("unexpected dir info: %+v", info)
		}
	})

	t.Run("remove path", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"test.txt": {Data: []byte("content")},
		})

		mockFS.RemovePath("test.txt")

		_, err := mockFS.Stat("test.txt")
		expectError(t, err, fs.ErrNotExist)
	})
}

// WritableFS tests

func TestWritableFS(t *testing.T) {
	t.Parallel()

	t.Run("mkdir", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"dir": {Mode: fs.ModeDir | 0755},
		})

		err := mockFS.Mkdir("dir/subdir", 0755)
		if err != nil {
			t.Fatalf("Mkdir failed: %v", err)
		}

		info, err := mockFS.Stat("dir/subdir")
		if err != nil || !info.IsDir() {
			t.Errorf("expected directory, got error: %v", err)
		}
	})

	t.Run("mkdir already exists", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"dir": {Mode: fs.ModeDir | 0755},
		})

		err := mockFS.Mkdir("dir", 0755)
		expectError(t, err, fs.ErrExist)
	})

	t.Run("mkdirall", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(nil)

		err := mockFS.MkdirAll("a/b/c", 0755)
		if err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}

		for _, path := range []string{"a", "a/b", "a/b/c"} {
			info, err := mockFS.Stat(path)
			if err != nil || !info.IsDir() {
				t.Errorf("expected %q to be directory, err: %v", path, err)
			}
		}
	})

	t.Run("remove file", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("content")},
		})

		err := mockFS.Remove("file.txt")
		if err != nil {
			t.Fatalf("Remove failed: %v", err)
		}

		_, err = mockFS.Stat("file.txt")
		expectError(t, err, fs.ErrNotExist)
	})

	t.Run("removeall", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"dir":          {Mode: fs.ModeDir | 0755},
			"dir/file.txt": {Data: []byte("content")},
			"dir/sub":      {Mode: fs.ModeDir | 0755},
		})

		err := mockFS.RemoveAll("dir")
		if err != nil {
			t.Fatalf("RemoveAll failed: %v", err)
		}

		for _, path := range []string{"dir", "dir/file.txt", "dir/sub"} {
			_, err := mockFS.Stat(path)
			expectError(t, err, fs.ErrNotExist)
		}
	})

	t.Run("rename file", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"old.txt": {Data: []byte("content")},
		})

		err := mockFS.Rename("old.txt", "new.txt")
		if err != nil {
			t.Fatalf("Rename failed: %v", err)
		}

		_, err = mockFS.Stat("old.txt")
		expectError(t, err, fs.ErrNotExist)

		content, err := mockFS.ReadFile("new.txt")
		if err != nil || string(content) != "content" {
			t.Errorf("expected renamed file with content, err: %v", err)
		}
	})

	t.Run("writefile", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(nil, mockfs.WithWritesEnabled(nil))

		err := mockFS.WriteFile("test.txt", []byte("new content"), 0644)
		if err != nil {
			t.Fatalf("WriteFile failed: %v", err)
		}

		content, err := mockFS.ReadFile("test.txt")
		if err != nil || string(content) != "new content" {
			t.Errorf("expected written content, err: %v", err)
		}
	})

	t.Run("writefile disabled", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(nil)

		err := mockFS.WriteFile("test.txt", []byte("content"), 0644)
		expectError(t, err, fs.ErrInvalid)
	})
}

// Write operations tests

func TestWriteOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		setupFS         func() *mockfs.MockFS
		filename        string
		writeData       []byte
		expectedContent string
		expectedError   error
	}{
		{
			name: "successful write",
			setupFS: func() *mockfs.MockFS {
				return mockfs.NewMockFS(map[string]*mockfs.MapFile{
					"test.txt": {Data: []byte("initial")},
				}, mockfs.WithWritesEnabled(nil))
			},
			filename:        "test.txt",
			writeData:       []byte("new"),
			expectedContent: "new",
		},
		{
			name: "write with error",
			setupFS: func() *mockfs.MockFS {
				m := mockfs.NewMockFS(map[string]*mockfs.MapFile{
					"test.txt": {Data: []byte("initial")},
				}, mockfs.WithWritesEnabled(nil))
				m.AddWriteError("test.txt", mockfs.ErrDiskFull)
				return m
			},
			filename:        "test.txt",
			writeData:       []byte("new"),
			expectedContent: "initial",
			expectedError:   mockfs.ErrDiskFull,
		},
		{
			name: "write disabled",
			setupFS: func() *mockfs.MockFS {
				return mockfs.NewMockFS(map[string]*mockfs.MapFile{
					"test.txt": {Data: []byte("initial")},
				})
			},
			filename:      "test.txt",
			writeData:     []byte("new"),
			expectedError: fs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := tt.setupFS()

			file, err := mockFS.Open(tt.filename)
			if err != nil {
				t.Fatalf("Open failed: %v", err)
			}
			defer file.Close()

			writer, ok := file.(io.Writer)
			if !ok {
				t.Fatal("file does not implement io.Writer")
			}

			_, err = writer.Write(tt.writeData)

			if tt.expectedError != nil {
				expectError(t, err, tt.expectedError)
			} else if err != nil {
				t.Fatalf("Write failed: %v", err)
			}

			content, _ := mockFS.ReadFile(tt.filename)
			if tt.expectedError == nil && string(content) != tt.expectedContent {
				t.Errorf("expected content %q, got %q", tt.expectedContent, string(content))
			}
		})
	}
}

// Closed file operations tests

func TestClosedFileOperations(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("data")},
	}, mockfs.WithWritesEnabled(nil))

	file, err := mockFS.Open("test.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	tests := []struct {
		name string
		op   func() error
	}{
		{
			name: "read on closed",
			op:   func() error { _, err := file.Read(make([]byte, 1)); return err },
		},
		{
			name: "write on closed",
			op: func() error {
				w, _ := file.(io.Writer)
				_, err := w.Write([]byte("x"))
				return err
			},
		},
		{
			name: "stat on closed",
			op:   func() error { _, err := file.Stat(); return err },
		},
		{
			name: "close again",
			op:   func() error { return file.Close() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op()
			expectError(t, err, fs.ErrClosed)
		})
	}
}

// MockFile Stat tests

func TestMockFileStat(t *testing.T) {
	t.Parallel()

	now := time.Now()
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("content"), ModTime: now},
	})

	file, err := mockFS.Open("file.txt")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	if info.Name() != "file.txt" {
		t.Errorf("expected name %q, got %q", "file.txt", info.Name())
	}
	if info.Size() != 7 {
		t.Errorf("expected size 7, got %d", info.Size())
	}
	if !info.ModTime().Equal(now) {
		t.Errorf("expected modtime %v, got %v", now, info.ModTime())
	}
}

// ReadFile error propagation tests

func TestReadFileErrorPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		wantErr error
	}{
		{
			name:    "open error",
			setup:   func(m *mockfs.MockFS) { m.AddOpenError("file.txt", fs.ErrPermission) },
			wantErr: fs.ErrPermission,
		},
		{
			name:    "read error",
			setup:   func(m *mockfs.MockFS) { m.AddReadError("file.txt", mockfs.ErrCorrupted) },
			wantErr: mockfs.ErrCorrupted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
				"file.txt": {Data: []byte("hello")},
			})
			tt.setup(mockFS)

			_, err := mockFS.ReadFile("file.txt")
			expectError(t, err, tt.wantErr)
		})
	}
}

// Custom injector tests

func TestWithInjector(t *testing.T) {
	t.Parallel()

	customInjector := mockfs.NewErrorManager()
	customInjector.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0)

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("content")},
	}, mockfs.WithInjector(customInjector))

	_, err := mockFS.Open("test.txt")
	expectError(t, err, mockfs.ErrTimeout)
}

// Concurrency tests

func TestConcurrentOperations(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file1.txt": {Data: []byte("1")},
		"file2.txt": {Data: []byte("2")},
		"file3.txt": {Data: []byte("3")},
	}, mockfs.WithLatency(1*time.Millisecond))

	mockFS.AddOpenErrorOnce("file1.txt", fs.ErrPermission)
	mockFS.AddReadErrorAfterN("file2.txt", mockfs.ErrCorrupted, 5)

	var wg sync.WaitGroup
	numGoroutines := 10
	numOpsPerG := 5

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < numOpsPerG; i++ {
				_, _ = mockFS.Stat("file1.txt")

				f1, err1 := mockFS.Open("file1.txt")
				if err1 == nil {
					_, _ = io.ReadAll(f1)
					_ = f1.Close()
				}

				f2, err2 := mockFS.Open("file2.txt")
				if err2 == nil {
					_, _ = io.ReadAll(f2)
					_ = f2.Close()
				}

				_, _ = mockFS.Stat("file3.txt")
			}
		}()
	}

	wg.Wait()

	stats := mockFS.GetStats()
	totalOps := numGoroutines * numOpsPerG
	if stats.Count(mockfs.OpStat) < totalOps {
		t.Errorf("expected at least %d stats, got %d", totalOps, stats.Count(mockfs.OpStat))
	}
}

// Empty filesystem tests

func TestEmptyFilesystem(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(nil)

	tests := []struct {
		name      string
		operation func() error
	}{
		{
			name:      "stat nonexistent",
			operation: func() error { _, err := mockFS.Stat("nonexistent.txt"); return err },
		},
		{
			name:      "open nonexistent",
			operation: func() error { _, err := mockFS.Open("nonexistent.txt"); return err },
		},
		{
			name:      "readfile nonexistent",
			operation: func() error { _, err := mockFS.ReadFile("nonexistent.txt"); return err },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.operation()
			expectError(t, err, fs.ErrNotExist)
		})
	}
}

// Complex scenario tests

func TestComplexScenario(t *testing.T) {
	t.Parallel()

	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"data":          {Mode: fs.ModeDir | 0755},
		"data/file.txt": {Data: []byte("content")},
	}, mockfs.WithWritesEnabled(nil))

	// Add directory
	mockFS.AddDirectory("logs", 0755)
	mockFS.AddFileString("logs/app.log", "log entry", 0644)

	// Test operations
	entries, err := mockFS.ReadDir("data")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}

	// Write to file
	file, err := mockFS.Open("logs/app.log")
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	writer, ok := file.(io.Writer)
	if !ok {
		t.Fatal("file does not implement io.Writer")
	}

	_, err = writer.Write([]byte(" appended"))
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	file.Close()

	// Verify
	content, err := mockFS.ReadFile("logs/app.log")
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(content) != " appended" {
		t.Errorf("expected ' appended', got %q", string(content))
	}

	// Remove
	err = mockFS.RemoveAll("logs")
	if err != nil {
		t.Fatalf("RemoveAll failed: %v", err)
	}

	_, err = mockFS.Stat("logs")
	expectError(t, err, fs.ErrNotExist)
}

// Edge cases

func TestEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("path cleaning", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"test.txt": {Data: []byte("content")},
		})

		_, err := mockFS.Stat("./test.txt")
		if err != nil {
			t.Errorf("cleaned path should work: %v", err)
		}
	})

	t.Run("empty read", func(t *testing.T) {
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"empty.txt": {Data: []byte{}},
		})

		content, err := mockFS.ReadFile("empty.txt")
		if err != nil {
			t.Fatalf("ReadFile empty failed: %v", err)
		}
		if len(content) != 0 {
			t.Errorf("expected empty content, got %d bytes", len(content))
		}
	})

	t.Run("large file", func(t *testing.T) {
		largeData := make([]byte, 1024*1024) // 1MB
		mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"large.bin": {Data: largeData},
		})

		content, err := mockFS.ReadFile("large.bin")
		if err != nil {
			t.Fatalf("ReadFile large failed: %v", err)
		}
		if len(content) != len(largeData) {
			t.Errorf("expected %d bytes, got %d", len(largeData), len(content))
		}
	})
}

// Benchmark tests

func BenchmarkMockFS_Open(b *testing.B) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("content")},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f, _ := mockFS.Open("test.txt")
		if f != nil {
			f.Close()
		}
	}
}

func BenchmarkMockFS_Stat(b *testing.B) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("content")},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mockFS.Stat("test.txt")
	}
}

func BenchmarkMockFS_ReadFile(b *testing.B) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("test content")},
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mockFS.ReadFile("test.txt")
	}
}

func BenchmarkMockFS_WithErrorInjection(b *testing.B) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("content")},
	})
	mockFS.AddStatError("other.txt", fs.ErrNotExist)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mockFS.Stat("test.txt")
	}
}

func BenchmarkMockFS_WithLatency(b *testing.B) {
	mockFS := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"test.txt": {Data: []byte("content")},
	}, mockfs.WithLatency(100*time.Nanosecond))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mockFS.Stat("test.txt")
	}
}
