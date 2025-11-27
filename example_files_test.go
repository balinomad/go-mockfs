package mockfs_test

import (
	"fmt"
	"io"
	"io/fs"
	"time"

	"github.com/balinomad/go-mockfs/v2"
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
	// Create entries using NewFileInfo
	entries := []fs.DirEntry{
		mockfs.NewFileInfo("file1.txt", 5, 0o644, time.Now()),
		mockfs.NewFileInfo("file2.txt", 5, 0o644, time.Now()),
	}

	// Create directory with these entries
	handler := mockfs.NewDirHandler(entries)
	dir := mockfs.NewMockDirectory("mydir", handler)

	// ReadDir returns entries in sorted order
	readDirFile := dir.(fs.ReadDirFile)
	list, _ := readDirFile.ReadDir(-1)

	fmt.Printf("Directory contains %d entries\n", len(list))
	for _, e := range list {
		fmt.Printf("  %s\n", e.Name())
	}
	// Output:
	// Directory contains 2 entries
	//   file1.txt
	//   file2.txt
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

// ExampleMockFile_Seek demonstrates file seeking.
func ExampleMockFile_Seek() {
	file := mockfs.NewMockFileFromString("test.txt", "Hello, World!")

	// Read first 5 bytes
	buf := make([]byte, 5)
	file.Read(buf)
	fmt.Printf("First read: %s\n", buf)

	// Seek back to beginning
	seeker := file.(io.Seeker)
	seeker.Seek(0, io.SeekStart)

	// Read again
	file.Read(buf)
	fmt.Printf("After rewind: %s\n", buf)

	// Seek to end
	seeker.Seek(0, io.SeekEnd)
	pos, _ := seeker.Seek(0, io.SeekCurrent)
	fmt.Printf("Position at end: %d\n", pos)
	// Output:
	// First read: Hello
	// After rewind: Hello
	// Position at end: 13
}

// ExampleNewDirHandler demonstrates creating a directory handler.
func ExampleNewDirHandler() {
	// Create entries manually using fileInfo helper
	mfs := mockfs.NewMockFS(
		mockfs.File("readme.txt", []byte("info")),
		mockfs.File("data.json", []byte("{}")),
	)

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

// ExampleNewFileInfo demonstrates creating directory entries for testing.
func ExampleNewFileInfo() {
	// Create file info entries
	file1 := mockfs.NewFileInfo("readme.txt", 1024, 0o644, time.Now())
	dir1 := mockfs.NewFileInfo("docs", 0, mockfs.ModeDir|0o755, time.Now())

	fmt.Printf("%s: size=%d, isDir=%v\n", file1.Name(), file1.Size(), file1.IsDir())
	fmt.Printf("%s: size=%d, isDir=%v\n", dir1.Name(), dir1.Size(), dir1.IsDir())
	// Output:
	// readme.txt: size=1024, isDir=false
	// docs: size=0, isDir=true
}
