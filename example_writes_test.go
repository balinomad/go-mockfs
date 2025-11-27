package mockfs_test

import (
	"fmt"
	"io/fs"

	"github.com/balinomad/go-mockfs/v2"
)

// ExampleMockFS_WriteFile demonstrates writing files.
func ExampleMockFS_WriteFile() {
	mfs := mockfs.NewMockFS(mockfs.WithCreateIfMissing(true))

	err := mfs.WriteFile("output.txt", []byte("Hello, World!"), 0o644)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	data, _ := fs.ReadFile(mfs, "output.txt")
	fmt.Printf("%s\n", data)
	// Output: Hello, World!
}

// ExampleMockFS_Mkdir demonstrates creating directories.
func ExampleMockFS_Mkdir() {
	mfs := mockfs.NewMockFS()

	err := mfs.Mkdir("logs", 0o755)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	info, _ := mfs.Stat("logs")
	fmt.Printf("Created directory: %v\n", info.IsDir())
	// Output: Created directory: true
}

// ExampleMockFS_MkdirAll demonstrates creating directory hierarchy.
func ExampleMockFS_MkdirAll() {
	mfs := mockfs.NewMockFS()

	err := mfs.MkdirAll("app/config/prod", 0o755)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	// Check each level exists
	for _, path := range []string{"app", "app/config", "app/config/prod"} {
		info, _ := mfs.Stat(path)
		fmt.Printf("%s exists: %v\n", path, info.IsDir())
	}
	// Output:
	// app exists: true
	// app/config exists: true
	// app/config/prod exists: true
}

// ExampleMockFS_Remove demonstrates removing files.
func ExampleMockFS_Remove() {
	mfs := mockfs.NewMockFS(mockfs.File("temp.txt", []byte("data"), 0o644))

	err := mfs.Remove("temp.txt")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	_, err = mfs.Stat("temp.txt")
	fmt.Printf("File removed: %v\n", err != nil)
	// Output: File removed: true
}

// ExampleMockFS_RemoveAll demonstrates removing directory trees.
func ExampleMockFS_RemoveAll() {
	mfs := mockfs.NewMockFS(
		mockfs.Dir("cache",
			mockfs.File("cache/file1.txt", []byte("1")),
			mockfs.File("cache/file2.txt", []byte("2")),
		))

	err := mfs.RemoveAll("cache")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	_, err = mfs.Stat("cache")
	fmt.Printf("Directory removed: %v\n", err != nil)
	// Output: Directory removed: true
}

// ExampleMockFS_Rename demonstrates renaming files and directories.
func ExampleMockFS_Rename() {
	mfs := mockfs.NewMockFS(mockfs.File("old.txt", []byte("content")))

	err := mfs.Rename("old.txt", "new.txt")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return
	}

	_, err = mfs.Stat("old.txt")
	fmt.Printf("Old name exists: %v\n", err == nil)

	_, err = mfs.Stat("new.txt")
	fmt.Printf("New name exists: %v\n", err == nil)
	// Output:
	// Old name exists: false
	// New name exists: true
}

// ExampleWithOverwrite demonstrates overwrite mode.
func ExampleWithOverwrite() {
	mfs := mockfs.NewMockFS(mockfs.File("file.txt", []byte("original")), mockfs.WithOverwrite())

	mfs.WriteFile("file.txt", []byte("replaced"), 0o644)

	data, _ := fs.ReadFile(mfs, "file.txt")
	fmt.Printf("%s\n", data)
	// Output: replaced
}

// ExampleWithAppend demonstrates append mode.
func ExampleWithAppend() {
	mfs := mockfs.NewMockFS(mockfs.File("log.txt", []byte("line1\n")), mockfs.WithAppend())

	mfs.WriteFile("log.txt", []byte("line2\n"), 0o644)
	mfs.WriteFile("log.txt", []byte("line3\n"), 0o644)

	data, _ := fs.ReadFile(mfs, "log.txt")
	fmt.Printf("%s", data)
	// Output:
	// line1
	// line2
	// line3
}
