# go-mockfs

*A flexible and feature-rich filesystem mocking library for Go, built on top of `testing/fstest.MapFS` with powerful error injection and behavior simulation capabilities.*

Perfect for testing code that interacts with the filesystem:
- File I/O error handling
- Filesystem edge cases
- Race conditions
- Performance degradation scenarios
- Path handling issues

## ✨ Features

- Simulates all standard filesystem operations (Open, Read, Write, Stat, ReadDir, Close)
- Precise error injection with multiple strategies:
  - Exact path matching
  - Regular expression pattern matching
  - Operation-specific errors
  - One-time errors
  - Errors after N successful operations
- Latency simulation for testing timeout handling
- Write simulation (modify the filesystem in tests)
- Operation statistics tracking for verification
- Full implementation of the `fs` interfaces:
  - `fs.FS`
  - `fs.ReadDirFS`
  - `fs.ReadFileFS`
  - `fs.StatFS`
  - `fs.SubFS`

## 🚀 Usage

### Basic Setup

```go
import "github.com/balinomad/go-mockfs"

func TestMyFunction(t *testing.T) {
    // Create initial filesystem data
    fsData := map[string]*mockfs.MapFile{
        "config.json": {
            Data:    []byte(`{"setting": "value"}`),
            Mode:    0644,
            ModTime: time.Now(),
        },
        "data": {
            Mode:    fs.ModeDir | 0755,
            ModTime: time.Now(),
        },
    }

    // Create mockfs with 10ms simulated latency
    mock := mockfs.NewMockFS(fsData, mockfs.WithLatency(10*time.Millisecond))

    // Test your code that uses the filesystem
    result := YourFunction(mock)

    // Check stats
    stats := mock.GetStats()
    if stats.ReadCalls != 1 {
        t.Errorf("expected 1 read call, got %d", stats.ReadCalls)
    }
}
```

### Error Injection

```go
// Inject errors for specific paths and operations
mock.AddStatError("config.json", fs.ErrPermission)
mock.AddOpenError("secret.txt", fs.ErrNotExist)

// Inject errors using patterns
mock.AddErrorPattern(mockfs.OpRead, `\.log$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)

// Inject one-time errors
mock.AddOpenErrorOnce("data.db", mockfs.ErrTimeout)

// Inject errors after N successful operations
mock.AddReadErrorAfterN("large.bin", io.EOF, 3)

// Mark paths as non-existent
mock.MarkNonExistent("missing.txt", "config/old.json")
mock.MarkDirectoryNonExistent("temp")

// Add error for all operations on a path
mock.AddPathError("unstable.dat", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
```

### Write Simulation

```go
// Enable write operations with a custom callback
mock := mockfs.NewMockFS(fsData, mockfs.WithWriteCallback(myWriteHandler))

// Or use the built-in write simulator
mock := mockfs.NewMockFS(fsData, mockfs.WithWritesEnabled())

// Then write to files
file, _ := mock.Open("newfile.txt")
writer := file.(io.Writer)
writer.Write([]byte("hello world"))
file.Close()
```

### Adding or Modifying Files

```go
// Add text files
mock.AddFileString("config.yml", "key: value", 0644)

// Add binary files
mock.AddFileBytes("image.png", pngData, 0644)

// Add directories
mock.AddDirectory("logs", 0755)

// Remove paths
mock.RemovePath("temp/cache.dat")
```

## 📌 Installation

```bash
go get github.com/yourusername/mockfs@latest
```

## 📘 API Reference

### Constructors and Options

| Function/Option | Description |
|-----------------|-------------|
| `NewMockFS(data, ...options)` | Creates a new mock filesystem |
| `WithLatency(duration)` | Adds simulated operation latency |
| `WithWriteCallback(func)` | Enables writes with custom handler |
| `WithWritesEnabled()` | Enables writes to modify the internal map |

### Error Injection

| Method | Description |
|--------|-------------|
| `AddErrorExactMatch(op, path, err, mode, successes)` | Adds error for exact path and operation |
| `AddErrorPattern(op, pattern, err, mode, successes)` | Adds error for paths matching a pattern |
| `AddPathError(path, err, mode, successes)` | Adds error for all operations on a path |
| `AddPathErrorPattern(pattern, err, mode, successes)` | Adds error for all operations on matching paths |
| `AddStatError(path, err)` | Adds error for Stat operation |
| `AddOpenError(path, err)` | Adds error for Open operation |
| `AddReadError(path, err)` | Adds error for Read operation |
| `AddWriteError(path, err)` | Adds error for Write operation |
| `AddReadDirError(path, err)` | Adds error for ReadDir operation |
| `AddCloseError(path, err)` | Adds error for Close operation |
| `AddOpenErrorOnce(path, err)` | Adds one-time error for Open operation |
| `AddReadErrorAfterN(path, err, n)` | Adds error after N successful reads |
| `MarkNonExistent(paths...)` | Marks paths as non-existent |
| `MarkDirectoryNonExistent(dirPath)` | Marks directory and contents as non-existent |
| `ClearErrors()` | Clears all configured errors |

### Error Modes

| Mode | Description |
|------|-------------|
| `ErrorModeAlways` | Error is returned on every operation |
| `ErrorModeOnce` | Error is returned once, then cleared |
| `ErrorModeAfterSuccesses` | Error is returned after N successful operations |

### Predefined Errors

| Error | Description |
|-------|-------------|
| `ErrInvalid` | Invalid argument error |
| `ErrPermission` | Permission denied error |
| `ErrExist` | File already exists error |
| `ErrNotExist` | File does not exist error |
| `ErrClosed` | File already closed error |
| `ErrDiskFull` | Disk full error |
| `ErrTimeout` | Operation timeout error |
| `ErrCorrupted` | Corrupted data error |
| `ErrTooManyHandles` | Too many open files error |
| `ErrNotDir` | Not a directory error |

### File and Directory Management

| Method | Description |
|--------|-------------|
| `AddFileString(path, content, mode)` | Adds a text file |
| `AddFileBytes(path, content, mode)` | Adds a binary file |
| `AddDirectory(path, mode)` | Adds a directory |
| `RemovePath(path)` | Removes a file or directory |
| `GetStats()` | Returns operation statistics |
| `ResetStats()` | Resets all operation counters |

## 🔍 Implementation Details

The mockfs library works by wrapping `testing/fstest.MapFS` with error injection and behavior simulation capabilities:

1. File operations go through wrappers that check for configured errors
2. If an error is configured for the operation and path, it's returned
3. Otherwise, the operation proceeds with the underlying MapFS
4. `MockFile` wraps `fs.File` to intercept Read/Write/Close operations
5. Operations statistics are tracked for test verification

## 🧪 Advanced Testing Patterns

### Testing Error Recovery

```go
func TestErrorRecovery(t *testing.T) {
    mock := mockfs.NewMockFS(nil)
    mock.AddFileString("data.txt", "initial content", 0644)

    // First read will fail, second will succeed
    mock.AddReadErrorOnce("data.txt", io.ErrUnexpectedEOF)

    // Test that your code retries on error
    result := YourFunctionThatRetries(mock)

    if !result.Success {
        t.Error("function should have recovered from error")
    }
}
```

### Testing Timeout Handling

```go
func TestTimeoutHandling(t *testing.T) {
    mock := mockfs.NewMockFS(nil, mockfs.WithLatency(2*time.Second))

    // Set a context with timeout
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    // Test your function handles timeout properly
    err := YourFunctionWithContext(ctx, mock)

    if !errors.Is(err, context.DeadlineExceeded) {
        t.Error("function should have timed out")
    }
}
```

## ⚖️ License

[License Type] — see `LICENSE` file for details.