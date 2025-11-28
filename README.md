[![GoDoc](https://pkg.go.dev/badge/github.com/balinomad/go-mockfs/v2?status.svg)](https://pkg.go.dev/github.com/balinomad/go-mockfs/v2?tab=doc)
[![GoMod](https://img.shields.io/github/go-mod/go-version/balinomad/go-mockfs/v2)](https://github.com/balinomad/go-mockfs/tree/v2)
[![Size](https://img.shields.io/github/languages/code-size/balinomad/go-mockfs)](https://github.com/balinomad/go-mockfs)
[![License](https://img.shields.io/github/license/balinomad/go-mockfs)](./LICENSE)
[![Go](https://github.com/balinomad/go-mockfs/actions/workflows/go.yml/badge.svg?branch=v2)](https://github.com/balinomad/go-mockfs/actions/workflows/go.yml?query=branch%3Av2)
[![Go Report Card](https://goreportcard.com/badge/github.com/balinomad/go-mockfs/v2)](https://goreportcard.com/report/github.com/balinomad/go-mockfs/v2)
[![codecov](https://codecov.io/github/balinomad/go-mockfs/graph/badge.svg?token=L1K68IIN51&branch=v2)](https://codecov.io/github/balinomad/go-mockfs?branch=v2)

# mockfs v2

*A flexible and feature-rich filesystem mocking library for Go, built on `testing/fstest.MapFS` with comprehensive error injection, latency simulation, and write operation support.*

## Table of Contents

- [mockfs v2](#mockfs-v2)
    - [Table of Contents](#table-of-contents)
    - [Overview](#overview)
    - [Key Features](#key-features)
    - [Installation](#installation)
    - [Quick Start](#quick-start)
    - [Core Concepts](#core-concepts)
        - [Statistics: Filesystem vs File-Handle Operations](#statistics-filesystem-vs-file-handle-operations)
        - [Error Injection](#error-injection)
        - [Operations](#operations)
        - [Path Matchers](#path-matchers)
        - [Latency Simulation](#latency-simulation)
        - [Write Support](#write-support)
    - [Usage Guide](#usage-guide)
        - [Creating a Mock Filesystem](#creating-a-mock-filesystem)
        - [Adding and Removing Files](#adding-and-removing-files)
        - [Error Injection: Simple Cases](#error-injection-simple-cases)
        - [Error Injection: Pattern Matching](#error-injection-pattern-matching)
        - [Statistics Tracking](#statistics-tracking)
        - [Latency Simulation](#latency-simulation-1)
        - [Write Operations](#write-operations)
        - [Standalone MockFile](#standalone-mockfile)
        - [SubFS Support](#subfs-support)
    - [Advanced Patterns](#advanced-patterns)
        - [Testing Error Recovery with Retries](#testing-error-recovery-with-retries)
        - [Testing Timeout Handling](#testing-timeout-handling)
        - [Testing Concurrent Access](#testing-concurrent-access)
        - [Dependency Injection for os Package Functions](#dependency-injection-for-os-package-functions)
    - [API Reference](#api-reference)
        - [`MockFS`](#mockfs)
            - [Constructors and Options](#constructors-and-options)
            - [Filesystem Operations (`fs.FS` Interface)](#filesystem-operations-fsfs-interface)
            - [`WritableFS` Operations](#writablefs-operations)
            - [File Management (Additive)](#file-management-additive)
            - [Error Injection (Convenience Methods)](#error-injection-convenience-methods)
            - [Statistics](#statistics)
        - [`MockFile`](#mockfile)
            - [Constructors and Options](#constructors-and-options-1)
            - [I/O Operations](#io-operations)
            - [Introspection](#introspection)
            - [Helpers](#helpers)
        - [`ErrorInjector`](#errorinjector)
            - [Management](#management)
            - [ErrorRule](#errorrule)
            - [ErrorMode Constants](#errormode-constants)
        - [`PathMatcher`](#pathmatcher)
            - [Interface](#interface)
            - [Implementations](#implementations)
        - [`Stats` Interface](#stats-interface)
            - [Operation Counters](#operation-counters)
            - [Byte Counters](#byte-counters)
            - [Comparison](#comparison)
        - [`LatencySimulator`](#latencysimulator)
            - [Constructors](#constructors)
            - [Methods](#methods)
            - [Simulation Options](#simulation-options)
        - [Operation Constants](#operation-constants)
        - [Predefined Errors](#predefined-errors)
        - [`MapFile`](#mapfile)
        - [`WritableFS` Interface](#writablefs-interface)
    - [Migrations](#migrations)
        - [Migration from *v1* to *v2*](#migration-from-v1-to-v2)
    - [Getting Help](#getting-help)
    - [License](#license)

## Overview

`mockfs` enables robust testing of filesystem-dependent code by providing a complete in-memory filesystem implementation with precise control over behavior, errors, and performance characteristics. Built around Go's standard `fs` interfaces, it integrates seamlessly with existing code while adding powerful testing capabilities.

## Key Features

- **Complete fs interface implementation**: `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.SubFS`
- **Writable filesystem operations**: `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename`, `WriteFile`
- **Advanced error injection**: Exact path matching, glob patterns, regex patterns, per-operation or cross-operation rules
- **Flexible error modes**: Always, once, or after N successful operations
- **Latency simulation**: Global, per-operation, serialized, or async with independent file-handle state
- **Dual statistics tracking**: Separate counters for filesystem-level vs file-handle-level operations
- **Standalone file mocking**: Create `MockFile` instances without a filesystem for testing `io.Reader`/`io.Writer`/`io.Seeker` functions
- **SubFS support**: Full sub-filesystem implementation with automatic error rule adjustment
- **Concurrency-safe**: All operations safe for concurrent use

## Installation

Add **mockfs v2** to your module dependencies:

```bash
go get github.com/balinomad/go-mockfs/v2@latest
```

## Quick Start

```go
package main_test

import (
    "io/fs"
    "testing"
    "time"

    "github.com/balinomad/go-mockfs/v2"
)

func TestBasicUsage(t *testing.T) {
    // Create filesystem with initial files
    mfs := mockfs.NewMockFS(
        mockfs.File("config.json", `{"setting": "value"}`),
        mockfs.Dir("data"),
    )

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

## Core Concepts

### Statistics: Filesystem vs File-Handle Operations

`mockfs` tracks operations at two levels:

- **Filesystem-level operations** (`MockFS.Stats()`): `Open`, `Stat`, `ReadDir`, `Mkdir`, `Remove`, `Rename`, `WriteFile`, etc. — operations on the filesystem structure itself
- **File-handle operations** (`MockFile.Stats()`): `Read`, `Write`, `Close` — operations on individual open files

This separation enables precise verification of I/O patterns. Example:
```go
mfs := mockfs.NewMockFS(mockfs.File("file.txt", "content"))

file, _ := mfs.Open("file.txt")   // Tracked in MockFS.Stats()
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

### Error Injection

Error injection is managed through the `ErrorInjector` interface, which supports:

- **Path matching strategies**: Exact (`AddExact`), glob (`AddGlob`), regex (`AddRegexp`), wildcard (`AddAll`)
- **Error modes**: `ErrorModeAlways`, `ErrorModeOnce`, `ErrorModeAfterSuccesses`
- **Scope**: Per-operation or cross-operation rules

Access via `mfs.ErrorInjector()` for advanced configuration, or use convenience methods (`FailOpen`, `FailRead`, etc.) for common cases.

### Operations

`Operation` constants identify filesystem operations for error injection and statistics:

- `OpUnknown`, `OpStat`, `OpOpen`, `OpRead`, `OpWrite`, `OpSeek`, `OpClose`, `OpReadDir`
- `OpMkdir`, `OpMkdirAll`, `OpRemove`, `OpRemoveAll`, `OpRename`

### Path Matchers

`PathMatcher` interface enables flexible path matching:

- `ExactMatcher`: Single path
- `GlobMatcher`: Glob patterns (`*.txt`, `logs/*.log`)
- `RegexpMatcher`: Regular expressions
- `WildcardMatcher`: Matches all paths

Matchers automatically adjust for `SubFS` operations.

### Latency Simulation

`LatencySimulator` interface simulates I/O latency with options:

- **Global or per-operation durations**
- **Once mode**: Apply latency only on first call per operation type
- **Async mode**: Non-blocking (releases lock before sleeping)
- **Independent file-handle state**: Each opened file gets a cloned simulator

### Write Support

`WritableFS` extends `fs.FS` with write operations:
```go
type WritableFS interface {
    fs.FS
    Mkdir(path string, perm FileMode) error
    MkdirAll(path string, perm FileMode) error
    Remove(path string) error
    RemoveAll(path string) error
    Rename(oldpath, newpath string) error
    WriteFile(path string, data []byte, perm FileMode) error
}
```

`MockFS` implements `WritableFS` for full filesystem mutation support.

## Usage Guide

### Creating a Mock Filesystem

```go
// Empty filesystem
mfs := mockfs.NewMockFS()

// With initial files
mfs = mockfs.NewMockFS(
    mockfs.File("file.txt", "content"),
    mockfs.Dir("dir"),
)

// With initial files and options
mfs = mockfs.NewMockFS(nil,
    mockfs.File("file.txt", "content"),
    mockfs.Dir("dir"),
    mockfs.WithLatency(10*time.Millisecond),
    mockfs.WithCreateIfMissing(true),
    mockfs.WithOverwrite(),
)
```

### Adding and Removing Files

```go
// Add text file
err := mfs.AddFile("config.json", `{"key": "value"}`, 0o644)

// Add binary file
err = mfs.AddFileBytes("data.bin", []byte{0x01, 0x02}, 0o644)

// Add directory
err = mfs.AddDir("logs", 0o755)

// Remove path
err = mfs.RemoveEntry("temp.txt")
```

### Error Injection: Simple Cases

```go
// Always fail on specific operations
mfs.FailOpen("secret.txt", mockfs.ErrPermission)
mfs.FailRead("data.txt", mockfs.ErrUnexpectedEOF)
mfs.FailStat("config.json", mockfs.ErrNotExist)

// One-time errors
mfs.FailOpenOnce("flaky.db", mockfs.ErrTimeout)
mfs.FailReadOnce("network.log", io.EOF)

// Error after N successes
mfs.FailReadAfter("large.bin", io.EOF, 3)
```

### Error Injection: Pattern Matching

```go
// Glob patterns use path.Match semantics (not shell glob)
// Supports: *, ?, [...], but NOT ** or brace expansion
mfs.ErrorInjector().AddGlob(mockfs.OpRead, "*.log", io.EOF, mockfs.ErrorModeAlways, 0)
mfs.ErrorInjector().AddGlob(mockfs.OpOpen, "temp/*", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

// Regular expressions
mfs.ErrorInjector().AddRegexp(mockfs.OpRead, `\.tmp$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)

// All paths for an operation
mfs.ErrorInjector().AddAll(mockfs.OpWrite, mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

// All operations for a path
mfs.ErrorInjector().AddExactForAllOps("unstable.dat", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)

// All operations for all paths
mfs.ErrorInjector().AddAllForAllOps(mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0)
```

### Statistics Tracking

```go
// Filesystem-level statistics
stats := mfs.Stats()
stats.Count(mockfs.OpOpen)          // Total opens
stats.CountSuccess(mockfs.OpOpen)   // Successful opens
stats.CountFailure(mockfs.OpOpen)   // Failed opens
stats.Operations()                  // Total operations across all types
stats.HasFailures()                 // Any failures?

// File-handle statistics
file, _ := mfs.Open("file.txt")
buf := make([]byte, 100)
file.Read(buf)

mockFile := file.(mockfs.MockFile)
fileStats := mockFile.Stats()
fileStats.Count(mockfs.OpRead)      // Reads on this handle
fileStats.BytesRead()               // Bytes read via this handle
fileStats.BytesWritten()            // Bytes written via this handle

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

### Latency Simulation

```go
// Global latency for all operations
mfs := mockfs.NewMockFS(mockfs.WithLatency(50*time.Millisecond))

// Per-operation latency
mfs = mockfs.NewMockFS(mockfs.WithPerOperationLatency(
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

mfs = mockfs.NewMockFS(mockfs.WithLatencySimulator(sim))
```

### Write Operations

```go
// Enable writes
mfs := mockfs.NewMockFS(mockfs.WithOverwrite())

// Write file
err := mfs.WriteFile("output.txt", []byte("data"), 0o644)

// Create if missing
mfs = mockfs.NewMockFS(nil,
    mockfs.WithOverwrite(),
    mockfs.WithCreateIfMissing(true),
)
err = mfs.WriteFile("new.txt", []byte("content"), 0o644)

// Append mode
mfs = mockfs.NewMockFS(mockfs.WithAppend())
mfs.WriteFile("log.txt", []byte("line1\n"), 0o644)
mfs.WriteFile("log.txt", []byte("line2\n"), 0o644) // Appends

// Directory operations
err = mfs.Mkdir("logs", 0o755)
err = mfs.MkdirAll("app/config/prod", 0o755)
err = mfs.Remove("temp.txt")
err = mfs.RemoveAll("cache")
err = mfs.Rename("old.txt", "new.txt")

// Write via file handle
file, _ := mfs.Open("file.txt")
file.(io.Writer).Write([]byte("data"))
file.Close()
```

### Standalone MockFile

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

// Create entries without a filesystem
entries := []fs.DirEntry{
    mockfs.NewFileInfo("readme.txt", 1024, 0o644, time.Now()),
    mockfs.NewFileInfo("docs", 0, fs.ModeDir|0o755, time.Now()),
}
handler := mockfs.NewDirHandler(entries)
dir := mockfs.NewMockDirectory("mydir", handler)

// Test functions accepting io.Reader/io.Writer
func ProcessReader(r io.Reader) error { /* ... */ }
err := ProcessReader(file)
```

### SubFS Support

```go
mfs := mockfs.NewMockFS(
    mockfs.Dir("app",
        mockfs.Dir("config",
            mockfs.File("dev.json", "{}"),
            mockfs.File("prod.json", "{}"),
        ),
        mockfs.Dir("logs",
            mockfs.File("app.log", ""),
        ),
    ),
)

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

## Advanced Patterns

### Testing Error Recovery with Retries

```go
func TestRetryLogic(t *testing.T) {
    mfs := mockfs.NewMockFS(mockfs.File("data.txt", "content"))

    // First two reads fail, third succeeds
    mfs.FailReadAfter("data.txt", mockfs.ErrUnexpectedEOF, 2)

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

### Testing Timeout Handling

```go
func TestTimeoutBehavior(t *testing.T) {
    // Simulate slow I/O
    mfs := mockfs.NewMockFS(mockfs.WithLatency(2*time.Second))

    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    // Function should respect context timeout
    err := YourFunctionWithContext(ctx, mfs)
    if !errors.Is(err, context.DeadlineExceeded) {
        t.Errorf("expected timeout, got %v", err)
    }
}
```

### Testing Concurrent Access

```go
func TestConcurrentReads(t *testing.T) {
    mfs := mockfs.NewMockFS(mockfs.File("shared.txt", strings.Repeat("data", 1000)))

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

### Dependency Injection for os Package Functions

When testing code that uses `os` package functions directly, use dependency injection:
```go
// Define abstraction
type FileSystem interface {
    MkdirAll(path string, perm FileMode) error
    WriteFile(path string, data []byte, perm FileMode) error
    ReadFile(path string) ([]byte, error)
}

// Production implementation
type OSFileSystem struct{}

func (OSFileSystem) MkdirAll(path string, perm FileMode) error {
    return os.MkdirAll(path, perm)
}

func (OSFileSystem) WriteFile(path string, data []byte, perm FileMode) error {
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
    mfs := mockfs.NewMockFS(mockfs.WithCreateIfMissing(true))
    svc := &Service{fs: MockFileSystem{mfs}}
    // Test with full error injection and statistics
}
```

## API Reference

Complete API organized by type. See [GoDoc](https://pkg.go.dev/github.com/balinomad/go-mockfs/v2) for detailed method signatures and examples.

### `MockFS`

Primary filesystem type implementing `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.SubFS`, and `WritableFS`.

#### Constructors and Options

**Constructor:**
```go
NewMockFS(opts ...MockFSOption) *MockFS
```

**Options:**

| Option | Description |
|--------|-------------|
| `File(name string, content any, mode ...FileMode)` | Creates mock file in filesystem |
| `Dir(name string, args ...any)` | Creates mock directory in filesystem |
| `WithLatency(duration)` | Sets uniform latency for all operations |
| `WithPerOperationLatency(map[Operation]time.Duration)` | Sets per-operation latency |
| `WithLatencySimulator(sim)` | Sets custom `LatencySimulator` instance |
| `WithErrorInjector(injector)` | Sets custom `ErrorInjector` instance |
| `WithCreateIfMissing(bool)` | Enables creating files on write if missing |
| `WithOverwrite()` | Sets write mode to overwrite existing content (default) |
| `WithAppend()` | Sets write mode to append to existing content |
| `WithReadOnly()` | Disables all write operations |

#### Filesystem Operations (`fs.FS` Interface)

| Method | Description |
|--------|-------------|
| `Open(name string) (fs.File, error)` | Opens file; returns `MockFile` |
| `Stat(name string) (fs.FileInfo, error)` | Returns file info without opening |
| `ReadFile(name string) ([]byte, error)` | Reads entire file (implements `fs.ReadFileFS`) |
| `ReadDir(name string) ([]fs.DirEntry, error)` | Lists directory contents (implements `fs.ReadDirFS`) |
| `Sub(dir string) (fs.FS, error)` | Returns sub-filesystem rooted at dir (implements `fs.SubFS`) |

#### `WritableFS` Operations

| Method | Description |
|--------|-------------|
| `Mkdir(path string, perm FileMode) error` | Creates directory (parent must exist) |
| `MkdirAll(path string, perm FileMode) error` | Creates directory and all parents |
| `Remove(path string) error` | Removes file or empty directory |
| `RemoveAll(path string) error` | Removes path and children recursively |
| `Rename(oldpath, newpath string) error` | Renames/moves file or directory |
| `WriteFile(path string, data []byte, perm FileMode) error` | Writes file atomically |

#### File Management (Additive)

| Method | Description |
|--------|-------------|
| `AddFile(path, content string, mode FileMode) error` | Adds text file |
| `AddFileBytes(path string, data []byte, mode FileMode) error` | Adds binary file |
| `AddDir(path string, mode FileMode) error` | Adds directory |
| `RemoveEntry(path string) error` | Removes file or directory from map |

#### Error Injection (Convenience Methods)

**Single-operation methods:**

| Method | Error Mode | Description |
|--------|------------|-------------|
| `FailStat(path, err)` | Always | Stat always fails |
| `FailStatOnce(path, err)` | Once | Stat fails once |
| `FailOpen(path, err)` | Always | Open always fails |
| `FailOpenOnce(path, err)` | Once | Open fails once |
| `FailRead(path, err)` | Always | Read always fails |
| `FailReadOnce(path, err)` | Once | Read fails once |
| `FailReadAfter(path, err, n)` | AfterSuccesses | Read fails after n successes |
| `FailWrite(path, err)` | Always | Write always fails |
| `FailWriteOnce(path, err)` | Once | Write fails once |
| `FailReadDir(path, err)` | Always | ReadDir always fails |
| `FailReadDirOnce(path, err)` | Once | ReadDir fails once |
| `FailClose(path, err)` | Always | Close always fails |
| `FailCloseOnce(path, err)` | Once | Close fails once |
| `FailMkdir(path, err)` | Always | Mkdir always fails |
| `FailMkdirOnce(path, err)` | Once | Mkdir fails once |
| `FailMkdirAll(path, err)` | Always | MkdirAll always fails |
| `FailMkdirAllOnce(path, err)` | Once | MkdirAll fails once |
| `FailRemove(path, err)` | Always | Remove always fails |
| `FailRemoveOnce(path, err)` | Once | Remove fails once |
| `FailRemoveAll(path, err)` | Always | RemoveAll always fails |
| `FailRemoveAllOnce(path, err)` | Once | RemoveAll fails once |
| `FailRename(path, err)` | Always | Rename always fails |
| `FailRenameOnce(path, err)` | Once | Rename fails once |

**Bulk methods:**

| Method | Description |
|--------|-------------|
| `MarkNonExistent(paths ...string)` | Marks paths as non-existent for all operations |
| `ClearErrors()` | Removes all configured error rules |

**Advanced access:**
```go
ErrorInjector() ErrorInjector
```
Returns the `ErrorInjector` interface for pattern-based rules (glob/regex), cross-operation rules, and custom configurations. See [ErrorInjector](#errorinjector).

#### Statistics

| Method | Description |
|--------|-------------|
| `Stats() Stats` | Returns immutable snapshot of filesystem-level operation statistics |
| `ResetStats()` | Resets all statistics counters to zero |

See [Stats Interface](#stats-interface) for available statistics methods.

---

### `MockFile`

File handle type returned by `MockFS.Open()` or created standalone. Implements `fs.File`, `fs.ReadDirFile`, `io.Reader`, `io.Writer`, `io.Seeker`, `io.Closer`.

#### Constructors and Options

**Constructors:**
```go
NewMockFile(mapFile *MapFile, name string, opts ...MockFileOption) MockFile
NewMockFileFromBytes(name string, data []byte, opts ...MockFileOption) MockFile
NewMockFileFromString(name string, content string, opts ...MockFileOption) MockFile
NewMockDirectory(name string, handler func(int)([]fs.DirEntry, error), opts ...MockFileOption) MockFile
```

**Options:**

| Option | Description |
|--------|-------------|
| `WithFileAppend()` | Sets file to append data on write |
| `WithFileOverwrite()` | Sets file to overwrite content on write (default) |
| `WithFileReadOnly()` | Disables all writes |
| `WithFileLatency(duration)` | Sets uniform latency for all operations |
| `WithFilePerOperationLatency(map[Operation]time.Duration)` | Sets per-operation latency |
| `WithFileLatencySimulator(sim)` | Sets custom `LatencySimulator` instance |
| `WithFileErrorInjector(injector)` | Sets custom `ErrorInjector` instance |
| `WithFileReadDirHandler(func(int)([]fs.DirEntry, error))` | Sets custom ReadDir handler for directories |
| `WithFileStats(stats)` | Sets custom `StatsRecorder` instance |

#### I/O Operations

| Method | Description |
|--------|-------------|
| `Read(b []byte) (int, error)` | Reads up to len(b) bytes (implements `io.Reader`) |
| `Write(b []byte) (int, error)` | Writes len(b) bytes (implements `io.Writer`) |
| `Seek(offset int64, whence int) (int64, error)` | Sets read/write position (implements `io.Seeker`) |
| `ReadDir(n int) ([]fs.DirEntry, error)` | Reads directory entries (implements `fs.ReadDirFile`) |
| `Stat() (fs.FileInfo, error)` | Returns file info (implements `fs.File`) |
| `Close() error` | Closes file handle (implements `io.Closer`) |

#### Introspection

| Method | Description |
|--------|-------------|
| `Stats() Stats` | Returns immutable snapshot of file-handle operation statistics |
| `ErrorInjector() ErrorInjector` | Returns the `ErrorInjector` instance for this file |
| `LatencySimulator() LatencySimulator` | Returns the `LatencySimulator` instance for this file |

See [Stats Interface](#stats-interface) for statistics methods.

#### Helpers

```go
NewDirHandler(entries []fs.DirEntry) func(int)([]fs.DirEntry, error)
```
Creates stateful ReadDir handler from static entry list. Used with `NewMockDirectory` or `WithFileReadDirHandler`.

```go
NewFileInfo(name string, size int64, mode FileMode, modTime time.Time) *FileInfo
```
Creates `fs.DirEntry` / `fs.FileInfo` for testing directory entries without requiring a filesystem.

---

### `ErrorInjector`

Interface for configuring error injection rules. Accessed via `MockFS.ErrorInjector()` or `MockFile.ErrorInjector()`.

#### Management

**Core method:**
```go
Add(op Operation, rule *ErrorRule)
```
Adds pre-configured `ErrorRule`. All other methods delegate to this.

**Path-specific methods:**

| Method | Description |
|--------|-------------|
| `AddExact(op, path, err, mode, after)` | Adds rule for exact path match |
| `AddGlob(op, pattern, err, mode, after) error` | Adds rule for glob pattern (e.g., `*.txt`) |
| `AddRegexp(op, pattern, err, mode, after) error` | Adds rule for regex pattern |
| `AddAll(op, err, mode, after)` | Adds rule matching all paths for operation |

**Cross-operation methods:**

| Method | Description |
|--------|-------------|
| `AddExactForAllOps(path, err, mode, after)` | Adds rule for exact path across all operations |
| `AddGlobForAllOps(pattern, err, mode, after) error` | Adds rule for glob pattern across all operations |
| `AddRegexpForAllOps(pattern, err, mode, after) error` | Adds rule for regex pattern across all operations |
| `AddAllForAllOps(err, mode, after)` | Adds rule matching all paths and all operations |

**Management methods:**

| Method | Description |
|--------|-------------|
| `Clear()` | Removes all configured error rules |
| `GetAll() map[Operation][]*ErrorRule` | Returns all rules for introspection |
| `CloneForSub(prefix string) ErrorInjector` | Returns injector adjusted for sub-filesystem |
| `CheckAndApply(op, path string) error` | Internal; checks and applies matching rules |

#### ErrorRule

```go
NewErrorRule(err error, mode ErrorMode, after int, matchers ...PathMatcher) *ErrorRule
```
Creates custom error rule with path matchers.

**ErrorRule fields (read-only after creation):**
- `Err error` — Error to return
- `Mode ErrorMode` — How error is applied (Always/Once/AfterSuccesses)
- `AfterN uint64` — Number of successes before error (AfterSuccesses mode only)

#### ErrorMode Constants

| Constant | Behavior |
|----------|----------|
| `ErrorModeAlways` | Error returned on every matching operation |
| `ErrorModeOnce` | Error returned once, then rule becomes inactive |
| `ErrorModeAfterSuccesses` | Error returned after N successful operations |

---

### `PathMatcher`

Interface for matching filesystem paths in error injection rules.

#### Interface

```go
type PathMatcher interface {
    Matches(path string) bool
    CloneForSub(prefix string) PathMatcher
}
```

#### Implementations

| Type | Constructor | Description |
|------|-------------|-------------|
| `ExactMatcher` | `NewExactMatcher(path string)` | Matches single exact path |
| `GlobMatcher` | `NewGlobMatcher(pattern string)` | Matches glob pattern (`*.txt`, `logs/*.log`) |
| `RegexpMatcher` | `NewRegexpMatcher(pattern string)` | Matches regular expression |
| `WildcardMatcher` | `NewWildcardMatcher()` | Matches all paths |

---

### `Stats` Interface

Immutable snapshot of operation statistics. Returned by `MockFS.Stats()` and `MockFile.Stats()`.

#### Operation Counters

| Method | Description |
|--------|-------------|
| `Count(op Operation) int` | Total calls for operation |
| `CountSuccess(op Operation) int` | Successful calls for operation |
| `CountFailure(op Operation) int` | Failed calls for operation |
| `Operations() int` | Total operations across all types |
| `HasFailures() bool` | Whether any operation has failed |
| `Failures() []Operation` | List of operations with at least one failure |

#### Byte Counters

| Method | Description |
|--------|-------------|
| `BytesRead() int` | Total bytes read via Read operations |
| `BytesWritten() int` | Total bytes written via Write operations |

#### Comparison

| Method | Description |
|--------|-------------|
| `Delta(other Stats) Stats` | Returns difference between this and other (can be negative) |
| `Equal(other Stats) bool` | Whether this has same values as other |
| `String() string` | Human-readable summary |

---

### `LatencySimulator`

Interface for simulating I/O latency. Accessed via `MockFile.LatencySimulator()` or configured with `WithLatencySimulator`.

#### Constructors

```go
NewLatencySimulator(duration time.Duration) LatencySimulator
NewLatencySimulatorPerOp(durations map[Operation]time.Duration) LatencySimulator
NewNoopLatencySimulator() LatencySimulator
```

#### Methods

| Method | Description |
|--------|-------------|
| `Simulate(op Operation, opts ...SimOpt)` | Simulates latency for operation |
| `Reset()` | Clears internal "seen" state for Once mode |
| `Clone() LatencySimulator` | Returns copy with reset state, same duration config |

#### Simulation Options

| Option | Description |
|--------|-------------|
| `Once()` | Apply latency at most once per operation type |
| `Async()` | Release lock before sleeping (non-blocking) |
| `OnceAsync()` | Combines Once and Async |

---

### Operation Constants

Operation identifiers for error injection and statistics.

| Constant | Description |
|----------|-------------|
| `OpUnknown` | Unknown/fallback operation |
| `OpStat` | Stat operation (filesystem-level) |
| `OpOpen` | Open operation (filesystem-level) |
| `OpRead` | Read operation (file-handle) |
| `OpWrite` | Write operation (file-handle) |
| `OpSeek` | Seek operation (file-handle) |
| `OpClose` | Close operation (file-handle) |
| `OpReadDir` | ReadDir operation (filesystem or file-handle) |
| `OpMkdir` | Mkdir operation (filesystem-level) |
| `OpMkdirAll` | MkdirAll operation (filesystem-level) |
| `OpRemove` | Remove operation (filesystem-level) |
| `OpRemoveAll` | RemoveAll operation (filesystem-level) |
| `OpRename` | Rename operation (filesystem-level) |

**Helper methods:**
```go
func (op Operation) String() string
func (op Operation) IsValid() bool
func StringToOperation(s string) Operation
```

---

### Predefined Errors

Common filesystem errors for use in error injection.

| Error | Description | Source |
|-------|-------------|--------|
| `ErrInvalid` | Invalid argument | `fs.ErrInvalid` |
| `ErrPermission` | Permission denied | `fs.ErrPermission` |
| `ErrExist` | File already exists | `fs.ErrExist` |
| `ErrNotExist` | File does not exist | `fs.ErrNotExist` |
| `ErrClosed` | File already closed | `fs.ErrClosed` |
| `ErrDiskFull` | Disk full | mockfs |
| `ErrTimeout` | Operation timeout | mockfs |
| `ErrCorrupted` | Corrupted data | mockfs |
| `ErrTooManyHandles` | Too many open files | mockfs |
| `ErrNotDir` | Not a directory | mockfs |
| `ErrNotEmpty` | Directory not empty | mockfs |

---

### `MapFile`

Type alias for `testing/fstest.MapFile`. Used to define initial filesystem state.
```go
type MapFile = fstest.MapFile
```

**Fields:**
- `Data []byte` — File content
- `Mode FileMode` — File permissions and type bits
- `ModTime time.Time` — Modification timestamp
- `Sys any` — System-specific data (unused by mockfs)

---

### `WritableFS` Interface

Extension of `fs.FS` with write operations. `MockFS` implements this interface.
```go
type WritableFS interface {
    fs.FS
    Mkdir(path string, perm FileMode) error
    MkdirAll(path string, perm FileMode) error
    Remove(path string) error
    RemoveAll(path string) error
    Rename(oldpath, newpath string) error
    WriteFile(path string, data []byte, perm FileMode) error
}
```
## Migrations

### Migration from *v1* to *v2*

**mockfs *v2* a major rewrite with breaking changes.**

Key differences:

- **Statistics**: `GetStats()` → `Stats()` (interface with methods, not struct fields)
- **Error injection**: `AddStatError()` → `FailStat()`, advanced features via `ErrorInjector()`
- **File management**: `AddFileString()` → `AddFile()`, `AddDirectory()` → `AddDir()`
- **Write support**: `WithWritesEnabled()` → `WithOverwrite()`/`WithAppend()`
- **Operation tracking**: Filesystem-level vs file-handle-level split
- **New features**: Glob patterns, standalone files, WritableFS, enhanced latency control

See [MIGRATION-v1-to-v2.md](MIGRATION-v1-to-v2.md) for complete migration guide with step-by-step instructions and code examples.

## Getting Help

- Review test files (`*_test.go`) in the repository for comprehensive examples
- Check GoDoc at `pkg.go.dev/github.com/balinomad/go-mockfs`
- File issues at `github.com/balinomad/go-mockfs/issues`

## License

MIT License — see [LICENSE](LICENSE) file for details.