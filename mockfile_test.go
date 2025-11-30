package mockfs_test

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/balinomad/go-mockfs/v2"
)

// --- Constructors ---

// TestNewMockFile tests the main constructor.
func TestNewMockFile(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("test data"),
		Mode:    0o644,
		ModTime: time.Now(),
	}

	tests := []struct {
		name      string
		mapFile   *fstest.MapFile
		fileName  string
		wantPanic bool
	}{
		{
			name:      "valid file",
			mapFile:   mapFile,
			fileName:  "test.txt",
			wantPanic: false,
		},
		{
			name:      "nil mapfile panics",
			mapFile:   nil,
			fileName:  "test.txt",
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if (r != nil) != tt.wantPanic {
					t.Errorf("panic = %v, wantPanic = %v", r != nil, tt.wantPanic)
				}
			}()

			file := mockfs.NewMockFile(tt.mapFile, tt.fileName, mockfs.WithFileOverwrite())
			if file == nil && !tt.wantPanic {
				t.Error("expected non-nil file")
			}
		})
	}
}

// TestNewMockFile_Defaults tests that nil arguments to NewMockFile are
// initialized with non-nil default implementations.
func TestNewMockFile_Defaults(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test")}
	// Call with nil for injector, latency, and stats
	file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileOverwrite())

	if file.ErrorInjector() == nil {
		t.Error("expected non-nil default ErrorInjector")
	}
	if file.LatencySimulator() == nil {
		t.Error("expected non-nil default LatencySimulator")
	}
	stats := file.Stats()
	if stats.BytesRead() != 0 || stats.BytesWritten() != 0 {
		t.Error("expected zero-initialized stats")
	}
}

// TestNewMockFileFromString tests creating file from string.
func TestNewMockFileFromString(t *testing.T) {
	file := mockfs.NewMockFileFromString("test.txt", "hello")
	if file == nil {
		t.Fatal("expected non-nil file")
	}

	// Should be able to read
	buf := make([]byte, 5)
	n, err := file.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Errorf("read = %q, want %q", buf[:n], "hello")
	}
}

// TestNewMockFileFromBytes tests creating file from bytes.
func TestNewMockFileFromBytes(t *testing.T) {
	data := []byte("test content")
	file := mockfs.NewMockFileFromBytes("test.txt", data)

	if file == nil {
		t.Fatal("expected non-nil file")
	}

	buf := make([]byte, len(data))
	n, err := file.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if n != len(data) || string(buf) != string(data) {
		t.Errorf("read = %q, want %q", buf[:n], data)
	}
}

// --- Options ---

// TestWithFileStats tests that the WithFileStats option works.
func TestWithFileStats(t *testing.T) {
	t.Parallel()

	sharedStats := mockfs.NewStatsRecorder(nil)
	file := mockfs.NewMockFileFromString("test.txt", "data", mockfs.WithFileStats(sharedStats))

	buf := make([]byte, 4)
	_, _ = file.Read(buf)

	if sharedStats.Count(mockfs.OpRead) != 1 {
		t.Errorf("shared stats not updated: count = %d, want 1", sharedStats.Count(mockfs.OpRead))
	}
}

// TestWithFileLatency tests latency constructor.
func TestWithFileLatency(t *testing.T) {
	file := mockfs.NewMockFileFromString("test.txt", "data", mockfs.WithFileLatency(testDuration))

	buf := make([]byte, 4)
	start := time.Now()
	_, err := file.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	assertDuration(t, start, testDuration, "read with latency")
}

// TestWithFilePerOperationLatency tests read/write focused constructor with per-op latency.
func TestWithFilePerOperationLatency(t *testing.T) {
	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrUnexpectedEOF, mockfs.ErrorModeOnce, 0)
	file := mockfs.NewMockFileFromString("test.txt", "test",
		mockfs.WithFileErrorInjector(injector),
		mockfs.WithFilePerOperationLatency(map[mockfs.Operation]time.Duration{
			mockfs.OpRead:  testDuration,
			mockfs.OpWrite: testDuration,
		}))

	// First read should fail
	buf := make([]byte, 4)
	_, err := file.Read(buf)
	if err != mockfs.ErrUnexpectedEOF {
		t.Errorf("first read error = %v, want %v", err, mockfs.ErrUnexpectedEOF)
	}

	// Second read should succeed with latency
	start := time.Now()
	n, err := file.Read(buf)
	if err != nil {
		t.Fatalf("second read failed: %v", err)
	}
	if n != 4 {
		t.Errorf("read n = %d, want 4", n)
	}
	assertDuration(t, start, testDuration, "read latency")

	// Close should not have latency (not in per-op config)
	start = time.Now()
	_ = file.Close()
	assertNoDuration(t, start, "close should be fast")
}

// TestNewMockDir tests directory constructor.
func TestNewMockDir(t *testing.T) {
	t.Run("with handler", func(t *testing.T) {
		handler := func(n int) ([]fs.DirEntry, error) {
			return []fs.DirEntry{}, nil
		}

		dir := mockfs.NewMockDir("testdir", handler)
		if dir == nil {
			t.Fatal("expected non-nil directory")
		}

		entries, err := dir.ReadDir(-1)
		if err != nil {
			t.Fatalf("readdir failed: %v", err)
		}
		if entries == nil {
			t.Error("expected non-nil entries")
		}
	})

	t.Run("nil handler is valid", func(t *testing.T) {
		dir := mockfs.NewMockDir("testdir", nil)
		if dir == nil {
			t.Fatal("expected non-nil directory")
		}
	})
}

// --- Methods ---

// TestMockFile_Seek tests Seek operation.
func TestMockFile_Seek(t *testing.T) {
	file := mockfs.NewMockFileFromString("test.txt", "0123456789")

	// Seek to 4
	_, err := file.Seek(4, io.SeekStart)
	if err != nil {
		t.Fatalf("seek to 4 failed: %v", err)
	}

	// Read from offset 4
	buf := make([]byte, 5)
	n, err := file.Read(buf)
	if err != nil {
		t.Fatalf("read from offset 4 failed: %v", err)
	}
	if n != 5 || string(buf) != "45678" {
		t.Errorf("read from offset 4 = %q, want %q", buf[:n], "45678")
	}

	// Seek to end
	_, err = file.Seek(0, io.SeekEnd)
	if err != nil {
		t.Fatalf("seek to end failed: %v", err)
	}

	// Read from end
	_, err = file.Read(buf)
	if err != io.EOF {
		t.Errorf("read from end = %v, want %v", err, io.EOF)
	}
}

// TestMockFile_Read tests Read operation.
func TestMockFile_Read(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		bufSize  int
		wantN    int
		wantData string
		wantErr  error
	}{
		{
			name:     "read full content",
			data:     []byte("hello world"),
			bufSize:  20,
			wantN:    11,
			wantData: "hello world",
			wantErr:  nil,
		},
		{
			name:     "read partial content",
			data:     []byte("hello world"),
			bufSize:  5,
			wantN:    5,
			wantData: "hello",
			wantErr:  nil,
		},
		{
			name:     "read empty file",
			data:     []byte(""),
			bufSize:  10,
			wantN:    0,
			wantData: "",
			wantErr:  io.EOF,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := mockfs.NewMockFileFromBytes("test.txt", tt.data)

			buf := make([]byte, tt.bufSize)
			n, err := file.Read(buf)

			if err != tt.wantErr {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
			if n != tt.wantN {
				t.Errorf("n = %d, want %d", n, tt.wantN)
			}
			if n > 0 && string(buf[:n]) != tt.wantData {
				t.Errorf("data = %q, want %q", buf[:n], tt.wantData)
			}
		})
	}
}

// TestMockFile_Read_Position tests that read position advances.
func TestMockFile_Read_Position(t *testing.T) {
	file := mockfs.NewMockFileFromString("test.txt", "abcdefghij")

	// First read
	buf1 := make([]byte, 3)
	n, err := file.Read(buf1)
	if err != nil || n != 3 || string(buf1) != "abc" {
		t.Fatalf("first read: n=%d, data=%q, err=%v", n, buf1, err)
	}

	// Second read should continue from position
	buf2 := make([]byte, 3)
	n, err = file.Read(buf2)
	if err != nil || n != 3 || string(buf2) != "def" {
		t.Fatalf("second read: n=%d, data=%q, err=%v", n, buf2, err)
	}

	// Third read
	buf3 := make([]byte, 10)
	n, err = file.Read(buf3)
	if err != nil || n != 4 || string(buf3[:n]) != "ghij" {
		t.Fatalf("third read: n=%d, data=%q, err=%v", n, buf3[:n], err)
	}

	// Fourth read should return EOF
	buf4 := make([]byte, 10)
	n, err = file.Read(buf4)
	if err != io.EOF || n != 0 {
		t.Fatalf("fourth read: n=%d, err=%v, want EOF", n, err)
	}
}

// TestMockFile_Read_Closed tests reading from closed file.
func TestMockFile_Read_Closed(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	if err := file.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	buf := make([]byte, 10)
	_, err := file.Read(buf)
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Read_ErrorInjection tests error injection on read.
func TestMockFile_Read_ErrorInjection(t *testing.T) {
	wantErr := mockfs.ErrUnexpectedEOF

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpRead, "test.txt", wantErr, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFileFromString("test.txt", "test", mockfs.WithFileErrorInjector(injector))

	buf := make([]byte, 10)
	_, err := file.Read(buf)
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// TestMockFile_Read_LargeFile tests reading a large file.
func TestMockFile_Read_LargeFile(t *testing.T) {
	size := 10 * 1024 * 1024 // 10MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	file := mockfs.NewMockFileFromBytes("large.bin", data)

	bufSize := 4096
	buf := make([]byte, bufSize)
	totalRead := 0

	for {
		n, err := file.Read(buf)
		totalRead += n

		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read failed at %d: %v", totalRead, err)
		}
	}

	if totalRead != size {
		t.Errorf("totalRead = %d, want %d", totalRead, size)
	}
}

// TestMockFile_Read_ZeroByte tests reading with zero-length buffer.
func TestMockFile_Read_ZeroByte(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	buf := make([]byte, 0)
	n, err := file.Read(buf)
	if err != nil {
		t.Errorf("zero-byte read failed: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

// TestMockFile_ReadAt tests ReadAt operation at various offsets.
func TestMockFile_ReadAt(t *testing.T) {
	t.Parallel()

	content := []byte("0123456789abcdef")
	file := mockfs.NewMockFileFromBytes("test.txt", content)

	tests := []struct {
		name    string
		offset  int64
		bufSize int
		wantN   int
		wantErr error
		wantStr string
	}{
		{
			name:    "read from start",
			offset:  0,
			bufSize: 5,
			wantN:   5,
			wantErr: nil,
			wantStr: "01234",
		},
		{
			name:    "read from middle",
			offset:  5,
			bufSize: 5,
			wantN:   5,
			wantErr: nil,
			wantStr: "56789",
		},
		{
			name:    "read to end exact",
			offset:  10,
			bufSize: 6,
			wantN:   6,
			wantErr: nil,
			wantStr: "abcdef",
		},
		{
			name:    "read beyond end",
			offset:  10,
			bufSize: 10,
			wantN:   6,
			wantErr: io.EOF,
			wantStr: "abcdef",
		},
		{
			name:    "read at end",
			offset:  16,
			bufSize: 10,
			wantN:   0,
			wantErr: io.EOF,
			wantStr: "",
		},
		{
			name:    "read past end",
			offset:  20,
			bufSize: 10,
			wantN:   0,
			wantErr: io.EOF,
			wantStr: "",
		},
		{
			name:    "negative offset",
			offset:  -1,
			bufSize: 10,
			wantN:   0,
			wantErr: mockfs.ErrNegativeOffset,
			wantStr: "",
		},
		{
			name:    "zero length buffer",
			offset:  5,
			bufSize: 0,
			wantN:   0,
			wantErr: nil,
			wantStr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.bufSize)
			n, err := file.ReadAt(buf, tt.offset)

			if n != tt.wantN {
				t.Errorf("ReadAt() n = %d, want %d", n, tt.wantN)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ReadAt() error = %v, want %v", err, tt.wantErr)
			}
			if got := string(buf[:n]); got != tt.wantStr {
				t.Errorf("ReadAt() data = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestMockFile_ReadAt_Closed(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
	_ = file.Close()

	buf := make([]byte, 10)
	_, err := file.ReadAt(buf, 0)
	assertError(t, err, fs.ErrClosed)
}

func TestMockFile_ReadAt_ErrorInjection(t *testing.T) {
	t.Parallel()

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrUnexpectedEOF, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"),
		mockfs.WithFileErrorInjector(injector))

	buf := make([]byte, 4)
	_, err := file.ReadAt(buf, 0)
	assertError(t, err, mockfs.ErrUnexpectedEOF)
}

func TestMockFile_ReadAt_Stats(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("0123456789"))

	buf := make([]byte, 3)
	file.ReadAt(buf, 0)
	file.ReadAt(buf, 5)

	stats := file.Stats()
	if stats.Count(mockfs.OpRead) != 2 {
		t.Errorf("Count(OpRead) = %d, want 2", stats.Count(mockfs.OpRead))
	}
	if stats.BytesRead() != 6 {
		t.Errorf("BytesRead() = %d, want 6", stats.BytesRead())
	}
}

// TestMockFile_Write_Overwrite tests overwrite mode.
func TestMockFile_Write_Overwrite(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("original content"),
		Mode:    0o644,
		ModTime: time.Now(),
	}

	// Overwrite mode is the default
	file := mockfs.NewMockFile(mapFile, "test.txt")

	newData := []byte("new")
	n, err := file.Write(newData)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len(newData) {
		t.Errorf("n = %d, want %d", n, len(newData))
	}

	// Verify data was replaced by checking mapFile directly
	if string(mapFile.Data) != "new" {
		t.Errorf("data = %q, want %q", mapFile.Data, "new")
	}
}

// TestMockFile_Write_Append tests append mode.
func TestMockFile_Write_Append(t *testing.T) {
	initialData := []byte("initial-")
	mapFile := &fstest.MapFile{
		Data:    append([]byte(nil), initialData...),
		Mode:    0o644,
		ModTime: time.Now(),
	}

	// Pass 0 (untyped const) for writeModeAppend
	file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileAppend())

	writeData := []byte("appended")
	n, err := file.Write(writeData)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if n != len(writeData) {
		t.Errorf("n = %d, want %d", n, len(writeData))
	}

	wantData := "initial-appended"
	if string(mapFile.Data) != wantData {
		t.Errorf("data = %q, want %q", mapFile.Data, wantData)
	}
}

// TestMockFile_Write_ReadOnly tests read-only mode.
func TestMockFile_Write_ReadOnly(t *testing.T) {
	initialData := []byte("initial")
	mapFile := &fstest.MapFile{
		Data:    append([]byte(nil), initialData...),
		Mode:    0o644,
		ModTime: time.Now(),
	}

	file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileReadOnly())

	_, err := file.Write([]byte("new data"))

	// Expect permission error
	if !errors.Is(err, mockfs.ErrPermission) {
		// fs.ErrPermission may be wrapped in fs.PathError
		if err == nil || !strings.Contains(err.Error(), mockfs.ErrPermission.Error()) {
			t.Errorf("error = %v, want %v (or wrapper)", err, mockfs.ErrPermission)
		}
	}

	// Data should not have changed
	if string(mapFile.Data) != string(initialData) {
		t.Errorf("data = %q, want %q", mapFile.Data, initialData)
	}
}

// TestMockFile_Write_Closed tests writing to closed file.
func TestMockFile_Write_Closed(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	if err := file.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	_, err := file.Write([]byte("new"))
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Write_ErrorInjection tests error injection on write.
func TestMockFile_Write_ErrorInjection(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0o644, ModTime: time.Now()}
	wantErr := fs.ErrPermission

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpWrite, "test.txt", wantErr, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileErrorInjector(injector))

	_, err := file.Write([]byte("data"))
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// TestMockFile_Write_ModTimeUpdate tests that ModTime is updated on write.
func TestMockFile_Write_ModTimeUpdate(t *testing.T) {
	initialTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mapFile := &fstest.MapFile{
		Data:    []byte("old"),
		Mode:    0o644,
		ModTime: initialTime,
	}

	file := mockfs.NewMockFile(mapFile, "test.txt")

	// Wait a bit to ensure time difference
	time.Sleep(10 * time.Millisecond)

	_, err := file.Write([]byte("new"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	if !mapFile.ModTime.After(initialTime) {
		t.Errorf("ModTime not updated: %v should be after %v", mapFile.ModTime, initialTime)
	}
}

// TestMockFile_Write_ZeroByte tests writing zero bytes.
func TestMockFile_Write_ZeroByte(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("initial"))

	n, err := file.Write([]byte{})
	if err != nil {
		t.Errorf("zero-byte write failed: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

// TestMockFile_WriteAt tests WriteAt operation at various offsets.
func TestMockFile_WriteAt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		initial   []byte
		offset    int64
		writeData []byte
		wantN     int
		wantErr   error
		wantFinal string
	}{
		{
			name:      "write at start",
			initial:   []byte("0123456789"),
			offset:    0,
			writeData: []byte("ABC"),
			wantN:     3,
			wantErr:   nil,
			wantFinal: "ABC3456789",
		},
		{
			name:      "write at middle",
			initial:   []byte("0123456789"),
			offset:    5,
			writeData: []byte("XYZ"),
			wantN:     3,
			wantErr:   nil,
			wantFinal: "01234XYZ89",
		},
		{
			name:      "write at end",
			initial:   []byte("0123456789"),
			offset:    10,
			writeData: []byte("END"),
			wantN:     3,
			wantErr:   nil,
			wantFinal: "0123456789END",
		},
		{
			name:      "write beyond end extends",
			initial:   []byte("012"),
			offset:    5,
			writeData: []byte("XY"),
			wantN:     2,
			wantErr:   nil,
			wantFinal: "012\x00\x00XY",
		},
		{
			name:      "negative offset",
			initial:   []byte("data"),
			offset:    -1,
			writeData: []byte("X"),
			wantN:     0,
			wantErr:   mockfs.ErrNegativeOffset,
			wantFinal: "data",
		},
		{
			name:      "empty write",
			initial:   []byte("data"),
			offset:    2,
			writeData: []byte{},
			wantN:     0,
			wantErr:   nil,
			wantFinal: "data",
		},
		{
			name:      "write to empty file",
			initial:   []byte{},
			offset:    0,
			writeData: []byte("NEW"),
			wantN:     3,
			wantErr:   nil,
			wantFinal: "NEW",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := mockfs.NewMockFileFromBytes("test.txt", tt.initial)

			n, err := file.WriteAt(tt.writeData, tt.offset)

			if n != tt.wantN {
				t.Errorf("WriteAt() n = %d, want %d", n, tt.wantN)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("WriteAt() error = %v, want %v", err, tt.wantErr)
			}

			// Read back entire content
			_, _ = file.Seek(0, io.SeekStart)
			buf := make([]byte, 100)
			nRead, _ := file.Read(buf)
			got := string(buf[:nRead])

			if got != tt.wantFinal {
				t.Errorf("WriteAt() final content = %q, want %q", got, tt.wantFinal)
			}
		})
	}
}

func TestMockFile_WriteAt_Closed(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
	_ = file.Close()

	_, err := file.WriteAt([]byte("X"), 0)
	assertError(t, err, fs.ErrClosed)
}

func TestMockFile_WriteAt_ReadOnly(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"),
		mockfs.WithFileReadOnly())

	_, err := file.WriteAt([]byte("X"), 0)
	assertError(t, err, mockfs.ErrPermission)
}

func TestMockFile_WriteAt_ErrorInjection(t *testing.T) {
	t.Parallel()

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpWrite, "test.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"),
		mockfs.WithFileErrorInjector(injector))

	_, err := file.WriteAt([]byte("X"), 0)
	assertError(t, err, mockfs.ErrDiskFull)
}

func TestMockFile_WriteAt_Stats(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", make([]byte, 20))

	file.WriteAt([]byte("ABC"), 0)
	file.WriteAt([]byte("XY"), 10)

	stats := file.Stats()
	if stats.Count(mockfs.OpWrite) != 2 {
		t.Errorf("Count(OpWrite) = %d, want 2", stats.Count(mockfs.OpWrite))
	}
	if stats.BytesWritten() != 5 {
		t.Errorf("BytesWritten() = %d, want 5", stats.BytesWritten())
	}
}

// TestMockFile_ReadDir_Valid tests valid directory reading.
func TestMockFile_ReadDir_Valid(t *testing.T) {
	entries := []fs.DirEntry{
		mockfs.NewFileInfo("file1.txt", 0, 0o644, time.Now()),
		mockfs.NewFileInfo("file2.txt", 0, 0o644, time.Now()),
	}

	handler := func(n int) ([]fs.DirEntry, error) {
		if n < 0 {
			return entries, nil
		}
		if n > len(entries) {
			n = len(entries)
		}
		return entries[:n], nil
	}

	dir := mockfs.NewMockDir("testdir", handler)

	result, err := dir.ReadDir(-1)
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

// TestMockFile_ReadDir_NilHandler tests ReadDir with nil handler returns empty result.
func TestMockFile_ReadDir_NilHandler(t *testing.T) {
	dir := mockfs.NewMockDir("testdir", nil)

	entries, err := dir.ReadDir(-1)
	if err != nil {
		t.Fatalf("readdir with nil handler failed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(entries))
	}
}

// TestMockFile_ReadDir_Closed tests closed directory.
func TestMockFile_ReadDir_Closed(t *testing.T) {
	handler := func(n int) ([]fs.DirEntry, error) {
		return []fs.DirEntry{}, nil
	}

	dir := mockfs.NewMockDir("testdir", handler)
	_ = dir.Close()

	_, err := dir.ReadDir(-1)
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_ReadDir_NotDirectory tests file not directory error.
func TestMockFile_ReadDir_NotDirectory(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	_, err := file.ReadDir(-1)
	if err == nil {
		t.Error("expected error for non-directory")
	}
}

// TestMockFile_ReadDir_ErrorInjection tests error injection on readdir.
func TestMockFile_ReadDir_ErrorInjection(t *testing.T) {
	wantErr := fs.ErrPermission

	handler := func(n int) ([]fs.DirEntry, error) {
		return []fs.DirEntry{}, nil
	}

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpReadDir, "testdir", wantErr, mockfs.ErrorModeAlways, 0)

	dir := mockfs.NewMockDir("testdir", handler, mockfs.WithFileErrorInjector(injector))

	_, err := dir.ReadDir(-1)
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// TestMockFile_ReadDir_Pagination tests reading directory entries in pages.
func TestMockFile_ReadDir_Pagination(t *testing.T) {
	entries := []fs.DirEntry{
		mockfs.NewFileInfo("file1", 0, 0o644, time.Now()),
		mockfs.NewFileInfo("file2", 0, 0o644, time.Now()),
		mockfs.NewFileInfo("file3", 0, 0o644, time.Now()),
		mockfs.NewFileInfo("file4", 0, 0o644, time.Now()),
	}

	pos := 0
	handler := func(n int) ([]fs.DirEntry, error) {
		if pos >= len(entries) {
			return []fs.DirEntry{}, nil
		}

		if n <= 0 {
			result := entries[pos:]
			pos = len(entries)
			return result, nil
		}

		end := pos + n
		if end > len(entries) {
			end = len(entries)
		}

		result := entries[pos:end]
		pos = end
		return result, nil
	}

	dir := mockfs.NewMockDir("testdir", handler)

	// Read 2 entries at a time
	result1, err := dir.ReadDir(2)
	if err != nil {
		t.Fatalf("readdir 1 failed: %v", err)
	}
	if len(result1) != 2 {
		t.Errorf("readdir 1: len = %d, want 2", len(result1))
	}

	result2, err := dir.ReadDir(2)
	if err != nil {
		t.Fatalf("readdir 2 failed: %v", err)
	}
	if len(result2) != 2 {
		t.Errorf("readdir 2: len = %d, want 2", len(result2))
	}

	// Next read should return empty
	result3, err := dir.ReadDir(2)
	if err != nil {
		t.Fatalf("readdir 3 failed: %v", err)
	}
	if len(result3) != 0 {
		t.Errorf("readdir 3: len = %d, want 0", len(result3))
	}
}

// TestMockFile_Stat tests Stat operation.
func TestMockFile_Stat(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("test data content"),
		Mode:    0o644,
		ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	file := mockfs.NewMockFile(mapFile, "testdir/test.txt")

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if info.Name() != "test.txt" {
		t.Errorf("name = %q, want %q", info.Name(), "test.txt")
	}
	if info.Size() != 17 {
		t.Errorf("size = %d, want 17", info.Size())
	}
	if info.Mode() != 0o644 {
		t.Errorf("mode = %v, want 0o644", info.Mode())
	}
	if info.IsDir() {
		t.Error("IsDir = true, want false")
	}
}

// TestMockFile_Stat_Closed tests stat on closed file.
func TestMockFile_Stat_Closed(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	if err := file.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	_, err := file.Stat()
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Stat_ErrorInjection tests error injection on stat.
func TestMockFile_Stat_ErrorInjection(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0o644, ModTime: time.Now()}
	wantErr := fs.ErrPermission

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpStat, "test.txt", wantErr, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileErrorInjector(injector))

	_, err := file.Stat()
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// TestMockFile_Stat_Basename tests correct basename extraction.
func TestMockFile_Stat_Basename(t *testing.T) {
	tests := []struct {
		path     string
		wantName string
	}{
		{"test.txt", "test.txt"},
		{"dir/test.txt", "test.txt"},
		{"a/b/c/file.dat", "file.dat"},
		{"/absolute/path/file", "file"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			file := mockfs.NewMockFileFromBytes(tt.path, []byte("test"))

			info, err := file.Stat()
			if err != nil {
				t.Fatalf("stat failed: %v", err)
			}

			if info.Name() != tt.wantName {
				t.Errorf("Name() = %q, want %q", info.Name(), tt.wantName)
			}
		})
	}
}

// TestMockFile_Stat_DirMode tests directory vs file detection.
func TestMockFile_Stat_DirMode(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		handler := func(n int) ([]fs.DirEntry, error) {
			return []fs.DirEntry{}, nil
		}
		dir := mockfs.NewMockDir("testdir", handler)

		info, err := dir.Stat()
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}

		if !info.IsDir() {
			t.Error("IsDir() = false, want true")
		}
	})

	t.Run("regular file", func(t *testing.T) {
		file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

		info, err := file.Stat()
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}

		if info.IsDir() {
			t.Error("IsDir() = true, want false")
		}
	})
}

// TestMockFile_Stat_Permissions tests different file modes.
func TestMockFile_Stat_Permissions(t *testing.T) {
	modes := []mockfs.FileMode{
		0o644,
		0o755,
		0o400,
		0o777,
	}

	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			mapFile := &fstest.MapFile{
				Data:    []byte("test"),
				Mode:    mode,
				ModTime: time.Now(),
			}
			file := mockfs.NewMockFile(mapFile, "test.txt")

			info, err := file.Stat()
			if err != nil {
				t.Fatalf("stat failed: %v", err)
			}

			if info.Mode() != mode {
				t.Errorf("mode = %v, want %v", info.Mode(), mode)
			}
		})
	}
}

// TestMockFile_Close tests Close operation.
func TestMockFile_Close(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	err := file.Close()
	if err != nil {
		t.Errorf("close failed: %v", err)
	}
}

// TestMockFile_Close_Double tests double close returns error.
func TestMockFile_Close_Double(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	_ = file.Close()
	err := file.Close()
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Close_ErrorInjection tests error injection on close.
func TestMockFile_Close_ErrorInjection(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0o644, ModTime: time.Now()}
	wantErr := fs.ErrPermission

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpClose, "test.txt", wantErr, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileErrorInjector(injector))

	err := file.Close()
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}

	// File should still be closed even with error
	err = file.Close()
	if err != fs.ErrClosed {
		t.Errorf("second close: error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Stats tests operation statistics.
func TestMockFile_Stats(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	// Perform operations
	buf := make([]byte, 4)
	_, _ = file.Read(buf)
	_, _ = file.Write([]byte("new"))
	_, _ = file.Stat()
	_ = file.Close()

	// Get stats after operations
	stats := file.Stats()

	if stats.Count(mockfs.OpRead) == 0 {
		t.Error("OpRead not counted")
	}
	if stats.Count(mockfs.OpWrite) == 0 {
		t.Error("OpWrite not counted")
	}
	if stats.Count(mockfs.OpStat) == 0 {
		t.Error("OpStat not counted")
	}
	if stats.Count(mockfs.OpClose) == 0 {
		t.Error("OpClose not counted")
	}
}

// TestMockFile_Stats_Snapshot tests that Stats returns a snapshot.
func TestMockFile_Stats_Snapshot(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	buf := make([]byte, 4)
	_, _ = file.Read(buf)

	snap1 := file.Stats()
	reads1 := snap1.Count(mockfs.OpRead)

	// Perform another read
	_, _ = file.Read(buf)

	// First snapshot should be unchanged
	if snap1.Count(mockfs.OpRead) != reads1 {
		t.Error("snapshot was modified after file operation")
	}

	// New snapshot should reflect new read
	snap2 := file.Stats()
	if snap2.Count(mockfs.OpRead) != reads1+1 {
		t.Errorf("new snapshot reads = %d, want %d", snap2.Count(mockfs.OpRead), reads1+1)
	}
}

// TestMockFile_ErrorInjector tests error injector access.
func TestMockFile_ErrorInjector(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	injector := file.ErrorInjector()
	if injector == nil {
		t.Fatal("injector is nil")
	}

	// Configure and verify injection works
	injector.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrUnexpectedEOF, mockfs.ErrorModeOnce, 0)

	buf := make([]byte, 4)
	_, err := file.Read(buf)
	if err != mockfs.ErrUnexpectedEOF {
		t.Errorf("error = %v, want %v", err, mockfs.ErrUnexpectedEOF)
	}
}

// TestMockFile_LatencySimulator_Exists tests latency simulator access.
func TestMockFile_LatencySimulator_Exists(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	sim := file.LatencySimulator()
	if sim == nil {
		t.Fatal("latency simulator is nil")
	}
}

// TestMockFile_latencySimulation tests that latency is properly applied.
func TestMockFile_latencySimulation(t *testing.T) {
	latencySim := mockfs.NewLatencySimulator(testDuration)

	file := mockfs.NewMockFileFromString("test.txt", "test", mockfs.WithFileLatencySimulator(latencySim))

	operations := []struct {
		name string
		fn   func() error
	}{
		{"read", func() error {
			buf := make([]byte, 4)
			_, err := file.Read(buf)
			return err
		}},
		{"write", func() error {
			_, err := file.Write([]byte("new"))
			return err
		}},
		{"stat", func() error {
			_, err := file.Stat()
			return err
		}},
		{"close", func() error {
			return file.Close()
		}},
	}

	for _, op := range operations {
		t.Run(op.name, func(t *testing.T) {
			start := time.Now()
			if err := op.fn(); err != nil && err != fs.ErrClosed {
				t.Fatalf("operation failed: %v", err)
			}
			assertDuration(t, start, testDuration, op.name)
		})
	}
}

// TestMockFile_LatencyReset tests that latency state is reset on close.
func TestMockFile_LatencyReset(t *testing.T) {
	latencySim := mockfs.NewLatencySimulator(testDuration)

	file := mockfs.NewMockFileFromString("test.txt", "test", mockfs.WithFileLatencySimulator(latencySim))

	// Read with latency
	buf := make([]byte, 4)
	start := time.Now()
	_, _ = file.Read(buf)
	assertDuration(t, start, testDuration, "first read")

	// Close should reset
	_ = file.Close()
}

// TestMockFile_LatencyOnceMode tests Once latency mode behavior.
func TestMockFile_LatencyOnceMode(t *testing.T) {
	latencySim := mockfs.NewLatencySimulator(testDuration)

	file := mockfs.NewMockFileFromString("test.txt", "test", mockfs.WithFileLatencySimulator(latencySim))

	// First read - should have latency
	buf := make([]byte, 4)
	start := time.Now()
	_, err := file.Read(buf)
	if err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	elapsed := time.Since(start)

	// We expect latency on first call
	if elapsed < testDuration-tolerance {
		t.Errorf("first read: expected latency ~%v, got %v", testDuration, elapsed)
	}
}

// TestMockFile_LatencySharedSimulator tests sharing latency simulator between files.
func TestMockFile_LatencySharedSimulator(t *testing.T) {
	sharedLatency := mockfs.NewLatencySimulator(testDuration)

	file1 := mockfs.NewMockFileFromString("file1.txt", "file1", mockfs.WithFileLatencySimulator(sharedLatency))
	file2 := mockfs.NewMockFileFromString("file2.txt", "file2", mockfs.WithFileLatencySimulator(sharedLatency))

	// Both files should experience latency
	buf := make([]byte, 5)

	start := time.Now()
	_, err := file1.Read(buf)
	if err != nil {
		t.Fatalf("file1 read failed: %v", err)
	}
	assertDuration(t, start, testDuration, "file1 read")

	start = time.Now()
	_, err = file2.Read(buf)
	if err != nil {
		t.Fatalf("file2 read failed: %v", err)
	}
	assertDuration(t, start, testDuration, "file2 read")
}

// TestMockFile_ConcurrentReads tests concurrent read operations.
func TestMockFile_ConcurrentReads(t *testing.T) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	file := mockfs.NewMockFileFromBytes("test.txt", data)

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 10)
			_, err := file.Read(buf)
			if err != nil && err != io.EOF {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent read error: %v", err)
	}
}

// TestMockFile_ConcurrentWrites tests concurrent write operations.
func TestMockFile_ConcurrentWrites(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte(""))

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			data := []byte{byte(val)}
			_, err := file.Write(data)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent write error: %v", err)
	}
}

// TestMockFile_ConcurrentCloses tests that concurrent closes are safe.
func TestMockFile_ConcurrentCloses(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	var wg sync.WaitGroup
	closedCount := int32(0)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := file.Close()
			if err == nil {
				atomic.AddInt32(&closedCount, 1)
			}
		}()
	}

	wg.Wait()

	// Only one close should succeed
	if closedCount != 1 {
		t.Errorf("closedCount = %d, want 1", closedCount)
	}
}

// TestMockFile_ReadWriteSequence tests mixed read/write operations.
func TestMockFile_ReadWriteSequence(t *testing.T) {
	file := mockfs.NewMockFileFromString("test.txt", "initial")

	// Read initial content
	buf := make([]byte, 7)
	n, err := file.Read(buf)
	if err != nil || n != 7 || string(buf) != "initial" {
		t.Fatalf("initial read failed: n=%d, data=%q, err=%v", n, buf, err)
	}

	// Write new content (overwrite mode)
	_, err = file.Write([]byte("replaced"))
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Position should be at end after write in overwrite mode
	n, err = file.Read(buf)
	if err != io.EOF {
		t.Errorf("read after write: expected EOF, got n=%d, err=%v", n, err)
	}
}

// TestMockFile_EmptyFile tests operations on empty file.
func TestMockFile_EmptyFile(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("empty.txt", []byte{})

	// Read should return EOF immediately
	buf := make([]byte, 10)
	n, err := file.Read(buf)
	if err != io.EOF || n != 0 {
		t.Errorf("read empty: n=%d, err=%v, want 0, EOF", n, err)
	}

	// Stat should work
	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}

	// Write should work
	_, err = file.Write([]byte("content"))
	if err != nil {
		t.Errorf("write failed: %v", err)
	}
}

// TestMockFile_MultipleFilesIndependent tests that files have independent state.
func TestMockFile_MultipleFilesIndependent(t *testing.T) {
	file1 := mockfs.NewMockFileFromBytes("file1.txt", []byte("content1"))
	file2 := mockfs.NewMockFileFromBytes("file2.txt", []byte("content2"))

	// Read from file1
	buf1 := make([]byte, 8)
	n1, err := file1.Read(buf1)
	if err != nil || string(buf1[:n1]) != "content1" {
		t.Fatalf("file1 read failed: n=%d, err=%v", n1, err)
	}

	// Read from file2
	buf2 := make([]byte, 8)
	n2, err := file2.Read(buf2)
	if err != nil || string(buf2[:n2]) != "content2" {
		t.Fatalf("file2 read failed: n=%d, err=%v", n2, err)
	}

	// Close file1 should not affect file2
	if err := file1.Close(); err != nil {
		t.Fatalf("file1 close failed: %v", err)
	}

	// file2 should still be readable
	buf3 := make([]byte, 8)
	_, err = file2.Read(buf3)
	if err != io.EOF {
		t.Errorf("file2 should still be open: err=%v", err)
	}
}

// TestMockFile_PartialReads tests continuing reads.
func TestMockFile_PartialReads(t *testing.T) {
	file := mockfs.NewMockFileFromString("test.txt", "0123456789")

	// Read in chunks
	chunks := []string{"012", "345", "6789"}
	for i, want := range chunks {
		buf := make([]byte, len(want))
		n, err := file.Read(buf)
		if err != nil {
			t.Fatalf("read %d failed: %v", i, err)
		}
		if n != len(want) {
			t.Errorf("read %d: n = %d, want %d", i, n, len(want))
		}
		if string(buf) != want {
			t.Errorf("read %d: data = %q, want %q", i, buf, want)
		}
	}

	// Next read should return EOF
	buf := make([]byte, 10)
	n, err := file.Read(buf)
	if err != io.EOF {
		t.Errorf("final read: n=%d, err=%v, want EOF", n, err)
	}
}

// TestMockFile_ConcurrentReadWrite tests race conditions in concurrent read/write.
func TestMockFile_ConcurrentReadWrite(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("initial data"))

	var wg sync.WaitGroup
	done := make(chan bool)

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 12)
			for j := 0; j < 10; j++ {
				_, _ = file.Read(buf)
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _ = file.Write([]byte("new"))
			}
		}()
	}

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent read/write deadlocked")
	}
}

// TestMockFile_ConcurrentStats tests stats tracking under concurrent operations.
func TestMockFile_ConcurrentStats(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4)
			for j := 0; j < 100; j++ {
				_, _ = file.Read(buf)
				_ = file.Stats() // Concurrent stats access
			}
		}()
	}

	wg.Wait()

	stats := file.Stats()
	if stats.Count(mockfs.OpRead) != 1000 {
		t.Errorf("read count = %d, want 1000", stats.Count(mockfs.OpRead))
	}
}

// TestMockFile_LatencyCloning tests that files get independent latency simulators.
func TestMockFile_LatencyCloning(t *testing.T) {
	// Create a MockFS with latency that uses Once mode
	mfs := mockfs.NewMockFS(
		mockfs.File("file1.txt", "data1"),
		mockfs.File("file2.txt", "data2"),
		mockfs.WithLatency(testDuration),
	)

	// Open two files - each should get cloned latency simulator
	f1, err := mfs.Open("file1.txt")
	if err != nil {
		t.Fatalf("open file1 failed: %v", err)
	}
	defer f1.Close()

	f2, err := mfs.Open("file2.txt")
	if err != nil {
		t.Fatalf("open file2 failed: %v", err)
	}
	defer f2.Close()

	// Both files should experience latency independently
	buf := make([]byte, 5)

	start := time.Now()
	_, _ = f1.Read(buf)
	assertDuration(t, start, testDuration, "file1 first read")

	start = time.Now()
	_, _ = f2.Read(buf)
	assertDuration(t, start, testDuration, "file2 first read")
}

func TestNewDirHandler(t *testing.T) {
	t.Parallel()

	info1, _ := mockfs.NewMockFileFromString("file1.txt", "").Stat()
	info2, _ := mockfs.NewMockFileFromString("file2.txt", "").Stat()
	entries := []fs.DirEntry{info1.(fs.DirEntry), info2.(fs.DirEntry)}

	handler := mockfs.NewDirHandler(entries)

	t.Run("read all with negative n", func(t *testing.T) {
		result, err := handler(-1)
		requireNoError(t, err)
		if len(result) != 2 {
			t.Errorf("len = %d, want 2", len(result))
		}
	})

	t.Run("read with pagination", func(t *testing.T) {
		handler := mockfs.NewDirHandler(entries)
		result, err := handler(1)
		if err != nil {
			t.Fatalf("first page failed: %v", err)
		}
		if len(result) != 1 {
			t.Errorf("first page len = %d, want 1", len(result))
		}

		result, err = handler(1)
		if err != io.EOF {
			t.Errorf("second page err = %v, want io.EOF", err)
		}
		if len(result) != 1 {
			t.Errorf("second page len = %d, want 1", len(result))
		}

		result, err = handler(1)
		if err != io.EOF {
			t.Errorf("beyond end err = %v, want io.EOF", err)
		}
		if len(result) != 0 {
			t.Errorf("beyond end len = %d, want 0", len(result))
		}
	})

	t.Run("read all after exhausted", func(t *testing.T) {
		handler := mockfs.NewDirHandler(entries)
		_, _ = handler(-1)
		result, err := handler(-1)
		requireNoError(t, err)
		if len(result) != 0 {
			t.Errorf("after exhausted len = %d, want 0", len(result))
		}
	})
}

// --- Benchmarks ---

// BenchmarkMockFile_Read benchmarks read performance.
func BenchmarkMockFile_Read(b *testing.B) {
	data := make([]byte, 1024)
	file := mockfs.NewMockFileFromBytes("test.txt", data)
	buf := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = file.Read(buf)
	}
}

// BenchmarkMockFile_Write benchmarks write performance.
func BenchmarkMockFile_Write(b *testing.B) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte{})
	data := []byte("benchmark data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = file.Write(data)
	}
}

// BenchmarkMockFile_Stat benchmarks stat performance.
func BenchmarkMockFile_Stat(b *testing.B) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("test"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = file.Stat()
	}
}

// BenchmarkMockFile_Stats benchmarks stats snapshot performance.
func BenchmarkMockFile_Stats(b *testing.B) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("test"))
	buf := make([]byte, 4)
	_, _ = file.Read(buf)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = file.Stats()
	}
}
