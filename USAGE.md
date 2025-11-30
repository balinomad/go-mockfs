# MockFS Usage Guide

Advanced patterns, best practices, and real-world examples for `github.com/balinomad/go-mockfs/v2`.

For basic usage and API reference, see:
- [README](README.md) – Quick start and overview
- [GoDoc](https://pkg.go.dev/github.com/balinomad/go-mockfs/v2) – Complete API documentation

## Table of Contents

- [Testing Error Recovery](#testing-error-recovery)
- [Testing Timeout Handling](#testing-timeout-handling)
- [Testing Concurrent Access](#testing-concurrent-access)
- [Dependency Injection Patterns](#dependency-injection-patterns)
- [Pattern Matching Semantics](#pattern-matching-semantics)
- [SubFS Patterns](#subfs-patterns)
- [Performance Testing](#performance-testing)
- [Best Practices](#best-practices)


## Testing Error Recovery

### Testing Retry Logic
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

### Testing Exponential Backoff
```go
func TestExponentialBackoff(t *testing.T) {
    mfs := mockfs.NewMockFS(
        mockfs.File("api-response.json", "{}"),
        mockfs.WithLatency(50*time.Millisecond),
    )

    // Fail first 3 attempts
    injector := mfs.ErrorInjector()
    rule := mockfs.NewErrorRule(mockfs.ErrTimeout, mockfs.ErrorModeNext, 3,
        mockfs.NewExactMatcher("api-response.json"))
    injector.Add(mockfs.OpOpen, rule)

    start := time.Now()
    result, err := YourBackoffFunction(mfs, "api-response.json")
    elapsed := time.Since(start)

    // Should succeed after backoff delays
    if err != nil {
        t.Fatalf("expected success after retries: %v", err)
    }

    // Verify backoff timing (e.g., 100ms + 200ms + 400ms + 50ms latency)
    if elapsed < 750*time.Millisecond {
        t.Errorf("backoff too short: %v", elapsed)
    }
}
```


## Testing Timeout Handling

### Context Deadline Exceeded
```go
func TestTimeoutBehavior(t *testing.T) {
    // Simulate slow I/O
    mfs := mockfs.NewMockFS(
        mockfs.File("large.bin", strings.Repeat("data", 10000)),
        mockfs.WithLatency(2*time.Second),
    )

    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    // Function should respect context timeout
    err := YourFunctionWithContext(ctx, mfs)
    if !errors.Is(err, context.DeadlineExceeded) {
        t.Errorf("expected timeout, got %v", err)
    }

    // Verify operation was attempted
    stats := mfs.Stats()
    if stats.Count(mockfs.OpOpen) == 0 {
        t.Error("expected at least one open attempt")
    }
}
```

### Testing Async Operations
```go
func TestAsyncTimeout(t *testing.T) {
    // Use async latency to allow concurrent operations
    mfs := mockfs.NewMockFS(mockfs.File("file.txt", "data"))

    sim := mockfs.NewLatencySimulator(500 * time.Millisecond)
    mfs = mockfs.NewMockFS(
        mockfs.File("file.txt", "data"),
        mockfs.WithLatencySimulator(sim),
    )

    // Multiple concurrent operations should complete in ~500ms, not 1500ms
    start := time.Now()
    var wg sync.WaitGroup
    for i := 0; i < 3; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _, _ = fs.ReadFile(mfs, "file.txt")
        }()
    }
    wg.Wait()
    elapsed := time.Since(start)

    if elapsed > 1*time.Second {
        t.Errorf("async operations took too long: %v", elapsed)
    }
}
```

## Testing Concurrent Access

### Concurrent Reads
```go
func TestConcurrentReads(t *testing.T) {
    mfs := mockfs.NewMockFS(
        mockfs.File("shared.txt", strings.Repeat("data", 1000)),
    )

    var wg sync.WaitGroup
    errors := make(chan error, 10)

    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            data, err := fs.ReadFile(mfs, "shared.txt")
            if err != nil {
                errors <- err
                return
            }
            if len(data) != 4000 {
                errors <- fmt.Errorf("expected 4000 bytes, got %d", len(data))
            }
        }()
    }

    wg.Wait()
    close(errors)

    for err := range errors {
        t.Errorf("concurrent read error: %v", err)
    }

    // Verify all reads counted
    stats := mfs.Stats()
    if stats.Count(mockfs.OpOpen) != 10 {
        t.Errorf("expected 10 opens, got %d", stats.Count(mockfs.OpOpen))
    }
}
```

### Testing Race Conditions
```go
func TestConcurrentWriteRace(t *testing.T) {
    mfs := mockfs.NewMockFS(
        mockfs.File("counter.txt", "0"),
        mockfs.WithOverwrite(),
    )

    // Deliberately create race condition
    var wg sync.WaitGroup
    for i := 0; i < 5; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            data := fmt.Sprintf("%d", n)
            _ = mfs.WriteFile("counter.txt", []byte(data), 0o644)
        }(i)
    }
    wg.Wait()

    // Result is non-deterministic, but operation should succeed
    stats := mfs.Stats()
    if stats.Count(mockfs.OpWrite) != 5 {
        t.Errorf("expected 5 writes, got %d", stats.Count(mockfs.OpWrite))
    }
}
```
## Dependency Injection Patterns

### Abstracting Filesystem Operations

When testing code that uses `os` package functions directly:
```go
// Define abstraction
type FileSystem interface {
    MkdirAll(path string, perm fs.FileMode) error
    WriteFile(path string, data []byte, perm fs.FileMode) error
    ReadFile(path string) ([]byte, error)
    ReadDir(path string) ([]fs.DirEntry, error)
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

func (OSFileSystem) ReadDir(path string) ([]fs.DirEntry, error) {
    return os.ReadDir(path)
}

// Test implementation
type MockFileSystem struct {
    *mockfs.MockFS
}

func (m MockFileSystem) ReadFile(path string) ([]byte, error) {
    return fs.ReadFile(m.MockFS, path)
}

func (m MockFileSystem) ReadDir(path string) ([]fs.DirEntry, error) {
    return m.MockFS.ReadDir(path)
}

// Usage in production
func NewService() *Service {
    return &Service{fs: OSFileSystem{}}
}

// Usage in tests
func TestService(t *testing.T) {
    mfs := mockfs.NewMockFS(
        mockfs.WithCreateIfMissing(true),
        mockfs.WithOverwrite(),
    )
    svc := &Service{fs: MockFileSystem{mfs}}

    // Test with full error injection and statistics
    mfs.FailWrite("output.txt", mockfs.ErrDiskFull)
    err := svc.ProcessData()
    if !errors.Is(err, mockfs.ErrDiskFull) {
        t.Errorf("expected disk full error, got %v", err)
    }
}
```

### Testing Repository Patterns
```go
type Repository interface {
    Save(id string, data []byte) error
    Load(id string) ([]byte, error)
}

type FileRepository struct {
    fs FileSystem
    dir string
}

func (r *FileRepository) Save(id string, data []byte) error {
    path := filepath.Join(r.dir, id+".dat")
    return r.fs.WriteFile(path, data, 0o644)
}

func (r *FileRepository) Load(id string) ([]byte, error) {
    path := filepath.Join(r.dir, id+".dat")
    return r.fs.ReadFile(path)
}

func TestRepositoryErrorHandling(t *testing.T) {
    mfs := mockfs.NewMockFS(
        mockfs.Dir("data"),
        mockfs.WithCreateIfMissing(true),
    )
    repo := &FileRepository{
        fs:  MockFileSystem{mfs},
        dir: "data",
    }

    // Test save failure
    mfs.FailWrite("data/test.dat", mockfs.ErrDiskFull)
    err := repo.Save("test", []byte("content"))
    if !errors.Is(err, mockfs.ErrDiskFull) {
        t.Errorf("expected disk full, got %v", err)
    }

    // Test load failure
    mfs.ClearErrors()
    mfs.FailRead("data/test.dat", mockfs.ErrCorrupted)
    _, err = repo.Load("test")
    if !errors.Is(err, mockfs.ErrCorrupted) {
        t.Errorf("expected corrupted data, got %v", err)
    }
}
```

## Pattern Matching Semantics

### Glob Patterns

`mockfs` uses `path.Match` for glob patterns:
```go
// Supported patterns
injector.AddGlob(OpRead, "*.txt", err, ...)           // All .txt in current dir
injector.AddGlob(OpRead, "logs/*.log", err, ...)      // All .log in logs/
injector.AddGlob(OpOpen, "temp/file?.dat", err, ...)  // Single char wildcard

// NOT supported (use regex instead)
// "**/*.txt"     - Recursive glob
// "*.{txt,log}"  - Brace expansion

// Use regex for recursive patterns
injector.AddRegexp(OpRead, `.*\.txt$`, err, ...)
```

**Glob vs Regex Performance:**
- Glob patterns are faster for simple cases
- Use glob when possible: `*.log` instead of `\.log$`
- Use regex for complex patterns: nested paths, alternation

### Regular Expressions

Full RE2 syntax support via Go's `regexp` package:
```go
// Match all .txt files recursively
injector.AddRegexp(OpRead, `\.txt$`, err, ...)

// Match files in any subdirectory of logs/
injector.AddRegexp(OpRead, `^logs/.*/.*\.log$`, err, ...)

// Alternation
injector.AddRegexp(OpOpen, `\.(txt|log|json)$`, err, ...)

// Anchoring for precision
injector.AddRegexp(OpStat, `^config/prod/`, err, ...)  // Only prod configs
```

### Combining Matchers
```go
// Multiple matchers in one rule (OR logic)
m1 := mockfs.NewExactMatcher("file1.txt")
m2 := mockfs.NewExactMatcher("file2.txt")
m3, _ := mockfs.NewRegexpMatcher(`\.tmp$`)

rule := mockfs.NewErrorRule(
    mockfs.ErrPermission,
    mockfs.ErrorModeAlways,
    0,
    m1, m2, m3, // Matches file1.txt OR file2.txt OR *.tmp
)
mfs.ErrorInjector().Add(mockfs.OpOpen, rule)
```

## SubFS Patterns

### Testing with Sub-Filesystems
```go
func TestConfigLoader(t *testing.T) {
    mfs := mockfs.NewMockFS(
        mockfs.Dir("app",
            mockfs.Dir("config",
                mockfs.File("dev.json", `{"env":"dev"}`),
                mockfs.File("prod.json", `{"env":"prod"}`),
            ),
        ),
    )

    // Configure error for production config
    mfs.FailRead("app/config/prod.json", mockfs.ErrPermission)

    // Create sub-filesystem for config directory
    configFS, err := mfs.Sub("app/config")
    if err != nil {
        t.Fatal(err)
    }

    // Error rules automatically adjusted
    loader := ConfigLoader{fs: configFS}

    // Dev config succeeds
    dev, err := loader.Load("dev.json")
    if err != nil {
        t.Errorf("dev config load failed: %v", err)
    }

    // Prod config fails with permission error
    _, err = loader.Load("prod.json")
    if !errors.Is(err, mockfs.ErrPermission) {
        t.Errorf("expected permission error, got %v", err)
    }

    // Verify statistics
    subMockFS := configFS.(*mockfs.MockFS)
    stats := subMockFS.Stats()
    if stats.Count(mockfs.OpOpen) != 2 {
        t.Errorf("expected 2 opens, got %d", stats.Count(mockfs.OpOpen))
    }
}
```

### Nested Sub-Filesystems
```go
func TestNestedSubFS(t *testing.T) {
    mfs := mockfs.NewMockFS(
        mockfs.Dir("app",
            mockfs.Dir("config",
                mockfs.Dir("prod",
                    mockfs.File("db.json", "{}"),
                ),
            ),
        ),
    )

    // Configure error for deeply nested path
    mfs.ErrorInjector().AddExact(
        mockfs.OpRead,
        "app/config/prod/db.json",
        mockfs.ErrPermission,
        mockfs.ErrorModeAlways,
        0,
    )

    // Create nested sub-filesystems
    appFS, _ := mfs.Sub("app")
    configFS, _ := appFS.(*mockfs.MockFS).Sub("config")
    prodFS, _ := configFS.(*mockfs.MockFS).Sub("prod")

    // Error rule adjusted through all levels
    _, err := fs.ReadFile(prodFS, "db.json")
    if !errors.Is(err, mockfs.ErrPermission) {
        t.Errorf("expected permission error, got %v", err)
    }
}
```

## Performance Testing

### Testing with Latency Profiles
```go
func TestPerformanceProfile(t *testing.T) {
    // Simulate realistic I/O latency
    mfs := mockfs.NewMockFS(
        mockfs.File("data.bin", make([]byte, 1024*1024)), // 1MB
        mockfs.WithPerOperationLatency(map[mockfs.Operation]time.Duration{
            mockfs.OpOpen:  5 * time.Millisecond,
            mockfs.OpRead:  1 * time.Millisecond,  // Per read call
            mockfs.OpClose: 1 * time.Millisecond,
        }),
    )

    start := time.Now()
    file, _ := mfs.Open("data.bin")
    buf := make([]byte, 4096)
    for {
        _, err := file.Read(buf)
        if err == io.EOF {
            break
        }
    }
    file.Close()
    elapsed := time.Since(start)

    // 1MB / 4KB = 256 reads
    // Expected: 5ms (open) + 256ms (reads) + 1ms (close) = ~262ms
    if elapsed < 250*time.Millisecond || elapsed > 300*time.Millisecond {
        t.Errorf("unexpected timing: %v", elapsed)
    }
}
```

### Benchmarking with MockFS
```go
func BenchmarkDataProcessing(b *testing.B) {
    mfs := mockfs.NewMockFS(
        mockfs.File("input.txt", strings.Repeat("test data\n", 1000)),
    )

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        data, err := fs.ReadFile(mfs, "input.txt")
        if err != nil {
            b.Fatal(err)
        }
        _ = ProcessData(data)
    }
}
```
## Best Practices

### 1. Separate Filesystem and Business Logic
```go
// Good: filesystem abstraction
type DataStore interface {
    Save(key string, value []byte) error
    Load(key string) ([]byte, error)
}

func ProcessData(store DataStore, key string) error {
    data, err := store.Load(key)
    if err != nil {
        return err
    }
    // Business logic here
    return store.Save(key, processedData)
}

// Bad: tight coupling to os package
func ProcessData(path string) error {
    data, err := os.ReadFile(path)
    // Hard to test without real filesystem
}
```

### 2. Use Specific Error Types
```go
// Good: test specific error conditions
mfs.FailRead("data.txt", mockfs.ErrCorrupted)
if !errors.Is(err, mockfs.ErrCorrupted) {
    t.Error("expected corrupted data error")
}

// Avoid: generic error checks
if err != nil {
    // Too broad, doesn't verify error type
}
```

### 3. Verify Statistics
```go
// Good: verify actual behavior
stats := mfs.Stats()
if stats.Count(mockfs.OpOpen) != expectedOpens {
    t.Errorf("wrong number of opens: %d", stats.Count(mockfs.OpOpen))
}

// Better: use fluent assertions
mfs.Stats().Expect().
    Count(mockfs.OpOpen, 3).
    Success(mockfs.OpRead, 10).
    NoFailures().
    Assert(t)
```

### 4. Test Edge Cases
```go
func TestEdgeCases(t *testing.T) {
    mfs := mockfs.NewMockFS(mockfs.File("file.txt", "data"))

    tests := []struct {
        name string
        test func(*testing.T)
    }{
        {"empty read", func(t *testing.T) {
            file, _ := mfs.Open("file.txt")
            n, _ := file.Read(make([]byte, 0))
            if n != 0 {
                t.Error("expected 0 bytes read")
            }
        }},
        {"double close", func(t *testing.T) {
            file, _ := mfs.Open("file.txt")
            file.Close()
            err := file.Close()
            if !errors.Is(err, fs.ErrClosed) {
                t.Error("expected ErrClosed on double close")
            }
        }},
        {"read after close", func(t *testing.T) {
            file, _ := mfs.Open("file.txt")
            file.Close()
            _, err := file.Read(make([]byte, 10))
            if !errors.Is(err, fs.ErrClosed) {
                t.Error("expected ErrClosed on read after close")
            }
        }},
    }

    for _, tt := range tests {
        t.Run(tt.name, tt.test)
    }
}
```

### 5. Clean Up in Defer
```go
func TestWithCleanup(t *testing.T) {
    mfs := mockfs.NewMockFS(mockfs.File("file.txt", "data"))
    file, err := mfs.Open("file.txt")
    if err != nil {
        t.Fatal(err)
    }
    defer file.Close() // Always clean up

    // Test code here
}
```

### 6. Use Table-Driven Tests
```go
func TestMultipleScenarios(t *testing.T) {
    tests := []struct {
        name       string
        setupError func(*mockfs.MockFS)
        wantErr    error
    }{
        {
            name: "permission denied",
            setupError: func(m *mockfs.MockFS) {
                m.FailOpen("file.txt", mockfs.ErrPermission)
            },
            wantErr: mockfs.ErrPermission,
        },
        {
            name: "file not found",
            setupError: func(m *mockfs.MockFS) {
                m.MarkNonExistent("file.txt")
            },
            wantErr: mockfs.ErrNotExist,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            mfs := mockfs.NewMockFS(mockfs.File("file.txt", "data"))
            tt.setupError(mfs)

            _, err := YourFunction(mfs)
            if !errors.Is(err, tt.wantErr) {
                t.Errorf("got %v, want %v", err, tt.wantErr)
            }
        })
    }
}
```

## Additional Resources

- **[API Reference](https://pkg.go.dev/github.com/balinomad/go-mockfs/v2)** – Complete API documentation
- **[README](README.md)** – Quick start guide
- **[Migration Guide](MIGRATION-v1-to-v2.md)** – Upgrading from v1
- **[Example Tests](https://github.com/balinomad/go-mockfs/tree/v2)** – Runnable examples in repository