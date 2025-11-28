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

// --- Constructor and Options ---

func TestNewMockFS(t *testing.T) {
	t.Parallel()

	customErr := errors.New("custom injector error")
	customInjector := mockfs.NewErrorInjector()
	customInjector.AddExact(mockfs.OpStat, "file.txt", customErr, mockfs.ErrorModeAlways, 0)

	tests := []struct {
		name         string
		opts         []mockfs.MockFSOption
		postCreation func(t *testing.T, m *mockfs.MockFS)
	}{
		{
			name: "empty filesystem",
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
				_, err := m.Stat(".")
				requireNoError(t, err)
			},
		},
		{
			name: "with initial files",
			opts: []mockfs.MockFSOption{
				mockfs.File("file.txt", "original"),
				mockfs.File("nil.txt", nil), // Should be ignored
			},
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
				content := mustReadFile(t, m, "file.txt")
				if string(content) != "original" {
					t.Errorf("expected file content 'original', got %q", content)
				}
				// Check that nil entries return empty files
				content = mustReadFile(t, m, "nil.txt")
				if string(content) != "" {
					t.Errorf("expected empty file content, got %q", content)
				}
			},
		},
		{
			name: "with latency",
			opts: []mockfs.MockFSOption{
				mockfs.File("file.txt", "original"),
				mockfs.WithLatency(10 * time.Millisecond),
			},
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
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
				mockfs.File("file.txt", "original"),
				mockfs.WithErrorInjector(customInjector),
			},
			postCreation: func(t *testing.T, m *mockfs.MockFS) {
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
			tt.postCreation(t, mfs)
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
			name:      "empty dir name",
			option:    mockfs.Dir(""),
			panicText: "empty directory name",
		},
	}

	for _, tt := range tests {
		tt := tt // Capture range variable
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Use a defer block with recover to catch the expected panic.
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected NewMockFS to panic, but it did not")
				}

				panicStr, ok := r.(string)
				if !ok {
					t.Fatalf("panic value was not a string: %v", r)
				}

				// Assert that the panic message contains the expected text.
				if !strings.Contains(panicStr, tt.panicText) {
					t.Errorf("panic message mismatch:\ngot:  %q\nwant fragment: %q", panicStr, tt.panicText)
				}
			}()

			// This call is expected to panic.
			mockfs.NewMockFS(tt.option)
		})
	}
}

func TestNewMockFS_AutoCreateRoot_EmptyMap(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS()
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

	mfs := mockfs.NewMockFS(
		mockfs.File("file.txt", "test"),
		mockfs.Dir("dir",
			mockfs.File("nested.txt", "nested"),
		))

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

func TestNewMockFS_AutoCreateRoot_NilEntries(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(
		mockfs.File("file.txt", nil),
	)

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

	mfs := mockfs.NewMockFS(
		mockfs.File("file.txt", "test"),
	)

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

	mfs := mockfs.NewMockFS()
	if mfs.ErrorInjector() == nil {
		t.Error("Injector() returned nil for default injector")
	}

	customInjector := mockfs.NewErrorInjector()
	mfs2 := mockfs.NewMockFS(mockfs.WithErrorInjector(customInjector))
	if mfs2.ErrorInjector() != customInjector {
		t.Error("Injector() did not return the provided custom injector")
	}
}

// --- FS Interface Methods ---

func TestMockFS_Stat(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(
		mockfs.File("file.txt", "content"),
		mockfs.Dir("dir"),
	)
	injectedErr := errors.New("injected stat error")
	mfs.FailStat("file.txt", injectedErr)

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
			info, err := mfs.Stat(tt.path)
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

	mfs := mockfs.NewMockFS(
		mockfs.File("file.txt", "hello"),
	)
	injectedErr := errors.New("injected open error")
	mfs.FailOpen("file.txt", injectedErr)
	mfs.FailOpenOnce("other.txt", mockfs.ErrPermission) // Not in FS, but error should trigger

	// Test successful open
	t.Run("open and read file", func(t *testing.T) {
		mfsSuccess := mockfs.NewMockFS(mockfs.File("a.txt", "data"))
		f, err := mfsSuccess.Open("a.txt")
		requireNoError(t, err)
		defer f.Close()

		content, err := io.ReadAll(f)
		requireNoError(t, err)
		if string(content) != "data" {
			t.Errorf("content mismatch: got %q, want %q", content, "data")
		}
	})

	t.Run("open with invalid path", func(t *testing.T) {
		_, err := mfs.Open("../invalid")
		assertError(t, err, mockfs.ErrInvalid)
	})

	// Test error cases
	t.Run("open with injected error", func(t *testing.T) {
		_, err := mfs.Open("file.txt")
		assertError(t, err, injectedErr)
	})

	t.Run("open non-existent", func(t *testing.T) {
		_, err := mfs.Open("missing.txt")
		assertError(t, err, mockfs.ErrNotExist)
	})

	t.Run("open with injected error once", func(t *testing.T) {
		_, err := mfs.Open("other.txt")
		assertError(t, err, mockfs.ErrPermission)
		// Second attempt should be different (not exist in this case)
		_, err = mfs.Open("other.txt")
		assertError(t, err, mockfs.ErrNotExist)
	})
}

func TestMockFS_ReadFile(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(
		mockfs.File("file.txt", "content"),
		mockfs.Dir("dir"),
	)
	injectedErr := errors.New("injected read error")
	mfs.FailRead("file.txt", injectedErr) // FailRead affects operations inside ReadFile

	t.Run("read success", func(t *testing.T) {
		mfsSuccess := mockfs.NewMockFS(mockfs.File("a.txt", "data"))
		content, err := mfsSuccess.ReadFile("a.txt")
		requireNoError(t, err)
		if string(content) != "data" {
			t.Errorf("content mismatch")
		}
	})

	t.Run("read with injected error", func(t *testing.T) {
		_, err := mfs.ReadFile("file.txt")
		assertError(t, err, injectedErr)
	})

	t.Run("read a directory", func(t *testing.T) {
		// MapFS behaviour: ReadFile on directory returns empty data, no error
		data, err := mfs.ReadFile("dir")
		requireNoError(t, err)
		if len(data) != 0 {
			t.Errorf("expected empty data for directory, got %d bytes", len(data))
		}
	})

	t.Run("read with invalid path", func(t *testing.T) {
		_, err := mfs.ReadFile("../invalid")
		assertError(t, err, mockfs.ErrInvalid)
	})
}

func TestMockFS_ReadDir(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(
		mockfs.File("file.txt", "not a dir"),
		mockfs.Dir("dir",
			mockfs.File("file1.txt", "1"),
			mockfs.Dir("sub",
				mockfs.File("file2.txt", "2"),
			)))
	injectedErr := errors.New("injected readdir error")
	mfs.FailReadDir("dir", injectedErr)

	t.Run("readdir success", func(t *testing.T) {
		mfsSuccess := mockfs.NewMockFS(
			mockfs.Dir("d",
				mockfs.File("file1.txt", "1"),
				mockfs.File("file2.txt", "2"),
			))
		entries, err := mfsSuccess.ReadDir("d")
		requireNoError(t, err)
		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
		names := map[string]bool{entries[0].Name(): true, entries[1].Name(): true}
		if !names["file1.txt"] || !names["file2.txt"] {
			t.Errorf("unexpected entry names")
		}
	})

	t.Run("readdir with injected error", func(t *testing.T) {
		_, err := mfs.ReadDir("dir")
		assertError(t, err, injectedErr)
	})

	t.Run("readdir on a file", func(t *testing.T) {
		_, err := mfs.ReadDir("file.txt")
		if err == nil {
			t.Error("expected an error when reading dir on a file")
		}
	})

	t.Run("readdir with invalid path", func(t *testing.T) {
		_, err := mfs.ReadDir("../invalid")
		assertError(t, err, mockfs.ErrInvalid)
	})
}

// --- SubFS ---

func TestMockFS_Sub(t *testing.T) {
	t.Parallel()

	mfsParent := mockfs.NewMockFS(
		mockfs.Dir("app",
			mockfs.File("config.json", "{}"),
			mockfs.Dir("src",
				mockfs.File("main.go", "package main"),
			)),
		mockfs.File("other.txt", "..."),
		mockfs.File("file.txt", "not a dir"),
	)
	injectedErr := errors.New("sub-error")
	// Inject errors to test if they are correctly scoped in the sub-fs
	mfsParent.FailOpen("app/src/main.go", injectedErr)
	mfsParent.FailStat("other.txt", injectedErr) // Should not be visible in sub-fs

	t.Run("subfs happy path", func(t *testing.T) {
		mfsSub, err := mfsParent.Sub("app")
		requireNoError(t, err)

		// Check file existence and content
		content := mustReadFile(t, mfsSub.(fs.ReadFileFS), "config.json")
		if string(content) != "{}" {
			t.Errorf("unexpected content in subfs file")
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

	t.Run("subfs error cases", func(t *testing.T) {
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
				return m.AddFile("file.txt", "hello", 0o644)
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
				return m.AddFile("bin.dat", []byte{0, 1, 2}, 0600)
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
				return m.AddDir("my/dir", 0o755)
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
			setup:   func(m *mockfs.MockFS) error { return m.AddDir("/mydir", 0) },
			wantErr: fs.ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			err := tt.setup(mfs)
			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
				return
			}
			requireNoError(t, err)
			tt.verify(t, mfs)
		})
	}
}

func TestMockFS_RemoveEntry(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(mockfs.File("file.txt", ""))

	t.Run("remove existing path", func(t *testing.T) {
		err := mfs.RemoveEntry("file.txt")
		requireNoError(t, err)
		_, err = mfs.Stat("file.txt")
		assertError(t, err, mockfs.ErrNotExist)
	})

	t.Run("remove non-existent path", func(t *testing.T) {
		// Should not return an error
		err := mfs.RemoveEntry("non-existent.txt")
		requireNoError(t, err)
	})

	t.Run("remove with invalid path", func(t *testing.T) {
		err := mfs.RemoveEntry("../invalid")
		assertError(t, err, mockfs.ErrInvalid)
	})
}

// --- WritableFS Implementation ---

func TestMockFS_WritableFS(t *testing.T) {
	t.Parallel()

	// Base filesystem for mutation tests
	setup := func() *mockfs.MockFS {
		return mockfs.NewMockFS(
			mockfs.Dir("dir",
				mockfs.File("file.txt", "content"),
				mockfs.Dir("empty_subdir"),
			),
			mockfs.File("file.txt", "root file"),
		)
	}

	// Mkdir / MkdirAll
	t.Run("mkdir", func(t *testing.T) {
		mfs := setup()
		err := mfs.Mkdir("dir/new_dir", 0o755)
		requireNoError(t, err)
		info, _ := mfs.Stat("dir/new_dir")
		if !info.IsDir() {
			t.Error("new_dir not created or not a directory")
		}

		// Error cases
		assertError(t, mfs.Mkdir("dir/new_dir", 0), mockfs.ErrExist)
		assertError(t, mfs.Mkdir("nonexistent/dir", 0), mockfs.ErrNotExist)
		assertError(t, mfs.Mkdir("dir/file.txt/fail", 0), mockfs.ErrNotDir)
	})

	t.Run("mkdirall", func(t *testing.T) {
		mfs := setup()
		err := mfs.MkdirAll("new/nested/dir", 0o755)
		requireNoError(t, err)
		info, _ := mfs.Stat("new/nested/dir")
		if !info.IsDir() {
			t.Error("nested dir not created or not a directory")
		}

		// No error if path already exists and is a directory
		err = mfs.MkdirAll("dir", 0o755)
		requireNoError(t, err)

		// Error if part of the path is a file
		assertError(t, mfs.MkdirAll("dir/file.txt/fail", 0), mockfs.ErrNotDir)
	})

	// Remove / RemoveAll
	t.Run("remove", func(t *testing.T) {
		mfs := setup()
		err := mfs.Remove("file.txt")
		requireNoError(t, err)
		_, err = mfs.Stat("file.txt")
		assertError(t, err, mockfs.ErrNotExist)

		// Error cases
		assertError(t, mfs.Remove("missing.txt"), mockfs.ErrNotExist)
		assertError(t, mfs.Remove("dir"), mockfs.ErrNotEmpty)

		// Remove empty dir should succeed
		err = mfs.Remove("dir/empty_subdir")
		requireNoError(t, err)
	})

	t.Run("remove all", func(t *testing.T) {
		mfs := setup()
		err := mfs.RemoveAll("dir")
		requireNoError(t, err)
		_, err = mfs.Stat("dir")
		assertError(t, err, mockfs.ErrNotExist)
		_, err = mfs.Stat("dir/file.txt")
		assertError(t, err, mockfs.ErrNotExist)

		// No error if path does not exist
		err = mfs.RemoveAll("missing_dir")
		requireNoError(t, err)
	})

	// Rename
	t.Run("rename", func(t *testing.T) {
		mfs := setup()
		// Rename file
		err := mfs.Rename("file.txt", "renamed.txt")
		requireNoError(t, err)
		_, err = mfs.Stat("file.txt")
		assertError(t, err, mockfs.ErrNotExist)
		content := mustReadFile(t, mfs, "renamed.txt")
		if string(content) != "root file" {
			t.Error("renamed file content mismatch")
		}

		// Rename directory
		err = mfs.Rename("dir", "renamed_dir")
		requireNoError(t, err)
		_, err = mfs.Stat("dir")
		assertError(t, err, mockfs.ErrNotExist)
		_, err = mfs.Stat("renamed_dir/file.txt")
		requireNoError(t, err)

		// Error case
		assertError(t, mfs.Rename("missing.txt", "new.txt"), mockfs.ErrNotExist)
	})

	// WriteFile and Write via Open
	t.Run("write file", func(t *testing.T) {
		mfs := setup()

		// Use createIfMissing to create a new file
		m2 := mockfs.NewMockFS(mockfs.WithCreateIfMissing(true))
		newData := []byte("new data")
		err := m2.WriteFile("newfile.txt", newData, 0o644)
		requireNoError(t, err)
		content := mustReadFile(t, m2, "newfile.txt")
		if !bytes.Equal(content, newData) {
			t.Error("written content mismatch")
		}

		// Overwrite existing file (works regardless of createIfMissing)
		err = mfs.WriteFile("file.txt", newData, 0o644)
		requireNoError(t, err)
		content = mustReadFile(t, mfs, "file.txt")
		if !bytes.Equal(content, newData) {
			t.Error("overwritten content mismatch")
		}
	})

	t.Run("overwrite via opened file", func(t *testing.T) {
		mfs := setup() // has "file.txt" with "root file"
		f, err := mfs.Open("file.txt")
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
		content := mustReadFile(t, mfs, "file.txt")
		if !bytes.Equal(content, updatedData) {
			t.Errorf("content mismatch, got %q want %q", content, updatedData)
		}
	})
}
func TestMockFS_WriteFile_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("write to read-only filesystem", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.WithReadOnly())
		err := mfs.WriteFile("test.txt", []byte("data"), 0o644)
		assertError(t, err, mockfs.ErrPermission)
	})

	t.Run("write without createIfMissing", func(t *testing.T) {
		mfs := mockfs.NewMockFS()
		err := mfs.WriteFile("test.txt", []byte("data"), 0o644)
		assertError(t, err, mockfs.ErrNotExist)
	})

	t.Run("overwrite existing in overwrite mode", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.File("file.txt", "old"), mockfs.WithOverwrite())
		err := mfs.WriteFile("file.txt", []byte("new"), 0o644)
		requireNoError(t, err)
		content := mustReadFile(t, mfs, "file.txt")
		if string(content) != "new" {
			t.Errorf("content = %q, want %q", content, "new")
		}
	})

	t.Run("append to existing in append mode", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.File("file.txt", "old"), mockfs.WithAppend())
		err := mfs.WriteFile("file.txt", []byte("new"), 0o644)
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			tc.setup(mfs)
			err := tc.operation(mfs)
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

		f, err := mfs.Open("file.txt")
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
		mfs := mockfs.NewMockFS(mockfs.File("file.txt", nil))
		mfs.MarkNonExistent("file.txt")

		// Path should be gone from the filesystem
		_, err := mfs.Stat("file.txt")
		assertError(t, err, mockfs.ErrNotExist)

		// Should also fail for other operations
		_, err = mfs.Open("file.txt")
		assertError(t, err, mockfs.ErrNotExist)
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

func TestMockFS_ErrorInjectionOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		inject func(m *mockfs.MockFS)
		op     func(m *mockfs.MockFS) error
	}{
		{
			name:   "FailStatOnce",
			inject: func(m *mockfs.MockFS) { m.FailStatOnce("file.txt", mockfs.ErrPermission) },
			op: func(m *mockfs.MockFS) error {
				_, err := m.Stat("file.txt")
				return err
			},
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
			op: func(m *mockfs.MockFS) error {
				return m.Mkdir("dir", 0o755)
			},
		},
		{
			name:   "FailMkdirAllOnce",
			inject: func(m *mockfs.MockFS) { m.FailMkdirAllOnce("dir/sub", mockfs.ErrPermission) },
			op: func(m *mockfs.MockFS) error {
				return m.MkdirAll("dir/sub", 0o755)
			},
		},
		{
			name: "FailRemoveOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("file.txt", "", 0o644)
				m.FailRemoveOnce("file.txt", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				return m.Remove("file.txt")
			},
		},
		{
			name: "FailRemoveAllOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddDir("dir", 0o755)
				m.FailRemoveAllOnce("dir", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				return m.RemoveAll("dir")
			},
		},
		{
			name: "FailRenameOnce",
			inject: func(m *mockfs.MockFS) {
				_ = m.AddFile("old.txt", "", 0o644)
				m.FailRenameOnce("old.txt", mockfs.ErrPermission)
			},
			op: func(m *mockfs.MockFS) error {
				return m.Rename("old.txt", "new.txt")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mfs := mockfs.NewMockFS()
			tt.inject(mfs)

			err := tt.op(mfs)
			assertError(t, err, mockfs.ErrPermission)

			err = tt.op(mfs)
			if err == fs.ErrPermission {
				t.Error("Once error injection triggered twice")
			}
		})
	}
}

func TestMockFS_Stats(t *testing.T) {
	t.Parallel()

	mfs := mockfs.NewMockFS(mockfs.File("a", ""), mockfs.File("b", ""))

	// Perform some operations
	_, _ = mfs.Stat("a")
	_, _ = mfs.Stat("a")
	f, _ := mfs.Open("b")
	if f != nil {
		_ = f.Close()
	}
	_ = mfs.Remove("a")

	stats := mfs.Stats()
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
		mfs := mockfs.NewMockFS(mockfs.WithReadOnly())
		err := mfs.WriteFile("test.txt", []byte("data"), 0o644)
		assertError(t, err, mockfs.ErrPermission)
	})

	t.Run("WithOverwrite", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.File("file.txt", "old"), mockfs.WithOverwrite())
		err := mfs.WriteFile("file.txt", []byte("new"), 0o644)
		requireNoError(t, err)
		content := mustReadFile(t, mfs, "file.txt")
		if string(content) != "new" {
			t.Errorf("content = %q, want %q", content, "new")
		}
	})

	t.Run("WithAppend", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.File("file.txt", "old"), mockfs.WithAppend())
		err := mfs.WriteFile("file.txt", []byte("new"), 0o644)
		requireNoError(t, err)
		content := mustReadFile(t, mfs, "file.txt")
		if string(content) != "oldnew" {
			t.Errorf("content = %q, want %q", content, "oldnew")
		}
	})

	t.Run("WithLatencySimulator", func(t *testing.T) {
		sim := mockfs.NewLatencySimulator(10 * time.Millisecond)
		mfs := mockfs.NewMockFS(mockfs.WithLatencySimulator(sim))
		start := time.Now()
		_, _ = mfs.Stat(".")
		if time.Since(start) < 5*time.Millisecond {
			t.Error("expected latency not applied")
		}
	})

	t.Run("WithPerOperationLatency", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.WithPerOperationLatency(map[mockfs.Operation]time.Duration{
			mockfs.OpStat: 10 * time.Millisecond,
		}))
		start := time.Now()
		_, _ = mfs.Stat(".")
		if time.Since(start) < 5*time.Millisecond {
			t.Error("expected per-op latency not applied")
		}
	})

	t.Run("ResetStats", func(t *testing.T) {
		mfs := mockfs.NewMockFS()
		_, _ = mfs.Stat(".")
		mfs.ResetStats()
		if mfs.Stats().Count(mockfs.OpStat) != 0 {
			t.Error("ResetStats did not clear stats")
		}
	})

	t.Run("joinPath", func(t *testing.T) {
		mfs := mockfs.NewMockFS(mockfs.Dir("dir"))
		err := mfs.AddFile("dir/file.txt", "test", 0o644)
		requireNoError(t, err)
		_, err = mfs.Stat("dir/file.txt")
		requireNoError(t, err)
	})
}

// --- Concurrency ---

func TestMockFS_ConcurrentReadWrite(t *testing.T) {
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

func TestMockFS_ConcurrentRemoveRename(t *testing.T) {
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

// stringer implements fmt.Stringer
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
		{"typed nil marshaler (panic -> err)", nilMarshaler, []byte{}, true}}

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
	f, err := mfs.Open(fname)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}

	// Verify FS content was NOT affected by mutation
	expected := []byte("original")
	if !bytes.Equal(got, expected) {
		t.Errorf("mutation safety failure: FS content changed.\ngot:  %q\nwant: %q", got, expected)
	}
}
