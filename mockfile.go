package mockfs

import (
	"io"
	"io/fs"
	"sync"
)

// MockFile wraps fs.File to inject errors and track operations.
type MockFile struct {
	file          fs.File                       // The underlying fs.File.
	name          string                        // Cleaned name used to open this file (relative to its MockFS).
	mu            sync.Mutex                    // Protects closed flag and serializes access to underlying file.
	closed        bool                          // Tracks if the file has been closed.
	writeHandler  func([]byte) (int, error)     // Write handler if configured.
	checkError    func(Operation, string) error // Check for configured errors.
	simulateDelay func()                        // Simulate latency.
	inc           func(Operation)               // Operation counter incrementer.
}

// Ensure interface implementations.
var (
	_ fs.File        = (*MockFile)(nil)
	_ fs.ReadDirFile = (*MockFile)(nil)
	_ io.Reader      = (*MockFile)(nil)
	_ io.Writer      = (*MockFile)(nil)
	_ io.Closer      = (*MockFile)(nil)
)

// NewMockFile constructs a MockFile.
//
// Parameters:
//   - underlyingFile: the fs.File returned by the underlying fs implementation.
//   - name: cleaned path used when checking injection rules.
//   - errorChecker: callback to check and apply configured errors (may be nil).
//   - delaySimulator: callback to simulate latency (may be nil).
//   - inc: function called with the Operation (may be nil).
//   - writeHandler: optional write handler; if nil and underlyingFile implements io.Writer,
//     that implementation is used.
func NewMockFile(
	underlyingFile fs.File,
	name string,
	errorChecker func(Operation, string) error,
	delaySimulator func(),
	inc func(Operation),
	writeHandler func([]byte) (int, error),
) *MockFile {
	// Default no-op callbacks
	if errorChecker == nil {
		errorChecker = func(op Operation, path string) error { return nil }
	}
	if delaySimulator == nil {
		delaySimulator = func() {}
	}
	if inc == nil {
		inc = func(Operation) {}
	}

	f := &MockFile{
		file:          underlyingFile,
		name:          name,
		checkError:    errorChecker,
		simulateDelay: delaySimulator,
		inc:           inc,
	}

	// Set write handler: prefer explicit handler, fallback to underlying Writer
	if writeHandler != nil {
		f.writeHandler = writeHandler
	} else if wf, ok := underlyingFile.(io.Writer); ok {
		f.writeHandler = wf.Write
	}

	return f
}

// Read implements io.Reader for MockFile.
func (f *MockFile) Read(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, fs.ErrClosed
	}

	f.inc(OpRead)

	if err := f.checkError(OpRead, f.name); err != nil {
		return 0, err
	}

	f.simulateDelay()
	return f.file.Read(b)
}

// Write implements io.Writer for MockFile.
func (f *MockFile) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return 0, fs.ErrClosed
	}

	if f.writeHandler == nil {
		return 0, &fs.PathError{Op: "write", Path: f.name, Err: fs.ErrInvalid}
	}

	f.inc(OpWrite)

	if err := f.checkError(OpWrite, f.name); err != nil {
		return 0, err
	}

	f.simulateDelay()
	return f.writeHandler(b)
}

// ReadDir reads the contents of the directory and returns
// a slice of up to n DirEntry values in directory order.
// Subsequent calls on the same file will yield further DirEntry values.
// It implements fs.ReadDirFile for MockFile.
func (f *MockFile) ReadDir(n int) ([]fs.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil, fs.ErrClosed
	}

	// Check if underlying file implements ReadDirFile
	rdf, ok := f.file.(fs.ReadDirFile)
	if !ok {
		return nil, &fs.PathError{Op: "ReadDir", Path: f.name, Err: fs.ErrInvalid}
	}

	f.inc(OpReadDir)

	if err := f.checkError(OpReadDir, f.name); err != nil {
		return nil, err
	}

	f.simulateDelay()
	return rdf.ReadDir(n)
}

// Stat implements fs.File.Stat.
func (f *MockFile) Stat() (fs.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil, fs.ErrClosed
	}

	// No file-level error injection for Stat here (FS.Stat covers lookup errors)
	return f.file.Stat()
}

// Close implements io.Closer for MockFile.
//
// Close returns fs.ErrClosed if called multiple times, allowing tests to detect
// double-close bugs. If an injected close error is configured, the underlying file
// is still closed to avoid resource leaks.
func (f *MockFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return fs.ErrClosed
	}

	f.inc(OpClose)

	// Check for injected close error first
	if err := f.checkError(OpClose, f.name); err != nil {
		// Attempt to close underlying file to avoid resource leaks
		_ = f.file.Close()
		f.closed = true
		return err
	}

	// No injected error, simulate latency and close the underlying file
	f.simulateDelay()
	err := f.file.Close()
	f.closed = true
	return err
}
