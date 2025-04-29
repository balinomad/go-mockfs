package mockfs

import (
	"io"
	"io/fs"
)

// MockFile wraps fs.File to inject errors and track operations.
type MockFile struct {
	file      fs.File
	name      string  // The cleaned name used to open this file (relative to its MockFS)
	mockFS    *MockFS // Reference to the owning MockFS for error checks/stats
	closed    bool
	writeFile io.Writer // Underlying writer if available
}

// Ensure interface implementations
var (
	_ fs.File   = (*MockFile)(nil) // This ensures Stat() is implemented
	_ io.Reader = (*MockFile)(nil)
	_ io.Writer = (*MockFile)(nil) // If underlying file supports it or callback exists
	_ io.Closer = (*MockFile)(nil)
)

// Read implements the io.Reader interface for MockFile.
func (f *MockFile) Read(b []byte) (int, error) {
	if f.closed {
		return 0, fs.ErrClosed
	}
	// Check for error *before* operation and latency
	err := f.mockFS.checkForError(OpRead, f.name) // f.name is already cleaned by Open
	if err != nil {
		return 0, err
	}
	f.mockFS.simulateLatency()
	return f.file.Read(b)
}

// Write implements the io.Writer interface for files that support it
// or if a write callback is configured on the MockFS.
func (f *MockFile) Write(b []byte) (int, error) {
	if f.closed {
		return 0, fs.ErrClosed
	}

	// Check for error *before* operation and latency
	err := f.mockFS.checkForError(OpWrite, f.name) // f.name is already cleaned by Open
	if err != nil {
		return 0, err
	}

	// Check if writing is possible
	canWrite := f.writeFile != nil || (f.mockFS.allowWrites && f.mockFS.writeCallback != nil)
	if !canWrite {
		// Return an error indicating writing is not supported/enabled
		// fs.ErrInvalid is generic; os.Errno could offer EPERM or EBADF if more specific simulation is needed.
		return 0, fs.ErrInvalid
	}

	f.mockFS.simulateLatency()

	// Prefer the direct embedded writer if available
	if f.writeFile != nil {
		return f.writeFile.Write(b)
	}

	// Otherwise, use the MockFS write callback (we know it's non-nil here)
	// The callback is responsible for handling the write logic (e.g., updating map)
	writeErr := f.mockFS.writeCallback(f.name, b) // Use the cleaned path f.name
	if writeErr != nil {
		return 0, writeErr
	}
	// Assuming callback performs a full write or returns an error
	return len(b), nil
}

// Stat implements the fs.File interface's Stat method.
func (f *MockFile) Stat() (fs.FileInfo, error) {
	if f.closed {
		return nil, fs.ErrClosed
	}
	// File.Stat generally doesn't have separate error injection from FS.Stat.
	// It simply returns the info for the underlying file.
	return f.file.Stat()
}

// Close implements the io.Closer interface for MockFile.
func (f *MockFile) Close() error {
	if f.closed {
		return fs.ErrClosed // Already closed
	}
	// Check for injected close error *before* attempting underlying close
	err := f.mockFS.checkForError(OpClose, f.name) // f.name is already cleaned by Open

	// Mark as closed regardless of the error outcome to prevent further operations
	// and ensure idempotency if Close is called again.
	f.closed = true

	if err != nil {
		// Return the injected error; do not attempt to close the underlying file.
		return err
	}

	// No injected error, simulate latency and close the underlying file.
	f.mockFS.simulateLatency()
	return f.file.Close()
}
