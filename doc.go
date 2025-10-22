// Package mockfs provides a mock filesystem implementation for testing, built
// on [testing/fstest.MapFS].
//
// MockFS extends the standard library's mock capabilities with critical
// features for robust testing, including:
//   - Configurable error injection for any filesystem operation and path.
//   - Simulated latency to test timeout and race condition handling.
//   - Operation counters for verifying filesystem access patterns.
//   - Writable filesystem operations (Mkdir, Remove, Rename, WriteFile, etc.).
//   - Full concurrency safety.
//
// # Basic Usage
//
// Create a mock filesystem with initial files:
//
//	mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
//	    "file.txt": {Data: []byte("content"), Mode: 0644},
//	    "dir":      {Mode: fs.ModeDir | 0755},
//	})
//
// Use it like any fs.FS:
//
//	data, err := fs.ReadFile(mfs, "file.txt")
//	entries, err := fs.ReadDir(mfs, "dir")
//
// # Error Injection
//
// Inject errors for specific operations and paths using the simple helpers:
//
//	mfs.FailOpen("file.txt", fs.ErrPermission)            // Always fail to open file.txt
//	mfs.FailReadAfter("data.bin", io.ErrUnexpectedEOF, 3) // Fail after 3 successful reads
//	mfs.FailOpenOnce("config.json", fs.ErrNotExist)       // Fail once, then succeed
//
// Mark files as non-existent for all operations:
//
//	mfs.MarkNonExistent("missing.txt")
//
// # Latency Simulation
//
// Add artificial delays to test timeout handling:
//
//	mfs := mockfs.NewMockFS(data, mockfs.WithLatency(100*time.Millisecond))
//
// Or configure latency for specific operations:
//
//	mfs := mockfs.NewMockFS(data, mockfs.WithPerOperationLatency(map[mockfs.Operation]time.Duration{
//	    mockfs.OpRead:  200 * time.Millisecond,
//	    mockfs.OpWrite: 500 * time.Millisecond,
//	}))
//
// # Operation Counters
//
// Track filesystem operations to verify test behavior:
//
//	stats := mfs.Stats()
//	fmt.Printf("Opens: %d, Reads: %d\n", stats.Count(mockfs.OpOpen), stats.Count(mockfs.OpRead))
//	mfs.ResetStats()
//
// # Write Operations
//
// MockFS implements WritableFS for mutation testing:
//
//	err := mfs.WriteFile("new.txt", []byte("data"), 0644)
//	err = mfs.Mkdir("newdir", 0755)
//	err = mfs.Rename("old.txt", "new.txt")
//	err = mfs.Remove("file.txt")
//
// # Advanced Error Injection
//
// Access the error injector directly for complex scenarios:
//
//	injector := mfs.ErrorInjector()
//	injector.AddPattern(mockfs.OpOpen, `.*\.tmp$`, fs.ErrPermission, mockfs.ErrorModeAlways, 0)
//	injector.AddExact(mockfs.OpRead, "flaky.txt", io.EOF, mockfs.ErrorModeAfterSuccesses, 5)
//
// # Testing Philosophy
//
// MockFS is designed to expose bugs, not hide them:
//   - Closing a file twice returns fs.ErrClosed (helps detect double-close bugs).
//   - Invalid paths in AddFile/AddDir return errors (no silent failures).
//   - All operations are counted, including failed ones (verify actual behavior).
//   - Error injection takes precedence (allows overriding natural filesystem state).
//
// This strict behavior helps tests catch real-world bugs that might otherwise be hidden.
//
// # Limitations
//
//   - Symlinks are not supported (mode can be set, but not followed).
//   - Path cleaning uses lexical processing only (no filesystem queries).
//   - Operations on open files may succeed even if the file is removed from the filesystem
//     (matching real filesystem behavior).
//   - This package is not optimized for large filesystems.
//
// # Concurrency
//
// MockFS is safe for concurrent use. Multiple goroutines can:
//   - Read from different files simultaneously
//   - Perform operations on the same filesystem
//   - Modify the filesystem structure (add/remove files)
//
// Note: Like real filesystems, concurrent modifications and reads may produce
// non-deterministic ordering. Use synchronization in tests if order matters.
package mockfs
