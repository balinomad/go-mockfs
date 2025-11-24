package mockfs_test

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/balinomad/go-mockfs/v2"
)

// ExampleMockFS_Sub demonstrates SubFS with path adjustment.
func ExampleMockFS_Sub() {
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"app":                  {Mode: fs.ModeDir | 0o755, ModTime: time.Now()},
		"app/config":           {Mode: fs.ModeDir | 0o755, ModTime: time.Now()},
		"app/config/dev.json":  {Data: []byte("dev"), Mode: 0o644, ModTime: time.Now()},
		"app/config/prod.json": {Data: []byte("prod"), Mode: 0o644, ModTime: time.Now()},
		"app/logs":             {Mode: fs.ModeDir | 0o755, ModTime: time.Now()},
		"app/logs/app.log":     {Data: []byte("log"), Mode: 0o644, ModTime: time.Now()},
	})

	// Error rule in parent filesystem
	mfs.FailRead("app/config/prod.json", fs.ErrPermission)

	// Create sub-filesystem
	subFS, err := mfs.Sub("app/config")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	// Read from sub-filesystem (paths are relative)
	data, err := fs.ReadFile(subFS, "dev.json")
	fmt.Printf("dev.json: %s, error: %v\n", data, err)

	// Error rule automatically adjusted for sub-filesystem
	_, err = fs.ReadFile(subFS, "prod.json")
	fmt.Printf("prod.json error: %v\n", err)

	// Files outside sub-filesystem not accessible
	_, err = fs.ReadFile(subFS, "../logs/app.log")
	fmt.Printf("Outside subFS accessible: %v\n", err == nil)
	// Output:
	// dev.json: dev, error: <nil>
	// prod.json error: permission denied
	// Outside subFS accessible: false
}

// ExampleNewErrorInjector demonstrates shared error injector.
func ExampleNewErrorInjector() {
	// Create shared injector
	injector := mockfs.NewErrorInjector()
	injector.AddGlob(mockfs.OpRead, "*.log", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
	injector.AddExact(mockfs.OpOpen, "locked.txt", fs.ErrPermission, mockfs.ErrorModeAlways, 0)

	// Use with multiple filesystems
	mfs1 := mockfs.NewMockFS(
		map[string]*mockfs.MapFile{
			"app.log":    {Data: []byte("log"), Mode: 0o644, ModTime: time.Now()},
			"locked.txt": {Data: []byte("data"), Mode: 0o644, ModTime: time.Now()},
		},
		mockfs.WithErrorInjector(injector),
	)

	mfs2 := mockfs.NewMockFS(
		map[string]*mockfs.MapFile{
			"error.log": {Data: []byte("err"), Mode: 0o644, ModTime: time.Now()},
		},
		mockfs.WithErrorInjector(injector),
	)

	// Both filesystems share error rules
	_, err := fs.ReadFile(mfs1, "app.log")
	fmt.Printf("mfs1 app.log: %v\n", err)

	_, err = fs.ReadFile(mfs2, "error.log")
	fmt.Printf("mfs2 error.log: %v\n", err)

	_, err = mfs1.Open("locked.txt")
	fmt.Printf("mfs1 locked.txt: %v\n", err)
	// Output:
	// mfs1 app.log: corrupted data
	// mfs2 error.log: corrupted data
	// mfs1 locked.txt: permission denied
}

// ExampleStats_Delta demonstrates comparing statistics snapshots.
func ExampleStats_Delta() {
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("data"), Mode: 0o644, ModTime: time.Now()},
	})

	// Capture initial stats
	before := mfs.Stats()

	// Perform operations
	mfs.Stat("file.txt")
	mfs.Stat("file.txt")
	mfs.Open("file.txt")
	mfs.ReadDir(".")

	// Capture final stats
	after := mfs.Stats()

	// Calculate delta
	delta := after.Delta(before)
	fmt.Printf("Stat operations: %d\n", delta.Count(mockfs.OpStat))
	fmt.Printf("Open operations: %d\n", delta.Count(mockfs.OpOpen))
	fmt.Printf("ReadDir operations: %d\n", delta.Count(mockfs.OpReadDir))
	fmt.Printf("Total new operations: %d\n", delta.Operations())
	// Output:
	// Stat operations: 2
	// Open operations: 1
	// ReadDir operations: 1
	// Total new operations: 4
}

// ExampleErrorInjector_AddAll demonstrates wildcard matching.
func ExampleErrorInjector_AddAll() {
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file1.txt": {Data: []byte("data"), Mode: 0o644, ModTime: time.Now()},
		"file2.txt": {Data: []byte("data"), Mode: 0o644, ModTime: time.Now()},
		"file3.txt": {Data: []byte("data"), Mode: 0o644, ModTime: time.Now()},
	})

	// All write operations fail (disk full simulation)
	mfs.ErrorInjector().AddAll(mockfs.OpWrite, mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

	// Try writing to different files
	for _, name := range []string{"file1.txt", "file2.txt", "file3.txt"} {
		err := mfs.WriteFile(name, []byte("new"), 0o644)
		fmt.Printf("%s: %v\n", name, err)
	}
	// Output:
	// file1.txt: disk full
	// file2.txt: disk full
	// file3.txt: disk full
}
