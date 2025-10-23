package mockfs

import (
	"io"
	"io/fs"
	"path"
	"sync"
	"testing/fstest"
	"time"
)

// writeMode controls how new data is applied to the file.
type writeMode uint8

const (
	writeModeAppend    writeMode = iota // Append data to existing contents.
	writeModeOverwrite                  // Overwrite existing contents.
	writeModeReadOnly                   // Write is not allowed.
)

// fileBackend is an interface for accessing the underlying file, stats, and error injector.
type fileBackend interface {
	// Stats returns the operation statistics for this file.
	Stats() *Stats

	// ErrorInjector returns the error injector for this file.
	ErrorInjector() ErrorInjector

	// LatencySimulator returns the latency simulator for this file.
	LatencySimulator() LatencySimulator
}

// MockFile represents an open file. It is a wrapper around fstest.MapFile to inject errors and track operations.
type MockFile interface {
	fs.ReadDirFile
	io.Writer
	fileBackend
}

// mockFile is the implementation of MockFile.
type mockFile struct {
	mapFile        *fstest.MapFile                  // The underlying file data.
	name           string                           // Cleaned name used to open this file (relative to its MockFS).
	position       int64                            // Current read position in the file.
	mu             sync.Mutex                       // Protects all mutable state.
	closed         bool                             // Tracks if the file has been closed.
	writeMode      writeMode                        // How writes modify the file data.
	readDirHandler func(int) ([]fs.DirEntry, error) // Handler for ReadDir operations (directories only).
	latency        LatencySimulator                 // Latency simulator for this file.
	stats          *Stats                           // Operation statistics.
	injector       ErrorInjector                    // Error injector for operations on this file.
}

// Ensure interface implementations.
var (
	_ fs.File        = (*mockFile)(nil)
	_ fs.ReadDirFile = (*mockFile)(nil)
	_ io.Reader      = (*mockFile)(nil)
	_ io.Writer      = (*mockFile)(nil)
	_ io.Closer      = (*mockFile)(nil)
	_ fileBackend    = (*mockFile)(nil)
)

// NewMockFile constructs a MockFile with full configuration.
//
// Parameters:
//   - mapFile: the fstest.MapFile containing file data and metadata.
//   - name: cleaned path used when checking injection rules.
//   - writeMode: how Write operations modify the file (append/overwrite/readonly).
//   - injector: the error injector to use (may be nil to use a new, empty injector).
//   - latencySimulator: simulator for operation latency (may be nil for no latency).
//   - readDirHandler: handler for ReadDir operations on directories (may be nil).
//   - stats: operation stats recorder. If nil, a new one is created.
//
// This is the most flexible constructor. For simpler use cases, consider using
// NewMockFileSimple, NewMockFileForReadWrite, or NewMockFileFromData.
func NewMockFile(
	mapFile *fstest.MapFile,
	name string,
	writeMode writeMode,
	injector ErrorInjector,
	latencySimulator LatencySimulator,
	readDirHandler func(int) ([]fs.DirEntry, error),
	stats *Stats,
) MockFile {
	if mapFile == nil {
		panic("mockfs: mapFile cannot be nil")
	}

	// Default no-op callbacks
	if injector == nil {
		injector = NewErrorInjector()
	}
	if latencySimulator == nil {
		latencySimulator = NewNoopLatencySimulator()
	}
	if stats == nil {
		stats = NewStats()
	}

	return &mockFile{
		mapFile:        mapFile,
		name:           name,
		writeMode:      writeMode,
		injector:       injector,
		latency:        latencySimulator,
		readDirHandler: readDirHandler,
		stats:          stats,
	}
}

// NewMockFileSimple constructs a MockFile with no error injection or latency simulation.
// The file is writable in overwrite mode.
func NewMockFileSimple(mapFile *fstest.MapFile, name string) MockFile {
	return NewMockFile(mapFile, name, writeModeOverwrite, nil, nil, nil, nil)
}

// NewMockFileFromData constructs a MockFile from raw data with no error injection or latency.
// The file is created with mode 0644 and current time as ModTime.
func NewMockFileFromData(name string, data []byte) MockFile {
	mapFile := &fstest.MapFile{
		Data:    append([]byte(nil), data...), // Copy data
		Mode:    0644,
		ModTime: time.Now(),
	}
	return NewMockFileSimple(mapFile, name)
}

// NewMockFileWithLatency constructs a MockFile with uniform latency for all operations.
// The file is writable in overwrite mode.
func NewMockFileWithLatency(mapFile *fstest.MapFile, name string, latency time.Duration) MockFile {
	return NewMockFile(mapFile, name, writeModeOverwrite, nil, NewLatencySimulator(latency), nil, nil)
}

// NewMockFileForReadWrite constructs a MockFile with error injection and latency
// only for Read and Write operations. Other operations (Open, Close, Stat) are fast
// and error-free. This is useful for testing I/O error handling without complicating
// the test setup.
func NewMockFileForReadWrite(
	mapFile *fstest.MapFile,
	name string,
	latency time.Duration,
	injector ErrorInjector,
) MockFile {
	// Create per-operation latency with only Read/Write having delays
	perOpLatency := NewLatencySimulatorPerOp(map[Operation]time.Duration{
		OpRead:  latency,
		OpWrite: latency,
	})

	return NewMockFile(mapFile, name, writeModeOverwrite, injector, perOpLatency, nil, nil)
}

// NewMockDirectory constructs a MockFile representing a directory.
// The readDirHandler is required for ReadDir operations.
func NewMockDirectory(
	name string,
	modTime time.Time,
	readDirHandler func(int) ([]fs.DirEntry, error),
	injector ErrorInjector,
	latencySimulator LatencySimulator,
	stats *Stats,
) MockFile {
	mapFile := &fstest.MapFile{
		Mode:    fs.ModeDir | 0755,
		ModTime: modTime,
	}

	if readDirHandler == nil {
		panic("mockfs: readDirHandler required for directories")
	}

	return NewMockFile(mapFile, name, writeModeReadOnly, injector, latencySimulator, readDirHandler, stats)
}

// Read implements io.Reader for MockFile.
func (f *mockFile) Read(b []byte) (n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Record the result of this operation on exit
	defer func() { f.stats.Record(OpRead, n, err) }()

	if f.closed {
		err = fs.ErrClosed
		return 0, err
	}

	// Simulate latency before checking for errors (models real I/O timing)
	f.latency.Simulate(OpRead)

	if err = f.injector.CheckAndApply(OpRead, f.name); err != nil {
		return 0, err
	}

	// Read from current position
	if f.position >= int64(len(f.mapFile.Data)) {
		err = io.EOF
		return 0, err
	}

	n = copy(b, f.mapFile.Data[f.position:])
	f.position += int64(n)

	return n, nil
}

// Write implements io.Writer for MockFile.
func (f *mockFile) Write(b []byte) (n int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Record the result of this operation on exit
	defer func() { f.stats.Record(OpWrite, n, err) }()

	if f.closed {
		err = fs.ErrClosed
		return 0, err
	}

	// Simulate latency before checking for errors (models real I/O timing)
	f.latency.Simulate(OpWrite)

	if err = f.injector.CheckAndApply(OpWrite, f.name); err != nil {
		return 0, err
	}

	// Check write mode
	switch f.writeMode {
	case writeModeReadOnly:
		err = &fs.PathError{Op: "Write", Path: f.name, Err: fs.ErrPermission}
		return 0, err

	case writeModeAppend:
		f.mapFile.Data = append(f.mapFile.Data, b...)
		f.mapFile.ModTime = time.Now()
		n = len(b)
		return n, nil

	case writeModeOverwrite:
		// Replace entire content
		f.mapFile.Data = append([]byte(nil), b...) // Copy data
		f.mapFile.ModTime = time.Now()
		n = len(b)
		f.position = int64(n)
		return n, nil

	default:
		panic("mockfs: invalid writeMode")
	}
}

// ReadDir reads the contents of the directory and returns
// a slice of up to n DirEntry values in directory order.
// Subsequent calls on the same file will yield further DirEntry values.
// It implements fs.ReadDirFile for MockFile.
func (f *mockFile) ReadDir(n int) (entries []fs.DirEntry, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Record the result of this operation on exit
	defer func() { f.stats.Record(OpReadDir, 0, err) }()

	if f.closed {
		err = fs.ErrClosed
		return nil, err
	}

	if !f.mapFile.Mode.IsDir() {
		err = &fs.PathError{Op: "ReadDir", Path: f.name, Err: fs.ErrInvalid}
		return nil, err
	}

	if f.readDirHandler == nil {
		err = &fs.PathError{Op: "ReadDir", Path: f.name, Err: fs.ErrInvalid}
		return nil, err
	}

	// Simulate latency before checking for errors (models real I/O timing)
	f.latency.Simulate(OpReadDir)

	if err = f.injector.CheckAndApply(OpReadDir, f.name); err != nil {
		return nil, err
	}

	return f.readDirHandler(n)
}

// Stat implements fs.File.Stat.
func (f *mockFile) Stat() (info fs.FileInfo, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Record the result of this operation on exit
	defer func() { f.stats.Record(OpStat, 0, err) }()

	if f.closed {
		err = fs.ErrClosed
		return nil, err
	}

	// Simulate latency before checking for errors
	f.latency.Simulate(OpStat)

	if err = f.injector.CheckAndApply(OpStat, f.name); err != nil {
		return nil, err
	}

	// Build FileInfo from MapFile content
	name := path.Base(f.name)
	size := int64(len(f.mapFile.Data))
	mode := f.mapFile.Mode
	modTime := f.mapFile.ModTime

	return &fileInfo{
		name:    name,
		size:    size,
		mode:    mode,
		modTime: modTime,
	}, nil
}

// Close implements io.Closer for MockFile.
//
// Close returns fs.ErrClosed if called multiple times, allowing tests to detect
// double-close bugs.
func (f *mockFile) Close() (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Record the result of this operation on exit
	defer func() { f.stats.Record(OpClose, 0, err) }()

	if f.closed {
		err = fs.ErrClosed
		return err
	}

	// Simulate latency before checking for errors (models real I/O timing)
	f.latency.Simulate(OpClose)

	// Check for injected error
	if err = f.injector.CheckAndApply(OpClose, f.name); err != nil {
		// Still mark as closed to prevent resource leaks
		f.closed = true
		f.latency.Reset()
		return err
	}

	// Mark as closed
	f.closed = true

	// Reset latency state after closing
	f.latency.Reset()

	return nil
}

// ErrorInjector returns the error injector for advanced configuration.
func (f *mockFile) ErrorInjector() ErrorInjector {
	return f.injector
}

// Stats returns the operation statistics for the MockFile.
func (f *mockFile) Stats() *Stats {
	return f.stats
}

// LatencySimulator returns the latency simulator for the MockFile.
func (f *mockFile) LatencySimulator() LatencySimulator {
	return f.latency
}
