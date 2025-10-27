package mockfs_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/balinomad/go-mockfs"
)

// --- Constructor and Options ---

func TestNewMockFS(t *testing.T) {
	t.Parallel()

	// initialData is used to test deep copy behavior.
	initialData := map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("original")},
		"nil.txt":  nil, // Should be ignored
	}
	customErr := errors.New("custom injector error")
	customInjector := mockfs.NewErrorInjector()
	customInjector.AddExact(mockfs.OpStat, "file.txt", customErr, mockfs.ErrorModeAlways, 0)

	tests := []struct {
		name         string
		initial      map[string]*mockfs.MapFile
		opts         []mockfs.MockFSOption
		postCreation func(t *testing.T, m *mockfs.MockFS)
	}{
		{
			name:    "empty filesystem",
			initial: nil,
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat(".")
				requireNoError(t, err)
			},
		},
		{
			name:    "with initial files",
			initial: initialData,
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "original" {
					t.Errorf("expected file content 'original', got %q", content)
				}
				// Check that nil entries are ignored
				_, err := m.Stat("nil.txt")
				assertError(t, err, fs.ErrNotExist)
			},
		},
		{
			name:    "deep copy verification",
			initial: initialData,
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
				// Modify the original map after creation
				initialData["file.txt"].Data = []byte("modified")
				initialData["new.txt"] = &mockfs.MapFile{Data: []byte("new")}

				// Content in MockFS should be unchanged
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "original" {
					t.Errorf("MockFS data was mutated externally, expected 'original', got %q", content)
				}
				// New file in original map should not appear in MockFS
				_, err := m.Stat("new.txt")
				assertError(t, err, fs.ErrNotExist)
			},
		},
		{
			name:    "with latency",
			initial: map[string]*mockfs.MapFile{"a": {Data: []byte("x")}},
			opts:    []mockfs.MockFSOption{mockfs.WithLatency(10 * time.Millisecond)},
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
				start := time.Now()
				_, _ = m.Stat("a")
				if time.Since(start) < 5*time.Millisecond {
					t.Error("expected latency to be applied")
				}
			},
		},
		{
			name: "with custom injector",
			opts: []mockfs.MockFSOption{mockfs.WithErrorInjector(customInjector)},
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat("file.txt")
				assertError(t, err, customErr)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mockfs.NewMockFS(tt.initial, tt.opts...)
			if m == nil {
				t.Fatal("NewMockFS returned nil")
			}
			tt.postCreation(t, m)
		})
	}
}

func TestNewMockFS_AutoCreateRoot_EmptyMap(t *testing.T) {
	t.Parallel()
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{})
	info, err := mfs.Stat(".")
	if err != nil {
		t.Fatalf("Stat(\".\") failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("auto-created root is not a directory")
	}
	if info.Mode()&fs.ModeDir == 0 {
		t.Error("auto-created root does not have ModeDir set")
	}
}

func TestNewMockFS_AutoCreateRoot_FilesWithoutRoot(t *testing.T) {
	t.Parallel()
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt":       {Data: []byte("test"), Mode: 0644},
		"dir/nested.txt": {Data: []byte("nested"), Mode: 0644},
	})
	info, err := mfs.Stat(".")
	if err != nil {
		t.Fatalf("Stat(\".\") failed: %v", err)
	}
	if !info.IsDir() {
		t.Error("auto-created root is not a directory")
	}
	_, err = mfs.Stat("file.txt")
	if err != nil {
		t.Errorf("file.txt should be accessible: %v", err)
	}
}

func TestNewMockFS_AutoCreateRoot_ExplicitNotOverwritten(t *testing.T) {
	t.Parallel()
	customTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		".":        {Mode: fs.ModeDir | 0700, ModTime: customTime},
		"file.txt": {Data: []byte("test"), Mode: 0644},
	})
	info, err := mfs.Stat(".")
	if err != nil {
		t.Fatalf("Stat(\".\") failed: %v", err)
	}
	if info.Mode().Perm() != 0700 {
		t.Errorf("explicit root permissions overwritten: got %v, want 0700", info.Mode().Perm())
	}
	if !info.ModTime().Equal(customTime) {
		t.Errorf("explicit root ModTime overwritten: got %v, want %v", info.ModTime(), customTime)
	}
}

func TestNewMockFS_AutoCreateRoot_NilEntries(t *testing.T) {
	t.Parallel()
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": nil,
	})
	info, err := mfs.Stat(".")
	if err != nil {
		t.Fatalf("Stat(\".\") failed with nil entries: %v", err)
	}
	if !info.IsDir() {
		t.Error("root not created when map has only nil entries")
	}
}

func TestNewMockFS_AutoCreateRoot_Operations(t *testing.T) {
	t.Parallel()
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("test"), Mode: 0644},
	})
	dir, err := mfs.Open(".")
	if err != nil {
		t.Fatalf("Open(\".\") failed: %v", err)
	}
	defer dir.Close()

	entries, err := mfs.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(\".\") failed: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("ReadDir(\".\") returned %d entries, want 1", len(entries))
	}
	if len(entries) > 0 && entries[0].Name() != "file.txt" {
		t.Errorf("ReadDir(\".\") entry name = %q, want \"file.txt\"", entries[0].Name())
	}
}

func TestMockFS_Injector(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(nil)
	if m.ErrorInjector() == nil {
		t.Error("Injector() returned nil for default injector")
	}

	customInjector := mockfs.NewErrorInjector()
	m2 := mockfs.NewMockFS(nil, mockfs.WithErrorInjector(customInjector))
	if m2.ErrorInjector() != customInjector {
		t.Error("Injector() did not return the provided custom injector")
	}
}

// --- FS Interface Methods ---

func TestMockFS_Stat(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("content"), Mode: 0644},
		"dir":      {Mode: fs.ModeDir | 0755},
	})
	injectedErr := errors.New("injected stat error")
	m.FailStat("file.txt", injectedErr)

	tests := []struct {
		name      string
		path      string
		wantErr   error
		wantIsDir bool
	}{
		{name: "stat existing file", path: "dir", wantErr: nil, wantIsDir: true},
		{name: "stat non-existent", path: "missing.txt", wantErr: fs.ErrNotExist},
		{name: "stat with injected error", path: "file.txt", wantErr: injectedErr},
		{name: "stat with invalid path", path: "../invalid", wantErr: fs.ErrInvalid},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := m.Stat(tt.path)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			if info.IsDir() != tt.wantIsDir {
				t.Errorf("expected IsDir to be %v, got %v", tt.wantIsDir, info.IsDir())
			}
		})
	}
}

func TestMockFS_Open(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("hello")},
	})
	injectedErr := errors.New("injected open error")
	m.FailOpen("file.txt", injectedErr)
	m.FailOpenOnce("other.txt", fs.ErrPermission) // Not in FS, but error should trigger

	// Test successful open
	t.Run("open and read file", func(t *testing.T) {
		mSuccess := mockfs.NewMockFS(map[string]*mockfs.MapFile{"a.txt": {Data: []byte("data")}})
		f, err := mSuccess.Open("a.txt")
		requireNoError(t, err)
		defer f.Close()

		content, err := io.ReadAll(f)
		requireNoError(t, err)
		if string(content) != "data" {
			t.Errorf("content mismatch: got %q, want %q", content, "data")
		}
	})

	t.Run("open with invalid path", func(t *testing.T) {
		_, err := m.Open("../invalid")
		assertError(t, err, fs.ErrInvalid)
	})

	// Test error cases
	t.Run("open with injected error", func(t *testing.T) {
		_, err := m.Open("file.txt")
		assertError(t, err, injectedErr)
	})

	t.Run("open non-existent", func(t *testing.T) {
		_, err := m.Open("missing.txt")
		assertError(t, err, fs.ErrNotExist)
	})

	t.Run("open with injected error once", func(t *testing.T) {
		_, err := m.Open("other.txt")
		assertError(t, err, fs.ErrPermission)
		// Second attempt should be different (not exist in this case)
		_, err = m.Open("other.txt")
		assertError(t, err, fs.ErrNotExist)
	})
}

func TestMockFS_ReadFile(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("content")},
		"dir":      {Mode: fs.ModeDir},
	})
	injectedErr := errors.New("injected read error")
	m.FailRead("file.txt", injectedErr) // FailRead affects operations inside ReadFile

	t.Run("read success", func(t *testing.T) {
		mSuccess := mockfs.NewMockFS(map[string]*mockfs.MapFile{"a.txt": {Data: []byte("data")}})
		content, err := mSuccess.ReadFile("a.txt")
		requireNoError(t, err)
		if string(content) != "data" {
			t.Errorf("content mismatch")
		}
	})

	t.Run("read with injected error", func(t *testing.T) {
		_, err := m.ReadFile("file.txt")
		assertError(t, err, injectedErr)
	})

	t.Run("read a directory", func(t *testing.T) {
		// MapFS behaviour: ReadFile on directory returns empty data, no error
		data, err := m.ReadFile("dir")
		requireNoError(t, err)
		if len(data) != 0 {
			t.Errorf("expected empty data for directory, got %d bytes", len(data))
		}
	})

	t.Run("read with invalid path", func(t *testing.T) {
		_, err := m.ReadFile("../invalid")
		assertError(t, err, fs.ErrInvalid)
	})
}

func TestMockFS_ReadDir(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"dir":              {Mode: fs.ModeDir},
		"dir/file1.txt":    {Data: []byte("1")},
		"dir/sub":          {Mode: fs.ModeDir},
		"dir/sub/file2.go": {Data: []byte("2")},
		"file.txt":         {Data: []byte("not a dir")},
	})
	injectedErr := errors.New("injected readdir error")
	m.FailReadDir("dir", injectedErr)

	t.Run("readdir success", func(t *testing.T) {
		mSuccess := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"d":        {Mode: fs.ModeDir},
			"d/f1.txt": {Data: []byte("1")},
			"d/f2.txt": {Data: []byte("2")},
		})
		entries, err := mSuccess.ReadDir("d")
		requireNoError(t, err)
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
		names := map[string]bool{entries[0].Name(): true, entries[1].Name(): true}
		if !names["f1.txt"] || !names["f2.txt"] {
			t.Errorf("unexpected entry names")
		}
	})

	t.Run("readdir with injected error", func(t *testing.T) {
		_, err := m.ReadDir("dir")
		assertError(t, err, injectedErr)
	})

	t.Run("readdir on a file", func(t *testing.T) {
		_, err := m.ReadDir("file.txt")
		if err == nil {
			t.Error("expected an error when reading dir on a file")
		}
	})

	t.Run("readdir with invalid path", func(t *testing.T) {
		_, err := m.ReadDir("../invalid")
		assertError(t, err, fs.ErrInvalid)
	})
}

// --- SubFS ---

func TestMockFS_Sub(t *testing.T) {
	t.Parallel()

	parent := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"app":             {Mode: fs.ModeDir | 0755},
		"app/config.json": {Data: []byte("{}")},
		"app/src":         {Mode: fs.ModeDir | 0755},
		"app/src/main.go": {Data: []byte("package main")},
		"other.txt":       {Data: []byte("...")},
		"not-a-dir.txt":   {Data: []byte("file")},
	})

	injectedErr := errors.New("sub-error")
	// Inject errors to test if they are correctly scoped in the sub-fs
	parent.FailOpen("app/src/main.go", injectedErr)
	parent.FailStat("other.txt", injectedErr) // Should not be visible in sub-fs

	t.Run("subfs happy path", func(t *testing.T) {
		sub, err := parent.Sub("app")
		requireNoError(t, err)

		// Check file existence and content
		content := mustReadFile(t, sub.(fs.ReadFileFS), "config.json")
		if string(content) != "{}" {
			t.Errorf("unexpected content in subfs file")
		}

		// Check that files outside the sub-fs are not accessible
		_, err = sub.Open("other.txt")
		assertError(t, err, fs.ErrNotExist)
		_, err = sub.Open("../other.txt") // Sub should prevent this
		assertError(t, err, fs.ErrInvalid)

		// Check that injector is cloned and path-adjusted
		_, err = sub.Open("src/main.go")
		assertError(t, err, injectedErr)
	})

	t.Run("subfs error cases", func(t *testing.T) {
		tests := []struct {
			name    string
			path    string
			wantErr error
		}{
			{"path not exist", "missing", fs.ErrNotExist},
			{"path is a file", "not-a-dir.txt", mockfs.ErrNotDir},
			{"invalid path dot", ".", fs.ErrInvalid},
			{"invalid path slash", "/", fs.ErrInvalid},
			{"invalid path parent", "../", fs.ErrInvalid},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := parent.Sub(tt.path)
				assertError(t, err, tt.wantErr)
			})
		}
	})
}

// --- File/Directory Management Helpers ---

func TestMockFS_AddFileAndDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(m *mockfs.MockFS) error
		verify  func(t *testing.T, m *mockfs.MockFS)
		wantErr error
	}{
		{
			name: "add text file",
			setup: func(m *mockfs.MockFS) error {
				return m.AddFile("file.txt", "hello", 0644)
			},
			verify: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "hello" {
					t.Errorf("content mismatch")
				}
			},
		},
		{
			name: "add binary file",
			setup: func(m *mockfs.MockFS) error {
				return m.AddFileBytes("bin.dat", []byte{0, 1, 2}, 0600)
			},
			verify: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "bin.dat")
				if !bytes.Equal(content, []byte{0, 1, 2}) {
					t.Errorf("content mismatch")
				}
			},
		},
		{
			name: "add directory",
			setup: func(m *mockfs.MockFS) error {
				return m.AddDir("my/dir", 0755)
			},
			verify: func(t *testing.T, m *mockfs.MockFS) {
				info, err := m.Stat("my/dir")
				requireNoError(t, err)
				if !info.IsDir() {
					t.Error("expected path to be a directory")
				}
			},
		},
		{
			name:    "add file with invalid path",
			setup:   func(m *mockfs.MockFS) error { return m.AddFile("../invalid.txt", "", 0) },
			wantErr: fs.ErrInvalid,
		},
		{
			name:    "add file with trailing slash",
			setup:   func(m *mockfs.MockFS) error { return m.AddFile("invalid/", "", 0) },
			wantErr: fs.ErrInvalid,
		},
		{
			name:    "add dir with invalid path",
			setup:   func(m *mockfs.MockFS) error { return m.AddDir(".", 0) },
			wantErr: fs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mockfs.NewMockFS(nil)
			err := tt.setup(m)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			tt.verify(t, m)
		})
	}
}

func TestMockFS_RemovePath(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(map[string]*mockfs.MapFile{"file.txt": {}})

	t.Run("remove existing path", func(t *testing.T) {
		err := m.RemovePath("file.txt")
		requireNoError(t, err)
		_, err = m.Stat("file.txt")
		assertError(t, err, fs.ErrNotExist)
	})

	t.Run("remove non-existent path", func(t *testing.T) {
		// Should not return an error
		err := m.RemovePath("non-existent.txt")
		requireNoError(t, err)
	})

	t.Run("remove with invalid path", func(t *testing.T) {
		err := m.RemovePath("../invalid")
		assertError(t, err, fs.ErrInvalid)
	})
}

// --- WritableFS Implementation ---

func TestMockFS_WritableFS(t *testing.T) {
	t.Parallel()

	// Base filesystem for mutation tests
	setup := func() *mockfs.MockFS {
		return mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"dir":              {Mode: fs.ModeDir | 0755},
			"dir/file.txt":     {Data: []byte("content")},
			"dir/empty_subdir": {Mode: fs.ModeDir | 0755},
			"file.txt":         {Data: []byte("root file")},
		})
	}

	// Mkdir / MkdirAll
	t.Run("mkdir", func(t *testing.T) {
		m := setup()
		err := m.Mkdir("dir/new_dir", 0755)
		requireNoError(t, err)
		info, _ := m.Stat("dir/new_dir")
		if !info.IsDir() {
			t.Error("new_dir not created or not a directory")
		}

		// Error cases
		assertError(t, m.Mkdir("dir/new_dir", 0), fs.ErrExist)
		assertError(t, m.Mkdir("nonexistent/dir", 0), fs.ErrNotExist)
		assertError(t, m.Mkdir("dir/file.txt/fail", 0), mockfs.ErrNotDir)
	})

	t.Run("mkdirall", func(t *testing.T) {
		m := setup()
		err := m.MkdirAll("new/nested/dir", 0755)
		requireNoError(t, err)
		info, _ := m.Stat("new/nested/dir")
		if !info.IsDir() {
			t.Error("nested dir not created or not a directory")
		}

		// No error if path already exists and is a directory
		err = m.MkdirAll("dir", 0755)
		requireNoError(t, err)

		// Error if part of the path is a file
		assertError(t, m.MkdirAll("dir/file.txt/fail", 0), mockfs.ErrNotDir)
	})

	// Remove / RemoveAll
	t.Run("remove", func(t *testing.T) {
		m := setup()
		err := m.Remove("file.txt")
		requireNoError(t, err)
		_, err = m.Stat("file.txt")
		assertError(t, err, fs.ErrNotExist)

		// Error cases
		assertError(t, m.Remove("missing.txt"), fs.ErrNotExist)
		assertError(t, m.Remove("dir"), mockfs.ErrNotEmpty)

		// Remove empty dir should succeed
		err = m.Remove("dir/empty_subdir")
		requireNoError(t, err)
	})

	t.Run("remove all", func(t *testing.T) {
		m := setup()
		err := m.RemoveAll("dir")
		requireNoError(t, err)
		_, err = m.Stat("dir")
		assertError(t, err, fs.ErrNotExist)
		_, err = m.Stat("dir/file.txt")
		assertError(t, err, fs.ErrNotExist)

		// No error if path does not exist
		err = m.RemoveAll("missing_dir")
		requireNoError(t, err)
	})

	// Rename
	t.Run("rename", func(t *testing.T) {
		m := setup()
		// Rename file
		err := m.Rename("file.txt", "renamed.txt")
		requireNoError(t, err)
		_, err = m.Stat("file.txt")
		assertError(t, err, fs.ErrNotExist)
		content := mustReadFile(t, m, "renamed.txt")
		if string(content) != "root file" {
			t.Error("renamed file content mismatch")
		}

		// Rename directory
		err = m.Rename("dir", "renamed_dir")
		requireNoError(t, err)
		_, err = m.Stat("dir")
		assertError(t, err, fs.ErrNotExist)
		_, err = m.Stat("renamed_dir/file.txt")
		requireNoError(t, err)

		// Error case
		assertError(t, m.Rename("missing.txt", "new.txt"), fs.ErrNotExist)
	})

	// WriteFile and Write via Open
	t.Run("write file", func(t *testing.T) {
		m := setup()

		// Use createIfMissing to create a new file
		m2 := mockfs.NewMockFS(nil, mockfs.WithCreateIfMissing(true))
		newData := []byte("new data")
		err := m2.WriteFile("newfile.txt", newData, 0644)
		requireNoError(t, err)
		content := mustReadFile(t, m2, "newfile.txt")
		if !bytes.Equal(content, newData) {
			t.Error("written content mismatch")
		}

		// Overwrite existing file (works regardless of createIfMissing)
		err = m.WriteFile("file.txt", newData, 0644)
		requireNoError(t, err)
		content = mustReadFile(t, m, "file.txt")
		if !bytes.Equal(content, newData) {
			t.Error("overwritten content mismatch")
		}
	})

	t.Run("overwrite via opened file", func(t *testing.T) {
		m := setup() // has "file.txt" with "root file"
		f, err := m.Open("file.txt")
		requireNoError(t, err)

		writer, ok := f.(io.Writer)
		if !ok {
			t.Fatal("opened file does not implement io.Writer")
		}

		updatedData := []byte("updated content")
		n, err := writer.Write(updatedData)
		requireNoError(t, err)
		if n != len(updatedData) {
			t.Errorf("wrote %d bytes, want %d", n, len(updatedData))
		}
		err = f.Close()
		requireNoError(t, err)

		// Re-read and verify content
		content := mustReadFile(t, m, "file.txt")
		if !bytes.Equal(content, updatedData) {
			t.Errorf("content mismatch, got %q want %q", content, updatedData)
		}
	})
}
func TestMockFS_WriteFile_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("write to read-only filesystem", func(t *testing.T) {
		mfs := mockfs.NewMockFS(nil, mockfs.WithReadOnly())
		err := mfs.WriteFile("test.txt", []byte("data"), 0644)
		assertError(t, err, fs.ErrPermission)
	})

	t.Run("write without createIfMissing", func(t *testing.T) {
		mfs := mockfs.NewMockFS(nil)
		err := mfs.WriteFile("test.txt", []byte("data"), 0644)
		assertError(t, err, fs.ErrNotExist)
	})

	t.Run("overwrite existing in overwrite mode", func(t *testing.T) {
		mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("old")},
		}, mockfs.WithOverwrite())
		err := mfs.WriteFile("file.txt", []byte("new"), 0644)
		requireNoError(t, err)
		content := mustReadFile(t, mfs, "file.txt")
		if string(content) != "new" {
			t.Errorf("content = %q, want %q", content, "new")
		}
	})

	t.Run("append to existing in append mode", func(t *testing.T) {
		mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("old")},
		}, mockfs.WithAppend())
		err := mfs.WriteFile("file.txt", []byte("new"), 0644)
		requireNoError(t, err)
		content := mustReadFile(t, mfs, "file.txt")
		if string(content) != "oldnew" {
			t.Errorf("content = %q, want %q", content, "oldnew")
		}
	})
}

// --- Error Injection and Stats ---

func TestMockFS_FailMethods(t *testing.T) {
	t.Parallel()
	injectedErr := errors.New("injected")

	// Each test case sets up a mockfs, applies a specific Fail method,
	// and then executes the corresponding operation to check if the error is returned.
	testCases := []struct {
		name      string
		setup     func(m *mockfs.MockFS)       // Injects the error
		operation func(m *mockfs.MockFS) error // Triggers the error
	}{
		{
			name:      "FailWrite",
			setup:     func(m *mockfs.MockFS) { m.FailWrite("file.txt", injectedErr) },
			operation: func(m *mockfs.MockFS) error { return m.WriteFile("file.txt", []byte("data"), 0644) },
		},
		{
			name: "FailClose",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0644)
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
			operation: func(m *mockfs.MockFS) error { return m.Mkdir("dir", 0755) },
		},
		{
			name:      "FailMkdirAll",
			setup:     func(m *mockfs.MockFS) { m.FailMkdirAll("dir/subdir", injectedErr) },
			operation: func(m *mockfs.MockFS) error { return m.MkdirAll("dir/subdir", 0755) },
		},
		{
			name: "FailRemove",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0644)
				m.FailRemove("file.txt", injectedErr)
			},
			operation: func(m *mockfs.MockFS) error { return m.Remove("file.txt") },
		},
		{
			name: "FailRemoveAll",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0755)
				m.FailRemoveAll("dir", injectedErr)
			},
			operation: func(m *mockfs.MockFS) error { return m.RemoveAll("dir") },
		},
		{
			name: "FailRename",
			setup: func(m *mockfs.MockFS) {
				_ = m.AddFile("old.txt", "", 0644)
				m.FailRename("old.txt", injectedErr)
			},
			operation: func(m *mockfs.MockFS) error { return m.Rename("old.txt", "new.txt") },
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := mockfs.NewMockFS(nil)
			tc.setup(m)
			err := tc.operation(m)
			assertError(t, err, injectedErr)
		})
	}
}

func TestMockFS_ErrorInjection(t *testing.T) {
	t.Parallel()

	t.Run("fail after n successes", func(t *testing.T) {
		m := mockfs.NewMockFS(map[string]*mockfs.MapFile{"file.txt": {Data: []byte("...read me...")}})
		injectedErr := errors.New("read failed")
		m.FailReadAfter("file.txt", injectedErr, 2)

		f, err := m.Open("file.txt")
		requireNoError(t, err)
		defer f.Close()

		// First two reads succeed
		buf := make([]byte, 4)
		_, err = f.Read(buf)
		requireNoError(t, err)
		_, err = f.Read(buf)
		requireNoError(t, err)

		// Third read fails
		_, err = f.Read(buf)
		assertError(t, err, injectedErr)

		// It should keep failing
		_, err = f.Read(buf)
		assertError(t, err, injectedErr)
	})

	t.Run("mark non existent", func(t *testing.T) {
		m := mockfs.NewMockFS(map[string]*mockfs.MapFile{"file.txt": {}})
		m.MarkNonExistent("file.txt")

		// Path should be gone from the filesystem
		_, err := m.Stat("file.txt")
		assertError(t, err, fs.ErrNotExist)

		// Should also fail for other operations
		_, err = m.Open("file.txt")
		assertError(t, err, fs.ErrNotExist)
	})

	t.Run("clear errors", func(t *testing.T) {
		m := mockfs.NewMockFS(map[string]*mockfs.MapFile{"file.txt": {}})
		m.FailStat("file.txt", fs.ErrPermission)

		_, err := m.Stat("file.txt")
		assertError(t, err, fs.ErrPermission)

		m.ClearErrors()
		_, err = m.Stat("file.txt")
		requireNoError(t, err)
	})
}

func TestMockFS_ErrorInjectionOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inject func(m *mockfs.MockFS)
		op     func(m *mockfs.MockFS) error
	}{
		{
			name:   "FailStatOnce",
			inject: func(m *mockfs.MockFS) { m.FailStatOnce("file.txt", fs.ErrPermission) },
			op: func(m *mockfs.MockFS) error {
				_, err := m.Stat("file.txt")
				return err
			},
		},
		{
			name: "FailReadOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "data", 0644)
				m.FailReadOnce("file.txt", fs.ErrPermission)
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
				_ = m.AddFile("file.txt", "", 0644)
				m.FailWriteOnce("file.txt", fs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				return m.WriteFile("file.txt", []byte("data"), 0644)
			},
		},
		{
			name: "FailReadDirOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0755)
				m.FailReadDirOnce("dir", fs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				_, err := m.ReadDir("dir")
				return err
			},
		},
		{
			name: "FailCloseOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0644)
				m.FailCloseOnce("file.txt", fs.ErrPermission)
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
			inject: func(m *mockfs.MockFS) { m.FailMkdirOnce("dir", fs.ErrPermission) },
			op: func(m *mockfs.MockFS) error {
				return m.Mkdir("dir", 0755)
			},
		},
		{
			name:   "FailMkdirAllOnce",
			inject: func(m *mockfs.MockFS) { m.FailMkdirAllOnce("dir/sub", fs.ErrPermission) },
			op: func(m *mockfs.MockFS) error {
				return m.MkdirAll("dir/sub", 0755)
			},
		},
		{
			name: "FailRemoveOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0644)
				m.FailRemoveOnce("file.txt", fs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				return m.Remove("file.txt")
			},
		},
		{
			name: "FailRemoveAllOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0755)
				m.FailRemoveAllOnce("dir", fs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				return m.RemoveAll("dir")
			},
		},
		{
			name: "FailRenameOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("old.txt", "", 0644)
				m.FailRenameOnce("old.txt", fs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				return m.Rename("old.txt", "new.txt")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mockfs.NewMockFS(nil)
			tt.inject(m)

			err := tt.op(m)
			assertError(t, err, fs.ErrPermission)

			err = tt.op(m)
			if err == fs.ErrPermission {
				t.Error("Once error injection triggered twice")
			}
		})
	}
}

func TestMockFS_Stats(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"a": {}, "b": {},
	})

	// Perform some operations
	_, _ = m.Stat("a")
	_, _ = m.Stat("a")
	f, _ := m.Open("b")
	if f != nil {
		_ = f.Close()
	}
	_ = m.Remove("a")

	stats := m.Stats()
	if stats.Count(mockfs.OpStat) != 2 {
		t.Errorf("expected 2 stat operations, got %d", stats.Count(mockfs.OpStat))
	}
	if stats.Count(mockfs.OpOpen) != 1 {
		t.Errorf("expected 1 open operation, got %d", stats.Count(mockfs.OpOpen))
	}
	if stats.Count(mockfs.OpRemove) != 1 {
		t.Errorf("expected 1 remove operation, got %d", stats.Count(mockfs.OpRemove))
	}
	// Check a zero-count operation
	if stats.Count(mockfs.OpMkdir) != 0 {
		t.Errorf("expected 0 mkdir operations, got %d", stats.Count(mockfs.OpMkdir))
	}
}

func TestMockFS_OptionsAndHelpers(t *testing.T) {
	t.Parallel()

	t.Run("WithReadOnly", func(t *testing.T) {
		mfs := mockfs.NewMockFS(nil, mockfs.WithReadOnly())
		err := mfs.WriteFile("test.txt", []byte("data"), 0644)
		assertError(t, err, fs.ErrPermission)
	})

	t.Run("WithOverwrite", func(t *testing.T) {
		mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("old")},
		}, mockfs.WithOverwrite())
		err := mfs.WriteFile("file.txt", []byte("new"), 0644)
		requireNoError(t, err)
		content := mustReadFile(t, mfs, "file.txt")
		if string(content) != "new" {
			t.Errorf("content = %q, want %q", content, "new")
		}
	})

	t.Run("WithAppend", func(t *testing.T) {
		mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"file.txt": {Data: []byte("old")},
		}, mockfs.WithAppend())
		err := mfs.WriteFile("file.txt", []byte("new"), 0644)
		requireNoError(t, err)
		content := mustReadFile(t, mfs, "file.txt")
		if string(content) != "oldnew" {
			t.Errorf("content = %q, want %q", content, "oldnew")
		}
	})

	t.Run("WithLatencySimulator", func(t *testing.T) {
		sim := mockfs.NewLatencySimulator(10 * time.Millisecond)
		mfs := mockfs.NewMockFS(nil, mockfs.WithLatencySimulator(sim))
		start := time.Now()
		_, _ = mfs.Stat(".")
		if time.Since(start) < 5*time.Millisecond {
			t.Error("expected latency not applied")
		}
	})

	t.Run("WithPerOperationLatency", func(t *testing.T) {
		mfs := mockfs.NewMockFS(nil, mockfs.WithPerOperationLatency(map[mockfs.Operation]time.Duration{
			mockfs.OpStat: 10 * time.Millisecond,
		}))
		start := time.Now()
		_, _ = mfs.Stat(".")
		if time.Since(start) < 5*time.Millisecond {
			t.Error("expected per-op latency not applied")
		}
	})

	t.Run("ResetStats", func(t *testing.T) {
		mfs := mockfs.NewMockFS(nil)
		_, _ = mfs.Stat(".")
		mfs.ResetStats()
		if mfs.Stats().Count(mockfs.OpStat) != 0 {
			t.Error("ResetStats did not clear stats")
		}
	})

	t.Run("joinPath", func(t *testing.T) {
		mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
			"dir": {Mode: fs.ModeDir | 0755},
		})
		err := mfs.AddFile("dir/file.txt", "test", 0644)
		requireNoError(t, err)
		_, err = mfs.Stat("dir/file.txt")
		requireNoError(t, err)
	})
}

// --- Concurrency ---

func TestMockFS_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(nil)
	var wg sync.WaitGroup
	numGoroutines := 50

	// Pre-create files to avoid ErrNotExist
	for i := 0; i < numGoroutines; i++ {
		path := fmt.Sprintf("file-%d.txt", i)
		if err := m.AddFile(path, "", 0644); err != nil {
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
			err := m.WriteFile(path, content, 0644)
			if err != nil {
				t.Errorf("concurrent WriteFile failed: %v", err)
				return
			}

			// Stat the file
			_, err = m.Stat(path)
			if err != nil {
				t.Errorf("concurrent Stat failed: %v", err)
				return
			}

			// Read it back
			readContent, err := m.ReadFile(path)
			if err != nil {
				t.Errorf("concurrent ReadFile failed: %v", err)
			} else if !bytes.Equal(content, readContent) {
				t.Errorf("content mismatch for %s", path)
			}
		}(i)
	}

	wg.Wait()

	// Final check
	stats := m.Stats()
	if stats.Count(mockfs.OpWrite) != numGoroutines {
		t.Errorf("expected %d writes, got %d", numGoroutines, stats.Count(mockfs.OpWrite))
	}
}

func TestMockFS_ConcurrentRemoveRename(t *testing.T) {
	t.Parallel()
	m := mockfs.NewMockFS(nil)
	var wg sync.WaitGroup
	numGoroutines := 20

	// Pre-populate with directories
	for i := 0; i < numGoroutines; i++ {
		_ = m.AddDir(path.Join("dir", fmt.Sprintf("sub-%d", i)), 0755)
	}
	_ = m.AddDir("other_dir", 0755)

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
			_ = m.Rename(oldName, newName)
			time.Sleep(1 * time.Microsecond)
		}
	}()

	// Other goroutines: Remove subdirectories
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Try removing from either potential parent name
			err1 := m.RemoveAll(path.Join("dir", fmt.Sprintf("sub-%d", id)))
			err2 := m.RemoveAll(path.Join("other_dir", fmt.Sprintf("sub-%d", id)))

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
