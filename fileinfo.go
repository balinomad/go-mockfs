package mockfs

import (
	"io/fs"
	"path"
	"time"
)

// FileInfo implements fs.FileInfo and fs.DirEntry for testing.
// It can be used to create directory entries for NewDirHandler without requiring a MockFS.
type FileInfo struct {
	name    string
	size    int64
	mode    FileMode
	modTime time.Time
}

// Ensure interface implementations.
var (
	_ fs.FileInfo = (*FileInfo)(nil)
	_ fs.DirEntry = (*FileInfo)(nil)
)

// NewFileInfo creates a FileInfo for testing directory entries.
// This is useful when creating mock directories with NewDirHandler.
//
// Example:
//
//	entries := []fs.DirEntry{
//	    mockfs.NewFileInfo("file1.txt", 100, 0o644, time.Now()),
//	    mockfs.NewFileInfo("file2.txt", 200, 0o644, time.Now()),
//	}
//	handler := mockfs.NewDirHandler(entries)
func NewFileInfo(name string, size int64, mode FileMode, modTime time.Time) *FileInfo {
	if name == "" {
		panic("name cannot be empty")
	}
	if !fs.ValidPath(name) {
		panic("name is not a valid path")
	}
	if mode.IsDir() && size != 0 {
		panic("size must be zero for directories")
	}

	t := modTime
	if modTime.IsZero() {
		t = time.Now()
	}

	return &FileInfo{
		name:    path.Base(name),
		size:    size,
		mode:    mode,
		modTime: t,
	}
}

// Name returns the name of the file (or subdirectory) described by the entry.
// This name is only the final element of the path (the base name), not the entire path.
// For example, Name would return "hello.go" not "home/gopher/hello.go".
func (fi *FileInfo) Name() string {
	return fi.name
}

// Size returns the length in bytes for regular files; zero for directories.
func (fi *FileInfo) Size() int64 {
	if fi.mode.IsDir() {
		return 0
	}
	return fi.size
}

func (fi *FileInfo) Mode() FileMode {
	return fi.mode
}
func (fi *FileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir reports whether the entry describes a directory.
func (fi *FileInfo) IsDir() bool {
	return fi.mode.IsDir()
}

// Sys always returns nil for a FileInfo.
func (fi *FileInfo) Sys() any {
	return nil
}

// Type returns the type bits for the entry.
func (fi *FileInfo) Type() FileMode {
	return fi.mode.Type()
}

// Info returns the entry as an fs.FileInfo and nil for an error.
func (fi *FileInfo) Info() (fs.FileInfo, error) {
	return fi, nil
}

// Equal reports whether this FileInfo has the same values as other.
func (fi *FileInfo) Equal(other fs.FileInfo) bool {
	return fi.name == other.Name() &&
		fi.size == other.Size() &&
		fi.mode == other.Mode() &&
		fi.modTime.Equal(other.ModTime())
}
