[![GoDoc](https://pkg.go.dev/badge/github.com/balinomad/go-mockfs/v2?status.svg)](https://pkg.go.dev/github.com/balinomad/go-mockfs/v2?tab=doc)
[![GoMod](https://img.shields.io/github/go-mod/go-version/balinomad/go-mockfs/v2)](https://github.com/balinomad/go-mockfs/tree/v2)
[![Size](https://img.shields.io/github/languages/code-size/balinomad/go-mockfs)](https://github.com/balinomad/go-mockfs)
[![License](https://img.shields.io/github/license/balinomad/go-mockfs)](./LICENSE)
[![Go](https://github.com/balinomad/go-mockfs/actions/workflows/go.yml/badge.svg?branch=v2)](https://github.com/balinomad/go-mockfs/actions/workflows/go.yml?query=branch%3Av2)
[![Go Report Card](https://goreportcard.com/badge/github.com/balinomad/go-mockfs/v2)](https://goreportcard.com/report/github.com/balinomad/go-mockfs/v2)
[![codecov](https://codecov.io/github/balinomad/go-mockfs/graph/badge.svg?token=L1K68IIN51&branch=v2)](https://codecov.io/github/balinomad/go-mockfs?branch=v2)

# mockfs

*A flexible and feature-rich mock filesystem for Go testing, built on `testing/fstest.MapFS` with comprehensive error injection, latency simulation, and write operation support.*

## Overview

`mockfs` enables robust testing of filesystem-dependent code by providing a complete in-memory filesystem with precise control over behavior, errors, and performance characteristics. It implements Go's standard `fs` interfaces and adds powerful testing capabilities designed for both experienced Go developers and those new to filesystem testing.

Built for testing scenarios that require:
- Simulating I/O failures and edge cases
- Testing timeout and retry logic
- Verifying filesystem access patterns
- Testing concurrent filesystem operations
- Validating write operations and transactions

## Key Features

- **Complete `fs` interface implementation** – `fs.FS`, `fs.ReadDirFS`, `fs.ReadFileFS`, `fs.StatFS`, `fs.SubFS`
- **Writable filesystem** – `Mkdir`, `Remove`, `Rename`, `WriteFile` with configurable modes
- **Flexible error injection** – Path matching (exact, glob, regex), operation-specific or cross-operation rules
- **Error modes** – Always fail, fail once, or fail after N successes
- **Latency simulation** – Global, per-operation, serialized, or async with independent file-handle state
- **Dual statistics tracking** – Separate counters for filesystem-level vs file-handle operations
- **Standalone file mocking** – Test `io.Reader`/`io.Writer` functions without a full filesystem
- **Full `SubFS` support** – Automatic path adjustment for sub-filesystems
- **Concurrency-safe** – All operations safe for concurrent use

## Installation

```bash
go get github.com/balinomad/go-mockfs/v2@latest
```

## Quick Start

### Basic Usage

```go
package main_test

import (
    "io/fs"
    "testing"

    "github.com/balinomad/go-mockfs/v2"
)

func TestBasicFileOperations(t *testing.T) {
    // Create filesystem with initial files
    mfs := mockfs.NewMockFS(
        mockfs.File("config.json", `{"setting": "value"}`),
        mockfs.Dir("data",
            mockfs.File("input.txt", "test data"),
        ),
    )

    // Read file
    data, err := fs.ReadFile(mfs, "config.json")
    if err != nil {
        t.Fatal(err)
    }

    // List directory
    entries, err := mfs.ReadDir("data")
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

### Error Injection

```go
func TestErrorHandling(t *testing.T) {
    mfs := mockfs.NewMockFS(
        mockfs.File("flaky.txt", "data"),
    )

    // Simulate permission error
    mfs.FailOpen("secret.txt", mockfs.ErrPermission)

    // Simulate intermittent read errors (fail after 3 successes)
    mfs.FailReadAfter("flaky.txt", io.EOF, 3)

    // Your code under test
    err := YourFunction(mfs)
    if !errors.Is(err, mockfs.ErrPermission) {
        t.Errorf("expected permission error, got %v", err)
    }
}
```

### Standalone File Testing

```go
func TestFileReader(t *testing.T) {
    // Create file without filesystem
    file := mockfs.NewMockFileFromString("test.txt", "content")

    // Test function that accepts io.Reader
    result := YourReaderFunction(file)

    // Verify statistics
    stats := file.Stats()
    if stats.BytesRead() == 0 {
        t.Error("expected bytes to be read")
    }
}
```

## Core Concepts

### Statistics: Filesystem vs File-Handle Operations

`mockfs` tracks operations at two levels for precise verification:

- **Filesystem operations** (`(*MockFS).Stats()`): `Open`, `Stat`, `ReadDir`, `Mkdir`, `Remove`, etc.
- **File-handle operations** (`(*MockFile).Stats()`): `Read`, `Write`, `Close` on individual open files
```go
file, _ := mfs.Open("file.txt")
file.Read(buf)

mfs.Stats().Count(mockfs.OpOpen)  // Filesystem: 1 open

// Assert to concrete pointer to access file-handle stats
file.(*mockfs.MockFile).Stats().Count(mockfs.OpRead)  // File handle: 1 read
```

### Error Injection

Control errors with fine-grained rules:
```go
// Simple: always fail specific operations
mfs.FailOpen("file.txt", mockfs.ErrPermission)

// Pattern matching: fail all .log files
mfs.ErrorInjector().AddGlob(mockfs.OpRead, "*.log", io.EOF, mockfs.ErrorModeAlways, 0)

// Conditional: fail after N successes
mfs.FailReadAfter("data.bin", io.EOF, 5)
```

### Write Operations

Full write support with configurable modes:
```go
// Enable writes with overwrite mode
mfs := mockfs.NewMockFS(mockfs.WithOverwrite())
mfs.WriteFile("output.txt", data, 0o644)

// Append mode
mfs = mockfs.NewMockFS(mockfs.WithAppend())
mfs.WriteFile("log.txt", []byte("line1\n"), 0o644)
mfs.WriteFile("log.txt", []byte("line2\n"), 0o644) // Appends
```

### Latency Simulation

Test timeout and performance handling:
```go
// Global latency
mfs := mockfs.NewMockFS(mockfs.WithLatency(100*time.Millisecond))

// Per-operation latency
mfs = mockfs.NewMockFS(mockfs.WithPerOperationLatency(
    map[mockfs.Operation]time.Duration{
        mockfs.OpRead:  200 * time.Millisecond,
        mockfs.OpWrite: 500 * time.Millisecond,
    },
))
```

## Documentation

- **[API Reference](https://pkg.go.dev/github.com/balinomad/go-mockfs/v2)** – Complete API documentation on pkg.go.dev
- **[Usage Guide](USAGE.md)** – Advanced patterns, best practices, and real-world examples
- **[Migration Guide](MIGRATION-v1-to-v2.md)** – Upgrading from v1 to v2

## Examples

See `example_*_test.go` files in the repository for runnable examples:
- [Basic operations](example_basic_test.go)
- [Error injection](example_errors_test.go)
- [Write operations](example_writes_test.go)
- [Latency simulation](example_latency_test.go)
- [Advanced features](example_advanced_test.go)

## Getting Help

- Review test files (`*_test.go`) for comprehensive usage examples
- Check [GoDoc](https://pkg.go.dev/github.com/balinomad/go-mockfs/v2) for detailed API documentation
- Read the [Usage Guide](USAGE.md) for patterns and best practices
- File issues at [github.com/balinomad/go-mockfs/issues](https://github.com/balinomad/go-mockfs/issues)

## License

MIT License – see [LICENSE](LICENSE) file for details.