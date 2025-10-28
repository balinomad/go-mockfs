[![GoDoc](https://pkg.go.dev/badge/github.com/balinomad/go-mockfs?status.svg)](https://pkg.go.dev/github.com/balinomad/go-mockfs?tab=doc)
[![GoMod](https://img.shields.io/github/go-mod/go-version/balinomad/go-mockfs)](https://github.com/balinomad/go-mockfs)
[![Size](https://img.shields.io/github/languages/code-size/balinomad/go-mockfs)](https://github.com/balinomad/go-mockfs)
[![License](https://img.shields.io/github/license/balinomad/go-mockfs)](./LICENSE)
[![Go](https://github.com/balinomad/go-mockfs/actions/workflows/go.yml/badge.svg)](https://github.com/balinomad/go-mockfs/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/balinomad/go-mockfs)](https://goreportcard.com/report/github.com/balinomad/go-mockfs)
[![codecov](https://codecov.io/github/balinomad/go-mockfs/graph/badge.svg?token=L1K68IIN51)](https://codecov.io/github/balinomad/go-mockfs)

# mockfs

*A flexible and feature-rich filesystem mocking library for Go, built on `testing/fstest.MapFS` with comprehensive error injection, latency simulation, and write operation support.*

## Table of Contents

1. [Overview](#overview)
2. [Key Features](#key-features)
3. [Installation](#installation)
4. [Quick Start](#quick-start)
5. [Core Concepts](#core-concepts)
6. [Usage Guide](#usage-guide)
7. [Advanced Patterns](#advanced-patterns)
8. [API Reference](#api-reference)
9. [Migrations](#migrations)
10. [License](#license)

## 1. Overview<a id="overview"></a>

`mockfs` enables robust testing of filesystem-dependent code by providing a complete in-memory filesystem implementation with precise control over behavior, errors, and performance characteristics. Built around Go's standard `fs` interfaces, it integrates seamlessly with existing code while adding powerful testing capabilities.

## 2. Key Features<a id="key-features"></a>

- **Complete fs interface implementation**: `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.SubFS`
- **Writable filesystem operations**: `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `WriteFile`
- **Advanced error injection**: Exact path matching, glob patterns, regex patterns, per-operation or cross-operation rules
- **Flexible error modes**: Always, once, or after N successful operations
- **Latency simulation**: Global, per-operation, serialized, or async with independent file-handle state
- **Dual statistics tracking**: Separate counters for filesystem-level vs file-handle-level operations
- **Standalone file mocking**: Create `MockFile` instances without a filesystem for testing `io.Reader`/`io.Writer` functions
- **SubFS support**: Full sub-filesystem implementation with automatic error rule adjustment
- **Concurrency-safe**: All operations safe for concurrent use

## 3. Installation<a id="installation"></a>

```bash
go get github.com/balinomad/go-mockfs@latest
```

## 4. Quick Start<a id="quick-start"></a>

```go
package main_test

import (
    "io/fs"
    "testing"
    "time"

    "github.com/balinomad/go-mockfs"
)

func TestBasicUsage(t *testing.T) {
    // Create filesystem with initial files
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "config.json": {
            Data:    []byte(`{"setting": "value"}`),
            Mode:    0644,
            ModTime: time.Now(),
        },
        "data": {
            Mode:    fs.ModeDir | 0755,
            ModTime: time.Now(),
        },
    })

    // Read file
    data, err := fs.ReadFile(mfs, "config.json")
    if err != nil {
        t.Fatal(err)
    }

    // Check statistics
    stats := mfs.Stats()
    if stats.Count(mockfs.OpOpen) != 1 {
        t.Errorf("expected 1 open, got %d", stats.Count(mockfs.OpOpen))
    }
}
```

## 5. Core Concepts<a id="core-concepts"></a>

### 5.1. Statistics: Filesystem vs File-Handle Operations

`mockfs` tracks operations at two levels:

- **Filesystem-level operations** (`MockFS.Stats()`): `Open`, `Stat`, `ReadDir`, `Mkdir`, `Remove`, `Rename`, etc. — operations on the filesystem structure itself
- **File-handle operations** (`MockFile.Stats()`): `Read`, `Write`, `Close` — operations on individual open files

This separation enables precise verification of I/O patterns. Example:
```go
mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "file.txt": {Data: []byte("content"), Mode: 0644},
})

file, _ := mfs.Open("file.txt")  // Tracked in MockFS.Stats()
buf := make([]byte, 100)
file.Read(buf)                    // Tracked in MockFile.Stats()
file.Close()                      // Tracked in MockFile.Stats()

fsStats := mfs.Stats()
fsStats.Count(mockfs.OpOpen)      // 1

fileStats := file.(mockfs.MockFile).Stats()
fileStats.Count(mockfs.OpRead)    // 1
fileStats.Count(mockfs.OpClose)   // 1
fileStats.BytesRead()             // 7
```

### 5.2.Error Injection via ErrorInjector

Error injection is managed through the `ErrorInjector` interface, which supports:

- **Path matching strategies**: Exact (`AddExact`), glob (`AddGlob`), regex (`AddRegexp`), wildcard (`AddAll`)
- **Error modes**: `ErrorModeAlways`, `ErrorModeOnce`, `ErrorModeAfterSuccesses`
- **Scope**: Per-operation or cross-operation rules

Access via `mfs.ErrorInjector()` for advanced configuration, or use convenience methods (`FailOpen`, `FailRead`, etc.) for common cases.

### 5.3. Operations

`Operation` constants identify filesystem operations for error injection and statistics:

- `OpStat`, `OpOpen`, `OpRead`, `OpWrite`, `OpClose`, `OpReadDir`
- `OpMkdir`, `OpMkdirAll`, `OpRemove`, `OpRemoveAll`, `OpRename`

### 5.4. Path Matchers

`PathMatcher` interface enables flexible path matching:

- `ExactMatcher`: Single path
- `GlobMatcher`: Glob patterns (`*.txt`, `logs/*.log`)
- `RegexpMatcher`: Regular expressions
- `WildcardMatcher`: Matches all paths

Matchers automatically adjust for `SubFS` operations.

### 5.5.Latency Simulation

`LatencySimulator` interface simulates I/O latency with options:

- **Global or per-operation durations**
- **Once mode**: Apply latency only on first call per operation type
- **Async mode**: Non-blocking (releases lock before sleeping)
- **Independent file-handle state**: Each opened file gets a cloned simulator

### 5.6. WritableFS Interface

`WritableFS` extends `fs.FS` with write operations:
```go
type WritableFS interface {
    fs.FS
    Mkdir(path string, perm fs.FileMode) error
    MkdirAll(path string, perm fs.FileMode) error
    Remove(path string) error
    RemoveAll(path string) error
    Rename(oldpath, newpath string) error
    WriteFile(path string, data []byte, perm fs.FileMode) error
}
```

`MockFS` implements `WritableFS` for full filesystem mutation support.

## 6. Usage Guide<a id="usage-guide"></a>

### 6.1. Creating a Mock Filesystem

```go
// Empty filesystem
mfs := mockfs.NewMockFS(nil)

// With initial files
mfs = mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "file.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
    "dir":      {Mode: fs.ModeDir | 0755, ModTime: time.Now()},
})

// With options
mfs = mockfs.NewMockFS(nil,
    mockfs.WithLatency(10*time.Millisecond),
    mockfs.WithCreateIfMissing(true),
    mockfs.WithOverwrite(),
)
```

### 6.2. Adding and Removing Files
```go
// Add text file
err := mfs.AddFile("config.json", `{"key": "value"}`, 0644)

// Add binary file
err = mfs.AddFileBytes("data.bin", []byte{0x01, 0x02}, 0644)

// Add directory
err = mfs.AddDir("logs", 0755)

// Remove path
err = mfs.RemovePath("temp.txt")
```

### 6.3. Error Injection: Simple Cases
```go
// Always fail on specific operations
mfs.FailOpen("secret.txt", fs.ErrPermission)
mfs.FailRead("data.txt", io.ErrUnexpectedEOF)
mfs.FailStat("config.json", fs.ErrNotExist)

// One-time errors
mfs.FailOpenOnce("flaky.db", mockfs.ErrTimeout)
mfs.FailReadOnce("network.log", io.EOF)

// Error after N successes
mfs.FailReadAfter("large.bin", io.EOF, 3)
```

### 6.4. Error Injection: Pattern Matching
```go
// Glob patterns
mfs.ErrorInjector().AddGlob(mockfs.OpRead, "*.log", io.EOF, mockfs.ErrorModeAlways, 0)
mfs.ErrorInjector().AddGlob(mockfs.OpOpen, "temp/*", fs.ErrNotExist, mockfs.ErrorModeAlways, 0)

// Regular expressions
mfs.ErrorInjector().AddRegexp(mockfs.OpRead, `\.tmp$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)

// All paths for an operation
mfs.ErrorInjector().AddAll(mockfs.OpWrite, mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

// All operations for a path
mfs.ErrorInjector().AddExactForAllOps("unstable.dat", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)

// All operations for all paths
mfs.ErrorInjector().AddAllForAllOps(mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0)
```

### 6.5. Statistics Tracking
```go
// Filesystem-level statistics
stats := mfs.Stats()
stats.Count(mockfs.OpOpen)          // Total opens
stats.CountSuccess(mockfs.OpOpen)   // Successful opens
stats.CountFailure(mockfs.OpOpen)   // Failed opens
stats.Operations()                   // Total operations across all types
stats.HasFailures()                  // Any failures?

// File-handle statistics
file, _ := mfs.Open("file.txt")
buf := make([]byte, 100)
file.Read(buf)

mockFile := file.(mockfs.MockFile)
fileStats := mockFile.Stats()
fileStats.Count(mockfs.OpRead)      // Reads on this handle
fileStats.BytesRead()                // Bytes read via this handle
fileStats.BytesWritten()             // Bytes written via this handle

// Compare statistics
before := mfs.Stats()
// ... perform operations ...
after := mfs.Stats()
delta := after.Delta(before)         // Difference
if !after.Equal(before) {
    // Stats changed
}

// Reset
mfs.ResetStats()
```

### 6.6. Latency Simulation
```go
// Global latency for all operations
mfs := mockfs.NewMockFS(nil, mockfs.WithLatency(50*time.Millisecond))

// Per-operation latency
mfs = mockfs.NewMockFS(nil, mockfs.WithPerOperationLatency(
    map[mockfs.Operation]time.Duration{
        mockfs.OpRead:  100 * time.Millisecond,
        mockfs.OpWrite: 200 * time.Millisecond,
        mockfs.OpStat:  10 * time.Millisecond,
    },
))

// Custom simulator with options
sim := mockfs.NewLatencySimulator(50 * time.Millisecond)
sim.Simulate(mockfs.OpRead, mockfs.Once())      // Latency only on first read
sim.Simulate(mockfs.OpWrite, mockfs.Async())    // Non-blocking

mfs = mockfs.NewMockFS(nil, mockfs.WithLatencySimulator(sim))
```

### 6.7. Write Operations

```go
// Enable writes
mfs := mockfs.NewMockFS(nil, mockfs.WithOverwrite())

// Write file
err := mfs.WriteFile("output.txt", []byte("data"), 0644)

// Create if missing
mfs = mockfs.NewMockFS(nil,
    mockfs.WithOverwrite(),
    mockfs.WithCreateIfMissing(true),
)
err = mfs.WriteFile("new.txt", []byte("content"), 0644)

// Append mode
mfs = mockfs.NewMockFS(nil, mockfs.WithAppend())
mfs.WriteFile("log.txt", []byte("line1\n"), 0644)
mfs.WriteFile("log.txt", []byte("line2\n"), 0644) // Appends

// Directory operations
err = mfs.Mkdir("logs", 0755)
err = mfs.MkdirAll("app/config/prod", 0755)
err = mfs.Remove("temp.txt")
err = mfs.RemoveAll("cache")
err = mfs.Rename("old.txt", "new.txt")

// Write via file handle
file, _ := mfs.Open("file.txt")
file.(io.Writer).Write([]byte("data"))
file.Close()
```

### 6.8. Standalone MockFile

```go
// Create file from string
file := mockfs.NewMockFileFromString("test.txt", "content")

// Create file from bytes
file = mockfs.NewMockFileFromBytes("data.bin", []byte{0x01, 0x02})

// With options
file = mockfs.NewMockFileFromString("test.txt", "content",
    mockfs.WithFileLatency(10*time.Millisecond),
    mockfs.WithFileReadOnly(),
)

// Create directory
entries := []fs.DirEntry{ /* ... */ }
handler := mockfs.NewDirHandler(entries)
dir := mockfs.NewMockDirectory("mydir", handler)

// Test functions accepting io.Reader/io.Writer
func ProcessReader(r io.Reader) error { /* ... */ }
err := ProcessReader(file)
```

### 6.9. SubFS Support

```go
mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "app/config/dev.json":  {Data: []byte("{}"), Mode: 0644},
    "app/config/prod.json": {Data: []byte("{}"), Mode: 0644},
    "app/logs/app.log":     {Data: []byte(""), Mode: 0644},
})

// Configure error in parent
mfs.ErrorInjector().AddGlob(mockfs.OpRead, "app/config/*.json", io.EOF, mockfs.ErrorModeAlways, 0)

// Create sub-filesystem
subFS, err := mfs.Sub("app/config")

// Error rules automatically adjusted
// "app/config/*.json" becomes "*.json" in subFS
data, err := fs.ReadFile(subFS, "dev.json") // Error injected

// Sub-filesystem has independent stats
subMockFS := subFS.(*mockfs.MockFS)
stats := subMockFS.Stats()
```

# 7. Advanced Patterns<a id="advanced-patterns"></a>

### 7.1. Testing Error Recovery with Retries

```go
func TestRetryLogic(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "data.txt": {Data: []byte("content"), Mode: 0644},
    })

    // First two reads fail, third succeeds
    mfs.FailReadAfter("data.txt", io.ErrUnexpectedEOF, 0)
    mfs.FailReadAfter("data.txt", io.ErrUnexpectedEOF, 0)

    // Function under test should retry
    result, err := YourRetryFunction(mfs, "data.txt")
    if err != nil {
        t.Fatalf("function should retry and succeed: %v", err)
    }

    // Verify retry behavior
    file, _ := mfs.Open("data.txt")
    mockFile := file.(mockfs.MockFile)
    stats := mockFile.Stats()
    if stats.Count(mockfs.OpRead) < 3 {
        t.Errorf("expected at least 3 read attempts, got %d", stats.Count(mockfs.OpRead))
    }
}
```

### 7.2. Testing Timeout Handling

```go
func TestTimeoutBehavior(t *testing.T) {
    // Simulate slow I/O
    mfs := mockfs.NewMockFS(nil, mockfs.WithLatency(2*time.Second))

    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    // Function should respect context timeout
    err := YourFunctionWithContext(ctx, mfs)
    if !errors.Is(err, context.DeadlineExceeded) {
        t.Errorf("expected timeout, got %v", err)
    }
}
```

### 7.3. Testing Concurrent Access

```go
func TestConcurrentReads(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "shared.txt": {Data: bytes.Repeat([]byte("data"), 1000), Mode: 0644},
    })

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            data, err := fs.ReadFile(mfs, "shared.txt")
            if err != nil {
                t.Errorf("concurrent read failed: %v", err)
            }
            if len(data) != 4000 {
                t.Errorf("expected 4000 bytes, got %d", len(data))
            }
        }()
    }
    wg.Wait()

    // Verify all reads counted
    stats := mfs.Stats()
    if stats.Count(mockfs.OpOpen) != 10 {
        t.Errorf("expected 10 opens, got %d", stats.Count(mockfs.OpOpen))
    }
}
```

### 7.4. Dependency Injection for os Package Functions

When testing code that uses `os` package functions directly, use dependency injection:
```go
// Define abstraction
type FileSystem interface {
    MkdirAll(path string, perm fs.FileMode) error
    WriteFile(path string, data []byte, perm fs.FileMode) error
    ReadFile(path string) ([]byte, error)
}

// Production implementation
type OSFileSystem struct{}

func (OSFileSystem) MkdirAll(path string, perm fs.FileMode) error {
    return os.MkdirAll(path, perm)
}

func (OSFileSystem) WriteFile(path string, data []byte, perm fs.FileMode) error {
    return os.WriteFile(path, data, perm)
}

func (OSFileSystem) ReadFile(path string) ([]byte, error) {
    return os.ReadFile(path)
}

// Test implementation
type MockFileSystem struct {
    *mockfs.MockFS
}

func (m MockFileSystem) ReadFile(path string) ([]byte, error) {
    return fs.ReadFile(m.MockFS, path)
}

// Usage in production
func NewService() *Service {
    return &Service{fs: OSFileSystem{}}
}

// Usage in tests
func TestService(t *testing.T) {
    mfs := mockfs.NewMockFS(nil, mockfs.WithCreateIfMissing(true))
    svc := &Service{fs: MockFileSystem{mfs}}
    // Test with full error injection and statistics
}
```
<
## 8. API Reference<a id="api-reference"></a>

### 8.1. Constructors and Options

| Function/Option | Description |
|-----------------|-------------|
| `NewMockFS(initial, ...opts)` | Creates a new mock filesystem |
| `WithLatency(duration)` | Sets uniform latency for all operations |
| `WithPerOperationLatency(map)` | Sets per-operation latency |
| `WithLatencySimulator(sim)` | Sets custom latency simulator |
| `WithErrorInjector(injector)` | Sets custom error injector |
| `WithCreateIfMissing(bool)` | Enables creating files on write if missing |
| `WithOverwrite()` | Sets write mode to overwrite (default) |
| `WithAppend()` | Sets write mode to append |
| `WithReadOnly()` | Disables write operations |

### 8.2. Common Error Injection Methods

| Method | Description |
|--------|-------------|
| `FailStat(path, err)` | Stat always fails |
| `FailOpen(path, err)` | Open always fails |
| `FailRead(path, err)` | Read always fails |
| `FailWrite(path, err)` | Write always fails |
| `FailClose(path, err)` | Close always fails |
| `FailReadDir(path, err)` | ReadDir always fails |
| `FailStatOnce(path, err)` | Stat fails once |
| `FailOpenOnce(path, err)` | Open fails once |
| `FailReadOnce(path, err)` | Read fails once |
| `FailReadAfter(path, err, n)` | Read fails after n successes |
| `MarkNonExistent(paths...)` | Marks paths as non-existent |
| `ClearErrors()` | Removes all error rules |

For advanced error injection (glob patterns, regex, cross-operation rules), use `ErrorInjector()` to access the full `ErrorInjector` interface. See [GoDoc](https://pkg.go.dev/github.com/balinomad/go-mockfs/v2) for complete API reference.

### 8.3. Error Modes

| Mode | Description |
|------|-------------|
| `ErrorModeAlways` | Error returned on every matching operation |
| `ErrorModeOnce` | Error returned once, then cleared |
| `ErrorModeAfterSuccesses` | Error returned after N successful operations |

### 8.4. Predefined Errors

| Error | Description |
|-------|-------------|
| `ErrInvalid` | Invalid argument (from `fs.ErrInvalid`) |
| `ErrPermission` | Permission denied (from `fs.ErrPermission`) |
| `ErrExist` | File already exists (from `fs.ErrExist`) |
| `ErrNotExist` | File does not exist (from `fs.ErrNotExist`) |
| `ErrClosed` | File already closed (from `fs.ErrClosed`) |
| `ErrDiskFull` | Disk full |
| `ErrTimeout` | Operation timeout |
| `ErrCorrupted` | Corrupted data |
| `ErrTooManyHandles` | Too many open files |
| `ErrNotDir` | Not a directory |
| `ErrNotEmpty` | Directory not empty |

### 8.5. File and Directory Management

| Method | Description |
|--------|-------------|
| `AddFile(path, content, mode)` | Adds text file |
| `AddFileBytes(path, data, mode)` | Adds binary file |
| `AddDir(path, mode)` | Adds directory |
| `RemovePath(path)` | Removes file or directory |
| `Stats()` | Returns filesystem operation statistics |
| `ResetStats()` | Resets statistics counters |

### 8.6. Statistics Interface

| Method | Description |
|--------|-------------|
| `Count(op)` | Total calls for operation |
| `CountSuccess(op)` | Successful calls |
| `CountFailure(op)` | Failed calls |
| `BytesRead()` | Total bytes read |
| `BytesWritten()` | Total bytes written |
| `Operations()` | Total operations across all types |
| `HasFailures()` | Whether any operation failed |
| `Failures()` | Operations with failures |
| `Delta(other)` | Difference between stats |
| `Equal(other)` | Whether stats are equal |
| `String()` | Human-readable summary |

### 8.7. WritableFS Operations

| Method | Description |
|--------|-------------|
| `Mkdir(path, perm)` | Creates directory |
| `MkdirAll(path, perm)` | Creates directory and parents |
| `Remove(path)` | Removes file or empty directory |
| `RemoveAll(path)` | Removes path and children recursively |
| `Rename(old, new)` | Renames/moves file or directory |
| `WriteFile(path, data, perm)` | Writes file |

### 8.8. Standalone MockFile Constructors

| Function | Description |
|----------|-------------|
| `NewMockFile(mapFile, name, ...opts)` | Creates file from MapFile |
| `NewMockFileFromBytes(name, data, ...opts)` | Creates file from bytes |
| `NewMockFileFromString(name, content, ...opts)` | Creates file from string |
| `NewMockDirectory(name, handler, ...opts)` | Creates directory |
| `NewDirHandler(entries)` | Creates ReadDir handler from entries |

## 9. Migrations<a id="migrations"></a>

## 9.1. Migration from *v1* to *v2*

**mockfs *v2* a major rewrite with breaking changes.**

Key differences:

- **Statistics**: `GetStats()` → `Stats()` (interface with methods, not struct fields)
- **Error injection**: `AddStatError()` → `FailStat()`, advanced features via `ErrorInjector()`
- **File management**: `AddFileString()` → `AddFile()`, `AddDirectory()` → `AddDir()`
- **Write support**: `WithWritesEnabled()` → `WithOverwrite()`/`WithAppend()`
- **Operation tracking**: Filesystem-level vs file-handle-level split
- **New features**: Glob patterns, standalone files, WritableFS, enhanced latency control

See [MIGRATION-v1-to-v2.md](MIGRATION-v1-to-v2.md) for complete migration guide with step-by-step instructions and code examples.

## 10. License<a id="license"></a>

MIT License — see [LICENSE](LICENSE) file for details.