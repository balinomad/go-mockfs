package mockfs_test

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/balinomad/go-mockfs"
)

// TestMockFile_Interface verifies MockFile implements required interfaces.
func TestMockFile_Interface(t *testing.T) {
	var _ fs.File = (*mockfs.MockFile)(nil)
	var _ io.Reader = (*mockfs.MockFile)(nil)
	var _ io.Writer = (*mockfs.MockFile)(nil)
	var _ io.Closer = (*mockfs.MockFile)(nil)
}

// createTestFS creates a test filesystem with a single file.
func createTestFS(content string) fstest.MapFS {
	return fstest.MapFS{
		"test.txt": &fstest.MapFile{
			Data: []byte(content),
			Mode: 0644,
		},
	}
}

// TestMockFile_Read tests the Read method.
func TestMockFile_Read(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		readSize    int
		injectError error
	}{
		{
			name:     "read full content",
			content:  "hello world",
			readSize: 100,
		},
		{
			name:     "read partial content",
			content:  "hello world",
			readSize: 5,
		},
		{
			name:     "read empty file",
			content:  "",
			readSize: 10,
		},
		{
			name:        "read with injected error",
			content:     "test",
			readSize:    10,
			injectError: mockfs.ErrCorrupted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFS := createTestFS(tt.content)
			underlyingFile, err := testFS.Open("test.txt")
			if err != nil {
				t.Fatalf("failed to open test file: %v", err)
			}
			defer underlyingFile.Close()

			errorChecker := func(op mockfs.Operation, path string) error {
				if tt.injectError != nil && op == mockfs.OpRead {
					return tt.injectError
				}
				return nil
			}

			f := mockfs.NewMockFile(underlyingFile, "test.txt", errorChecker, nil, nil, nil)

			buf := make([]byte, tt.readSize)
			n, err := f.Read(buf)

			if tt.injectError != nil {
				if !errors.Is(err, tt.injectError) {
					t.Errorf("expected error %v, got %v", tt.injectError, err)
				}
				return
			}

			if err != nil && err != io.EOF {
				t.Errorf("unexpected error: %v", err)
			}

			expectedLen := len(tt.content)
			if expectedLen > tt.readSize {
				expectedLen = tt.readSize
			}
			if n != expectedLen {
				t.Errorf("read %d bytes, expected %d", n, expectedLen)
			}

			if n > 0 && string(buf[:n]) != tt.content[:n] {
				t.Errorf("read %q, expected %q", string(buf[:n]), tt.content[:n])
			}
		})
	}
}

// TestMockFile_Read_Closed tests reading from a closed file.
func TestMockFile_Read_Closed(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)

	if err := f.Close(); err != nil {
		t.Fatalf("failed to close file: %v", err)
	}

	buf := make([]byte, 10)
	_, err = f.Read(buf)
	if !errors.Is(err, fs.ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

// TestMockFile_Read_Multiple tests multiple sequential reads.
func TestMockFile_Read_Multiple(t *testing.T) {
	content := "hello world"
	testFS := createTestFS(content)
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)
	defer f.Close()

	// first read
	buf1 := make([]byte, 5)
	n1, err := f.Read(buf1)
	if err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if n1 != 5 || string(buf1) != "hello" {
		t.Errorf("first read: got %d bytes %q, expected 5 bytes 'hello'", n1, string(buf1))
	}

	// second read
	buf2 := make([]byte, 6)
	n2, err := f.Read(buf2)
	if err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if n2 != 6 || string(buf2) != " world" {
		t.Errorf("second read: got %d bytes %q, expected 6 bytes ' world'", n2, string(buf2))
	}
}

// TestMockFile_Read_WithDelay tests read with simulated delay.
func TestMockFile_Read_WithDelay(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	delayCalled := false
	delaySimulator := func() {
		delayCalled = true
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, delaySimulator, nil, nil)
	defer f.Close()

	buf := make([]byte, 10)
	_, err = f.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read failed: %v", err)
	}

	if !delayCalled {
		t.Error("delay simulator was not called")
	}
}

// TestMockFile_Read_WithCounter tests operation counting.
func TestMockFile_Read_WithCounter(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	var readCount int
	var lastOp mockfs.Operation
	counter := func(op mockfs.Operation) {
		if op == mockfs.OpRead {
			readCount++
		}
		lastOp = op
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, counter, nil)
	defer f.Close()

	buf := make([]byte, 10)
	_, _ = f.Read(buf)

	if readCount != 1 {
		t.Errorf("read count = %d, expected 1", readCount)
	}
	if lastOp != mockfs.OpRead {
		t.Errorf("last operation = %v, expected OpRead", lastOp)
	}
}

// TestMockFile_Write tests the Write method.
func TestMockFile_Write(t *testing.T) {
	tests := []struct {
		name        string
		data        string
		injectError error
	}{
		{
			name: "write simple data",
			data: "hello",
		},
		{
			name: "write empty data",
			data: "",
		},
		{
			name: "write multiline data",
			data: "line1\nline2\nline3",
		},
		{
			name:        "write with injected error",
			data:        "test",
			injectError: mockfs.ErrDiskFull,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFS := createTestFS("")
			underlyingFile, err := testFS.Open("test.txt")
			if err != nil {
				t.Fatalf("failed to open test file: %v", err)
			}
			defer underlyingFile.Close()

			errorChecker := func(op mockfs.Operation, path string) error {
				if tt.injectError != nil && op == mockfs.OpWrite {
					return tt.injectError
				}
				return nil
			}

			var written []byte
			writeHandler := func(b []byte) (int, error) {
				written = append(written, b...)
				return len(b), nil
			}

			f := mockfs.NewMockFile(underlyingFile, "test.txt", errorChecker, nil, nil, writeHandler)
			defer f.Close()

			n, err := f.Write([]byte(tt.data))

			if tt.injectError != nil {
				if !errors.Is(err, tt.injectError) {
					t.Errorf("expected error %v, got %v", tt.injectError, err)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if n != len(tt.data) {
				t.Errorf("wrote %d bytes, expected %d", n, len(tt.data))
			}
			if string(written) != tt.data {
				t.Errorf("written data %q, expected %q", string(written), tt.data)
			}
		})
	}
}

// TestMockFile_Write_Closed tests writing to a closed file.
func TestMockFile_Write_Closed(t *testing.T) {
	testFS := createTestFS("")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}

	writeHandler := func(b []byte) (int, error) {
		return len(b), nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, writeHandler)

	if err := f.Close(); err != nil {
		t.Fatalf("failed to close file: %v", err)
	}

	_, err = f.Write([]byte("data"))
	if !errors.Is(err, fs.ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

// TestMockFile_Write_NoHandler tests writing without a handler.
func TestMockFile_Write_NoHandler(t *testing.T) {
	testFS := createTestFS("")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)
	defer f.Close()

	_, err = f.Write([]byte("data"))
	if !errors.Is(err, fs.ErrInvalid) {
		t.Errorf("expected ErrInvalid for no write handler, got %v", err)
	}
}

// TestMockFile_Write_WithDelay tests write with simulated delay.
func TestMockFile_Write_WithDelay(t *testing.T) {
	testFS := createTestFS("")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	delayCalled := false
	delaySimulator := func() {
		delayCalled = true
	}

	writeHandler := func(b []byte) (int, error) {
		return len(b), nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, delaySimulator, nil, writeHandler)
	defer f.Close()

	_, err = f.Write([]byte("data"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if !delayCalled {
		t.Error("delay simulator was not called")
	}
}

// TestMockFile_Write_WithCounter tests operation counting.
func TestMockFile_Write_WithCounter(t *testing.T) {
	testFS := createTestFS("")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	var writeCount int
	var lastOp mockfs.Operation
	counter := func(op mockfs.Operation) {
		if op == mockfs.OpWrite {
			writeCount++
		}
		lastOp = op
	}

	writeHandler := func(b []byte) (int, error) {
		return len(b), nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, counter, writeHandler)
	defer f.Close()

	_, _ = f.Write([]byte("data"))

	if writeCount != 1 {
		t.Errorf("write count = %d, expected 1", writeCount)
	}
	if lastOp != mockfs.OpWrite {
		t.Errorf("last operation = %v, expected OpWrite", lastOp)
	}
}

// TestMockFile_Stat tests the Stat method.
func TestMockFile_Stat(t *testing.T) {
	content := "test content"
	testFS := createTestFS(content)
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}

	if info.Name() != "test.txt" {
		t.Errorf("name = %q, want 'test.txt'", info.Name())
	}
	if info.Size() != int64(len(content)) {
		t.Errorf("size = %d, want %d", info.Size(), len(content))
	}
	if info.IsDir() {
		t.Error("IsDir() = true, want false")
	}
}

// TestMockFile_Stat_Closed tests Stat on a closed file.
func TestMockFile_Stat_Closed(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)

	if err := f.Close(); err != nil {
		t.Fatalf("failed to close file: %v", err)
	}

	_, err = f.Stat()
	if !errors.Is(err, fs.ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

// TestMockFile_Close tests the Close method.
func TestMockFile_Close(t *testing.T) {
	tests := []struct {
		name        string
		injectError error
	}{
		{
			name: "normal close",
		},
		{
			name:        "close with injected error",
			injectError: mockfs.ErrTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFS := createTestFS("content")
			underlyingFile, err := testFS.Open("test.txt")
			if err != nil {
				t.Fatalf("failed to open test file: %v", err)
			}

			errorChecker := func(op mockfs.Operation, path string) error {
				if tt.injectError != nil && op == mockfs.OpClose {
					return tt.injectError
				}
				return nil
			}

			f := mockfs.NewMockFile(underlyingFile, "test.txt", errorChecker, nil, nil, nil)

			err = f.Close()

			if tt.injectError != nil {
				if !errors.Is(err, tt.injectError) {
					t.Errorf("expected error %v, got %v", tt.injectError, err)
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			// verify file is closed
			_, readErr := f.Read(make([]byte, 10))
			if !errors.Is(readErr, fs.ErrClosed) {
				t.Errorf("file should be closed, got error: %v", readErr)
			}
		})
	}
}

// TestMockFile_Close_Multiple tests closing a file multiple times.
func TestMockFile_Close_Multiple(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)

	// first close
	err = f.Close()
	if err != nil {
		t.Errorf("first close failed: %v", err)
	}

	// second close should return ErrClosed
	err = f.Close()
	if !errors.Is(err, fs.ErrClosed) {
		t.Errorf("second close: expected ErrClosed, got %v", err)
	}

	// third close should also return ErrClosed
	err = f.Close()
	if !errors.Is(err, fs.ErrClosed) {
		t.Errorf("third close: expected ErrClosed, got %v", err)
	}
}

// TestMockFile_Close_WithInjectedError_StillCloses tests that
// file is marked closed even when error is injected.
func TestMockFile_Close_WithInjectedError_StillCloses(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}

	errorChecker := func(op mockfs.Operation, path string) error {
		if op == mockfs.OpClose {
			return mockfs.ErrTimeout
		}
		return nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", errorChecker, nil, nil, nil)

	err = f.Close()
	if !errors.Is(err, mockfs.ErrTimeout) {
		t.Errorf("expected ErrTimeout, got %v", err)
	}

	// verify file is marked as closed despite error
	_, readErr := f.Read(make([]byte, 10))
	if !errors.Is(readErr, fs.ErrClosed) {
		t.Errorf("file should be closed after error, got: %v", readErr)
	}
}

// TestMockFile_Close_WithDelay tests close with simulated delay.
func TestMockFile_Close_WithDelay(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}

	delayCalled := false
	delaySimulator := func() {
		delayCalled = true
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, delaySimulator, nil, nil)

	err = f.Close()
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if !delayCalled {
		t.Error("delay simulator was not called")
	}
}

// TestMockFile_Close_WithCounter tests operation counting.
func TestMockFile_Close_WithCounter(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}

	var closeCount int
	var lastOp mockfs.Operation
	counter := func(op mockfs.Operation) {
		if op == mockfs.OpClose {
			closeCount++
		}
		lastOp = op
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, counter, nil)

	_ = f.Close()

	if closeCount != 1 {
		t.Errorf("close count = %d, expected 1", closeCount)
	}
	if lastOp != mockfs.OpClose {
		t.Errorf("last operation = %v, expected OpClose", lastOp)
	}
}

// TestMockFile_Concurrent_Read tests concurrent reads from the same file.
func TestMockFile_Concurrent_Read(t *testing.T) {
	content := strings.Repeat("test content ", 100)
	testFS := createTestFS(content)
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)
	defer f.Close()

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 10)
			_, err := f.Read(buf)
			if err == nil || err == io.EOF {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if successCount == 0 {
		t.Error("expected at least some successful concurrent reads")
	}
}

// TestMockFile_Concurrent_Close tests concurrent closes.
func TestMockFile_Concurrent_Close(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := f.Close()
			if err == nil {
				mu.Lock()
				successCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	// only one close should succeed
	if successCount != 1 {
		t.Errorf("expected 1 successful close, got %d", successCount)
	}
}

// TestMockFile_Concurrent_ReadWrite tests concurrent reads and writes.
func TestMockFile_Concurrent_ReadWrite(t *testing.T) {
	content := strings.Repeat("x", 1000)
	testFS := createTestFS(content)
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	writeHandler := func(b []byte) (int, error) {
		return len(b), nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, writeHandler)
	defer f.Close()

	var wg sync.WaitGroup

	// concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 10)
			_, _ = f.Read(buf)
		}()
	}

	// concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = f.Write([]byte("data"))
		}()
	}

	wg.Wait()
}

// TestMockFile_LargeFile tests reading large files.
func TestMockFile_LargeFile(t *testing.T) {
	// create large content (1MB)
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	testFS := fstest.MapFS{
		"large.bin": &fstest.MapFile{
			Data: largeData,
			Mode: 0644,
		},
	}

	underlyingFile, err := testFS.Open("large.bin")
	if err != nil {
		t.Fatalf("failed to open large file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "large.bin", nil, nil, nil, nil)
	defer f.Close()

	// read in chunks
	buf := make([]byte, 4096)
	totalRead := 0
	for {
		n, err := f.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read error at offset %d: %v", totalRead, err)
		}
	}

	if totalRead != len(largeData) {
		t.Errorf("read %d bytes, expected %d", totalRead, len(largeData))
	}
}

// TestMockFile_EmptyFile tests operations on empty files.
func TestMockFile_EmptyFile(t *testing.T) {
	testFS := createTestFS("")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)
	defer f.Close()

	buf := make([]byte, 10)
	n, err := f.Read(buf)
	if err != io.EOF {
		t.Errorf("expected EOF for empty file, got %v", err)
	}
	if n != 0 {
		t.Errorf("read %d bytes from empty file, expected 0", n)
	}

	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, expected 0", info.Size())
	}
}

// TestMockFile_ErrorInjection_AllOperations tests error injection for all operations.
func TestMockFile_ErrorInjection_AllOperations(t *testing.T) {
	tests := []struct {
		name string
		op   mockfs.Operation
		exec func(*mockfs.MockFile) error
	}{
		{
			name: "read error",
			op:   mockfs.OpRead,
			exec: func(f *mockfs.MockFile) error {
				_, err := f.Read(make([]byte, 10))
				return err
			},
		},
		{
			name: "write error",
			op:   mockfs.OpWrite,
			exec: func(f *mockfs.MockFile) error {
				_, err := f.Write([]byte("data"))
				return err
			},
		},
		{
			name: "close error",
			op:   mockfs.OpClose,
			exec: func(f *mockfs.MockFile) error {
				return f.Close()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testFS := createTestFS("content")
			underlyingFile, err := testFS.Open("test.txt")
			if err != nil {
				t.Fatalf("failed to open test file: %v", err)
			}
			defer underlyingFile.Close()

			expectedErr := mockfs.ErrCorrupted
			errorChecker := func(op mockfs.Operation, path string) error {
				if op == tt.op {
					return expectedErr
				}
				return nil
			}

			writeHandler := func(b []byte) (int, error) {
				return len(b), nil
			}

			f := mockfs.NewMockFile(underlyingFile, "test.txt", errorChecker, nil, nil, writeHandler)

			err = tt.exec(f)
			if !errors.Is(err, expectedErr) {
				t.Errorf("expected %v, got %v", expectedErr, err)
			}
		})
	}
}

// TestMockFile_BinaryData tests reading and writing binary data.
func TestMockFile_BinaryData(t *testing.T) {
	// create binary data with all byte values
	binaryData := make([]byte, 256)
	for i := range binaryData {
		binaryData[i] = byte(i)
	}

	testFS := fstest.MapFS{
		"binary.dat": &fstest.MapFile{
			Data: binaryData,
			Mode: 0644,
		},
	}

	underlyingFile, err := testFS.Open("binary.dat")
	if err != nil {
		t.Fatalf("failed to open binary file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "binary.dat", nil, nil, nil, nil)
	defer f.Close()

	readData := make([]byte, 256)
	n, err := f.Read(readData)
	if err != nil && err != io.EOF {
		t.Fatalf("read failed: %v", err)
	}

	if n != len(binaryData) {
		t.Errorf("read %d bytes, expected %d", n, len(binaryData))
	}

	for i := range binaryData {
		if readData[i] != binaryData[i] {
			t.Errorf("byte %d: got %d, expected %d", i, readData[i], binaryData[i])
			break
		}
	}
}

// TestMockFile_SpecialPaths tests files with special path characters.
func TestMockFile_SpecialPaths(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"spaces", "file with spaces.txt"},
		{"dashes", "file-with-dashes.txt"},
		{"underscores", "file_with_underscores.txt"},
		{"dots", "file.with.dots.txt"},
		{"mixed", "file-name_test.v2.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := "test content"
			testFS := fstest.MapFS{
				tt.filename: &fstest.MapFile{
					Data: []byte(content),
					Mode: 0644,
				},
			}

			underlyingFile, err := testFS.Open(tt.filename)
			if err != nil {
				t.Fatalf("failed to open file: %v", err)
			}
			defer underlyingFile.Close()

			f := mockfs.NewMockFile(underlyingFile, tt.filename, nil, nil, nil, nil)
			defer f.Close()

			buf := make([]byte, 100)
			n, err := f.Read(buf)
			if err != nil && err != io.EOF {
				t.Fatalf("read failed: %v", err)
			}

			if string(buf[:n]) != content {
				t.Errorf("read %q, expected %q", string(buf[:n]), content)
			}
		})
	}
}

// TestMockFile_NilCallbacks tests MockFile with nil callbacks.
func TestMockFile_NilCallbacks(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	// all callbacks nil
	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)
	defer f.Close()

	// should not panic
	buf := make([]byte, 10)
	_, err = f.Read(buf)
	if err != nil && err != io.EOF {
		t.Errorf("read with nil callbacks failed: %v", err)
	}

	err = f.Close()
	if err != nil {
		t.Errorf("close with nil callbacks failed: %v", err)
	}
}

// TestMockFile_ErrorChecker_CalledWithCorrectParams tests error checker receives correct parameters.
func TestMockFile_ErrorChecker_CalledWithCorrectParams(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	var capturedOp mockfs.Operation
	var capturedPath string
	errorChecker := func(op mockfs.Operation, path string) error {
		capturedOp = op
		capturedPath = path
		return nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", errorChecker, nil, nil, nil)
	defer f.Close()

	_, _ = f.Read(make([]byte, 10))

	if capturedOp != mockfs.OpRead {
		t.Errorf("error checker received op %v, expected OpRead", capturedOp)
	}
	if capturedPath != "test.txt" {
		t.Errorf("error checker received path %q, expected 'test.txt'", capturedPath)
	}
}

// TestMockFile_MultipleOperations tests a sequence of operations.
func TestMockFile_MultipleOperations(t *testing.T) {
	testFS := createTestFS("hello world")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	var ops []mockfs.Operation
	counter := func(op mockfs.Operation) {
		ops = append(ops, op)
	}

	writeHandler := func(b []byte) (int, error) {
		return len(b), nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, counter, writeHandler)

	// perform operations
	_, _ = f.Read(make([]byte, 5))
	_, _ = f.Write([]byte("test"))
	info, _ := f.Stat()
	_ = info
	_, _ = f.Read(make([]byte, 5))
	_ = f.Close()

	expected := []mockfs.Operation{
		mockfs.OpRead,
		mockfs.OpWrite,
		mockfs.OpRead,
		mockfs.OpClose,
	}

	if len(ops) != len(expected) {
		t.Fatalf("captured %d operations, expected %d", len(ops), len(expected))
	}

	for i, op := range expected {
		if ops[i] != op {
			t.Errorf("operation %d: got %v, expected %v", i, ops[i], op)
		}
	}
}

// writerFile is a test helper that implements io.Writer.
type writerFile struct {
	fs.File
	written []byte
}

func (wf *writerFile) Write(b []byte) (int, error) {
	wf.written = append(wf.written, b...)
	return len(b), nil
}

// writerFileWithFlag is a test helper that tracks if Write was called.
type writerFileWithFlag struct {
	fs.File
	underlyingCalled bool
}

func (wf *writerFileWithFlag) Write(b []byte) (int, error) {
	wf.underlyingCalled = true
	return len(b), nil
}

// TestMockFile_WriteHandler_FromUnderlying tests that write handler from underlying file is used.
func TestMockFile_WriteHandler_FromUnderlying(t *testing.T) {
	baseFS := createTestFS("content")
	baseFile, _ := baseFS.Open("test.txt")

	wf := &writerFile{File: baseFile}

	// NewMockFile should use the underlying file's Write method
	f := mockfs.NewMockFile(wf, "test.txt", nil, nil, nil, nil)
	defer f.Close()

	data := []byte("test data")
	n, err := f.Write(data)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("wrote %d bytes, expected %d", n, len(data))
	}
	if string(wf.written) != string(data) {
		t.Errorf("underlying writer got %q, expected %q", string(wf.written), string(data))
	}
}

// TestMockFile_WriteHandler_Priority tests that explicit handler takes priority.
func TestMockFile_WriteHandler_Priority(t *testing.T) {
	baseFS := createTestFS("content")
	baseFile, _ := baseFS.Open("test.txt")

	wf := &writerFileWithFlag{File: baseFile}

	explicitCalled := false
	explicitHandler := func(b []byte) (int, error) {
		explicitCalled = true
		return len(b), nil
	}

	f := mockfs.NewMockFile(wf, "test.txt", nil, nil, nil, explicitHandler)
	defer f.Close()

	_, _ = f.Write([]byte("data"))

	if !explicitCalled {
		t.Error("explicit handler was not called")
	}
	if wf.underlyingCalled {
		t.Error("underlying writer should not be called when explicit handler provided")
	}
}

// TestMockFile_PathInErrorChecker tests that correct path is used in error checking.
func TestMockFile_PathInErrorChecker(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	expectedPath := "custom/path/file.txt"
	var receivedPath string

	errorChecker := func(op mockfs.Operation, path string) error {
		receivedPath = path
		return nil
	}

	f := mockfs.NewMockFile(underlyingFile, expectedPath, errorChecker, nil, nil, nil)
	defer f.Close()

	_, _ = f.Read(make([]byte, 10))

	if receivedPath != expectedPath {
		t.Errorf("error checker received path %q, expected %q", receivedPath, expectedPath)
	}
}

// TestMockFile_Stat_NoErrorInjection tests that Stat doesn't inject errors.
func TestMockFile_Stat_NoErrorInjection(t *testing.T) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		t.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	errorCheckerCalled := false
	errorChecker := func(op mockfs.Operation, path string) error {
		errorCheckerCalled = true
		return mockfs.ErrCorrupted
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", errorChecker, nil, nil, nil)
	defer f.Close()

	_, err = f.Stat()
	if err != nil {
		t.Errorf("Stat() should not inject errors, got: %v", err)
	}
	if errorCheckerCalled {
		t.Error("error checker should not be called for Stat()")
	}
}

// Benchmark tests
func BenchmarkMockFile_Read(b *testing.B) {
	content := make([]byte, 4096)
	testFS := fstest.MapFS{
		"bench.txt": &fstest.MapFile{
			Data: content,
			Mode: 0644,
		},
	}

	underlyingFile, err := testFS.Open("bench.txt")
	if err != nil {
		b.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "bench.txt", nil, nil, nil, nil)
	defer f.Close()

	buf := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Read(buf)
	}
}

func BenchmarkMockFile_Write(b *testing.B) {
	testFS := createTestFS("")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		b.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	writeHandler := func(data []byte) (int, error) {
		return len(data), nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, writeHandler)
	defer f.Close()

	data := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Write(data)
	}
}

func BenchmarkMockFile_Stat(b *testing.B) {
	testFS := createTestFS("content")
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		b.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)
	defer f.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Stat()
	}
}

func BenchmarkMockFile_Close(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		testFS := createTestFS("content")
		underlyingFile, err := testFS.Open("test.txt")
		if err != nil {
			b.Fatalf("failed to open test file: %v", err)
		}

		f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, nil, nil)
		b.StartTimer()

		_ = f.Close()
	}
}

func BenchmarkMockFile_ReadWithErrorCheck(b *testing.B) {
	testFS := createTestFS(string(make([]byte, 4096)))
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		b.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	errorChecker := func(op mockfs.Operation, path string) error {
		return nil
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", errorChecker, nil, nil, nil)
	defer f.Close()

	buf := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Read(buf)
	}
}

func BenchmarkMockFile_ReadWithDelay(b *testing.B) {
	testFS := createTestFS(string(make([]byte, 4096)))
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		b.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	delaySimulator := func() {
		// no-op delay for benchmark
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, delaySimulator, nil, nil)
	defer f.Close()

	buf := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Read(buf)
	}
}

func BenchmarkMockFile_ReadWithCounter(b *testing.B) {
	testFS := createTestFS(string(make([]byte, 4096)))
	underlyingFile, err := testFS.Open("test.txt")
	if err != nil {
		b.Fatalf("failed to open test file: %v", err)
	}
	defer underlyingFile.Close()

	counter := func(op mockfs.Operation) {
		// no-op counter for benchmark
	}

	f := mockfs.NewMockFile(underlyingFile, "test.txt", nil, nil, counter, nil)
	defer f.Close()

	buf := make([]byte, 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = f.Read(buf)
	}
}
