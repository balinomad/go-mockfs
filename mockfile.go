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
	// Stats returns a snapshot of operation statistics for this file handle.
	// This includes only operations performed on this specific file handle
	// (Read, Write, Close, Stat, ReadDir). It does NOT include filesystem-level
	// operations. Use MockFS.Stats() to inspect filesystem-level operations.
	Stats() Stats

	// ErrorInjector returns the error injector for this file.
	ErrorInjector() ErrorInjector

	// LatencySimulator returns the latency simulator for this file.
	LatencySimulator() LatencySimulator
}

// MockFile represents an open file. It is a wrapper around fstest.MapFile to inject errors and track operations.
type MockFile interface {
	fs.File
	fs.ReadDirFile
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
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
	stats          StatsRecorder                    // Operation statistics.
	injector       ErrorInjector                    // Error injector for operations on this file.
}

// Ensure interface implementations.
var (
	_ fs.File        = (*mockFile)(nil)
	_ fs.ReadDirFile = (*mockFile)(nil)
	_ io.Reader      = (*mockFile)(nil)
	_ io.Writer      = (*mockFile)(nil)
	_ io.Closer      = (*mockFile)(nil)
	_ io.Seeker      = (*mockFile)(nil)
	_ fileBackend    = (*mockFile)(nil)
)

// fileOptions holds the configurable state for a new mockFile.
type fileOptions struct {
	writeMode      writeMode
	injector       ErrorInjector
	latency        LatencySimulator
	readDirHandler func(int) ([]fs.DirEntry, error)
	stats          StatsRecorder
}

// MockFileOption is a function type for configuring a new MockFile.
type MockFileOption func(*fileOptions)

// WithFileAppend sets the file to append data on write.
func WithFileAppend() MockFileOption {
	return func(o *fileOptions) {
		o.writeMode = writeModeAppend
	}
}

// WithFileOverwrite sets the file to overwrite content on write (default).
func WithFileOverwrite() MockFileOption {
	return func(o *fileOptions) {
		o.writeMode = writeModeOverwrite
	}
}

// WithFileReadOnly sets the file to reject all writes.
func WithFileReadOnly() MockFileOption {
	return func(o *fileOptions) {
		o.writeMode = writeModeReadOnly
	}
}

// WithFileErrorInjector sets the error injector for the file.
func WithFileErrorInjector(injector ErrorInjector) MockFileOption {
	return func(o *fileOptions) {
		if injector != nil {
			o.injector = injector
		}
	}
}

// WithFileLatency sets a uniform simulated latency for all operations.
func WithFileLatency(duration time.Duration) MockFileOption {
	return func(o *fileOptions) {
		o.latency = NewLatencySimulator(duration)
	}
}

// WithFileLatencySimulator sets a custom latency simulator.
func WithFileLatencySimulator(sim LatencySimulator) MockFileOption {
	return func(o *fileOptions) {
		if sim != nil {
			o.latency = sim
		}
	}
}

// WithFilePerOperationLatency sets different latencies for different operations.
func WithFilePerOperationLatency(durations map[Operation]time.Duration) MockFileOption {
	return func(o *fileOptions) {
		o.latency = NewLatencySimulatorPerOp(durations)
	}
}

// WithFileReadDirHandler sets the handler for ReadDir operations.
// The purpose of this handler is to simulate directory contents.
// If nil, an empty directory will be created.
func WithFileReadDirHandler(handler func(int) ([]fs.DirEntry, error)) MockFileOption {
	return func(o *fileOptions) {
		o.readDirHandler = handler
	}
}

// WithFileStats sets the stats recorder for the file handle.
// If nil, a new one is created.
func WithFileStats(stats StatsRecorder) MockFileOption {
	return func(o *fileOptions) {
		if stats != nil {
			o.stats = stats
		}
	}
}

// newMockFile is the internal constructor with full configuration.
// It is called by MockFS.Open() and the public constructors.
//
// Parameters:
//   - mapFile: the fstest.MapFile containing file data and metadata.
//   - name: cleaned path used when checking injection rules.
//   - writeMode: how Write operations modify the file (append/overwrite/readonly).
//   - injector: the error injector to use (may be nil to use a new, empty injector).
//   - latencySimulator: simulator for operation latency (may be nil for no latency).
//   - readDirHandler: handler for ReadDir operations on directories (may be nil).
//   - stats: operation stats recorder. If nil, a new one is created for this file handle.
func newMockFile(
	mapFile *fstest.MapFile,
	name string,
	writeMode writeMode,
	injector ErrorInjector,
	latencySimulator LatencySimulator,
	readDirHandler func(int) ([]fs.DirEntry, error),
	stats StatsRecorder,
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
		stats = NewStatsRecorder(nil)
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

// NewMockFile constructs a MockFile with the given MapFile and options.
// This is the primary constructor if you already have a *fstest.MapFile.
// By default, the file is writable in overwrite mode.
func NewMockFile(mapFile *fstest.MapFile, name string, opts ...MockFileOption) MockFile {
	if mapFile == nil {
		panic("mockfs: mapFile cannot be nil")
	}

	// Set defaults
	options := &fileOptions{
		writeMode: writeModeOverwrite,
		// newMockFile will default the rest
	}

	// Apply options
	for _, opt := range opts {
		opt(options)
	}

	return newMockFile(
		mapFile,
		name,
		options.writeMode,
		options.injector,
		options.latency,
		options.readDirHandler,
		options.stats,
	)
}

// NewMockFileFromBytes creates a writable file from raw data and options.
// The data is copied, so subsequent modifications to the input slice
// will not affect the file's content.
func NewMockFileFromBytes(name string, data []byte, opts ...MockFileOption) MockFile {
	mapFile := &fstest.MapFile{
		Data:    append([]byte(nil), data...), // Copy data
		Mode:    0o644,
		ModTime: time.Now(),
	}

	return NewMockFile(mapFile, name, opts...)
}

// NewMockFileFromString creates a writable file from string content and options.
func NewMockFileFromString(name string, content string, opts ...MockFileOption) MockFile {
	mapFile := &fstest.MapFile{
		Data:    []byte(content),
		Mode:    0o644,
		ModTime: time.Now(),
	}

	return NewMockFile(mapFile, name, opts...)
}

// NewMockDirectory constructs a MockFile representing a directory.
// The readDirHandler is required for ReadDir operations.
// Additional options (like latency) can be passed.
func NewMockDirectory(
	name string,
	readDirHandler func(int) ([]fs.DirEntry, error),
	opts ...MockFileOption,
) MockFile {
	mapFile := &fstest.MapFile{
		Mode:    fs.ModeDir | 0o755,
		ModTime: time.Now(),
	}

	// Prepend mandatory options for a directory
	allOptions := append(
		[]MockFileOption{
			WithFileReadOnly(), // Directories are read-only for Write()
			WithFileReadDirHandler(readDirHandler),
		},
		opts...,
	)

	return NewMockFile(mapFile, name, allOptions...)
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

// Seek implements io.Seeker for MockFile.
// It sets the offset for the next Read or Write operation.
func (f *mockFile) Seek(offset int64, whence int) (n int64, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Record the result of this operation on exit
	defer func() { f.stats.Record(OpSeek, int(n), err) }()

	if f.closed {
		return 0, ErrClosed
	}

	// Simulate latency before checking for errors (models real I/O timing)
	f.latency.Simulate(OpSeek)

	if err = f.injector.CheckAndApply(OpSeek, f.name); err != nil {
		return 0, err
	}

	switch whence {
	case io.SeekStart:
		n = offset
	case io.SeekCurrent:
		n = f.position + offset
	case io.SeekEnd:
		n = int64(len(f.mapFile.Data)) + offset
	default:
		err = &fs.PathError{Op: "Seek", Path: f.name, Err: fs.ErrInvalid}
		return 0, err
	}

	if n < 0 {
		err = &fs.PathError{Op: "Seek", Path: f.name, Err: fs.ErrInvalid}
		return 0, err
	}

	f.position = n
	return n, nil
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
		// No handler provided; this is a standalone, empty directory.
		if n <= 0 {
			return []fs.DirEntry{}, nil
		}
		return []fs.DirEntry{}, io.EOF
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

	return &FileInfo{
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

// Stats returns the operation statistics for this file handle.
// This includes only operations performed on this specific file handle
// (Read, Write, Close, Stat, ReadDir). It does NOT include filesystem-level
// operations like MockFS.Open or MockFS.Stat.
func (f *mockFile) Stats() Stats {
	return f.stats.Snapshot()
}

// LatencySimulator returns the latency simulator for the MockFile.
func (f *mockFile) LatencySimulator() LatencySimulator {
	return f.latency
}

// NewDirHandler creates a stateful fs.ReadDirFile handler from a static list of entries.
// The returned handler correctly implements pagination and returns io.EOF
// when no more entries are available, as required by fs.ReadDirFile.
//
// Example usage:
//
//	entries := []fs.DirEntry{
//	    mockfs.NewMockFileFromBytes("file1.txt", nil).Stat(), // Helper to get a fs.FileInfo
//	    mockfs.NewMockFileFromBytes("file2.txt", nil).Stat(),
//	}
//	handler := mockfs.NewDirHandler(entries)
//	dir := mockfs.NewMockDirectory("my-dir", handler)
func NewDirHandler(entries []fs.DirEntry) func(int) ([]fs.DirEntry, error) {
	var offset int
	return func(n int) ([]fs.DirEntry, error) {
		// Check if we are already at the end
		if offset >= len(entries) {
			// As per fs.ReadDirFile specification,
			// return io.EOF *with* an empty slice if n > 0
			if n > 0 {
				return []fs.DirEntry{}, io.EOF
			}
			return []fs.DirEntry{}, nil
		}

		// Calculate the end of the batch
		end := offset + n
		if n <= 0 { // Read all remaining
			end = len(entries)
		}
		if end > len(entries) {
			end = len(entries)
		}

		batch := entries[offset:end]
		offset = end

		// If n > 0 and we've reached the end, return io.EOF with the last batch
		if n > 0 && offset >= len(entries) {
			return batch, io.EOF
		}

		return batch, nil
	}
}
