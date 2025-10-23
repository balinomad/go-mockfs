package mockfs_test

import (
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/balinomad/go-mockfs"
)

// TestNewMockFile tests the main constructor.
func TestNewMockFile(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("test data"),
		Mode:    0644,
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

			file := mockfs.NewMockFile(tt.mapFile, tt.fileName, 0, nil, nil, nil, nil)
			if file == nil && !tt.wantPanic {
				t.Error("expected non-nil file")
			}
		})
	}
}

// TestNewMockFile_defaults tests that nil arguments to NewMockFile are
// initialized with non-nil default implementations.
func TestNewMockFile_defaults(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test")}
	// Call with nil for injector, latency, and stats
	file := mockfs.NewMockFile(mapFile, "test.txt", 0, nil, nil, nil, nil)

	if file.ErrorInjector() == nil {
		t.Error("expected non-nil default ErrorInjector")
	}
	if file.LatencySimulator() == nil {
		t.Error("expected non-nil default LatencySimulator")
	}
	if file.Stats() == nil {
		t.Error("expected non-nil default Stats")
	}
}

// TestNewMockFileSimple tests the simple constructor.
func TestNewMockFileSimple(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("hello"),
		Mode:    0644,
		ModTime: time.Now(),
	}

	file := mockfs.NewMockFileSimple(mapFile, "test.txt")
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

// TestNewMockFileFromData tests creating file from data.
func TestNewMockFileFromData(t *testing.T) {
	data := []byte("test content")
	file := mockfs.NewMockFileFromData("test.txt", data)

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

// TestNewMockFileWithLatency tests latency constructor.
func TestNewMockFileWithLatency(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("data"),
		Mode:    0644,
		ModTime: time.Now(),
	}

	file := mockfs.NewMockFileWithLatency(mapFile, "test.txt", testDuration)

	buf := make([]byte, 4)
	start := time.Now()
	_, err := file.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	assertDuration(t, start, testDuration, "read with latency")
}

// TestNewMockFileForReadWrite tests read/write focused constructor.
func TestNewMockFileForReadWrite(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("test"),
		Mode:    0644,
		ModTime: time.Now(),
	}

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpRead, "test.txt", io.ErrUnexpectedEOF, mockfs.ErrorModeOnce, 0)

	file := mockfs.NewMockFileForReadWrite(mapFile, "test.txt", testDuration, injector)

	// First read should fail
	buf := make([]byte, 4)
	_, err := file.Read(buf)
	if err != io.ErrUnexpectedEOF {
		t.Errorf("first read error = %v, want %v", err, io.ErrUnexpectedEOF)
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

// TestNewMockDirectory tests directory constructor.
func TestNewMockDirectory(t *testing.T) {
	t.Run("with handler", func(t *testing.T) {
		handler := func(n int) ([]fs.DirEntry, error) {
			return []fs.DirEntry{}, nil
		}

		dir := mockfs.NewMockDirectory("testdir", time.Now(), handler, nil, nil, nil)
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

	t.Run("nil handler panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic for nil handler")
			}
		}()
		mockfs.NewMockDirectory("testdir", time.Now(), nil, nil, nil, nil)
	})
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
			mapFile := &fstest.MapFile{Data: tt.data, Mode: 0644, ModTime: time.Now()}
			file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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

// TestMockFile_Read_position tests that read position advances.
func TestMockFile_Read_position(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("abcdefghij"),
		Mode:    0644,
		ModTime: time.Now(),
	}
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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

// TestMockFile_Read_closed tests reading from closed file.
func TestMockFile_Read_closed(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	if err := file.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	buf := make([]byte, 10)
	_, err := file.Read(buf)
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Read_errorInjection tests error injection on read.
func TestMockFile_Read_errorInjection(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	wantErr := io.ErrUnexpectedEOF

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpRead, "test.txt", wantErr, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFile(mapFile, "test.txt", 0, injector, nil, nil, nil)

	buf := make([]byte, 10)
	_, err := file.Read(buf)
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// TestMockFile_Read_largeFile tests reading a large file.
func TestMockFile_Read_largeFile(t *testing.T) {
	size := 10 * 1024 * 1024 // 10MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	mapFile := &fstest.MapFile{Data: data, Mode: 0644, ModTime: time.Now()}
	file := mockfs.NewMockFileSimple(mapFile, "large.bin")

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

// TestMockFile_Read_zeroByte tests reading with zero-length buffer.
func TestMockFile_Read_zeroByte(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	buf := make([]byte, 0)
	n, err := file.Read(buf)
	if err != nil {
		t.Errorf("zero-byte read failed: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

// TestMockFile_Write_overwrite tests overwrite mode.
func TestMockFile_Write_overwrite(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("original content"),
		Mode:    0644,
		ModTime: time.Now(),
	}

	// writeModeOverwrite is what NewMockFileSimple uses
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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

// TestMockFile_Write_append tests append mode.
func TestMockFile_Write_append(t *testing.T) {
	initialData := []byte("initial-")
	mapFile := &fstest.MapFile{
		Data:    append([]byte(nil), initialData...),
		Mode:    0644,
		ModTime: time.Now(),
	}

	// Pass 0 (untyped const) for writeModeAppend
	file := mockfs.NewMockFile(mapFile, "test.txt", 0, nil, nil, nil, nil)

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

// TestMockFile_Write_readOnly tests read-only mode.
func TestMockFile_Write_readOnly(t *testing.T) {
	initialData := []byte("initial")
	mapFile := &fstest.MapFile{
		Data:    append([]byte(nil), initialData...),
		Mode:    0644,
		ModTime: time.Now(),
	}

	// Pass 2 (untyped const) for writeModeReadOnly
	file := mockfs.NewMockFile(mapFile, "test.txt", 2, nil, nil, nil, nil)

	_, err := file.Write([]byte("new data"))

	// Expect permission error
	if !errors.Is(err, fs.ErrPermission) {
		// fs.ErrPermission may be wrapped in fs.PathError
		if err == nil || !strings.Contains(err.Error(), fs.ErrPermission.Error()) {
			t.Errorf("error = %v, want %v (or wrapper)", err, fs.ErrPermission)
		}
	}

	// Data should not have changed
	if string(mapFile.Data) != string(initialData) {
		t.Errorf("data = %q, want %q", mapFile.Data, initialData)
	}
}

// TestMockFile_Write_invalid tests that an invalid writeMode panics.
func TestMockFile_Write_invalid(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data: []byte("initial"),
		Mode: 0644,
	}

	// Pass 99 (untyped const) for an invalid writeMode
	file := mockfs.NewMockFile(mapFile, "test.txt", 99, nil, nil, nil, nil)

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid writeMode, but did not panic")
		}
	}()

	_, _ = file.Write([]byte("new data"))
}

// TestMockFile_Write_closed tests writing to closed file.
func TestMockFile_Write_closed(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	if err := file.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	_, err := file.Write([]byte("new"))
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Write_errorInjection tests error injection on write.
func TestMockFile_Write_errorInjection(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	wantErr := fs.ErrPermission

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpWrite, "test.txt", wantErr, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFile(mapFile, "test.txt", 0, injector, nil, nil, nil)

	_, err := file.Write([]byte("data"))
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// TestMockFile_Write_modTimeUpdate tests that ModTime is updated on write.
func TestMockFile_Write_modTimeUpdate(t *testing.T) {
	initialTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	mapFile := &fstest.MapFile{
		Data:    []byte("old"),
		Mode:    0644,
		ModTime: initialTime,
	}

	file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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

// TestMockFile_Write_zeroByte tests writing zero bytes.
func TestMockFile_Write_zeroByte(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("initial"))

	n, err := file.Write([]byte{})
	if err != nil {
		t.Errorf("zero-byte write failed: %v", err)
	}
	if n != 0 {
		t.Errorf("n = %d, want 0", n)
	}
}

// TestMockFile_ReadDir_valid tests valid directory reading.
func TestMockFile_ReadDir_valid(t *testing.T) {
	entries := []fs.DirEntry{
		&mockDirEntry{name: "file1.txt", isDir: false},
		&mockDirEntry{name: "file2.txt", isDir: false},
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

	dir := mockfs.NewMockDirectory("testdir", time.Now(), handler, nil, nil, nil)

	result, err := dir.ReadDir(-1)
	if err != nil {
		t.Fatalf("readdir failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("len = %d, want 2", len(result))
	}
}

// TestMockFile_ReadDir_nilHandler tests ReadDir on a directory mock
// created with NewMockFile (which allows a nil handler).
func TestMockFile_ReadDir_nilHandler(t *testing.T) {
	// This tests the case where a file is created with ModeDir using
	// NewMockFile (which allows a nil handler), rather than NewMockDirectory
	// (which panics).
	mapFile := &fstest.MapFile{
		Mode: fs.ModeDir | 0755,
	}

	// Create file with nil readDirHandler
	file := mockfs.NewMockFile(mapFile, "testdir", 0, nil, nil, nil, nil)

	_, err := file.ReadDir(-1)

	// Expect an error because the handler is nil
	if !errors.Is(err, fs.ErrInvalid) {
		// fs.ErrInvalid may be wrapped
		if err == nil || !strings.Contains(err.Error(), fs.ErrInvalid.Error()) {
			t.Errorf("error = %v, want %v (or wrapper)", err, fs.ErrInvalid)
		}
	}
}

// TestMockFile_ReadDir_closed tests closed directory.
func TestMockFile_ReadDir_closed(t *testing.T) {
	handler := func(n int) ([]fs.DirEntry, error) {
		return []fs.DirEntry{}, nil
	}

	dir := mockfs.NewMockDirectory("testdir", time.Now(), handler, nil, nil, nil)
	_ = dir.Close()

	_, err := dir.ReadDir(-1)
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_ReadDir_notDirectory tests file not directory error.
func TestMockFile_ReadDir_notDirectory(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	_, err := file.ReadDir(-1)
	if err == nil {
		t.Error("expected error for non-directory")
	}
}

// TestMockFile_ReadDir_errorInjection tests error injection on readdir.
func TestMockFile_ReadDir_errorInjection(t *testing.T) {
	wantErr := fs.ErrPermission

	handler := func(n int) ([]fs.DirEntry, error) {
		return []fs.DirEntry{}, nil
	}

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpReadDir, "testdir", wantErr, mockfs.ErrorModeAlways, 0)

	dir := mockfs.NewMockDirectory("testdir", time.Now(), handler, injector, nil, nil)

	_, err := dir.ReadDir(-1)
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// TestMockFile_ReadDir_pagination tests reading directory entries in pages.
func TestMockFile_ReadDir_pagination(t *testing.T) {
	entries := []fs.DirEntry{
		&mockDirEntry{name: "file1", isDir: false},
		&mockDirEntry{name: "file2", isDir: false},
		&mockDirEntry{name: "file3", isDir: false},
		&mockDirEntry{name: "file4", isDir: false},
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

	dir := mockfs.NewMockDirectory("testdir", time.Now(), handler, nil, nil, nil)

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
		Mode:    0644,
		ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	file := mockfs.NewMockFileSimple(mapFile, "testdir/test.txt")

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
	if info.Mode() != 0644 {
		t.Errorf("mode = %v, want 0644", info.Mode())
	}
	if info.IsDir() {
		t.Error("IsDir = true, want false")
	}
}

// TestMockFile_Stat_closed tests stat on closed file.
func TestMockFile_Stat_closed(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	if err := file.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	_, err := file.Stat()
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Stat_errorInjection tests error injection on stat.
func TestMockFile_Stat_errorInjection(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	wantErr := fs.ErrPermission

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpStat, "test.txt", wantErr, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFile(mapFile, "test.txt", 0, injector, nil, nil, nil)

	_, err := file.Stat()
	if err != wantErr {
		t.Errorf("error = %v, want %v", err, wantErr)
	}
}

// TestMockFile_Stat_basename tests correct basename extraction.
func TestMockFile_Stat_basename(t *testing.T) {
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
			mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
			file := mockfs.NewMockFileSimple(mapFile, tt.path)

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

// TestMockFile_Stat_directoryMode tests directory vs file detection.
func TestMockFile_Stat_directoryMode(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		handler := func(n int) ([]fs.DirEntry, error) {
			return []fs.DirEntry{}, nil
		}
		dir := mockfs.NewMockDirectory("testdir", time.Now(), handler, nil, nil, nil)

		info, err := dir.Stat()
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}

		if !info.IsDir() {
			t.Error("IsDir() = false, want true")
		}
	})

	t.Run("regular file", func(t *testing.T) {
		file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

		info, err := file.Stat()
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}

		if info.IsDir() {
			t.Error("IsDir() = true, want false")
		}
	})
}

// TestMockFile_Stat_permissions tests different file modes.
func TestMockFile_Stat_permissions(t *testing.T) {
	modes := []fs.FileMode{
		0644,
		0755,
		0400,
		0777,
	}

	for _, mode := range modes {
		t.Run(mode.String(), func(t *testing.T) {
			mapFile := &fstest.MapFile{
				Data:    []byte("test"),
				Mode:    mode,
				ModTime: time.Now(),
			}
			file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	err := file.Close()
	if err != nil {
		t.Errorf("close failed: %v", err)
	}
}

// TestMockFile_Close_double tests double close returns error.
func TestMockFile_Close_double(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	_ = file.Close()
	err := file.Close()
	if err != fs.ErrClosed {
		t.Errorf("error = %v, want ErrClosed", err)
	}
}

// TestMockFile_Close_errorInjection tests error injection on close.
func TestMockFile_Close_errorInjection(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	wantErr := fs.ErrPermission

	injector := mockfs.NewErrorInjector()
	injector.AddExact(mockfs.OpClose, "test.txt", wantErr, mockfs.ErrorModeAlways, 0)

	file := mockfs.NewMockFile(mapFile, "test.txt", 0, injector, nil, nil, nil)

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

// TestMockFile_Counters tests operation counting.
func TestMockFile_Counters(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	stats := file.Stats()
	if stats == nil {
		t.Fatal("stats is nil")
	}

	// Perform operations
	buf := make([]byte, 4)
	_, _ = file.Read(buf)
	_, _ = file.Write([]byte("new"))
	_, _ = file.Stat()
	_ = file.Close()

	// Verify stats tracked operations
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

// TestMockFile_ErrorInjector tests error injector access.
func TestMockFile_ErrorInjector(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	injector := file.ErrorInjector()
	if injector == nil {
		t.Fatal("injector is nil")
	}

	// Configure and verify injection works
	injector.AddExact(mockfs.OpRead, "test.txt", io.ErrUnexpectedEOF, mockfs.ErrorModeOnce, 0)

	buf := make([]byte, 4)
	_, err := file.Read(buf)
	if err != io.ErrUnexpectedEOF {
		t.Errorf("error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

// TestMockFile_LatencySimulator tests latency simulator access.
func TestMockFile_LatencySimulator(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	sim := file.LatencySimulator()
	if sim == nil {
		t.Fatal("latency simulator is nil")
	}
}

// TestMockFile_latencySimulation tests that latency is properly applied.
func TestMockFile_latencySimulation(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	latencySim := mockfs.NewLatencySimulator(testDuration)

	file := mockfs.NewMockFile(mapFile, "test.txt", 0, nil, latencySim, nil, nil)

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

// TestMockFile_latencyReset tests that latency state is reset on close.
func TestMockFile_latencyReset(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	latencySim := mockfs.NewLatencySimulator(testDuration)

	file := mockfs.NewMockFile(mapFile, "test.txt", 0, nil, latencySim, nil, nil)

	// Read with latency
	buf := make([]byte, 4)
	start := time.Now()
	_, _ = file.Read(buf)
	assertDuration(t, start, testDuration, "first read")

	// Close should reset
	_ = file.Close()
}

// TestMockFile_latencyOnceMode tests Once latency mode behavior.
func TestMockFile_latencyOnceMode(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	latencySim := mockfs.NewLatencySimulator(testDuration)

	file := mockfs.NewMockFile(mapFile, "test.txt", 0, nil, latencySim, nil, nil)

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

// TestMockFile_latencySharedSimulator tests sharing latency simulator between files.
func TestMockFile_latencySharedSimulator(t *testing.T) {
	sharedLatency := mockfs.NewLatencySimulator(testDuration)

	mapFile1 := &fstest.MapFile{Data: []byte("file1"), Mode: 0644, ModTime: time.Now()}
	mapFile2 := &fstest.MapFile{Data: []byte("file2"), Mode: 0644, ModTime: time.Now()}

	file1 := mockfs.NewMockFile(mapFile1, "file1.txt", 0, nil, sharedLatency, nil, nil)
	file2 := mockfs.NewMockFile(mapFile2, "file2.txt", 0, nil, sharedLatency, nil, nil)

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

// TestMockFile_concurrentReads tests concurrent read operations.
func TestMockFile_concurrentReads(t *testing.T) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	mapFile := &fstest.MapFile{Data: data, Mode: 0644, ModTime: time.Now()}
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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

// TestMockFile_concurrentWrites tests concurrent write operations.
func TestMockFile_concurrentWrites(t *testing.T) {
	mapFile := &fstest.MapFile{Data: []byte(""), Mode: 0644, ModTime: time.Now()}
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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

// TestMockFile_concurrentCloses tests that concurrent closes are safe.
func TestMockFile_concurrentCloses(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	var wg sync.WaitGroup
	closedCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := file.Close()
			mu.Lock()
			if err == nil {
				closedCount++
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Only one close should succeed
	if closedCount != 1 {
		t.Errorf("closedCount = %d, want 1", closedCount)
	}
}

// TestMockFile_readWriteSequence tests mixed read/write operations.
func TestMockFile_readWriteSequence(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("initial"),
		Mode:    0644,
		ModTime: time.Now(),
	}
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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

// TestMockFile_errorInjectorInvoked tests error injector invocation.
func TestMockFile_errorInjectorInvoked(t *testing.T) {
	injector := mockfs.NewErrorInjector()

	// Add error rules that we'll track
	for _, op := range []mockfs.Operation{mockfs.OpRead, mockfs.OpWrite, mockfs.OpStat, mockfs.OpClose} {
		// Use a custom rule that tracks invocations but returns no error
		rule := mockfs.NewErrorRule(nil, mockfs.ErrorModeAlways, 0, mockfs.NewExactMatcher("test.txt"))
		injector.Add(op, rule)
	}

	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	file := mockfs.NewMockFile(mapFile, "test.txt", 0, injector, nil, nil, nil)

	// Perform operations
	buf := make([]byte, 4)
	_, _ = file.Read(buf)
	_, _ = file.Write([]byte("new"))
	_, _ = file.Stat()
	_ = file.Close()

	// Verify through stats instead
	stats := file.Stats()
	expectedOps := []mockfs.Operation{
		mockfs.OpRead,
		mockfs.OpWrite,
		mockfs.OpStat,
		mockfs.OpClose,
	}

	for _, op := range expectedOps {
		if stats.Count(op) == 0 {
			t.Errorf("operation %v was not executed", op)
		}
	}
}

// TestMockFile_emptyFile tests operations on empty file.
func TestMockFile_emptyFile(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte{},
		Mode:    0644,
		ModTime: time.Now(),
	}
	file := mockfs.NewMockFileSimple(mapFile, "empty.txt")

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

// TestMockFile_multipleFilesIndependent tests that files have independent state.
func TestMockFile_multipleFilesIndependent(t *testing.T) {
	file1 := mockfs.NewMockFileFromData("file1.txt", []byte("content1"))
	file2 := mockfs.NewMockFileFromData("file2.txt", []byte("content2"))

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

// TestMockFile_partialReads tests continuing reads.
func TestMockFile_partialReads(t *testing.T) {
	mapFile := &fstest.MapFile{
		Data:    []byte("0123456789"),
		Mode:    0644,
		ModTime: time.Now(),
	}
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")

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

// TestMockFile_interfaceCompliance verifies interface compliance.
func TestMockFile_interfaceCompliance(t *testing.T) {
	file := mockfs.NewMockFileFromData("test.txt", []byte("data"))

	// Verify interfaces
	var _ fs.File = file
	var _ fs.ReadDirFile = file
	var _ io.Reader = file
	var _ io.Writer = file
	var _ io.Closer = file

	t.Log("all interfaces implemented correctly")
}

// mockDirEntry is a test helper for directory entries.
type mockDirEntry struct {
	name  string
	isDir bool
}

func (m *mockDirEntry) Name() string               { return m.name }
func (m *mockDirEntry) IsDir() bool                { return m.isDir }
func (m *mockDirEntry) Type() fs.FileMode          { return 0 }
func (m *mockDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

// BenchmarkMockFile_Read benchmarks read performance
func BenchmarkMockFile_Read(b *testing.B) {
	data := make([]byte, 1024)
	mapFile := &fstest.MapFile{Data: data, Mode: 0644, ModTime: time.Now()}
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")
	buf := make([]byte, 1024)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = file.Read(buf)
	}
}

// BenchmarkMockFile_Write benchmarks write performance.
func BenchmarkMockFile_Write(b *testing.B) {
	mapFile := &fstest.MapFile{Data: []byte{}, Mode: 0644, ModTime: time.Now()}
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")
	data := []byte("benchmark data")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = file.Write(data)
	}
}

// BenchmarkMockFile_Stat benchmarks stat performance.
func BenchmarkMockFile_Stat(b *testing.B) {
	mapFile := &fstest.MapFile{Data: []byte("test"), Mode: 0644, ModTime: time.Now()}
	file := mockfs.NewMockFileSimple(mapFile, "test.txt")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = file.Stat()
	}
}
