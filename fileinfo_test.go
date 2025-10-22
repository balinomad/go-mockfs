package mockfs_test

import (
	"io/fs"
	"testing"
	"time"

	"github.com/balinomad/go-mockfs"
)

// TestFileInfoInterface checks that exported functions return types
// that implement fs.FileInfo and fs.DirEntry.
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

// fileInfoTest represents the expected properties of a fileInfo object.
type fileInfoTest struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
	isDir   bool
}

// TestFileInfoMethods_FromStat tests properties of fs.FileInfo
// objects returned by Stat() calls.
func TestFileInfoMethods_FromStat(t *testing.T) {
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
	tests := map[string]struct {
		// getInfo is the operation to get the fs.FileInfo
		getInfo func(t *testing.T) (fs.FileInfo, error)
		// want is the expected result
		want fileInfoTest
	}{
		"mfs.stat file": {
			getInfo: func(t *testing.T) (fs.FileInfo, error) {
				return mfs.Stat("file.txt")
			},
			want: fileInfoTest{name: "file.txt", size: 9, mode: 0644, modTime: now},
		},
		"mfs.stat dir": {
			getInfo: func(t *testing.T) (fs.FileInfo, error) {
				return mfs.Stat("src")
			},
			want: fileInfoTest{name: "src", size: 0, mode: fs.ModeDir | 0755, modTime: hourAgo, isDir: true},
		},
		"mfs.stat symlink": {
			getInfo: func(t *testing.T) (fs.FileInfo, error) {
				return mfs.Stat("src/link.go")
			},
			want: fileInfoTest{name: "link.go", size: 4, mode: fs.ModeSymlink | 0644, modTime: now},
		},
		"file.stat file": {
			getInfo: func(t *testing.T) (fs.FileInfo, error) {
				f, err := mfs.Open("file.txt")
				if err != nil {
					return nil, err
				}
				defer f.Close()
				return f.Stat()
			},
			want: fileInfoTest{name: "file.txt", size: 9, mode: 0644, modTime: now},
		},
		"file.stat symlink": {
			getInfo: func(t *testing.T) (fs.FileInfo, error) {
				f, err := mfs.Open("src/link.go")
				if err != nil {
					return nil, err
				}
				defer f.Close()
				return f.Stat()
			},
			want: fileInfoTest{name: "link.go", size: 4, mode: fs.ModeSymlink | 0644, modTime: now},
		},
	}

	// Run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			info, err := tt.getInfo(t)
			if err != nil {
				t.Fatalf("getInfo() error = %v", err)
			}
			assertFileInfoMatches(t, info, tt.want)
		})
	}
}

// TestFileInfoMethods_FromReadDir tests properties of fs.DirEntry
// objects returned by ReadDir() calls.
func TestFileInfoMethods_FromReadDir(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	hourAgo := now.Add(-1 * time.Hour)
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		".":        {Mode: fs.ModeDir | 0755, ModTime: hourAgo},
		"file.txt": {Data: []byte("123"), Mode: 0644, ModTime: now},
		"src":      {Mode: fs.ModeDir | 0755, ModTime: hourAgo},
	})

	// Define test cases
	tests := map[string]struct {
		// getEntry finds the specific entry to test
		getEntry func(t *testing.T) (fs.DirEntry, error)
		// want is the expected result
		want fileInfoTest
	}{
		"mfs.readdir file": {
			getEntry: func(t *testing.T) (fs.DirEntry, error) {
				entries, err := mfs.ReadDir(".")
				if err != nil {
					return nil, err
				}
				return findEntry(entries, "file.txt")
			},
			want: fileInfoTest{name: "file.txt", size: 3, mode: 0644, modTime: now},
		},
		"mfs.readdir dir": {
			getEntry: func(t *testing.T) (fs.DirEntry, error) {
				entries, err := mfs.ReadDir(".")
				if err != nil {
					return nil, err
				}
				return findEntry(entries, "src")
			},
			want: fileInfoTest{name: "src", size: 0, mode: fs.ModeDir | 0755, modTime: hourAgo, isDir: true},
		},
		"file.readdir file": {
			getEntry: func(t *testing.T) (fs.DirEntry, error) {
				d, err := mfs.Open(".")
				if err != nil {
					return nil, err
				}
				defer d.Close()
				rd, _ := d.(fs.ReadDirFile)
				entries, err := rd.ReadDir(-1)
				if err != nil {
					return nil, err
				}
				return findEntry(entries, "file.txt")
			},
			want: fileInfoTest{name: "file.txt", size: 3, mode: 0644, modTime: now},
		},
	}

	// Run tests
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			entry, err := tt.getEntry(t)
			if err != nil {
				t.Fatalf("getEntry() error = %v", err)
			}
			assertDirEntryMatches(t, entry, tt.want)
		})
	}
}

// assertFileInfoMatches is a helper to check fs.FileInfo properties.
func assertFileInfoMatches(t *testing.T, info fs.FileInfo, want fileInfoTest) {
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

// assertDirEntryMatches is a helper to check fs.DirEntry properties.
func assertDirEntryMatches(t *testing.T, entry fs.DirEntry, want fileInfoTest) {
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

// findEntry is a test helper to find a DirEntry by name.
func findEntry(entries []fs.DirEntry, name string) (fs.DirEntry, error) {
	for _, e := range entries {
		if e.Name() == name {
			return e, nil
		}
	}
	return nil, fs.ErrNotExist
}
