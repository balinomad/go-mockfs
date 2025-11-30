// Package mockfs provides a mock filesystem implementation for testing, built
// on [testing/fstest.MapFS].
//
// MockFS extends the standard library's mock capabilities with critical
// features for robust testing, including:
//   - Configurable error injection for any filesystem operation and path.
//   - Simulated latency to test timeout and race condition handling.
//   - Operation counters for verifying filesystem access patterns.
//   - Comprehensive [io/fs] interface implementation: fs.FS, fs.ReadDirFS, fs.ReadFileFS, fs.StatFS, fs.SubFS
//   - Writable filesystem operations (Mkdir, Remove, Rename, WriteFile, etc.).
//   - Full concurrency safety.
//
// # Target Audience
//
// This library is designed for both experienced Go developers and those new to
// filesystem testing. It follows Go conventions while providing comprehensive
// features for testing complex I/O scenarios.
//
// # Basic Usage
//
// Create a mock filesystem with initial files:
//
//	mfs := mockfs.NewMockFS(
//	    mockfs.File("file.txt", "content"),
//	    mockfs.Dir("dir",
//	        mockfs.File("file1.txt", "1"),
//	        mockfs.File("file2.txt", "2"),
//	    ),
//	)
//
// Use it like any fs.FS:
//
//	data, err := fs.ReadFile(mfs, "file.txt")
//	entries, err := fs.ReadDir(mfs, "dir")
//
// # Statistics: Filesystem vs File-Handle Operations
//
// MockFS tracks operations at two distinct levels:
//
// Filesystem-level operations (MockFS.Stats()): Operations on the filesystem
// structure itself - Open, Stat, ReadDir, Mkdir, Remove, Rename, WriteFile.
//
// File-handle operations (MockFile.Stats()): Operations on individual open
// files - Read, Write, Close, Seek, ReadDir (on directory handles).
//
// This separation enables precise verification of I/O patterns:
//
//	mfs := mockfs.NewMockFS(mockfs.File("file.txt", "content"))
//	file, _ := mfs.Open("file.txt")   // Tracked in MockFS.Stats()
//	buf := make([]byte, 100)
//	file.Read(buf)                    // Tracked in MockFile.Stats()
//	file.Close()                      // Tracked in MockFile.Stats()
//
//	fsStats := mfs.Stats()
//	fsStats.Count(mockfs.OpOpen)      // 1
//
//	fileStats := file.(mockfs.MockFile).Stats()
//	fileStats.Count(mockfs.OpRead)    // 1
//	fileStats.Count(mockfs.OpClose)   // 1
//	fileStats.BytesRead()             // 7
//
// # Error Injection
//
// Inject errors to simulate I/O failures. Use convenience methods for common cases:
//
//	// Always fail specific operations
//	mfs.FailOpen("bad.txt", mockfs.ErrPermission)
//	mfs.FailRead("data.txt", io.EOF)
//
//	// Fail once then succeed
//	mfs.FailOpenOnce("flaky.db", mockfs.ErrTimeout)
//
//	// Fail after N successes
//	mfs.FailReadAfter("stream.bin", io.EOF, 5)
//
// For advanced scenarios, use the ErrorInjector interface directly:
//
//	injector := mfs.ErrorInjector()
//
//	// Glob patterns (uses path.Match semantics)
//	injector.AddGlob(mockfs.OpRead, "*.log", io.EOF, mockfs.ErrorModeAlways, 0)
//
//	// Regular expressions
//	injector.AddRegexp(mockfs.OpRead, `\.tmp$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
//
//	// All paths for an operation
//	injector.AddAll(mockfs.OpWrite, mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)
//
//	// All operations for a path
//	injector.AddExactForAllOps("critical.dat", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
//
// Error modes control when errors are returned:
//   - ErrorModeAlways: Error returned on every matching operation
//   - ErrorModeOnce: Error returned once, then rule becomes inactive
//   - ErrorModeAfterSuccesses: Error returned after N successful operations
//   - ErrorModeNext: Error returned next N times, then rule becomes inactive
//
// # Latency Simulation
//
// Add artificial delays to test timeout handling:
//
//	// Global latency for all operations
//	mfs := mockfs.NewMockFS(mockfs.WithLatency(100*time.Millisecond))
//
//	// Per-operation latency
//	mfs = mockfs.NewMockFS(mockfs.WithPerOperationLatency(
//	    map[mockfs.Operation]time.Duration{
//	        mockfs.OpRead:  200 * time.Millisecond,
//	        mockfs.OpWrite: 500 * time.Millisecond,
//	    },
//	))
//
// Latency simulation options:
//   - Once(): Apply latency only on first call per operation type
//   - Async(): Non-blocking (releases lock before sleeping)
//   - OnceAsync(): Combines both Once and Async
//
// Each opened file gets an independent latency simulator (cloned from the
// filesystem's simulator), ensuring file handles have independent Once() state.
//
// # Write Operations
//
// MockFS implements the WritableFS interface for full filesystem mutation:
//
//	mfs := mockfs.NewMockFS(mockfs.WithOverwrite())
//	mfs.WriteFile("output.txt", data, 0o644)
//	mfs.Mkdir("logs", 0o755)
//	mfs.MkdirAll("app/config/prod", 0o755)
//	mfs.Remove("temp.txt")
//	mfs.RemoveAll("cache")
//	mfs.Rename("old.txt", "new.txt")
//
// Write modes:
//   - WithOverwrite(): Replace existing content (default)
//   - WithAppend(): Append to existing content
//   - WithReadOnly(): Disable all writes
//   - WithCreateIfMissing(true): Create files if they don't exist
//
// # Standalone File Testing
//
// Create MockFile instances without a filesystem for testing functions that
// accept io.Reader, io.Writer, or io.Seeker:
//
//	// From string
//	file := mockfs.NewMockFileFromString("test.txt", "content")
//
//	// From bytes
//	file = mockfs.NewMockFileFromBytes("data.bin", []byte{0x01, 0x02})
//
//	// With options
//	file = mockfs.NewMockFileFromString("test.txt", "content",
//	    mockfs.WithFileLatency(10*time.Millisecond),
//	    mockfs.WithFileReadOnly(),
//	)
//
//	// Test your function
//	result := YourReaderFunction(file)
//
//	// Verify statistics
//	stats := file.Stats()
//	if stats.BytesRead() == 0 {
//	    t.Error("expected bytes to be read")
//	}
//
// # SubFS Support
//
// Full fs.SubFS implementation with automatic path adjustment for error rules:
//
//	mfs := mockfs.NewMockFS(
//	    mockfs.Dir("app",
//	        mockfs.Dir("config",
//	            mockfs.File("dev.json", "{}"),
//	            mockfs.File("prod.json", "{}"),
//	        ),
//	    ),
//	)
//
//	// Configure error in parent
//	mfs.ErrorInjector().AddGlob(mockfs.OpRead, "app/config/*.json", io.EOF, mockfs.ErrorModeAlways, 0)
//
//	// Create sub-filesystem
//	subFS, _ := mfs.Sub("app/config")
//
//	// Error rules automatically adjusted: "app/config/*.json" → "*.json"
//	_, err := fs.ReadFile(subFS, "dev.json") // io.EOF injected
//
// # Statistics Tracking
//
// Track filesystem operations to verify test behavior:
//
//	stats := mfs.Stats()
//	fmt.Printf("Opens: %d, Reads: %d\n", stats.Count(mockfs.OpOpen), stats.Count(mockfs.OpRead))
//
//	// Success/failure breakdown
//	successes := stats.CountSuccess(mockfs.OpRead)
//	failures := stats.CountFailure(mockfs.OpRead)
//
//	// Byte counters
//	bytesRead := stats.BytesRead()
//	bytesWritten := stats.BytesWritten()
//
//	// Compare snapshots
//	before := mfs.Stats()
//	// ... perform operations ...
//	after := mfs.Stats()
//	delta := after.Delta(before)
//
//	// Fluent assertions in tests
//	mfs.Stats().Expect().
//	    Count(mockfs.OpOpen, 1).
//	    Success(mockfs.OpRead, 5).
//	    NoFailures().
//	    Assert(t)
//
//	// Reset counters
//	mfs.ResetStats()
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
//   - File permissions (MapFile.Mode) are metadata only and not enforced.
//     Use ErrorInjector to simulate permission errors explicitly.
//   - Path cleaning uses lexical processing only (no filesystem queries).
//   - Operations on open files may succeed even if the file is removed from the filesystem
//     (matching real filesystem behavior).
//   - This package is not optimized for large filesystems.
//   - ReadFile on a directory returns empty data without error (matches MapFS behaviour).
//     Use Stat or Open+ReadDir to distinguish directories from empty files.
//
// # Concurrency
//
// MockFS is safe for concurrent use. Multiple goroutines can:
//   - Read from different files simultaneously
//   - Perform operations on the same filesystem
//   - Modify the filesystem structure (add/remove files)
//
// Each file handle clones the latency simulator on Open(), ensuring independent
// Once() state even when multiple files are opened concurrently.
//
// Note: Like real filesystems, concurrent modifications and reads may produce
// non-deterministic ordering. Use synchronization in tests if order matters.
//
// # Advanced Usage
//
// For advanced patterns including:
//   - Testing error recovery with retries
//   - Testing timeout handling
//   - Testing concurrent access
//   - Dependency injection patterns
//   - Performance testing strategies
//
// See the USAGE.md file in the repository: https://github.com/balinomad/go-mockfs/blob/v2/USAGE.md
//
// # Migration from v1
//
// If upgrading from v1, see MIGRATION-v1-to-v2.md for a comprehensive migration guide:
// https://github.com/balinomad/go-mockfs/blob/v2/MIGRATION-v1-to-v2.md
package mockfs
