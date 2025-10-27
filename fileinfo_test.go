package mockfs_test

import (
	"io/fs"
	"testing"
	"time"

	"github.com/balinomad/go-mockfs"
)

// TestFileInfoInterface verifies that MockFS returns concrete types implementing
// fs.FileInfo and fs.DirEntry interfaces at compile time.
func TestFileInfoInterface(t *testing.T) {
	now := time.Now()
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		".":         {Mode: fs.ModeDir | 0755, ModTime: now},
		"file.txt":  {Data: []byte("test"), Mode: 0644, ModTime: now},
		"dir":       {Mode: fs.ModeDir | 0755, ModTime: now},
		"dir/child": {Data: []byte("test"), Mode: 0644, ModTime: now},
	})

	// Test fs.FileInfo from MockFS.Stat
	info, err := mfs.Stat("file.txt")
	if err != nil {
		t.Fatalf("mfs.Stat(file) failed: %v", err)
	}
	var _ fs.FileInfo = info // Compile-time check

	// Test fs.DirEntry from MockFS.ReadDir
	entries, err := mfs.ReadDir(".")
	if err != nil {
		t.Fatalf("mfs.ReadDir(.) failed: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("mfs.ReadDir(.) returned no entries")
	}
	var _ fs.DirEntry = entries[0] // Compile-time check

	// Test fs.FileInfo from MockFile.Stat
	f, err := mfs.Open("file.txt")
	if err != nil {
		t.Fatalf("mfs.Open(file) failed: %v", err)
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		t.Fatalf("f.Stat() failed: %v", err)
	}
	var _ fs.FileInfo = fileInfo // Compile-time check

	// Test fs.DirEntry from MockFile.ReadDir
	d, err := mfs.Open("dir")
	if err != nil {
		t.Fatalf("mfs.Open(dir) failed: %v", err)
	}
	defer d.Close()

	// FIX: Type-assert the returned fs.File to fs.ReadDirFile
	dReadDir, ok := d.(fs.ReadDirFile)
	if !ok {
		t.Fatal("mfs.Open(dir) did not return a file implementing fs.ReadDirFile")
	}

	dirEntries, err := dReadDir.ReadDir(-1)
	if err != nil {
		t.Fatalf("dReadDir.ReadDir() failed: %v", err)
	}
	if len(dirEntries) == 0 {
		t.Fatal("dReadDir.ReadDir() returned no entries")
	}
	var _ fs.DirEntry = dirEntries[0] // Compile-time check
}

// TestFileInfoMethods_Stat verifies fs.FileInfo properties returned by
// MockFS.Stat() and MockFile.Stat() match expected file metadata.
func TestFileInfoMethods_Stat(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	hourAgo := now.Add(-1 * time.Hour)
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {
			Data:    []byte("123 bytes"), // 9 bytes
			Mode:    0644,
			ModTime: now,
		},
		"src": {
			Mode:    fs.ModeDir | 0755,
			ModTime: hourAgo,
		},
		"src/link.go": {
			Data:    []byte("link"), // 4 bytes
			Mode:    fs.ModeSymlink | 0644,
			ModTime: now,
		},
	})

	// Define test cases
	tests := []struct {
		name string
		path string
		via  string // "mfs.Stat" or "file.Stat"
		want expectedFileInfo
	}{
		{
			name: "file via mfs.Stat",
			path: "file.txt",
			via:  "mfs.Stat",
			want: expectedFileInfo{name: "file.txt", size: 9, mode: 0644, modTime: now},
		},
		{
			name: "dir via mfs.Stat",
			path: "src",
			via:  "mfs.Stat",
			want: expectedFileInfo{name: "src", size: 0, mode: fs.ModeDir | 0755, modTime: hourAgo, isDir: true},
		},
		{
			name: "symlink via mfs.Stat",
			path: "src/link.go",
			via:  "mfs.Stat",
			want: expectedFileInfo{name: "link.go", size: 4, mode: fs.ModeSymlink | 0644, modTime: now},
		},
		{
			name: "file via file.Stat",
			path: "file.txt",
			via:  "file.Stat",
			want: expectedFileInfo{name: "file.txt", size: 9, mode: 0644, modTime: now},
		},
		{
			name: "symlink via file.Stat",
			path: "src/link.go",
			via:  "file.Stat",
			want: expectedFileInfo{name: "link.go", size: 4, mode: fs.ModeSymlink | 0644, modTime: now},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var info fs.FileInfo
			var err error

			if tt.via == "file.Stat" {
				f, openErr := mfs.Open(tt.path)
				if openErr != nil {
					t.Fatalf("Open(%q) failed: %v", tt.path, openErr)
				}
				defer f.Close()
				info, err = f.Stat()
			} else {
				info, err = mfs.Stat(tt.path)
			}

			if err != nil {
				t.Fatalf("%s(%q) failed: %v", tt.via, tt.path, err)
			}
			assertFileInfoMatches(t, info, tt.want)
		})
	}
}

// TestFileInfoMethods_ReadDir verifies fs.DirEntry properties returned by
// MockFS.ReadDir() and MockFile.ReadDir() match expected directory entries.
func TestFileInfoMethods_ReadDir(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	hourAgo := now.Add(-1 * time.Hour)
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		".":        {Mode: fs.ModeDir | 0755, ModTime: hourAgo},
		"file.txt": {Data: []byte("123"), Mode: 0644, ModTime: now},
		"src":      {Mode: fs.ModeDir | 0755, ModTime: hourAgo},
	})

	tests := []struct {
		name     string
		dir      string
		via      string // "mfs.ReadDir" or "file.ReadDir"
		findName string
		want     expectedFileInfo
	}{
		{
			name:     "file via mfs.ReadDir",
			dir:      ".",
			via:      "mfs.ReadDir",
			findName: "file.txt",
			want:     expectedFileInfo{name: "file.txt", size: 3, mode: 0644, modTime: now},
		},
		{
			name:     "dir via mfs.ReadDir",
			dir:      ".",
			via:      "mfs.ReadDir",
			findName: "src",
			want:     expectedFileInfo{name: "src", size: 0, mode: fs.ModeDir | 0755, modTime: hourAgo, isDir: true},
		},
		{
			name:     "file via file.ReadDir",
			dir:      ".",
			via:      "file.ReadDir",
			findName: "file.txt",
			want:     expectedFileInfo{name: "file.txt", size: 3, mode: 0644, modTime: now},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var entries []fs.DirEntry
			var err error

			if tt.via == "file.ReadDir" {
				d, openErr := mfs.Open(tt.dir)
				if openErr != nil {
					t.Fatalf("Open(%q) failed: %v", tt.dir, openErr)
				}
				defer d.Close()
				rd, ok := d.(fs.ReadDirFile)
				if !ok {
					t.Fatalf("Open(%q) did not return fs.ReadDirFile", tt.dir)
				}
				entries, err = rd.ReadDir(-1)
			} else {
				entries, err = mfs.ReadDir(tt.dir)
			}

			if err != nil {
				t.Fatalf("%s(%q) failed: %v", tt.via, tt.dir, err)
			}

			entry, findErr := findEntry(entries, tt.findName)
			if findErr != nil {
				t.Fatalf("entry %q not found in %s(%q): %v", tt.findName, tt.via, tt.dir, findErr)
			}
			assertDirEntryMatches(t, entry, tt.want)
		})
	}
}

// expectedFileInfo defines expected fs.FileInfo/fs.DirEntry properties for test assertions.
// Used by assertFileInfoMatches and assertDirEntryMatches to verify returned values.
type expectedFileInfo struct {
	name    string      // Expected value from Name()
	size    int64       // Expected value from Size()
	mode    fs.FileMode // Expected value from Mode()
	modTime time.Time   // Expected value from ModTime()
	isDir   bool        // Expected value from IsDir()
}

// assertFileInfoMatches verifies fs.FileInfo properties match expected values.
func assertFileInfoMatches(t *testing.T, info fs.FileInfo, want expectedFileInfo) {
	t.Helper()
	if info.Name() != want.name {
		t.Errorf("Name() = %q, want %q", info.Name(), want.name)
	}
	if info.Size() != want.size {
		t.Errorf("Size() = %d, want %d", info.Size(), want.size)
	}
	if info.Mode() != want.mode {
		t.Errorf("Mode() = %v, want %v", info.Mode(), want.mode)
	}
	if !info.ModTime().Equal(want.modTime) {
		t.Errorf("ModTime() = %v, want %v", info.ModTime(), want.modTime)
	}
	if info.IsDir() != want.isDir {
		t.Errorf("IsDir() = %v, want %v", info.IsDir(), want.isDir)
	}
	if info.Sys() != nil {
		t.Errorf("Sys() = %v, want nil", info.Sys())
	}
}

// assertDirEntryMatches verifies fs.DirEntry properties match expected values.
func assertDirEntryMatches(t *testing.T, entry fs.DirEntry, want expectedFileInfo) {
	t.Helper()
	if entry.Name() != want.name {
		t.Errorf("Name() = %q, want %q", entry.Name(), want.name)
	}
	if entry.IsDir() != want.isDir {
		t.Errorf("IsDir() = %v, want %v", entry.IsDir(), want.isDir)
	}
	if entry.Type() != want.mode.Type() {
		t.Errorf("Type() = %v, want %v", entry.Type(), want.mode.Type())
	}

	// Check .Info() properties as well
	info, err := entry.Info()
	if err != nil {
		t.Fatalf("entry.Info() failed: %v", err)
	}
	assertFileInfoMatches(t, info, want)
}

// findEntry finds a DirEntry by name in a slice of entries.
func findEntry(entries []fs.DirEntry, name string) (fs.DirEntry, error) {
	for _, e := range entries {
		if e.Name() == name {
			return e, nil
		}
	}
	return nil, fs.ErrNotExist
}
