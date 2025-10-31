package mockfs_test

import (
	"fmt"
	"io/fs"
	"time"

	"github.com/balinomad/go-mockfs/v2"
)

// Example demonstrates basic mockfs usage.
func Example() {
	// Create a mock filesystem with initial files
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"hello.txt": {
			Data:    []byte("Hello, World!"),
			Mode:    0644,
			ModTime: time.Now(),
		},
	})

	// Read the file
	data, err := fs.ReadFile(mfs, "hello.txt")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	fmt.Printf("%s\n", data)
	// Output: Hello, World!
}

// ExampleNewMockFS demonstrates creating a filesystem with multiple files.
func ExampleNewMockFS() {
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"config.json": {
			Data:    []byte(`{"debug": true}`),
			Mode:    0644,
			ModTime: time.Now(),
		},
		"data": {
			Mode:    fs.ModeDir | 0755,
			ModTime: time.Now(),
		},
		"data/file.txt": {
			Data:    []byte("content"),
			Mode:    0644,
			ModTime: time.Now(),
		},
	})

	// List root directory
	entries, _ := fs.ReadDir(mfs, ".")
	for _, e := range entries {
		fmt.Printf("%s (dir: %v)\n", e.Name(), e.IsDir())
	}
	// Output:
	// config.json (dir: false)
	// data (dir: true)
}

// ExampleMockFS_Open demonstrates opening and reading a file.
func ExampleMockFS_Open() {
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"data.txt": {
			Data:    []byte("line1\nline2\nline3"),
			Mode:    0644,
			ModTime: time.Now(),
		},
	})

	file, err := mfs.Open("data.txt")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}
	defer file.Close()

	buf := make([]byte, 5)
	n, _ := file.Read(buf)
	fmt.Printf("Read %d bytes: %s\n", n, buf)
	// Output: Read 5 bytes: line1
}

// ExampleMockFS_Stat demonstrates getting file information.
func ExampleMockFS_Stat() {
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {
			Data:    []byte("content"),
			Mode:    0644,
			ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	})

	info, err := mfs.Stat("file.txt")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	fmt.Printf("Name: %s\n", info.Name())
	fmt.Printf("Size: %d\n", info.Size())
	fmt.Printf("Mode: %v\n", info.Mode())
	// Output:
	// Name: file.txt
	// Size: 7
	// Mode: -rw-r--r--
}

// ExampleMockFS_ReadDir demonstrates listing directory contents.
func ExampleMockFS_ReadDir() {
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"logs":         {Mode: fs.ModeDir | 0755, ModTime: time.Now()},
		"logs/app.log": {Data: []byte("log"), Mode: 0644, ModTime: time.Now()},
		"logs/err.log": {Data: []byte("err"), Mode: 0644, ModTime: time.Now()},
	})

	entries, err := mfs.ReadDir("logs")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	for _, e := range entries {
		fmt.Printf("%s\n", e.Name())
	}
	// Output:
	// app.log
	// err.log
}

// ExampleMockFS_Stats demonstrates tracking filesystem operations.
func ExampleMockFS_Stats() {
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file.txt": {Data: []byte("data"), Mode: 0644, ModTime: time.Now()},
	})

	// Perform operations
	mfs.Stat("file.txt")
	mfs.Open("file.txt")
	mfs.ReadDir(".")

	stats := mfs.Stats()
	fmt.Printf("Stat calls: %d\n", stats.Count(mockfs.OpStat))
	fmt.Printf("Open calls: %d\n", stats.Count(mockfs.OpOpen))
	fmt.Printf("ReadDir calls: %d\n", stats.Count(mockfs.OpReadDir))
	fmt.Printf("Total operations: %d\n", stats.Operations())
	// Output:
	// Stat calls: 1
	// Open calls: 1
	// ReadDir calls: 1
	// Total operations: 3
}

// ExampleMockFS_AddFile demonstrates adding files dynamically.
func ExampleMockFS_AddFile() {
	mfs := mockfs.NewMockFS(nil)

	// Add a file
	err := mfs.AddFile("config.json", `{"key": "value"}`, 0644)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	data, _ := fs.ReadFile(mfs, "config.json")
	fmt.Printf("%s\n", data)
	// Output: {"key": "value"}
}

// ExampleMockFS_AddDir demonstrates adding directories.
func ExampleMockFS_AddDir() {
	mfs := mockfs.NewMockFS(nil)

	// Add directory
	err := mfs.AddDir("logs", 0755)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	info, _ := mfs.Stat("logs")
	fmt.Printf("Is directory: %v\n", info.IsDir())
	// Output: Is directory: true
}
