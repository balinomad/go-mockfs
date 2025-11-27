package mockfs_test

import (
	"fmt"
	"io"
	"io/fs"

	"github.com/balinomad/go-mockfs/v2"
)

// ExampleMockFS_FailOpen demonstrates basic error injection.
func ExampleMockFS_FailOpen() {
	mfs := mockfs.NewMockFS(mockfs.File("secret.txt", []byte("classified")))

	// Inject permission error
	mfs.FailOpen("secret.txt", mockfs.ErrPermission)

	_, err := mfs.Open("secret.txt")
	fmt.Printf("Error: %v\n", err)
	// Output: Error: permission denied
}

// ExampleMockFS_FailReadOnce demonstrates one-time error injection.
func ExampleMockFS_FailReadOnce() {
	mfs := mockfs.NewMockFS(mockfs.File("flaky.txt", []byte("data")))

	mfs.FailReadOnce("flaky.txt", io.ErrUnexpectedEOF)

	file, _ := mfs.Open("flaky.txt")
	defer file.Close()

	buf := make([]byte, 10)

	// First read fails
	_, err := file.Read(buf)
	fmt.Printf("First read: %v\n", err)

	// Second read succeeds
	_, err = file.Read(buf)
	fmt.Printf("Second read: %v\n", err)
	// Output:
	// First read: unexpected EOF
	// Second read: <nil>
}

// ExampleMockFS_FailReadAfter demonstrates error after N successes.
func ExampleMockFS_FailReadAfter() {
	mfs := mockfs.NewMockFS(mockfs.File("stream.txt", []byte("123456789")))

	// Error after 3 successful reads
	mfs.FailReadAfter("stream.txt", io.EOF, 3)

	file, _ := mfs.Open("stream.txt")
	defer file.Close()

	buf := make([]byte, 1)
	for i := 1; i <= 5; i++ {
		_, err := file.Read(buf)
		if err != nil {
			fmt.Printf("Read %d: error - %v\n", i, err)
			break
		}
		fmt.Printf("Read %d: success\n", i)
	}
	// Output:
	// Read 1: success
	// Read 2: success
	// Read 3: success
	// Read 4: error - EOF
}

// ExampleErrorInjector_AddGlob demonstrates glob pattern matching.
func ExampleErrorInjector_AddGlob() {
	mfs := mockfs.NewMockFS(
		mockfs.File("app.log", []byte("log")),
		mockfs.File("error.log", []byte("err")),
		mockfs.File("data.txt", []byte("txt")),
	)

	// All .log files fail to read
	mfs.ErrorInjector().AddGlob(mockfs.OpRead, "*.log", io.ErrUnexpectedEOF, mockfs.ErrorModeAlways, 0)

	// Try reading each file
	for _, name := range []string{"app.log", "error.log", "data.txt"} {
		_, err := fs.ReadFile(mfs, name)
		fmt.Printf("%s: %v\n", name, err)
	}
	// Output:
	// app.log: unexpected EOF
	// error.log: unexpected EOF
	// data.txt: <nil>
}

// ExampleErrorInjector_AddRegexp demonstrates regex pattern matching.
func ExampleErrorInjector_AddRegexp() {
	mfs := mockfs.NewMockFS(
		mockfs.File("file.tmp", []byte("tmp")),
		mockfs.File("data.tmp", []byte("tmp")),
		mockfs.File("file.txt", []byte("txt")),
	)

	// All .tmp files return corrupted error
	mfs.ErrorInjector().AddRegexp(mockfs.OpRead, `\.tmp$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)

	for _, name := range []string{"file.tmp", "data.tmp", "file.txt"} {
		_, err := fs.ReadFile(mfs, name)
		fmt.Printf("%s: %v\n", name, err)
	}
	// Output:
	// file.tmp: corrupted data
	// data.tmp: corrupted data
	// file.txt: <nil>
}

// ExampleErrorInjector_AddExactForAllOps demonstrates cross-operation errors.
func ExampleErrorInjector_AddExactForAllOps() {
	mfs := mockfs.NewMockFS(mockfs.File("broken.txt", []byte("data")))

	// All operations on this file fail
	mfs.ErrorInjector().AddExactForAllOps("broken.txt", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)

	// Try different operations
	_, err := mfs.Stat("broken.txt")
	fmt.Printf("Stat: %v\n", err)

	_, err = mfs.Open("broken.txt")
	fmt.Printf("Open: %v\n", err)

	_, err = fs.ReadFile(mfs, "broken.txt")
	fmt.Printf("ReadFile: %v\n", err)
	// Output:
	// Stat: corrupted data
	// Open: corrupted data
	// ReadFile: corrupted data
}

// ExampleMockFS_MarkNonExistent demonstrates simulating missing files.
func ExampleMockFS_MarkNonExistent() {
	mfs := mockfs.NewMockFS(
		mockfs.File("exists.txt", []byte("data")),
		mockfs.File("deleted.txt", []byte("data")),
	)

	// Mark as non-existent
	mfs.MarkNonExistent("deleted.txt")

	_, err := mfs.Stat("exists.txt")
	fmt.Printf("exists.txt: %v\n", err)

	_, err = mfs.Stat("deleted.txt")
	fmt.Printf("deleted.txt: %v\n", err)
	// Output:
	// exists.txt: <nil>
	// deleted.txt: file does not exist
}
