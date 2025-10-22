package mockfs

import (
	"io/fs"
	"time"
)

// fileInfo implements fs.FileInfo for fstest.MapFile-backed files.
type fileInfo struct {
	name    string
	size    int64
	mode    fs.FileMode
	modTime time.Time
}

// Ensure interface implementations.
var (
	_ fs.FileInfo = (*fileInfo)(nil)
	_ fs.DirEntry = (*fileInfo)(nil)
)

func (m *fileInfo) Name() string               { return m.name }
func (m *fileInfo) Size() int64                { return m.size }
func (m *fileInfo) Mode() fs.FileMode          { return m.mode }
func (m *fileInfo) ModTime() time.Time         { return m.modTime }
func (m *fileInfo) IsDir() bool                { return m.mode.IsDir() }
func (m *fileInfo) Sys() any                   { return nil }
func (m *fileInfo) Type() fs.FileMode          { return m.mode.Type() }
func (m *fileInfo) Info() (fs.FileInfo, error) { return m, nil }
