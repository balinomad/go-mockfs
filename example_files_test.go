package mockfs_test

import (
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/balinomad/go-mockfs"
)

// ExampleNewMockFileFromString demonstrates standalone file creation.
func ExampleNewMockFileFromString() {
	file := mockfs.NewMockFileFromString("test.txt", "Hello, World!")

	buf := make([]byte, 5)
	n, _ := file.Read(buf)

	fmt.Printf("Read %d bytes: %s\n", n, buf)
	// Output: Read 5 bytes: Hello
}

// ExampleNewMockFileFromBytes demonstrates binary file creation.
func ExampleNewMockFileFromBytes() {
	data := []byte{0x48, 0x65, 0x6C, 0x6C, 0x6F} // "Hello"
	file := mockfs.NewMockFileFromBytes("data.bin", data)

	buf := make([]byte, 5)
	file.Read(buf)

	fmt.Printf("%s\n", buf)
	// Output: Hello
}

// ExampleNewMockDirectory demonstrates directory creation with ReadDir.
func ExampleNewMockDirectory() {
	// Create mock filesystem to generate proper DirEntry values
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"file1.txt": {Data: []byte("data1"), Mode: 0644, ModTime: time.Now()},
	})

	// Collect entries from filesystem
	entries, _ := mfs.ReadDir(".")

	// Create directory with these entries
	handler := mockfs.NewDirHandler(entries)
	dir := mockfs.NewMockDirectory("mydir", handler)

	// ReadDir returns entries
	readDirFile := dir.(fs.ReadDirFile)
	list, _ := readDirFile.ReadDir(-1)

	fmt.Printf("Directory contains %d entries\n", len(list))
	// Output: Directory contains 1 entries
}

// ExampleMockFile_Stats demonstrates file-handle statistics.
func ExampleMockFile_Stats() {
	file := mockfs.NewMockFileFromString("test.txt", "data content here")

	// Perform operations
	buf := make([]byte, 4)
	file.Read(buf)
	file.Read(buf)
	file.Read(buf)
	file.Close()

	// Check stats
	stats := file.Stats()
	fmt.Printf("Read operations: %d\n", stats.Count(mockfs.OpRead))
	fmt.Printf("Bytes read: %d\n", stats.BytesRead())
	fmt.Printf("Close operations: %d\n", stats.Count(mockfs.OpClose))
	// Output:
	// Read operations: 3
	// Bytes read: 12
	// Close operations: 1
}

// ExampleNewDirHandler demonstrates creating a directory handler.
func ExampleNewDirHandler() {
	// Create entries manually using fileInfo helper
	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
		"readme.txt": {Data: []byte("info"), Mode: 0644, ModTime: time.Now()},
		"data.json":  {Data: []byte("{}"), Mode: 0644, ModTime: time.Now()},
	})

	entries, _ := mfs.ReadDir(".")
	handler := mockfs.NewDirHandler(entries)

	// Use handler to read directory in chunks
	batch1, err := handler(1) // Read first entry
	fmt.Printf("First batch: %d entries, EOF: %v\n", len(batch1), err == io.EOF)

	batch2, err := handler(1) // Read second entry
	fmt.Printf("Second batch: %d entries, EOF: %v\n", len(batch2), err == io.EOF)

	batch3, err := handler(1) // No more entries
	fmt.Printf("Third batch: %d entries, EOF: %v\n", len(batch3), err == io.EOF)
	// Output:
	// First batch: 1 entries, EOF: false
	// Second batch: 1 entries, EOF: true
	// Third batch: 0 entries, EOF: true
}
