package mockfs_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/balinomad/go-mockfs/v2"
)

// --- Helpers ---

// stringer implements fmt.Stringertype stringer string
type stringer string

func (s stringer) String() string { return string(s) }

// panicStringer panics when called, useful for testing lazy evaluation or nil checks.
type panicStringer struct{}

func (p *panicStringer) String() string {
	panic("should not be called")
}

// binaryMarshaler implements encoding.BinaryMarshaler
type binaryMarshaler struct {
	Data []byte
}

func (b *binaryMarshaler) MarshalBinary() ([]byte, error) {
	return b.Data, nil
}

// panicBinaryMarshaler implements encoding.BinaryMarshaler but panics on nil receiver.
type panicBinaryMarshaler struct{}

func (p *panicBinaryMarshaler) MarshalBinary() ([]byte, error) {
	// Intentionally cause a panic if the receiver is nil by dereferencing it.
	if p == nil {
		panic("nil receiver call to MarshalBinary")
	}
	return []byte{}, nil
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

// --- Constructor and Options ---

func TestNewMockFS(t *testing.T) {
	t.Parallel()

	customErr := errors.New("custom injector error")
	customInjector := mockfs.NewErrorInjector()
	customInjector.AddExact(mockfs.OpStat, "file.txt", customErr, mockfs.ErrorModeAlways, 0)

	tests := []struct {
		name  string
		opts  []mockfs.MockFSOption
		check func(t *testing.T, m *mockfs.MockFS)
	}{
		{
			name: "empty filesystem",
			check: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat(".")
				requireNoError(t, err)
			},
		},
		{
			name: "with initial files",
			opts: []mockfs.MockFSOption{
				mockfs.File("file.txt", "original"),
				mockfs.File("nil.txt", nil), // Should not be ignored
			},
			check: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "original" {
					t.Errorf("non-nil filecontent = %q, want %q", content, "original")
				}
				content = mustReadFile(t, m, "nil.txt")
				if string(content) != "" {
					t.Errorf("nil file content = %q, want empty", content)
				}
			},
		},
		{
			name: "with latency",
			opts: []mockfs.MockFSOption{
				mockfs.File("file.txt", "data"),
				mockfs.WithLatency(10 * time.Millisecond),
			},
			check: func(t *testing.T, m *mockfs.MockFS) {
				start := time.Now()
				_, _ = m.Stat("file.txt")
				if time.Since(start) < 5*time.Millisecond {
					t.Error("expected latency to be applied")
				}
			},
		},
		{
			name: "with custom injector",
			opts: []mockfs.MockFSOption{
				mockfs.File("file.txt", "data"),
				mockfs.WithErrorInjector(customInjector),
			},
			check: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat("file.txt")
				assertError(t, err, customErr)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS(tt.opts...)
			if mfs == nil {
				t.Fatal("NewMockFS returned nil")
			}
			tt.check(t, mfs)
		})
	}
}

func TestNewMockFS_Builder(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(
		mockfs.Dir("dir1", mockfs.FileMode(0o777)),
		mockfs.File("file1.txt", "1"),
		mockfs.Dir("dir2",
			mockfs.File("file2.txt", "2"),
		),
		mockfs.Dir("dir3", mockfs.FileMode(0o777),
			mockfs.File("file3.txt", "3"),
		),
	)
	if mfs == nil {
		t.Fatal("NewMockFS returned nil")
	}

	tests := []struct {
		path  string
		isDir bool
	}{
		{"dir1", true},
		{"dir2", true},
		{"dir3", true},
		{"file1.txt", false},
		{"dir2/file2.txt", false},
		{"dir3/file3.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			info, err := mfs.Stat(tt.path)
			requireNoError(t, err)
			if info.IsDir() != tt.isDir {
				t.Errorf("IsDir() = %v, want %v", info.IsDir(), tt.isDir)
			}
		})
	}
}

// TestNewMockFS_OptionPanic verifies that NewMockFS panics when provided with
// an invalid (empty) path via mockfs.File or mockfs.Dir.
func TestNewMockFS_OptionPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		option    mockfs.MockFSOption
		panicText string
	}{
		{
			name:      "empty file name",
			option:    mockfs.File("", "content"),
			panicText: "empty file name",
		},
		{
			name:      "invalid file name",
			option:    mockfs.File("/", "content"),
			panicText: "invalid name",
		},
		{
			name:      "empty dir name",
			option:    mockfs.Dir(""),
			panicText: "empty directory name",
		},
		{
			name:      "invalid dir name",
			option:    mockfs.Dir(".."),
			panicText: "invalid name",
		},
		{
			name:      "invalid dir argument",
			option:    mockfs.Dir("dir", 0),
			panicText: "invalid argument",
		},
		{
			name:      "dir with invalid child option",
			option:    mockfs.Dir("validdir", mockfs.File("", "content")),
			panicText: "empty file name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic")
				}

				panicStr, ok := r.(string)
				if !ok || !strings.Contains(panicStr, tt.panicText) {
					t.Errorf("panic = %q, want fragment %q", panicStr, tt.panicText)
				}
			}()

			_ = mockfs.NewMockFS(tt.option)
		})
	}
}

func TestNewMockFS_AutoCreateRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		opts  []mockfs.MockFSOption
		check func(*testing.T, *mockfs.MockFS)
	}{
		{
			name: "empty map",
			check: func(t *testing.T, m *mockfs.MockFS) {
				info, err := m.Stat(".")
				requireNoError(t, err)
				if !info.IsDir() || info.Mode()&fs.ModeDir == 0 {
					t.Error("root not created as directory")
				}
			},
		},
		{
			name: "files without root",
			opts: []mockfs.MockFSOption{
				mockfs.File("file.txt", "test"),
				mockfs.Dir("dir", mockfs.File("nested.txt", "nested")),
			},
			check: func(t *testing.T, m *mockfs.MockFS) {
				info, err := m.Stat(".")
				requireNoError(t, err)
				if !info.IsDir() {
					t.Error("root not created")
				}
				_, err = m.Stat("file.txt")
				requireNoError(t, err, "Stat(\"file.txt\")")
			},
		},
		{
			name: "nil entries",
			opts: []mockfs.MockFSOption{mockfs.File("file.txt", nil)},
			check: func(t *testing.T, m *mockfs.MockFS) {
				info, err := m.Stat(".")
				requireNoError(t, err, "Stat(\".\") with nil entries")
				if !info.IsDir() {
					t.Error("root not created with nil entries")
				}
			},
		},
		{
			name: "operations on root",
			opts: []mockfs.MockFSOption{mockfs.File("file.txt", "test")},
			check: func(t *testing.T, m *mockfs.MockFS) {
				dir, err := m.Open(".")
				requireNoError(t, err)
				defer dir.Close()

				entries, err := m.ReadDir(".")
				requireNoError(t, err)
				if len(entries) != 1 || entries[0].Name() != "file.txt" {
					t.Errorf("ReadDir returned %v", entries)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS(tt.opts...)
			tt.check(t, mfs)
		})
	}
}

func TestMockFS_Injector(t *testing.T) {
	t.Parallel()

	t.Run("default injector", func(t *testing.T) {
		mfs := mockfs.NewMockFS()
		if mfs.ErrorInjector() == nil {
			t.Error("default injector is nil")
		}
	})

	t.Run("custom injector", func(t *testing.T) {
		customInjector := mockfs.NewErrorInjector()
		mfs := mockfs.NewMockFS(mockfs.WithErrorInjector(customInjector))
		if mfs.ErrorInjector() != customInjector {
			t.Error("custom injector not used")
		}
	})
}

// --- FS Interface Methods ---

func TestMockFS_Stat(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(
		mockfs.File("file.txt", "content"),
		mockfs.Dir("dir"),
	)
	injectedErr := errors.New("injected")
	mfs.FailStat("file.txt", injectedErr)

	tests := []struct {
		name    string
		path    string
		wantErr error
		isDir   bool
	}{
		{
			name:    "existing file",
			path:    "dir",
			wantErr: nil,
			isDir:   true,
		},
		{
			name:    "non-existent",
			path:    "missing.txt",
			wantErr: fs.ErrNotExist,
		},
		{
			name:    "injected error",
			path:    "file.txt",
			wantErr: injectedErr,
		},
		{
			name:    "invalid path",
			path:    "../invalid",
			wantErr: fs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := mfs.Stat(tt.path)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			if info.IsDir() != tt.isDir {
				t.Errorf("IsDir() = %v, want %v", info.IsDir(), tt.isDir)
			}
		})
	}
}

func TestMockFS_Open(t *testing.T) {
	t.Parallel()

	injectedErr := errors.New("injected")

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		path    string
		wantErr error
		check   func(*testing.T, fs.File)
	}{
		{
			name: "success",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("a.txt", "data", 0o644)
			},
			path: "a.txt",
			check: func(t *testing.T, f fs.File) {
				content, err := io.ReadAll(f)
				requireNoError(t, err)
				if string(content) != "data" {
					t.Errorf("content = %q, want %q", content, "data")
				}
			},
		},
		{
			name:    "invalid path",
			path:    "../invalid",
			wantErr: mockfs.ErrInvalid,
		},
		{
			name: "injected error",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "data", 0o644)
				m.FailOpen("file.txt", injectedErr)
			},
			path:    "file.txt",
			wantErr: injectedErr,
		},
		{
			name:    "non-existent",
			path:    "missing.txt",
			wantErr: mockfs.ErrNotExist,
		},
		{
			name: "once error",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("other.txt", "data", 0o644)
				m.FailOpenOnce("other.txt", mockfs.ErrPermission)
			},
			path:    "other.txt",
			wantErr: mockfs.ErrPermission,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			if tt.setup != nil {
				tt.setup(mfs)
			}

			f, err := mfs.Open(tt.path)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			defer f.Close()
			if tt.check != nil {
				tt.check(t, f)
			}
		})
	}

	t.Run("once error then success", func(t *testing.T) {
		mfs := mockfs.NewMockFS()
		_ = mfs.AddFile("other.txt", "data", 0o644)
		mfs.FailOpenOnce("other.txt", mockfs.ErrPermission)

		_, err := mfs.Open("other.txt")
		assertError(t, err, mockfs.ErrPermission)

		_, err = mfs.Open("other.txt")
		requireNoError(t, err)
	})
}

func TestMockFS_ReadFile(t *testing.T) {
	t.Parallel()

	injectedErr := errors.New("injected")

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		path    string
		wantErr error
		want    string
	}{
		{
			name: "success",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("a.txt", "data", 0o644)
			},
			path: "a.txt",
			want: "data",
		},
		{
			name: "injected error",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "content", 0o644)
				m.FailRead("file.txt", injectedErr)
			},
			path:    "file.txt",
			wantErr: injectedErr,
		},
		{
			name: "directory",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0o755)
			},
			path: "dir",
			want: "",
		},
		{
			name:    "invalid path",
			path:    "../invalid",
			wantErr: mockfs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			if tt.setup != nil {
				tt.setup(mfs)
			}

			data, err := mfs.ReadFile(tt.path)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			if string(data) != tt.want {
				t.Errorf("content = %q, want %q", data, tt.want)
			}
		})
	}
}

func TestMockFS_ReadDir(t *testing.T) {
	t.Parallel()

	injectedErr := errors.New("injected")

	tests := []struct {
		name      string
		setup     func(*mockfs.MockFS)
		path      string
		wantErr   error
		wantCount int
		wantNames []string
	}{
		{
			name: "success",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddDir("d", 0o755)
				_ = m.AddFile("d/file1.txt", "1", 0o644)
				_ = m.AddFile("d/file2.txt", "2", 0o644)
			},
			path:      "d",
			wantCount: 2,
			wantNames: []string{"file1.txt", "file2.txt"},
		},
		{
			name: "injected error",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0o755)
				m.FailReadDir("dir", injectedErr)
			},
			path:    "dir",
			wantErr: injectedErr,
		},
		{
			name: "file not dir",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "data", 0o644)
			},
			path:    "file.txt",
			wantErr: mockfs.ErrNotDir,
		},
		{
			name:    "invalid path",
			path:    "../invalid",
			wantErr: mockfs.ErrInvalid,
		},
		{
			name:    "non-existent",
			path:    "nonexistent",
			wantErr: mockfs.ErrNotExist,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			if tt.setup != nil {
				tt.setup(mfs)
			}

			entries, err := mfs.ReadDir(tt.path)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			if len(entries) != tt.wantCount {
				t.Fatalf("len = %d, want %d", len(entries), tt.wantCount)
			}
			if tt.wantNames != nil {
				names := make(map[string]bool)
				for _, e := range entries {
					names[e.Name()] = true
				}
				for _, want := range tt.wantNames {
					if !names[want] {
						t.Errorf("missing entry %q", want)
					}
				}
			}
		})
	}
}

// --- SubFS ---

func TestMockFS_Sub(t *testing.T) {
	t.Parallel()

	mfsParent := mockfs.NewMockFS(
		mockfs.Dir("app",
			mockfs.File("config.json", "{}"),
			mockfs.Dir("src", mockfs.File("main.go", "package main")),
		),
		mockfs.File("other.txt", "..."),
		mockfs.File("file.txt", "data"),
	)
	injectedErr := errors.New("sub-error")
	// Inject errors to test if they are correctly scoped in the sub-fs
	mfsParent.FailOpen("app/src/main.go", injectedErr)
	mfsParent.FailStat("other.txt", injectedErr) // Should not be visible in sub-fs

	t.Run("happy path", func(t *testing.T) {
		mfsSub, err := mfsParent.Sub("app")
		requireNoError(t, err)

		// Check file existence and content
		content := mustReadFile(t, mfsSub.(fs.ReadFileFS), "config.json")
		if string(content) != "{}" {
			t.Error("unexpected content in subfs")
		}

		// Check that files outside the sub-fs are not accessible
		_, err = mfsSub.Open("other.txt")
		assertError(t, err, mockfs.ErrNotExist)

		_, err = mfsSub.Open("../other.txt") // Sub should prevent this
		assertError(t, err, mockfs.ErrInvalid)

		// Check that injector is cloned and path-adjusted
		_, err = mfsSub.Open("src/main.go")
		assertError(t, err, injectedErr)
	})

	t.Run("error cases", func(t *testing.T) {
		tests := []struct {
			name    string
			path    string
			wantErr error
		}{
			{"path not exist", "missing", mockfs.ErrNotExist},
			{"path is a file", "file.txt", mockfs.ErrNotDir},
			{"invalid path dot", ".", mockfs.ErrInvalid},
			{"invalid path slash", "/", mockfs.ErrInvalid},
			{"invalid path parent", "../", mockfs.ErrInvalid},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := mfsParent.Sub(tt.path)
				assertError(t, err, tt.wantErr)
			})
		}
	})
}

// --- File/Directory Management ---

func TestMockFS_AddFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setup       func(*mockfs.MockFS) error
		check       func(*testing.T, *mockfs.MockFS)
		wantErr     bool
		expectedErr error
	}{
		{
			name: "text file",
			setup: func(m *mockfs.MockFS) error {
				return m.AddFile("file.txt", "hello", 0o644)
			},
			check: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "hello" {
					t.Error("content mismatch")
				}
			},
		},
		{
			name: "binary file",
			setup: func(m *mockfs.MockFS) error {
				return m.AddFile("bin.dat", []byte{0, 1, 2}, 0o600)
			},
			check: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "bin.dat")
				if !bytes.Equal(content, []byte{0, 1, 2}) {
					t.Error("content mismatch")
				}
			},
		},
		{
			name: "invalid path",
			setup: func(m *mockfs.MockFS) error {
				return m.AddFile("../invalid.txt", "", 0)
			},
			wantErr:     true,
			expectedErr: fs.ErrInvalid,
		},
		{
			name: "trailing slash",
			setup: func(m *mockfs.MockFS) error {
				return m.AddFile("invalid/", "", 0)
			},
			wantErr:     true,
			expectedErr: fs.ErrInvalid,
		},
		{
			name: "parent is file",
			setup: func(m *mockfs.MockFS) error {
				_ = m.AddFile("blocked", "data", 0o644)
				return m.AddFile("blocked/child.txt", "content", 0o644)
			},
			wantErr:     true,
			expectedErr: mockfs.ErrNotDir,
		},
		{
			name: "target is directory",
			setup: func(m *mockfs.MockFS) error {
				_ = m.AddDir("dir", 0o755)
				return m.AddFile("dir", "content", 0o644)
			},
			wantErr:     true,
			expectedErr: mockfs.ErrIsDir,
		},
		{
			name: "invalid content",
			setup: func(m *mockfs.MockFS) error {
				var nilReader *bytes.Buffer
				return m.AddFile("bad.txt", nilReader, 0o644)
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			assertErrorWant(t, tt.setup(mfs), tt.wantErr, tt.expectedErr, "AddFile()")
			if !tt.wantErr && tt.check != nil {
				tt.check(t, mfs)
			}
		})
	}
}

func TestMockFS_AddDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		path    string
		mode    mockfs.FileMode
		wantErr error
	}{
		{"normal", "my/dir", 0o755, nil},
		{"invalid path", "/mydir", 0, fs.ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			err := mfs.AddDir(tt.path, tt.mode)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			info, _ := mfs.Stat(tt.path)
			if !info.IsDir() {
				t.Error("not a directory")
			}
		})
	}
}

func TestMockFS_RemoveEntry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		remove  string
		wantErr error
		check   func(*testing.T, *mockfs.MockFS)
	}{
		{
			name: "existing path",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
			},
			remove: "file.txt",
			check: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat("file.txt")
				assertError(t, err, mockfs.ErrNotExist)
			},
		},
		{
			name:   "non-existent",
			remove: "non-existent.txt",
		},
		{
			name:    "invalid path",
			remove:  "../invalid",
			wantErr: mockfs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			if tt.setup != nil {
				tt.setup(mfs)
			}
			err := mfs.RemoveEntry(tt.remove)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			if tt.check != nil {
				tt.check(t, mfs)
			}
		})
	}
}

// --- WritableFS Implementation ---

func TestMockFS_Mkdir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		path    string
		perm    mockfs.FileMode
		wantErr error
	}{
		{
			name:    "create new directory",
			setup:   func(m *mockfs.MockFS) {},
			path:    "newdir",
			perm:    0o755,
			wantErr: nil,
		},
		{
			name: "directory already exists",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddDir("existing", 0o755)
			},
			path:    "existing",
			perm:    0o755,
			wantErr: mockfs.ErrExist,
		},
		{
			name:    "parent does not exist",
			setup:   func(m *mockfs.MockFS) {},
			path:    "nonexistent/child",
			perm:    0o755,
			wantErr: mockfs.ErrNotExist,
		},
		{
			name: "parent is file",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
			},
			path:    "file.txt/child",
			perm:    0o755,
			wantErr: mockfs.ErrNotDir,
		},
		{
			name:    "invalid path dot",
			setup:   func(m *mockfs.MockFS) {},
			path:    ".",
			perm:    0o755,
			wantErr: mockfs.ErrInvalid,
		},
		{
			name:    "invalid path parent ref",
			setup:   func(m *mockfs.MockFS) {},
			path:    "../invalid",
			perm:    0o755,
			wantErr: mockfs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS(
				mockfs.Dir("dir",
					mockfs.File("file.txt", "content"),
					mockfs.Dir("empty_subdir"),
				),
				mockfs.File("file.txt", "root file"),
			)
			tt.setup(mfs)

			err := mfs.Mkdir(tt.path, tt.perm)
			assertError(t, err, tt.wantErr)

			if tt.wantErr == nil {
				info, statErr := mfs.Stat(tt.path)
				requireNoError(t, statErr, "stat after mkdir")
				if !info.IsDir() {
					t.Error("created path is not a directory")
				}
			}
		})
	}
}

func TestMockFS_MkdirAll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		path    string
		perm    mockfs.FileMode
		wantErr error
		check   func(*testing.T, *mockfs.MockFS)
	}{
		{
			name:    "create nested directories",
			setup:   func(m *mockfs.MockFS) {},
			path:    "new/nested/dir",
			perm:    0o755,
			wantErr: nil,
			check: func(t *testing.T, m *mockfs.MockFS) {
				for _, p := range []string{"new", "new/nested", "new/nested/dir"} {
					info, err := m.Stat(p)
					requireNoError(t, err, fmt.Sprintf("stat %s", p))
					if !info.IsDir() {
						t.Errorf("%s is not a directory", p)
					}
				}
			},
		},
		{
			name: "no error if exists as directory",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddDir("existing", 0o755)
			},
			path:    "existing",
			perm:    0o755,
			wantErr: nil,
		},
		{
			name: "error if part is file",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("dir/file.txt", "", 0o644)
			},
			path:    "dir/file.txt/child",
			perm:    0o755,
			wantErr: mockfs.ErrNotDir,
		},
		{
			name:    "invalid path",
			setup:   func(m *mockfs.MockFS) {},
			path:    "../invalid",
			perm:    0o755,
			wantErr: mockfs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			tt.setup(mfs)

			err := mfs.MkdirAll(tt.path, tt.perm)
			assertError(t, err, tt.wantErr)

			if tt.check != nil {
				tt.check(t, mfs)
			}
		})
	}
}

func TestMockFS_Remove(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		path    string
		wantErr error
		check   func(*testing.T, *mockfs.MockFS)
	}{
		{
			name: "remove file",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
			},
			path:    "file.txt",
			wantErr: nil,
			check: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat("file.txt")
				assertError(t, err, mockfs.ErrNotExist)
			},
		},
		{
			name: "remove empty directory",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddDir("empty", 0o755)
			},
			path:    "empty",
			wantErr: nil,
		},
		{
			name: "remove non-empty directory fails",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("dir/file.txt", "", 0o644)
			},
			path:    "dir",
			wantErr: mockfs.ErrNotEmpty,
		},
		{
			name:    "remove non-existent",
			setup:   func(m *mockfs.MockFS) {},
			path:    "missing.txt",
			wantErr: mockfs.ErrNotExist,
		},
		{
			name:    "invalid path",
			setup:   func(m *mockfs.MockFS) {},
			path:    "../invalid",
			wantErr: mockfs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			tt.setup(mfs)

			err := mfs.Remove(tt.path)
			assertError(t, err, tt.wantErr)

			if tt.check != nil {
				tt.check(t, mfs)
			}
		})
	}
}

func TestMockFS_RemoveAll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		path    string
		wantErr error
		check   func(*testing.T, *mockfs.MockFS)
	}{
		{
			name: "remove directory tree",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("dir/file.txt", "", 0o644)
				_ = m.AddFile("dir/sub/nested.txt", "", 0o644)
			},
			path:    "dir",
			wantErr: nil,
			check: func(t *testing.T, m *mockfs.MockFS) {
				for _, p := range []string{"dir", "dir/file.txt", "dir/sub", "dir/sub/nested.txt"} {
					_, err := m.Stat(p)
					assertError(t, err, mockfs.ErrNotExist, fmt.Sprintf("path %s", p))
				}
			},
		},
		{
			name:    "no error if path does not exist",
			setup:   func(m *mockfs.MockFS) {},
			path:    "missing",
			wantErr: nil,
		},
		{
			name:    "invalid path",
			setup:   func(m *mockfs.MockFS) {},
			path:    "../invalid",
			wantErr: mockfs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			tt.setup(mfs)

			err := mfs.RemoveAll(tt.path)
			assertError(t, err, tt.wantErr)

			if tt.check != nil {
				tt.check(t, mfs)
			}
		})
	}
}

func TestMockFS_Rename(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(*mockfs.MockFS)
		oldpath string
		newpath string
		wantErr error
		check   func(*testing.T, *mockfs.MockFS)
	}{
		{
			name: "rename file",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("old.txt", "content", 0o644)
			},
			oldpath: "old.txt",
			newpath: "new.txt",
			wantErr: nil,
			check: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat("old.txt")
				assertError(t, err, mockfs.ErrNotExist, "old path")

				content := mustReadFile(t, m, "new.txt")
				if string(content) != "content" {
					t.Errorf("content = %q, want %q", content, "content")
				}
			},
		},
		{
			name: "rename directory with children",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("olddir/file.txt", "data", 0o644)
				_ = m.AddFile("olddir/sub/nested.txt", "nested", 0o644)
			},
			oldpath: "olddir",
			newpath: "newdir",
			wantErr: nil,
			check: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat("olddir")
				assertError(t, err, mockfs.ErrNotExist, "old dir")

				content := mustReadFile(t, m, "newdir/file.txt")
				if string(content) != "data" {
					t.Error("file content not preserved")
				}

				content = mustReadFile(t, m, "newdir/sub/nested.txt")
				if string(content) != "nested" {
					t.Error("nested file content not preserved")
				}
			},
		},
		{
			name:    "rename non-existent",
			setup:   func(m *mockfs.MockFS) {},
			oldpath: "missing.txt",
			newpath: "new.txt",
			wantErr: mockfs.ErrNotExist,
		},
		{
			name:    "invalid old path",
			setup:   func(m *mockfs.MockFS) {},
			oldpath: "../invalid",
			newpath: "new.txt",
			wantErr: mockfs.ErrInvalid,
		},
		{
			name: "invalid new path",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("old.txt", "", 0o644)
			},
			oldpath: "old.txt",
			newpath: "../invalid",
			wantErr: mockfs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			tt.setup(mfs)

			err := mfs.Rename(tt.oldpath, tt.newpath)
			assertError(t, err, tt.wantErr)

			if tt.check != nil {
				tt.check(t, mfs)
			}
		})
	}
}

func TestMockFS_WriteFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    []mockfs.MockFSOption
		setup   func(*mockfs.MockFS)
		path    string
		data    []byte
		perm    mockfs.FileMode
		wantErr error
		check   func(*testing.T, *mockfs.MockFS)
	}{
		{
			name: "create new file with createIfMissing",
			opts: []mockfs.MockFSOption{mockfs.WithCreateIfMissing(true)},
			path: "new.txt",
			data: []byte("content"),
			perm: 0o644,
		},
		{
			name:    "fail without createIfMissing",
			opts:    nil,
			path:    "new.txt",
			data:    []byte("content"),
			perm:    0o644,
			wantErr: mockfs.ErrNotExist,
		},
		{
			name: "overwrite existing file",
			opts: []mockfs.MockFSOption{mockfs.WithOverwrite()},
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "old", 0o644)
			},
			path: "file.txt",
			data: []byte("new"),
			perm: 0o644,
			check: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "new" {
					t.Errorf("content = %q, want %q", content, "new")
				}
			},
		},
		{
			name: "append to existing file",
			opts: []mockfs.MockFSOption{mockfs.WithAppend()},
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "old", 0o644)
			},
			path: "file.txt",
			data: []byte("new"),
			perm: 0o644,
			check: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "oldnew" {
					t.Errorf("content = %q, want %q", content, "oldnew")
				}
			},
		},
		{
			name:    "fail in read-only mode",
			opts:    []mockfs.MockFSOption{mockfs.WithReadOnly()},
			path:    "file.txt",
			data:    []byte("data"),
			perm:    0o644,
			wantErr: mockfs.ErrPermission,
		},
		{
			name:    "invalid path",
			opts:    []mockfs.MockFSOption{mockfs.WithCreateIfMissing(true)},
			path:    "../invalid",
			data:    []byte("data"),
			perm:    0o644,
			wantErr: mockfs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS(tt.opts...)
			if tt.setup != nil {
				tt.setup(mfs)
			}

			err := mfs.WriteFile(tt.path, tt.data, tt.perm)
			assertError(t, err, tt.wantErr)

			if tt.check != nil {
				tt.check(t, mfs)
			}
		})
	}
}

func TestMockFS_WriteViaOpenedFile(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(mockfs.File("file.txt", "old"))
	file, err := mfs.Open("file.txt")
	requireNoError(t, err)

	writer, ok := file.(io.Writer)
	if !ok {
		t.Fatal("opened file does not implement io.Writer")
	}

	newData := []byte("updated")
	n, err := writer.Write(newData)
	requireNoError(t, err)
	if n != len(newData) {
		t.Errorf("wrote %d bytes, want %d", n, len(newData))
	}

	requireNoError(t, file.Close())

	content := mustReadFile(t, mfs, "file.txt")
	if string(content) != "updated" {
		t.Errorf("content = %q, want %q", content, "updated")
	}
}

// --- Error Injection and Stats ---

func TestMockFS_FailMethods(t *testing.T) {
	t.Parallel()

	injectedErr := errors.New("injected")

	tests := []struct {
		name      string
		setup     func(*mockfs.MockFS)       // Injects the error
		operation func(*mockfs.MockFS) error // Triggers the error
	}{
		{
			name:      "FailWrite",
			setup:     func(m *mockfs.MockFS) { m.FailWrite("file.txt", injectedErr) },
			operation: func(m *mockfs.MockFS) error { return m.WriteFile("file.txt", []byte("data"), 0o644) },
		},
		{
			name: "FailClose",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
				m.FailClose("file.txt", injectedErr)
			},
			operation: func(m *mockfs.MockFS) error {
				f, err := m.Open("file.txt")
				if err != nil {
					return err
				}
				return f.Close()
			},
		},
		{
			name:      "FailMkdir",
			setup:     func(m *mockfs.MockFS) { m.FailMkdir("dir", injectedErr) },
			operation: func(m *mockfs.MockFS) error { return m.Mkdir("dir", 0o755) },
		},
		{
			name:      "FailMkdirAll",
			setup:     func(m *mockfs.MockFS) { m.FailMkdirAll("dir/subdir", injectedErr) },
			operation: func(m *mockfs.MockFS) error { return m.MkdirAll("dir/subdir", 0o755) },
		},
		{
			name: "FailRemove",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
				m.FailRemove("file.txt", injectedErr)
			},
			operation: func(m *mockfs.MockFS) error { return m.Remove("file.txt") },
		},
		{
			name: "FailRemoveAll",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0o755)
				m.FailRemoveAll("dir", injectedErr)
			},
			operation: func(m *mockfs.MockFS) error { return m.RemoveAll("dir") },
		},
		{
			name: "FailRename",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("old.txt", "", 0o644)
				m.FailRename("old.txt", injectedErr)
			},
			operation: func(m *mockfs.MockFS) error { return m.Rename("old.txt", "new.txt") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			tt.setup(mfs)
			err := tt.operation(mfs)
			assertError(t, err, injectedErr)
		})
	}
}

func TestMockFS_ErrorInjection(t *testing.T) {
	t.Parallel()

	t.Run("fail after n successes", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.File("file.txt", "...read me..."))
		injectedErr := errors.New("read failed")
		mfs.FailReadAfter("file.txt", injectedErr, 2)

		file, err := mfs.Open("file.txt")
		requireNoError(t, err)
		defer file.Close()

		buf := make([]byte, 4)
		for i := 0; i < 2; i++ {
			_, err = file.Read(buf)
			requireNoError(t, err, fmt.Sprintf("read %d", i+1))
		}

		// Third read fails
		_, err = file.Read(buf)
		assertError(t, err, injectedErr, "read 3")

		_, err = file.Read(buf)
		assertError(t, err, injectedErr, "read 4")
	})

	t.Run("fail read next", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.File("flaky.txt", "datadatadatadata"))
		mfs.FailReadNext("flaky.txt", io.EOF, 3)

		file, _ := mfs.Open("flaky.txt")
		buf := make([]byte, 1)

		for i := 0; i < 3; i++ {
			_, err := file.Read(buf)
			assertError(t, err, io.EOF, fmt.Sprintf("read %d", i+1))
		}

		_, err := file.Read(buf)
		requireNoError(t, err, "read 4")
	})

	t.Run("mark non existent", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.File("file.txt", nil))
		mfs.MarkNonExistent("file.txt")

		// Path should be gone from the filesystem
		_, err := mfs.Stat("file.txt")
		assertError(t, err, mockfs.ErrNotExist, "stat")

		// Should also fail for other operations
		_, err = mfs.Open("file.txt")
		assertError(t, err, mockfs.ErrNotExist, "open")
	})

	t.Run("clear errors", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.File("file.txt", ""))
		mfs.FailStat("file.txt", mockfs.ErrPermission)

		_, err := mfs.Stat("file.txt")
		assertError(t, err, mockfs.ErrPermission)

		mfs.ClearErrors()

		_, err = mfs.Stat("file.txt")
		requireNoError(t, err)
	})
}

func TestMockFS_ErrorInjection_Once(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inject func(*mockfs.MockFS)
		op     func(*mockfs.MockFS) error
	}{
		{
			name:   "FailStatOnce",
			inject: func(m *mockfs.MockFS) { m.FailStatOnce("file.txt", mockfs.ErrPermission) },
			op:     func(m *mockfs.MockFS) error { _, err := m.Stat("file.txt"); return err },
		},
		{
			name: "FailReadOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "data", 0o644)
				m.FailReadOnce("file.txt", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				f, err := m.Open("file.txt")
				if err != nil {
					return err
				}
				defer f.Close()
				buf := make([]byte, 4)
				_, err = f.Read(buf)
				return err
			},
		},
		{
			name: "FailWriteOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
				m.FailWriteOnce("file.txt", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				return m.WriteFile("file.txt", []byte("data"), 0o644)
			},
		},
		{
			name: "FailReadDirOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0o755)
				m.FailReadDirOnce("dir", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				_, err := m.ReadDir("dir")
				return err
			},
		},
		{
			name: "FailCloseOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
				m.FailCloseOnce("file.txt", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				f, err := m.Open("file.txt")
				if err != nil {
					return err
				}
				return f.Close()
			},
		},
		{
			name:   "FailMkdirOnce",
			inject: func(m *mockfs.MockFS) { m.FailMkdirOnce("dir", mockfs.ErrPermission) },
			op:     func(m *mockfs.MockFS) error { return m.Mkdir("dir", 0o755) },
		},
		{
			name:   "FailMkdirAllOnce",
			inject: func(m *mockfs.MockFS) { m.FailMkdirAllOnce("dir/sub", mockfs.ErrPermission) },
			op:     func(m *mockfs.MockFS) error { return m.MkdirAll("dir/sub", 0o755) },
		},
		{
			name: "FailRemoveOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
				m.FailRemoveOnce("file.txt", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error { return m.Remove("file.txt") },
		},
		{
			name: "FailRemoveAllOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0o755)
				m.FailRemoveAllOnce("dir", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error { return m.RemoveAll("dir") },
		},
		{
			name: "FailRenameOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("old.txt", "", 0o644)
				m.FailRenameOnce("old.txt", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error { return m.Rename("old.txt", "new.txt") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			tt.inject(mfs)

			err := tt.op(mfs)
			assertError(t, err, mockfs.ErrPermission, "first call")

			err = tt.op(mfs)
			if err == fs.ErrPermission {
				t.Error("once error injection triggered twice")
			}
		})
	}
}

func TestMockFS_ErrorInjection_Next(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(mockfs.File("flaky.txt", "datadatadatadata"))

	// Next 3 reads fail, then succeed
	mfs.FailReadNext("flaky.txt", io.EOF, 3)

	f, _ := mfs.Open("flaky.txt")
	buf := make([]byte, 1)

	// First 3 reads fail
	for i := 0; i < 3; i++ {
		_, err := f.Read(buf)
		if err != io.EOF {
			t.Errorf("read %d: expected EOF, got %v", i+1, err)
		}
	}

	// 4th read succeeds
	_, err := f.Read(buf)
	if err != nil {
		t.Errorf("read 4: expected success, got %v", err)
	}
}

func TestMockFS_Stats(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(mockfs.File("a", ""), mockfs.File("b", ""))

	// Perform some operations
	_, _ = mfs.Stat("a")
	_, _ = mfs.Stat("a")
	file, _ := mfs.Open("b")
	if file != nil {
		_ = file.Close()
	}
	_ = mfs.Remove("a")

	stats := mfs.Stats()

	tests := []struct {
		op   mockfs.Operation
		want int
	}{
		{mockfs.OpStat, 2},
		{mockfs.OpOpen, 1},
		{mockfs.OpRemove, 1},
		{mockfs.OpMkdir, 0},
	}

	for _, tt := range tests {
		if got := stats.Count(tt.op); got != tt.want {
			t.Errorf("Count(%s) = %d, want %d", tt.op, got, tt.want)
		}
	}
}

func TestMockFS_Options(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opts   []mockfs.MockFSOption
		verify func(*testing.T, *mockfs.MockFS)
	}{
		{
			name: "with read only",
			opts: []mockfs.MockFSOption{mockfs.WithReadOnly()},
			verify: func(t *testing.T, m *mockfs.MockFS) {
				err := m.WriteFile("test.txt", []byte("data"), 0o644)
				assertError(t, err, mockfs.ErrPermission)
			},
		},
		{
			name: "with overwrite",
			opts: []mockfs.MockFSOption{
				mockfs.File("file.txt", "old"),
				mockfs.WithOverwrite(),
			},
			verify: func(t *testing.T, m *mockfs.MockFS) {
				err := m.WriteFile("file.txt", []byte("new"), 0o644)
				requireNoError(t, err)
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "new" {
					t.Errorf("content = %q, want %q", content, "new")
				}
			},
		},
		{
			name: "with append",
			opts: []mockfs.MockFSOption{
				mockfs.File("file.txt", "old"),
				mockfs.WithAppend(),
			},
			verify: func(t *testing.T, m *mockfs.MockFS) {
				err := m.WriteFile("file.txt", []byte("new"), 0o644)
				requireNoError(t, err)
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "oldnew" {
					t.Errorf("content = %q, want %q", content, "oldnew")
				}
			},
		},
		{
			name: "with latency simulator",
			opts: []mockfs.MockFSOption{
				mockfs.WithLatencySimulator(mockfs.NewLatencySimulator(10 * time.Millisecond)),
			},
			verify: func(t *testing.T, m *mockfs.MockFS) {
				start := time.Now()
				_, _ = m.Stat(".")
				if time.Since(start) < 5*time.Millisecond {
					t.Error("expected latency not applied")
				}
			},
		},
		{
			name: "with per operation latency",
			opts: []mockfs.MockFSOption{
				mockfs.WithPerOperationLatency(map[mockfs.Operation]time.Duration{
					mockfs.OpStat: 10 * time.Millisecond,
				}),
			},
			verify: func(t *testing.T, m *mockfs.MockFS) {
				start := time.Now()
				_, _ = m.Stat(".")
				if time.Since(start) < 5*time.Millisecond {
					t.Error("expected per-op latency not applied")
				}
			},
		},
		{
			name: "reset stats",
			opts: nil,
			verify: func(t *testing.T, m *mockfs.MockFS) {
				_, _ = m.Stat(".")
				m.ResetStats()
				if m.Stats().Count(mockfs.OpStat) != 0 {
					t.Error("ResetStats did not clear stats")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS(tt.opts...)
			tt.verify(t, mfs)
		})
	}
}

// --- Concurrency ---

func TestMockFS_Read_Write_Concurrent(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS()
	var wg sync.WaitGroup
	numGoroutines := 50

	// Pre-create files to avoid ErrNotExist
	for i := 0; i < numGoroutines; i++ {
		path := fmt.Sprintf("file-%d.txt", i)
		if err := mfs.AddFile(path, "", 0o644); err != nil {
			t.Fatalf("setup failed: %v", err)
		}
	}

	// Mix of reads, writes, and stats
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			path := fmt.Sprintf("file-%d.txt", id)
			content := []byte(fmt.Sprintf("content-%d", id))

			// Write a file
			err := mfs.WriteFile(path, content, 0o644)
			if err != nil {
				t.Errorf("concurrent WriteFile failed: %v", err)
				return
			}

			// Stat the file
			_, err = mfs.Stat(path)
			if err != nil {
				t.Errorf("concurrent Stat failed: %v", err)
				return
			}

			// Read it back
			readContent, err := mfs.ReadFile(path)
			if err != nil {
				t.Errorf("concurrent ReadFile failed: %v", err)
			} else if !bytes.Equal(content, readContent) {
				t.Errorf("content mismatch for %s", path)
			}
		}(i)
	}

	wg.Wait()

	// Final check
	stats := mfs.Stats()
	if stats.Count(mockfs.OpWrite) != numGoroutines {
		t.Errorf("expected %d writes, got %d", numGoroutines, stats.Count(mockfs.OpWrite))
	}
}

func TestMockFS_Remove_Rename_Concurrent(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS()
	var wg sync.WaitGroup
	numGoroutines := 20

	// Pre-populate with directories
	for i := 0; i < numGoroutines; i++ {
		_ = mfs.AddDir(path.Join("dir", fmt.Sprintf("sub-%d", i)), 0o755)
	}
	_ = mfs.AddDir("other_dir", 0o755)

	errorCount := int32(0)

	// Goroutine 1: Continuously renames a directory
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numGoroutines*2; i++ {
			oldName := "dir"
			newName := "other_dir"
			if i%2 == 1 {
				oldName, newName = newName, oldName
			}
			// Errors are expected as it races with RemoveAll
			_ = mfs.Rename(oldName, newName)
			time.Sleep(1 * time.Microsecond)
		}
	}()

	// Other goroutines: Remove subdirectories
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Try removing from either potential parent name
			err1 := mfs.RemoveAll(path.Join("dir", fmt.Sprintf("sub-%d", id)))
			err2 := mfs.RemoveAll(path.Join("other_dir", fmt.Sprintf("sub-%d", id)))

			// We don't care about errors here, just that it doesn't panic.
			// This test is purely for race condition detection.
			if err1 != nil && err2 != nil {
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	wg.Wait()
	// The test passes if it completes without the race detector firing.
	t.Logf("test finished with %d (expected) errors", atomic.LoadInt32(&errorCount))
}

func Test_toBytes(t *testing.T) {
	const fname = "testfile"

	// This panic message is expected from mockfs when toBytes returns an error.
	const expectedPanicPrefix = "mockfs: failed to apply option File: invalid content in " + fname

	// Typed nil pointers for testing panic safety
	var nilBuffer *bytes.Buffer
	var nilStringer *panicStringer
	var nilMarshaler *panicBinaryMarshaler

	tests := []struct {
		name    string
		content any
		want    []byte
		wantErr bool
	}{
		{"string", "hello", []byte("hello"), false},
		{"bytes", []byte{1, 2, 3}, []byte{1, 2, 3}, false},
		{"nil", nil, []byte{}, false},
		{"stringer", stringer("xyz"), []byte("xyz"), false},
		{"reader", bytes.NewBufferString("reader-data"), []byte("reader-data"), false},
		{"binary marshaler", &binaryMarshaler{Data: []byte{9, 8, 7}}, []byte{9, 8, 7}, false},
		{"int fallback", 42, []byte("42"), false},

		// Cases that test error/panic handling.
		{"typed nil reader (panic -> err)", nilBuffer, []byte{}, true},
		{"typed nil stringer (panic -> err)", nilStringer, []byte{}, true},
		{"typed nil marshaler (panic -> err)", nilMarshaler, []byte{}, true},
		{"reader with error", &errorReader{}, []byte{}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			var err error
			var recoveredPanic any

			// Function to wrap the execution, allowing us to defer a recover()
			// to catch the expected panic from mockfs.NewMockFS
			func() {
				defer func() {
					recoveredPanic = recover()
				}()

				mfs := mockfs.NewMockFS(mockfs.File(fname, tc.content))

				f, errOpen := mfs.Open(fname)
				if errOpen != nil {
					t.Fatalf("file open failed: %v", errOpen)
				}
				defer f.Close()

				got, err = io.ReadAll(f)
			}()

			// Check for recovered panic if error is expected
			if tc.wantErr {
				if recoveredPanic == nil {
					t.Fatalf("expected panic (error) during setup, but none occurred")
				}
				panicStr, ok := recoveredPanic.(string)
				if !ok || !strings.Contains(panicStr, expectedPanicPrefix) {
					t.Fatalf("recovered unexpected panic:\ngot: %v\nwant prefix: %q", recoveredPanic, expectedPanicPrefix)
				}
				// Since panic occurred during setup, we stop here
				return
			}

			// For successful cases, ensure no panic occurred
			if recoveredPanic != nil {
				t.Fatalf("unexpected panic during setup: %v", recoveredPanic)
			}

			if err != nil {
				t.Fatalf("file read failed: %v", err)
			}

			if !bytes.Equal(got, tc.want) {
				t.Errorf("content mismatch\ngot:  %q\nwant: %q", got, tc.want)
			}
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func Test_toBytes_MutationSafety(t *testing.T) {
	const fname = "mutable"

	// Create source slice
	src := []byte("original")

	// Initialize FS with source
	mfs := mockfs.NewMockFS(mockfs.File(fname, src))

	// Mutate source immediately after FS creation
	src[0] = 'X' // "Xriginal"

	// Read from FS
	file, err := mfs.Open(fname)
	requireNoError(t, err, "open file")
	defer file.Close()

	got, err := io.ReadAll(file)
	requireNoError(t, err, "read all")

	// Verify FS content was NOT affected by mutation
	expected := []byte("original")
	if !bytes.Equal(got, expected) {
		t.Errorf("mutation safety failure: got %q, want %q", got, expected)
	}
}
