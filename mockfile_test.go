package mockfs_test

import (
	"fmt"
	"io"
	"io/fs"
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
	t.Parallel()

	mapFile := &fstest.MapFile{
		Data:    []byte("test data"),
		Mode:    0o644,
		ModTime: time.Now(),
	}

	tests := []struct {
		name      string
		mapFile   *fstest.MapFile
		wantPanic bool
	}{
		{
			name:      "valid file",
			mapFile:   mapFile,
			wantPanic: false,
		},
		{
			name:      "nil mapfile panics",
			mapFile:   nil,
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				requirePanic(t, func() { mockfs.NewMockFile(tt.mapFile, "test.txt", mockfs.WithFileOverwrite()) }, "NewMockFile()")
				return
			}
			if file := mockfs.NewMockFile(tt.mapFile, "test.txt", mockfs.WithFileOverwrite()); file == nil {
				t.Error("expected non-nil file")
			}
		})
	}
}

// TestNewMockFile_Defaults tests that nil arguments to NewMockFile are
// initialized with non-nil default implementations.
func TestNewMockFile_Defaults(t *testing.T) {
	t.Parallel()

	mapFile := &fstest.MapFile{Data: []byte("test")}
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
	t.Parallel()

	file := mockfs.NewMockFileFromString("test.txt", "hello")
	requireNoError(t, nil) // Constructor doesn't return error

	// Should be able to read
	buf := make([]byte, 5)
	n, err := file.Read(buf)
	requireNoError(t, err)
	if n != 5 || string(buf) != "hello" {
		t.Errorf("read = %q, want %q", buf[:n], "hello")
	}
}

// TestNewMockFileFromBytes tests creating file from bytes.
func TestNewMockFileFromBytes(t *testing.T) {
	t.Parallel()

	data := []byte("test content")
	file := mockfs.NewMockFileFromBytes("test.txt", data)

	buf := make([]byte, len(data))
	n, err := file.Read(buf)
	requireNoError(t, err)
	if n != len(data) || string(buf) != string(data) {
		t.Errorf("read = %q, want %q", buf[:n], data)
	}
}

// TestNewMockDir tests directory constructor.
func TestNewMockDir(t *testing.T) {
	t.Parallel()

	t.Run("with handler", func(t *testing.T) {
		handler := func(n int) ([]fs.DirEntry, error) {
			return []fs.DirEntry{}, nil
		}

		dir := mockfs.NewMockDir("testdir", handler)
		if dir == nil {
			t.Fatal("expected non-nil directory")
		}

		entries, err := dir.ReadDir(-1)
		requireNoError(t, err)
		if entries == nil {
			t.Error("expected non-nil entries")
		}
	})

	t.Run("nil handler", func(t *testing.T) {
		dir := mockfs.NewMockDir("testdir", nil)
		if dir == nil {
			t.Fatal("expected non-nil directory")
		}
	})
}

func TestNewDirHandler(t *testing.T) {
	t.Parallel()

	info1, err1 := mockfs.NewMockFileFromString("file1.txt", "").Stat()
	requireNoError(t, err1, "NewMockFileFromString(\"file1.txt\")")
	info2, err2 := mockfs.NewMockFileFromString("file2.txt", "").Stat()
	requireNoError(t, err2, "NewMockFileFromString(\"file2.txt\")")
	entries := []fs.DirEntry{info1.(fs.DirEntry), info2.(fs.DirEntry)}

	handler := mockfs.NewDirHandler(entries)

	t.Run("read all with negative n", func(t *testing.T) {
		result, err := handler(-1)
		requireNoError(t, err)
		if len(result) != 2 {
			t.Errorf("len = %d, want 2", len(result))
		}
	})

	t.Run("pagination", func(t *testing.T) {
		handler := mockfs.NewDirHandler(entries)
		result, err := handler(1)
		requireNoError(t, err, "first page")
		if len(result) != 1 {
			t.Errorf("first page len = %d, want 1", len(result))
		}

		result, err = handler(1)
		if err != io.EOF || len(result) != 1 {
			t.Errorf("second page: len=%d, err=%v, want len=1, err=io.EOF", len(result), err)
		}

		result, err = handler(1)
		if err != io.EOF || len(result) != 0 {
			t.Errorf("beyond end: len=%d, err=%v, want len=0, err=io.EOF", len(result), err)
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
	t.Run("read all after already exhausted with n > 0", func(t *testing.T) {
		handler := mockfs.NewDirHandler(entries)
		_, _ = handler(-1)        // Exhaust
		result, err := handler(5) // Try to read with n > 0
		if err != io.EOF {
			t.Errorf("expected io.EOF after exhaustion, got %v", err)
		}
		if len(result) != 0 {
			t.Errorf("expected 0 entries after exhaustion, got %d", len(result))
		}
	})

	t.Run("pagination boundary exact", func(t *testing.T) {
		handler := mockfs.NewDirHandler(entries)
		result, err := handler(2) // Read exactly all entries
		if err != io.EOF {
			t.Errorf("expected io.EOF when reading exact count, got %v", err)
		}
		if len(result) != 2 {
			t.Errorf("expected 2 entries, got %d", len(result))
		}
	})

	t.Run("pagination boundary overflow", func(t *testing.T) {
		moreEntries := []fs.DirEntry{
			mockfs.NewFileInfo("file1.txt", 5, 0o644, time.Now()),
			mockfs.NewFileInfo("file2.txt", 5, 0o644, time.Now()),
			mockfs.NewFileInfo("file3.txt", 5, 0o644, time.Now()),
		}
		handler := mockfs.NewDirHandler(moreEntries)

		// Read 2, then request 5 (but only 1 remains)
		_, _ = handler(2)
		result, err := handler(5)
		if err != io.EOF {
			t.Errorf("expected io.EOF, got %v", err)
		}
		if len(result) != 1 {
			t.Errorf("expected 1 entry (remaining), got %d", len(result))
		}
	})
}

// --- Options ---

func TestFileOptions(t *testing.T) {
	t.Parallel()

	t.Run("shared stats", func(t *testing.T) {
		sharedStats := mockfs.NewStatsRecorder(nil)
		file := mockfs.NewMockFileFromString("test.txt", "data", mockfs.WithFileStats(sharedStats))

		buf := make([]byte, 4)
		_, _ = file.Read(buf)

		if sharedStats.Count(mockfs.OpRead) != 1 {
			t.Errorf("shared stats not updated: count = %d, want 1", sharedStats.Count(mockfs.OpRead))
		}
	})

	t.Run("latency", func(t *testing.T) {
		file := mockfs.NewMockFileFromString("test.txt", "data", mockfs.WithFileLatency(testDuration))

		buf := make([]byte, 4)
		start := time.Now()
		_, err := file.Read(buf)
		requireNoError(t, err)
		assertDuration(t, start, testDuration, "read with latency")
	})

	t.Run("per-operation latency", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrUnexpectedEOF, mockfs.ErrorModeOnce, 0)
		file := mockfs.NewMockFileFromString("test.txt", "test",
			mockfs.WithFileErrorInjector(inj),
			mockfs.WithFilePerOperationLatency(map[mockfs.Operation]time.Duration{
				mockfs.OpRead:  testDuration,
				mockfs.OpWrite: testDuration,
			}))

		// First read should fail
		buf := make([]byte, 4)
		_, err := file.Read(buf)
		assertError(t, err, mockfs.ErrUnexpectedEOF, "first read")

		// Second read should succeed with latency
		start := time.Now()
		n, err := file.Read(buf)
		requireNoError(t, err, "second read")
		if n != 4 {
			t.Errorf("read n = %d, want 4", n)
		}
		assertDuration(t, start, testDuration, "read latency")

		// Close should not have latency (not in per-op config)
		start = time.Now()
		_ = file.Close()
		assertNoDuration(t, start, "close should be fast")
	})
}

// --- Read Operations ---

// TestMockFile_Read tests Read operation.
func TestMockFile_Read(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		data     []byte
		bufSize  int
		wantN    int
		wantData string
		wantErr  error
	}{
		{
			name:     "full content",
			data:     []byte("hello world"),
			bufSize:  20,
			wantN:    11,
			wantData: "hello world",
			wantErr:  nil,
		},
		{
			name:     "partial content",
			data:     []byte("hello world"),
			bufSize:  5,
			wantN:    5,
			wantData: "hello",
			wantErr:  nil,
		},
		{
			name:     "empty file",
			data:     []byte(""),
			bufSize:  10,
			wantN:    0,
			wantData: "",
			wantErr:  io.EOF,
		},
		{
			name:     "zero-byte buffer",
			data:     []byte("data"),
			bufSize:  0,
			wantN:    0,
			wantData: "",
			wantErr:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := mockfs.NewMockFileFromBytes("test.txt", tt.data)
			buf := make([]byte, tt.bufSize)
			n, err := file.Read(buf)

			assertError(t, err, tt.wantErr)
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
	t.Parallel()

	t.Run("position advances", func(t *testing.T) {
		file := mockfs.NewMockFileFromString("test.txt", "abcdefghij")

		// First read
		buf := make([]byte, 3)
		n, _ := file.Read(buf)
		if n != 3 || string(buf) != "abc" {
			t.Fatalf("first read: n=%d, data=%q", n, buf)
		}

		// Second read should continue from position
		n, _ = file.Read(buf)
		if n != 3 || string(buf) != "def" {
			t.Fatalf("second read: n=%d, data=%q", n, buf)
		}

		// Third read
		buf = make([]byte, 10)
		n, _ = file.Read(buf)
		if n != 4 || string(buf[:n]) != "ghij" {
			t.Fatalf("third read: n=%d, data=%q", n, buf[:n])
		}

		// Fourth read should return EOF
		_, err := file.Read(buf)
		assertError(t, err, io.EOF, "fourth read")
	})
}

// TestMockFile_Read_Closed tests reading from closed file.
func TestMockFile_Read_Closed(t *testing.T) {
	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
	_ = file.Close()

	buf := make([]byte, 10)
	_, err := file.Read(buf)
	assertError(t, err, fs.ErrClosed)
}

// TestMockFile_Read_ErrorInjection tests error injection on read.
func TestMockFile_Read_ErrorInjection(t *testing.T) {
	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrUnexpectedEOF, mockfs.ErrorModeAlways, 0)
	file := mockfs.NewMockFileFromString("test.txt", "test", mockfs.WithFileErrorInjector(inj))

	buf := make([]byte, 10)
	_, err := file.Read(buf)
	assertError(t, err, mockfs.ErrUnexpectedEOF)
}

// TestMockFile_Read_LargeFile tests reading a large file.
func TestMockFile_Read_LargeFile(t *testing.T) {
	size := 10 * 1024 * 1024 // 10MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	file := mockfs.NewMockFileFromBytes("large.bin", data)
	buf := make([]byte, 4096)
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

	tests := []struct {
		name    string
		offset  int64
		bufSize int
		wantN   int
		wantErr error
		wantStr string
	}{
		{
			name:    "from start",
			offset:  0,
			bufSize: 5,
			wantN:   5,
			wantErr: nil,
			wantStr: "01234",
		},
		{
			name:    "from middle",
			offset:  5,
			bufSize: 5,
			wantN:   5,
			wantErr: nil,
			wantStr: "56789",
		},
		{
			name:    "to end exact",
			offset:  10,
			bufSize: 6,
			wantN:   6,
			wantErr: nil,
			wantStr: "abcdef",
		},
		{
			name:    "beyond end",
			offset:  10,
			bufSize: 10,
			wantN:   6,
			wantErr: io.EOF,
			wantStr: "abcdef",
		},
		{
			name:    "at end",
			offset:  16,
			bufSize: 10,
			wantN:   0,
			wantErr: io.EOF,
			wantStr: "",
		},
		{
			name:    "past end",
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
			file := mockfs.NewMockFileFromBytes("test.txt", content)
			buf := make([]byte, tt.bufSize)
			n, err := file.ReadAt(buf, tt.offset)

			if n != tt.wantN {
				t.Errorf("n = %d, want %d", n, tt.wantN)
			}
			assertError(t, err, tt.wantErr)
			if got := string(buf[:n]); got != tt.wantStr {
				t.Errorf("data = %q, want %q", got, tt.wantStr)
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

	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrUnexpectedEOF, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"),
		mockfs.WithFileErrorInjector(inj))

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

// --- Write Operations ---

func TestMockFile_Write(t *testing.T) {
	t.Parallel()

	t.Run("overwrite mode", func(t *testing.T) {
		mapFile := &fstest.MapFile{
			Data:    []byte("original content"),
			Mode:    0o644,
			ModTime: time.Now(),
		}
		file := mockfs.NewMockFile(mapFile, "test.txt")

		newData := []byte("new")
		n, err := file.Write(newData)
		requireNoError(t, err)
		if n != len(newData) {
			t.Errorf("n = %d, want %d", n, len(newData))
		}
		if string(mapFile.Data) != "new" {
			t.Errorf("data = %q, want %q", mapFile.Data, "new")
		}
	})

	t.Run("append mode", func(t *testing.T) {
		mapFile := &fstest.MapFile{
			Data:    []byte("initial-"),
			Mode:    0o644,
			ModTime: time.Now(),
		}
		file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileAppend())

		writeData := []byte("appended")
		n, err := file.Write(writeData)
		requireNoError(t, err)
		if n != len(writeData) {
			t.Errorf("n = %d, want %d", n, len(writeData))
		}
		if string(mapFile.Data) != "initial-appended" {
			t.Errorf("data = %q, want %q", mapFile.Data, "initial-appended")
		}
	})

	t.Run("readonly mode", func(t *testing.T) {
		initialData := []byte("initial")
		mapFile := &fstest.MapFile{
			Data:    append([]byte(nil), initialData...),
			Mode:    0o644,
			ModTime: time.Now(),
		}
		file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileReadOnly())

		_, err := file.Write([]byte("new data"))
		assertError(t, err, mockfs.ErrPermission)
		if string(mapFile.Data) != string(initialData) {
			t.Errorf("data modified: %q", mapFile.Data)
		}
	})

	t.Run("closed file", func(t *testing.T) {
		file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
		_ = file.Close()
		_, err := file.Write([]byte("new"))
		assertError(t, err, fs.ErrClosed)
	})

	t.Run("error injection", func(t *testing.T) {
		mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0o644, ModTime: time.Now()}
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpWrite, "test.txt", fs.ErrPermission, mockfs.ErrorModeAlways, 0)
		file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileErrorInjector(inj))

		_, err := file.Write([]byte("data"))
		assertError(t, err, fs.ErrPermission)
	})

	t.Run("modtime update", func(t *testing.T) {
		initialTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		mapFile := &fstest.MapFile{Data: []byte("old"), Mode: 0o644, ModTime: initialTime}
		file := mockfs.NewMockFile(mapFile, "test.txt")

		time.Sleep(10 * time.Millisecond)
		_, err := file.Write([]byte("new"))
		requireNoError(t, err)
		if !mapFile.ModTime.After(initialTime) {
			t.Errorf("ModTime not updated: %v should be after %v", mapFile.ModTime, initialTime)
		}
	})

	t.Run("zero-byte write", func(t *testing.T) {
		file := mockfs.NewMockFileFromBytes("test.txt", []byte("initial"))
		n, err := file.Write([]byte{})
		requireNoError(t, err)
		if n != 0 {
			t.Errorf("n = %d, want 0", n)
		}
	})
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
			name:      "at start",
			initial:   []byte("0123456789"),
			offset:    0,
			writeData: []byte("ABC"),
			wantN:     3,
			wantErr:   nil,
			wantFinal: "ABC3456789",
		},
		{
			name:      "at middle",
			initial:   []byte("0123456789"),
			offset:    5,
			writeData: []byte("XYZ"),
			wantN:     3,
			wantErr:   nil,
			wantFinal: "01234XYZ89",
		},
		{
			name:      "at end",
			initial:   []byte("0123456789"),
			offset:    10,
			writeData: []byte("END"),
			wantN:     3,
			wantErr:   nil,
			wantFinal: "0123456789END",
		},
		{
			name:      "beyond end extends",
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
			name:      "to empty file",
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
			assertError(t, err, tt.wantErr, "WriteAt()")

			if n != tt.wantN {
				t.Errorf("WriteAt() n = %d, want %d", n, tt.wantN)
			}

			// Read back entire content
			_, _ = file.Seek(0, io.SeekStart)
			buf := make([]byte, 100)
			nRead, _ := file.Read(buf)
			if got := string(buf[:nRead]); got != tt.wantFinal {
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

	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpWrite, "test.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"),
		mockfs.WithFileErrorInjector(inj))

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

// --- Seek Operations ---

func TestMockFile_Seek(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromString("test.txt", "0123456789")

	t.Run("seek to position", func(t *testing.T) {
		_, err := file.Seek(4, io.SeekStart)
		requireNoError(t, err)

		buf := make([]byte, 5)
		n, _ := file.Read(buf)
		if n != 5 || string(buf) != "45678" {
			t.Errorf("read from offset 4 = %q, want %q", buf[:n], "45678")
		}
	})

	t.Run("seek to end", func(t *testing.T) {
		_, err := file.Seek(0, io.SeekEnd)
		requireNoError(t, err)

		buf := make([]byte, 8)
		_, err = file.Read(buf)
		assertError(t, err, io.EOF)
	})

	t.Run("closed file", func(t *testing.T) {
		f := mockfs.NewMockFileFromString("test.txt", "data")
		_ = f.Close()
		_, err := f.Seek(0, io.SeekStart)
		assertError(t, err, fs.ErrClosed)
	})

	t.Run("error injection", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpSeek, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		f := mockfs.NewMockFileFromString("test.txt", "data",
			mockfs.WithFileErrorInjector(inj))

		_, err := f.Seek(0, io.SeekStart)
		assertError(t, err, mockfs.ErrPermission)
	})

	t.Run("invalid whence", func(t *testing.T) {
		f := mockfs.NewMockFileFromString("test.txt", "data")
		_, err := f.Seek(0, 99)
		assertError(t, err, fs.ErrInvalid)
	})

	t.Run("negative result", func(t *testing.T) {
		f := mockfs.NewMockFileFromString("test.txt", "data")
		_, err := f.Seek(-100, io.SeekStart)
		assertError(t, err, fs.ErrInvalid)
	})
}

// --- ReadDir Operations ---

func TestMockFile_ReadDir(t *testing.T) {
	t.Parallel()
	t.Run("valid directory", func(t *testing.T) {
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
		requireNoError(t, err)
		if len(result) != 2 {
			t.Errorf("len = %d, want 2", len(result))
		}
	})

	t.Run("nil handler", func(t *testing.T) {
		dir := mockfs.NewMockDir("testdir", nil)

		entries, err := dir.ReadDir(-1)
		requireNoError(t, err)
		if len(entries) != 0 {
			t.Errorf("expected empty entries, got %d", len(entries))
		}
	})

	t.Run("nil handler with positive n", func(t *testing.T) {
		dir := mockfs.NewMockDir("emptydir", nil)
		entries, err := dir.ReadDir(5)
		if err != io.EOF {
			t.Errorf("expected io.EOF for empty dir with n > 0, got %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("expected 0 entries, got %d", len(entries))
		}
	})

	t.Run("closed directory", func(t *testing.T) {
		handler := func(n int) ([]fs.DirEntry, error) {
			return []fs.DirEntry{}, nil
		}

		dir := mockfs.NewMockDir("testdir", handler)
		_ = dir.Close()

		_, err := dir.ReadDir(-1)
		assertError(t, err, fs.ErrClosed)
	})

	t.Run("not directory", func(t *testing.T) {
		file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
		_, err := file.ReadDir(-1)
		assertAnyError(t, err, "not a directory")
	})

	t.Run("error injection", func(t *testing.T) {
		handler := func(n int) ([]fs.DirEntry, error) {
			return []fs.DirEntry{}, nil
		}

		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpReadDir, "testdir", fs.ErrPermission, mockfs.ErrorModeAlways, 0)

		dir := mockfs.NewMockDir("testdir", handler, mockfs.WithFileErrorInjector(inj))

		_, err := dir.ReadDir(-1)
		assertError(t, err, fs.ErrPermission)
	})

	t.Run("pagination", func(t *testing.T) {
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
		requireNoError(t, err, "page 1")
		if len(result1) != 2 {
			t.Errorf("page 1: len = %d, want 2", len(result1))
		}

		result2, err := dir.ReadDir(2)
		requireNoError(t, err, "page 2")
		if len(result2) != 2 {
			t.Errorf("page 2: len = %d, want 2", len(result2))
		}

		// Next read should return empty
		result3, err := dir.ReadDir(2)
		requireNoError(t, err, "page 3")
		if len(result3) != 0 {
			t.Errorf("page 3: len = %d, want 0", len(result3))
		}
	})
}

// --- Stat Operations ---

// TestMockFile_Stat tests Stat operation.
func TestMockFile_Stat(t *testing.T) {
	t.Parallel()

	t.Run("file info", func(t *testing.T) {
		mapFile := &fstest.MapFile{
			Data:    []byte("test data content"),
			Mode:    0o644,
			ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		file := mockfs.NewMockFile(mapFile, "testdir/test.txt")

		info, err := file.Stat()
		requireNoError(t, err)

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
	})

	t.Run("closed file", func(t *testing.T) {
		file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
		_ = file.Close()
		_, err := file.Stat()
		assertError(t, err, fs.ErrClosed)
	})

	t.Run("error injection", func(t *testing.T) {
		mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0o644, ModTime: time.Now()}
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpStat, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)

		file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileErrorInjector(inj))

		_, err := file.Stat()
		assertError(t, err, mockfs.ErrPermission)
	})

	t.Run("basename extraction", func(t *testing.T) {
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
			file := mockfs.NewMockFileFromBytes(tt.path, []byte("test"))
			info, err := file.Stat()
			requireNoError(t, err)
			if info.Name() != tt.wantName {
				t.Errorf("path %q: Name() = %q, want %q", tt.path, info.Name(), tt.wantName)
			}
		}
	})

	t.Run("directory mode", func(t *testing.T) {
		handler := func(n int) ([]fs.DirEntry, error) { return []fs.DirEntry{}, nil }
		dir := mockfs.NewMockDir("testdir", handler)

		info, err := dir.Stat()
		requireNoError(t, err)
		if !info.IsDir() {
			t.Error("IsDir() = false, want true")
		}
	})

	t.Run("regular file", func(t *testing.T) {
		file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

		info, err := file.Stat()
		requireNoError(t, err)
		if info.IsDir() {
			t.Error("IsDir() = true, want false")
		}
	})

	t.Run("permissions", func(t *testing.T) {
		modes := []mockfs.FileMode{0o644, 0o755, 0o400, 0o777}
		for _, mode := range modes {
			mapFile := &fstest.MapFile{
				Data:    []byte("test"),
				Mode:    mode,
				ModTime: time.Now(),
			}
			file := mockfs.NewMockFile(mapFile, "test.txt")

			info, err := file.Stat()
			requireNoError(t, err)
			if info.Mode() != mode {
				t.Errorf("mode = %v, want %v", info.Mode(), mode)
			}
		}
	})
}

// --- Close Operations ---

// TestMockFile_Close tests Close operation.
func TestMockFile_Close(t *testing.T) {
	t.Parallel()

	t.Run("normal close", func(t *testing.T) {
		file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
		requireNoError(t, file.Close())
	})

	t.Run("double close", func(t *testing.T) {
		file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

		_ = file.Close()
		assertError(t, file.Close(), fs.ErrClosed)
	})

	t.Run("error injection", func(t *testing.T) {
		mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0o644, ModTime: time.Now()}
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpClose, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		file := mockfs.NewMockFile(mapFile, "test.txt", mockfs.WithFileErrorInjector(inj))

		assertError(t, file.Close(), mockfs.ErrPermission)
		assertError(t, file.Close(), mockfs.ErrClosed, "second close should return ErrClosed")
	})
}

// --- Statistics and Accessors ---

// TestMockFile_Stats tests operation statistics.
func TestMockFile_Stats(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
	buf := make([]byte, 4)
	_, _ = file.Read(buf)

	snap1 := file.Stats()
	reads1 := snap1.Count(mockfs.OpRead)

	// Perform another read
	_, _ = file.Read(buf)

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
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
	inj := file.ErrorInjector()
	if inj == nil {
		t.Fatal("injector is nil")
	}

	// Configure and verify injection works
	inj.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrUnexpectedEOF, mockfs.ErrorModeOnce, 0)

	buf := make([]byte, 4)
	_, err := file.Read(buf)
	assertError(t, err, mockfs.ErrUnexpectedEOF)
}

// TestMockFile_LatencySimulator_Exists tests latency simulator access.
func TestMockFile_LatencySimulator_Exists(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))
	sim := file.LatencySimulator()
	if sim == nil {
		t.Fatal("latency simulator is nil")
	}
}

// --- Latency Tests ---

// TestMockFile_LatencySimulation tests that latency is properly applied.
func TestMockFile_LatencySimulation(t *testing.T) {
	t.Parallel()

	latencySim := mockfs.NewLatencySimulator(testDuration)
	file := mockfs.NewMockFileFromString("test.txt", "test", mockfs.WithFileLatencySimulator(latencySim))

	operations := []struct {
		name string
		fn   func() error
	}{
		{
			name: "read",
			fn: func() error {
				buf := make([]byte, 4)
				_, err := file.Read(buf)
				return err
			},
		},
		{
			name: "write",
			fn: func() error {
				_, err := file.Write([]byte("new"))
				return err
			},
		},
		{
			name: "stat",
			fn: func() error {
				_, err := file.Stat()
				return err
			},
		},
		{
			name: "close",
			fn: func() error {
				return file.Close()
			},
		},
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
	t.Parallel()

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
	t.Parallel()

	latencySim := mockfs.NewLatencySimulator(testDuration)

	file := mockfs.NewMockFileFromString("test.txt", "test", mockfs.WithFileLatencySimulator(latencySim))

	// First read - should have latency
	buf := make([]byte, 4)
	start := time.Now()
	_, err := file.Read(buf)
	requireNoError(t, err, "first read")
	elapsed := time.Since(start)

	// We expect latency on first call
	if elapsed < testDuration-tolerance {
		t.Errorf("first read: expected latency ~%v, got %v", testDuration, elapsed)
	}
}

// TestMockFile_LatencySharedSimulator tests sharing latency simulator between files.
func TestMockFile_LatencySharedSimulator(t *testing.T) {
	t.Parallel()

	sharedLatency := mockfs.NewLatencySimulator(testDuration)

	file1 := mockfs.NewMockFileFromString("file1.txt", "file1", mockfs.WithFileLatencySimulator(sharedLatency))
	file2 := mockfs.NewMockFileFromString("file2.txt", "file2", mockfs.WithFileLatencySimulator(sharedLatency))

	// Both files should experience latency
	buf := make([]byte, 5)

	start := time.Now()
	_, err := file1.Read(buf)
	requireNoError(t, err, "file1 read")
	assertDuration(t, start, testDuration, "file1 read")

	start = time.Now()
	_, err = file2.Read(buf)
	requireNoError(t, err, "file2 read")
	assertDuration(t, start, testDuration, "file2 read")
}

// TestMockFile_LatencyCloning tests that files get independent latency simulators.
func TestMockFile_LatencyCloning(t *testing.T) {
	t.Parallel()

	// Create a MockFS with latency that uses Once mode
	mfs := mockfs.NewMockFS(
		mockfs.File("file1.txt", "data1"),
		mockfs.File("file2.txt", "data2"),
		mockfs.WithLatency(testDuration),
	)

	// Open two files - each should get cloned latency simulator
	f1, err := mfs.Open("file1.txt")
	requireNoError(t, err, "open file1")
	defer f1.Close()

	f2, err := mfs.Open("file2.txt")
	requireNoError(t, err, "open file2")
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

// --- Concurrency Tests ---

// TestMockFile_ConcurrentReads tests concurrent read operations.
func TestMockFile_ConcurrentReads(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("test.txt", []byte("data"))

	var wg sync.WaitGroup
	closedCount := int32(0)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := file.Close(); err == nil {
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
	t.Parallel()

	file := mockfs.NewMockFileFromString("test.txt", "initial")

	// Read initial content
	buf := make([]byte, 7)
	n, err := file.Read(buf)
	if err != nil || n != 7 || string(buf) != "initial" {
		t.Fatalf("initial read failed: n=%d, data=%q, err=%v", n, buf, err)
	}

	// Write new content (overwrite mode)
	_, err = file.Write([]byte("replaced"))
	requireNoError(t, err, "write")

	// Position should be at end after write in overwrite mode
	n, err = file.Read(buf)
	if err != io.EOF {
		t.Errorf("read after write: expected EOF, got n=%d, err=%v", n, err)
	}
}

// TestMockFile_EmptyFile tests operations on empty file.
func TestMockFile_EmptyFile(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromBytes("empty.txt", []byte{})

	// Read should return EOF immediately
	buf := make([]byte, 10)
	n, err := file.Read(buf)
	if err != io.EOF || n != 0 {
		t.Errorf("read empty: n=%d, err=%v, want 0, EOF", n, err)
	}

	// Stat should work
	info, err := file.Stat()
	requireNoError(t, err, "stat")
	if info.Size() != 0 {
		t.Errorf("size = %d, want 0", info.Size())
	}

	// Write should work
	_, err = file.Write([]byte("content"))
	requireNoError(t, err, "write")
}

// TestMockFile_MultipleFilesIndependent tests that files have independent state.
func TestMockFile_MultipleFilesIndependent(t *testing.T) {
	t.Parallel()

	file1 := mockfs.NewMockFileFromBytes("file1.txt", []byte("content1"))
	file2 := mockfs.NewMockFileFromBytes("file2.txt", []byte("content2"))

	// Read from file1
	buf1 := make([]byte, 8)
	n1, err := file1.Read(buf1)
	requireNoError(t, err, "file1 read")
	if string(buf1[:n1]) != "content1" {
		t.Fatalf("file1 read failed: %q", buf1[:n1])
	}

	// Read from file2
	buf2 := make([]byte, 8)
	n2, err := file2.Read(buf2)
	requireNoError(t, err, "file2 read")
	if string(buf2[:n2]) != "content2" {
		t.Fatalf("file2 read failed: %q", buf2[:n2])
	}

	// Close file1 should not affect file2
	requireNoError(t, file1.Close(), "file1 close")

	// file2 should still be readable
	buf3 := make([]byte, 8)
	_, err = file2.Read(buf3)
	assertError(t, err, io.EOF, "file2 still be open")
	_ = file2.Close()
}

// TestMockFile_PartialReads tests continuing reads.
func TestMockFile_PartialReads(t *testing.T) {
	t.Parallel()

	file := mockfs.NewMockFileFromString("test.txt", "0123456789")

	// Read in chunks
	chunks := []string{"012", "345", "6789"}
	for i, want := range chunks {
		buf := make([]byte, len(want))
		n, err := file.Read(buf)
		requireNoError(t, err, fmt.Sprintf("read %d", i))
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
	assertError(t, err, io.EOF, "final read")
	if n != 0 {
		t.Errorf("final read: n=%d, want 0", n)
	}
}

// TestMockFile_ConcurrentReadWrite tests race conditions in concurrent read/write.
func TestMockFile_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
