package mockfs_test

import (
	"fmt"
	"io/fs"

	"github.com/balinomad/go-mockfs/v2"
)

// Example demonstrates basic mockfs usage.
func Example() {
	// Create a mock filesystem with initial files
	mfs := mockfs.NewMockFS(mockfs.File("hello.txt", []byte("Hello, World!")))

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
	mfs := mockfs.NewMockFS(
		mockfs.File("config.json", []byte(`{"debug": true}`)),
		mockfs.Dir("data",
			mockfs.File("file.txt", []byte("content")),
		),
	)

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
	mfs := mockfs.NewMockFS(mockfs.File("data.txt", []byte("line1\nline2\nline3")))

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
	mfs := mockfs.NewMockFS(mockfs.File("file.txt", []byte("content"), 0o664))

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
	// Mode: -rw-rw-r--
}

// ExampleMockFS_ReadDir demonstrates listing directory contents.
func ExampleMockFS_ReadDir() {
	mfs := mockfs.NewMockFS(
		mockfs.Dir("logs",
			mockfs.File("app.log", []byte("log")),
			mockfs.File("err.log", []byte("err")),
		))

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
	mfs := mockfs.NewMockFS(mockfs.File("file.txt", []byte("data")))

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
	mfs := mockfs.NewMockFS()

	// Add a file
	err := mfs.AddFile("config.json", `{"key": "value"}`, 0o644)
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
	mfs := mockfs.NewMockFS()

	// Add directory
	err := mfs.AddDir("logs", 0o755)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	info, _ := mfs.Stat("logs")
	fmt.Printf("Is directory: %v\n", info.IsDir())
	// Output: Is directory: true
}
