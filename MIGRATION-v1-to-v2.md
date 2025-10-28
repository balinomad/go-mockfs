# `movkfs` *v1* to *v2* Migration Guide

## Table of Contents

1. [Overview](#overview)
2. [Architectural Changes](#architectural-changes)
3. [Breaking Changes Summary](#breaking-changes-summary)
4. [Step-by-Step Migration](#step-by-step-migration)
5. [New Features](#new-features)
6. [Common Migration Patterns](#common-migration-patterns)
7. [Migration Checklist](#migration-checklist)
8. [Getting Help](#getting-help)

## 1. Overview<a id="overview"></a>

*Version 2.0.0* represents a major architectural redesign of the `mockfs` library focused on improved API ergonomics, enhanced testability, and more flexible configuration patterns.

### 1.1. What Changed

***v1* Architecture:**
- Monolithic design with tight coupling
- Wrapped `fstest.MapFS` directly as embedded field
- `MockFile` was a thin wrapper around `fs.File` with reference back to parent `MockFS`
- Error injection via `ErrorConfig` structs with atomic counters
- Stats exposed as mutable struct
- Supported error modes: Always, Once, AfterSuccesses
- Supported path matching: exact strings, regex patterns
- Latency simulation: global or per-operation via `map[Operation]time.Duration`
- Limited `fs.SubFS` support

***v2* Architecture:**
- Uses `MapFile` for file storage (not embedded `fstest.MapFS`)
- `MockFile` is a full implementation managing its own state, data, and position
- Error injection via `ErrorRule` with `PathMatcher` interface hierarchy (Exact/Glob/Regexp/Wildcard matchers)
- Stats exposed via immutable `Stats` interface with separate `StatsRecorder` for mutation
- Separate stats tracking for filesystem-level vs file-handle-level operations
- Adds `WritableFS` interface with mutating filesystem operations (Mkdir, Remove, Rename, WriteFile)
- Enhanced error injection API with operation-specific and cross-operation helpers
- Latency simulation redesigned with `LatencySimulator` interface and `Once()`/`Async()` options
- Full `fs.SubFS` support with automatic path/error rule adjustment for sub-filesystems

### 1.2. Key Improvements

- **Cleaner separation of concerns**: File handles maintain independent state from filesystem
- **Improved statistics**: Filesystem-level vs. file-handle-level operations tracked independently; stats snapshot pattern enables clean before/after comparisons
- **More flexible error injection**: Path matchers enable glob patterns alongside exact/regex matching
- **Enhanced write support**: First-class `WritableFS` interface with proper directory operations
- **Better latency control**: Latency simulator supports per-operation latency, serialized and async modes, independent file handle state

## 2. Architectural Changes<a id="architectural-changes"></a>

### 2.1 File Storage

**v1**
```go
type MockFS struct {
    fsys fstest.MapFS  // Embedded
    // ...
}
```

**v2**
```go
type MockFS struct {
    files map[string]*fstest.MapFile  // Direct map ownership
    // ...
}
```

**Impact:** *v2* has full control over file lifecycle and modification. The underlying `fstest.MapFS` is no longer exposed.

### 2.2 MockFile Implementation

**v1**
```go
type MockFile struct {
    file   fs.File    // Wrapped underlying file
    name   string
    mockFS *MockFS    // Parent reference
    closed bool
    writeFile io.Writer
}
```

**v2**
```go
type mockFile struct {
    mapFile        *fstest.MapFile  // Direct data ownership
    name           string
    position       int64            // Read position tracking
    mu             sync.Mutex       // Per-file concurrency
    closed         bool
    writeMode      writeMode
    readDirHandler func(int) ([]fs.DirEntry, error)
    latency        LatencySimulator  // Per-handle instance
    stats          StatsRecorder     // Per-handle stats
    injector       ErrorInjector     // Shared injector
}
```

**Impact:**
- *v2* `MockFile` is a complete file implementation, not a wrapper
- Each file handle has independent latency and stats tracking
- File position and write modes managed internally
- No direct access to underlying `fs.File`

### 2.3. Statistics Architecture

**v1**: Single `Stats` struct with public fields
```go
type Stats struct {
    StatCalls    int
    OpenCalls    int
    ReadCalls    int
    WriteCalls   int
    ReadDirCalls int
    CloseCalls   int
}

func (m *MockFS) GetStats() Stats {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.stats  // Returns copy of struct
}
```

**v2**: `Stats` and `StatsRecorder` interfaces, split by scope
```go
// Read-only interface
type Stats interface {
    Count(op Operation) int
    CountSuccess(op Operation) int
    CountFailure(op Operation) int
    BytesRead() int
    BytesWritten() int
    HasFailures() bool
    Operations() int
    Failures() []Operation
    Delta(other Stats) Stats
    Equal(other Stats) bool
    String() string
}

// Mutation interface (internal)
type StatsRecorder interface {
    Stats
    Record(op Operation, bytes int, err error)
    Set(op Operation, total int, failures int)
    SetBytes(read int, written int)
    Reset()
    Snapshot() Stats
}

func (m *MockFS) Stats() Stats {
    return m.stats.Snapshot()  // Returns immutable snapshot
}
```

**Impact:**
- *v2* stats are immutable snapshots, enabling safe concurrent reads
- Success/failure tracking added
- Byte counters added for Read/Write operations
- Stats comparison via `Delta()` and `Equal()`
- Separate tracking: `MockFS.Stats()` for filesystem ops, `MockFile.Stats()` for file-handle ops

### 2.4. Error Injection Architecture

**v1**
```go
type ErrorConfig struct {
    Error    error
    Mode     ErrorMode
    Counter  atomic.Int64
    Matches  []string           // Exact paths
    Patterns []*regexp.Regexp   // Regex patterns
    used     atomic.Bool
}

// Direct configuration methods
func (m *MockFS) AddError(op Operation, config *ErrorConfig)
func (m *MockFS) AddErrorExactMatch(op Operation, path string, err error, mode ErrorMode, successes int)
func (m *MockFS) AddErrorPattern(op Operation, pattern string, err error, mode ErrorMode, successes int) error
```

**v2**
```go
// Path matching abstraction
type PathMatcher interface {
    Matches(path string) bool
    CloneForSub(prefix string) PathMatcher
}

type ErrorRule struct {
    Err      error
    Mode     ErrorMode
    AfterN   uint64
    matchers []PathMatcher  // Composable matchers
    usedOnce atomic.Bool
    hits     atomic.Uint64
}

// ErrorInjector interface
type ErrorInjector interface {
    Add(op Operation, rule *ErrorRule)
    AddExact(op Operation, path string, err error, mode ErrorMode, after int)
    AddGlob(op Operation, pattern string, err error, mode ErrorMode, after int) error
    AddRegexp(op Operation, pattern string, err error, mode ErrorMode, after int) error
    AddAll(op Operation, err error, mode ErrorMode, after int)
    AddExactForAllOps(path string, err error, mode ErrorMode, after int)
    AddGlobForAllOps(pattern string, err error, mode ErrorMode, after int) error
    AddRegexpForAllOps(pattern string, err error, mode ErrorMode, after int) error
    AddAllForAllOps(err error, mode ErrorMode, after int)
    Clear()
    CheckAndApply(op Operation, path string) error
    CloneForSub(prefix string) ErrorInjector
    GetAll() map[Operation][]*ErrorRule
}
```

**Impact:**
- *v2* uses `PathMatcher` interface hierarchy (Exact/Glob/Regexp/Wildcard)
- Glob patterns via `path.Match` semantics (e.g., `"dir/*.txt"`)
- Cross-operation helpers (`AddExactForAllOps`, `AddGlobForAllOps`, etc.)
- `CloneForSub()` enables automatic rule adjustment for sub-filesystems
- `ErrorInjector` interface enables custom implementations and testing

### 2.5. Latency Simulation

**v1**: Simple duration, serialized by default
```go
type MockFS struct {
    latency time.Duration  // Single global duration
    // ...
}

func WithLatency(d time.Duration) Option
```

**v2**: `LatencySimulator` interface with advanced control
```go
type LatencySimulator interface {
    Simulate(op Operation, opts ...SimOpt)
    Reset()
    Clone() LatencySimulator
}

func NewLatencySimulator(duration time.Duration) LatencySimulator
func NewLatencySimulatorPerOp(durations map[Operation]time.Duration) LatencySimulator

// Simulation options
func Once() SimOpt       // Apply latency at most once per operation type
func Async() SimOpt      // Release lock before sleeping
func OnceAsync() SimOpt  // Both Once and Async
```

**Impact:**
- *v2* latency is per-file-handle with independent `Once()` state
- `Async()` mode enables non-serialized I/O simulation
- `Clone()` gives each file handle independent tracking while preserving duration config
- `Reset()` clears `Once()` state (automatically called on file close)

### 2.6 WritableFS Interface

**v1**: Write support via `WithWritesEnabled()` option and callback pattern.

**v2**
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

**Impact:**
- *v2* provides full write operations as first-class methods
- Proper directory hierarchy management with `MkdirAll`
- `Rename` supports moving directories with all children
- Write modes: append, overwrite, or read-only via options

### 2.7 SubFS Support

**v1**: `Sub()` implementation present but limited path adjustment logic.

**v2**: Full `fs.SubFS` support with:
- Automatic path adjustment for error rules via `PathMatcher.CloneForSub()`
- Independent stats for sub-filesystems
- Shared error injector with adjusted matchers
- Proper handling of directory-relative paths

## 3. Breaking Changes Summary<a id="breaking-changes-summary"></a>

### 3.1. Removed Methods

These methods were removed from `MockFS`:

| *v1* | *v2* | Migration |
|------|------|-----------|
| `NewMockFS(data map[string]*MapFile, opts...)` | `NewMockFS(initial map[string]*MapFile, opts...)` | Rename parameter only; behavior unchanged |
| `WithWritesEnabled()` | `WithOverwrite()` / `WithAppend()` | Use explicit write mode option |
| `WithWriteCallback(func)` | N/A | Use `WritableFS` methods directly |
| `GetStats()` | `Stats()` | Rename only |
| `AddError(op, *ErrorConfig)` | `ErrorInjector().Add(op, *ErrorRule)` | Use `ErrorInjector()` accessor |
| `AddErrorExactMatch(...)` | `ErrorInjector().AddExact(...)` | Use `ErrorInjector()` accessor |
| `AddErrorPattern(...)` | `ErrorInjector().AddRegexp(...)` | Use `ErrorInjector()` accessor |
| `AddPathError(...)` | `ErrorInjector().AddExactForAllOps(...)` | Use cross-op helper |
| `AddPathErrorPattern(...)` | `ErrorInjector().AddRegexpForAllOps(...)` | Use cross-op helper |

### 3.2 Type Changes

| Type | *v1* | *v2* | Impact |
|------|------|------|--------|
| `Stats` | Struct with exported fields | Interface with methods | Cannot access fields directly; use methods |
| `MockFile` | Struct with exported `file fs.File` field | Unexported struct, full implementation | Cannot access underlying file; use `MockFile` methods |
| `Operation` | Constants `OpStat=0, OpOpen, OpRead, OpWrite, OpReadDir, OpClose` | Constants `OpStat=1, OpOpen, OpRead, OpWrite, OpClose, OpReadDir, OpMkdir, OpMkdirAll, OpRemove, OpRemoveAll, OpRename` | `OpReadDir` moved; new operations added; `OpUnknown=0` added |

### 3.3 Behavior Changes

- **Stats mutation**: *v1* `GetStats()` returned mutable copy; *v2* `Stats()` returns immutable snapshot
- **File handle independence**: *v2* each file has independent latency/stats; *v1* shared global latency
- **Error injection scope**: *v2* distinguishes filesystem-level ops from file-handle ops
- **Path cleaning**: *v2* consistently uses `path.Clean()` for all paths; *v1* used `filepath.Clean()`
- **Operation tracking**: *v2* tracks success/failure separately; *v1* tracked calls only
- **Byte tracking**: *v2* tracks `BytesRead()`/`BytesWritten()`; *v1* had no byte counters

### 3.4 New Required Steps

- **Access error injector explicitly**: `mockFS.ErrorInjector().AddExact(...)` instead of `mockFS.AddErrorExactMatch(...)`
- **Use Stats interface methods**: `stats.Count(OpRead)` instead of `stats.ReadCalls`
- **Choose write mode explicitly**: `WithOverwrite()` or `WithAppend()` instead of `WithWritesEnabled()`
- **Use operation constants correctly**: `OpReadDir` renumbered; check all operation references

### 3.5. Convenience Methods

*v2* provides convenience methods directly on `MockFS` that wrap `ErrorInjector()` calls. These replace *v1*'s `Add*Error` methods while retaining the same behavior.

| *v1* Method | *v2* Equivalent |
|-------------|-----------------|
| `AddStatError(path, err)` | `FailStat(path, err)` |
| `AddOpenError(path, err)` | `FailOpen(path, err)` |
| `AddReadError(path, err)` | `FailRead(path, err)` |
| `AddWriteError(path, err)` | `FailWrite(path, err)` |
| `AddReadDirError(path, err)` | `FailReadDir(path, err)` |
| `AddCloseError(path, err)` | `FailClose(path, err)` |
| `AddOpenErrorOnce(path, err)` | `FailOpenOnce(path, err)` |
| `AddReadErrorAfterN(path, err, n)` | `FailReadAfter(path, err, n)` |

New convenience methods in *v2* include:

| *v2* Method | Description |
|-------------|-------------|
| `FailStatOnce(path, err)` | Stat fails once |
| `FailReadOnce(path, err)` | Read fails once |
| `FailWriteOnce(path, err)` | Write fails once |
| `FailReadDirOnce(path, err)` | ReadDir fails once |
| `FailCloseOnce(path, err)` | Close fails once |
| `FailMkdir(path, err)` / `FailMkdirOnce(path, err)` | Mkdir errors |
| `FailMkdirAll(path, err)` / `FailMkdirAllOnce(path, err)` | MkdirAll errors |
| `FailRemove(path, err)` / `FailRemoveOnce(path, err)` | Remove errors |
| `FailRemoveAll(path, err)` / `FailRemoveAllOnce(path, err)` | RemoveAll errors |
| `FailRename(path, err)` / `FailRenameOnce(path, err)` | Rename errors |

## 4. Step-by-Step Migration<a id="step-by-step-migration"></a>

### 4.1. Creating a Mock Filesystem

**Before (v1)**:
```go
mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "file.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
    "dir":      {Mode: fs.ModeDir | 0755, ModTime: time.Now()},
}, mockfs.WithLatency(10*time.Millisecond))
```

**After (v2)**:
```go
mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "file.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
    "dir":      {Mode: fs.ModeDir | 0755, ModTime: time.Now()},
}, mockfs.WithLatency(10*time.Millisecond))
```

✅ **No change required** for basic creation.

### 4.2. Adding Files

**Before (v1)**:
```go
mfs.AddFileString("config.json", `{"key": "value"}`, 0644)
mfs.AddFileBytes("data.bin", []byte{0x00, 0x01}, 0644)
```

**After (v2)**:
```go
// AddFileString renamed to AddFile, now returns error
err := mfs.AddFile("config.json", `{"key": "value"}`, 0644)

// AddFileBytes now returns error
err = mfs.AddFileBytes("data.bin", []byte{0x00, 0x01}, 0644)
```

### 4.3. Adding Directories

**Before (v1)**:
```go
mfs.AddDirectory("logs", 0755)
```

**After (v2)**:
```go
// Renamed method, now returns error
err := mfs.AddDir("logs", 0755)
```

### 4.4. Removing Paths

**Before (v1)**:
```go
mfs.RemovePath("temp.txt")
```

**After (v2)**:
```go
err := mfs.RemovePath("temp.txt") // Now returns error
```

### 4.5. Error Injection - Simple Cases

**Before (v1)**:
```go
mfs.AddStatError("config.json", fs.ErrPermission)
mfs.AddOpenError("secret.txt", fs.ErrNotExist)
mfs.AddReadError("data.txt", io.ErrUnexpectedEOF)
```

**After (v2)**:
```go
// Use renamed convenience methods
mfs.FailStat("config.json", fs.ErrPermission)
mfs.FailOpen("secret.txt", fs.ErrNotExist)
mfs.FailRead("data.txt", io.ErrUnexpectedEOF)
```

### 4.6. Error Injection - Pattern Matching

**Before (v1)**:
```go
// Regex pattern
err := mfs.AddErrorPattern(mockfs.OpRead, `\.log$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
```

**After (v2)**:
```go
// Access ErrorInjector interface for regex
err := mfs.ErrorInjector().AddRegexp(mockfs.OpRead, `\.log$`, mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)

// New: Glob pattern support
err = mfs.ErrorInjector().AddGlob(mockfs.OpRead, "*.log", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
```

### 4.7. Error Injection - One-Time Errors

**Before (v1)**:
```go
mfs.AddOpenErrorOnce("data.db", mockfs.ErrTimeout)
```

**After (v2)**:
```go
// Use renamed convenience method
mfs.FailOpenOnce("data.db", mockfs.ErrTimeout)

// Or via ErrorInjector
mfs.ErrorInjector().AddExact(mockfs.OpOpen, "data.db", mockfs.ErrTimeout, mockfs.ErrorModeOnce, 0)
```

### 4.8. Error Injection - After N Successes

**Before (v1)**:
```go
mfs.AddReadErrorAfterN("large.bin", io.EOF, 3)
```

**After (v2)**:
```go
// Use renamed convenience method
mfs.FailReadAfter("large.bin", io.EOF, 3)

// Or via ErrorInjector
mfs.ErrorInjector().AddExact(mockfs.OpRead, "large.bin", io.EOF, mockfs.ErrorModeAfterSuccesses, 3)
```

### 4.9. Error Injection - All Operations on a Path

**Before (v1)**:
```go
mfs.AddPathError("unstable.dat", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
```

**After (v2)**:
```go
// Via ErrorInjector
mfs.ErrorInjector().AddExactForAllOps("unstable.dat", mockfs.ErrCorrupted, mockfs.ErrorModeAlways, 0)
```

### 4.10. Marking Paths as Non-Existent

**Before (v1)**:
```go
mfs.MarkNonExistent("missing.txt", "old.json")
mfs.MarkDirectoryNonExistent("temp")
```

**After (v2)**:
```go
// MarkNonExistent still exists (same API)
mfs.MarkNonExistent("missing.txt", "old.json")

// MarkDirectoryNonExistent removed - use combination of RemoveAll and error injection
mfs.RemoveAll("temp")
// Inject errors for directory and all potential paths under it
mfs.ErrorInjector().AddExact(mockfs.OpOpen, "temp", fs.ErrNotExist, mockfs.ErrorModeAlways, 0)
mfs.ErrorInjector().AddRegexp(mockfs.OpOpen, `^temp/`, fs.ErrNotExist, mockfs.ErrorModeAlways, 0)
```

### 4.11. Clearing Errors

**Before (v1)**:
```go
mfs.ClearErrors()
```

**After (v2)**:
```go
mfs.ClearErrors() // Same API
```

✅ **No change required**.

### 4.12. Statistics - Reading

**Before (v1)**:
```go
// All operations tracked in MockFS stats
stats := mfs.GetStats()
if stats.OpenCalls != 1 {
    t.Errorf("expected 1 open, got %d", stats.OpenCalls)
}
if stats.ReadCalls != 2 {
    t.Errorf("expected 2 reads, got %d", stats.ReadCalls)
}
if stats.StatCalls != 0 {
    t.Errorf("expected 0 stats, got %d", stats.StatCalls)
}
```

**After (v2)**:
```go
// Stats split: MockFS tracks filesystem ops, MockFile tracks file-handle ops
// Stats is now an interface with methods

// Filesystem-level operations
stats := mfs.Stats()
if stats.Count(mockfs.OpOpen) != 1 {
    t.Errorf("expected 1 open, got %d", stats.Count(mockfs.OpOpen))
}
if stats.Count(mockfs.OpStat) != 0 {
    t.Errorf("expected 0 stats, got %d", stats.Count(mockfs.OpStat))
}

// File-handle operations (Read, Write, Close on opened files)
file, _ := mfs.Open("file.txt")
buf := make([]byte, 100)
file.Read(buf)
file.Read(buf)

// Access MockFile stats
mockFile := file.(mockfs.MockFile)
fileStats := mockFile.Stats()
if fileStats.Count(mockfs.OpRead) != 2 {
    t.Errorf("expected 2 reads, got %d", fileStats.Count(mockfs.OpRead))
}

// Additional methods available
successes := fileStats.CountSuccess(mockfs.OpRead)
failures := fileStats.CountFailure(mockfs.OpRead)
totalOps := stats.Operations()
hasErrors := stats.HasFailures()
bytesRead := fileStats.BytesRead()
```

### 4.13. Statistics - Reset

**Before (v1)**:
```go
mfs.ResetStats()
```

**After (v2)**:
```go
mfs.ResetStats() // Same API
```

✅ **No change required**.

### 4.14. Write Operations - Basic

**Before (v1)**:
```go
// Via WriteCallback
callback := func(path string, data []byte) error {
    // Custom write logic
    return nil
}
mfs := mockfs.NewMockFS(data, mockfs.WithWriteCallback(callback))

// Or built-in
mfs := mockfs.NewMockFS(data, mockfs.WithWritesEnabled())

// Write via MockFile.Write() if file supports io.Writer
file, _ := mfs.Open("file.txt")
n, err := file.(io.Writer).Write([]byte("data"))
```

**After (v2)**:
```go
// Use WriteFile method (part of WritableFS interface)
mfs := mockfs.NewMockFS(nil, mockfs.WithCreateIfMissing(true))
err := mfs.WriteFile("new.txt", []byte("content"), 0644)

// Control write mode
mfs = mockfs.NewMockFS(nil, mockfs.WithOverwrite()) // default
mfs = mockfs.NewMockFS(nil, mockfs.WithAppend())
mfs = mockfs.NewMockFS(nil, mockfs.WithReadOnly())

// Write via MockFile.Write()
file, _ := mfs.Open("file.txt")
n, err := file.(io.Writer).Write([]byte("data"))
```

### 4.15. Write Operations - Advanced

**Before (v1)**:
```go
// Directory operations via manual map manipulation
mfs := mockfs.NewMockFS(nil)
mfs.AddDirectory("dir", 0755)
mfs.AddDirectory("dir/subdir", 0755)
mfs.AddDirectory("dir/subdir/nested", 0755)

// No built-in Remove, Rename operations
// Must manipulate internal map via RemovePath
mfs.RemovePath("file.txt")
```

**After (v2)**:
```go
// Full WritableFS interface
mfs := mockfs.NewMockFS(nil)
err := mfs.Mkdir("dir", 0755)
err = mfs.MkdirAll("dir/subdir/nested", 0755)
err = mfs.Remove("file.txt")
err = mfs.RemoveAll("dir")
err = mfs.Rename("old.txt", "new.txt")
err = mfs.WriteFile("file.txt", data, 0644)
```

### 4.16. Latency - Per-Operation

**Before (v1)**:
```go
// Only uniform latency supported
mfs := mockfs.NewMockFS(nil, mockfs.WithLatency(100*time.Millisecond))
```

**After (v2)**:
```go
// Per-operation latency configuration
mfs := mockfs.NewMockFS(nil, mockfs.WithPerOperationLatency(map[mockfs.Operation]time.Duration{
    mockfs.OpRead:  100 * time.Millisecond,
    mockfs.OpWrite: 200 * time.Millisecond,
    mockfs.OpStat:  10 * time.Millisecond,
}))
```

### 4.17. Using Standalone MockFile

**Before (v1)**:
```go
// MockFile requires MockFS context - no standalone constructor
// Must create via MockFS.Open()
mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "test.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
})
file, _ := mfs.Open("test.txt")

// Test a function
func ProcessFile(f io.ReadWriter) error { /* ... */ }
err := ProcessFile(file)
```

**After (v2)**:
```go
// Create standalone file for testing functions that accept io.ReadWriter
file := mockfs.NewMockFileFromString("test.txt", "content")

// With options
file = mockfs.NewMockFileFromBytes("test.txt", data,
    mockfs.WithFileLatency(10*time.Millisecond),
    mockfs.WithFileReadOnly(),
)

// Test a function
func ProcessFile(f io.ReadWriter) error { /* ... */ }
err := ProcessFile(file)
```

### 4.18. File Statistics

**Before (v1)**:
```go
// All operations counted in MockFS stats (including file-handle operations)
mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "file.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
})
f, _ := mfs.Open("file.txt")
buf := make([]byte, 100)
f.Read(buf)
f.Close()

stats := mfs.GetStats()
// stats.OpenCalls == 1
// stats.ReadCalls == 1
// stats.CloseCalls == 1
```

**After (v2)**:
```go
// File statistics are separate from filesystem statistics
mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "file.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
})
f, _ := mfs.Open("file.txt")
buf := make([]byte, 100)
f.Read(buf)
f.Close()

// MockFS stats - filesystem operations only
fsStats := mfs.Stats()
fsStats.Count(mockfs.OpOpen) // 1 (the Open call on MockFS)

// MockFile stats - file handle operations only
mockFile := f.(mockfs.MockFile)
fileStats := mockFile.Stats()
fileStats.Count(mockfs.OpRead)  // 1 (the Read call on file handle)
fileStats.Count(mockfs.OpClose) // 1 (the Close call on file handle)
fileStats.BytesRead()           // Number of bytes read
```

## 5. New Features<a id="new-features"></a>

### 5.1. Glob Pattern Matching

*v1* only supported regex patterns. *v2* adds glob pattern matching using `path.Match` semantics.
```go
// Match all .txt files
mfs.ErrorInjector().AddGlob(mockfs.OpRead, "*.txt", io.EOF, mockfs.ErrorModeAlways, 0)

// Match nested paths
mfs.ErrorInjector().AddGlob(mockfs.OpOpen, "logs/*.log", fs.ErrPermission, mockfs.ErrorModeAlways, 0)

// Apply to all operations
mfs.ErrorInjector().AddGlobForAllOps("temp/*", fs.ErrNotExist, mockfs.ErrorModeAlways, 0)
```

### 5.2. Wildcard Matcher

*v2* introduces `WildcardMatcher` to match all paths without specifying patterns.
```go
// Apply error to all paths for a specific operation
mfs.ErrorInjector().AddAll(mockfs.OpWrite, mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

// Apply error to all paths for all operations
mfs.ErrorInjector().AddAllForAllOps(mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0)

// Create custom rule with wildcard
rule := mockfs.NewErrorRule(io.EOF, mockfs.ErrorModeAlways, 0, mockfs.NewWildcardMatcher())
mfs.ErrorInjector().Add(mockfs.OpRead, rule)
```

### 5.3. Latency Simulation Options

*v1* had basic latency simulation with global duration only. *v2* adds advanced control with simulation options.
```go
// Create a simulator
sim := mockfs.NewLatencySimulator(50 * time.Millisecond)

// Use Once mode - latency only on first call
sim.Simulate(mockfs.OpRead, mockfs.Once())

// Use Async mode - non-blocking (releases lock before sleeping)
sim.Simulate(mockfs.OpRead, mockfs.Async())

// Combine options
sim.Simulate(mockfs.OpRead, mockfs.OnceAsync())

// Reset state (clears Once tracking)
sim.Reset()

// Clone simulator (independent state, same duration config)
cloned := sim.Clone()

// Use with MockFS
mfs := mockfs.NewMockFS(nil, mockfs.WithLatencySimulator(sim))

// Or create directly with per-operation durations
mfs = mockfs.NewMockFS(nil, mockfs.WithPerOperationLatency(map[mockfs.Operation]time.Duration{
    mockfs.OpRead:  100 * time.Millisecond,
    mockfs.OpWrite: 200 * time.Millisecond,
}))
```

### 5.4. Stats Interface Methods

*v1* exposed stats as a struct with public fields. *v2* provides a `Stats` interface with methods for querying and comparing statistics.
```go
stats := mfs.Stats()

// Count methods
total := stats.Count(mockfs.OpRead)        // Total calls
successes := stats.CountSuccess(mockfs.OpRead) // Successful calls
failures := stats.CountFailure(mockfs.OpRead)  // Failed calls

// Byte counters (new in v2)
bytesRead := stats.BytesRead()
bytesWritten := stats.BytesWritten()

// Aggregate methods
totalOps := stats.Operations()  // Total operation count across all types
hasErrors := stats.HasFailures() // Any failures?
failedOps := stats.Failures()    // []Operation with at least one failure

// Comparison (new in v2)
before := mfs.Stats()
// ... perform operations ...
after := mfs.Stats()
delta := after.Delta(before)  // Difference between snapshots
if !after.Equal(before) {
    // Stats changed
}

// Human-readable summary
fmt.Println(stats.String()) // "Stats{Ops: 10 (2 failed), Bytes: 1024 read, 512 written}"
```

### 5.5. WritableFS Interface

*v1* required manual map manipulation or write callbacks for filesystem modifications. *v2* provides a complete `WritableFS` interface with proper hierarchy management.
```go
mfs := mockfs.NewMockFS(nil)

// Create directory hierarchy (new methods in v2)
err := mfs.MkdirAll("app/config/prod", 0755)

// Create single directory
err = mfs.Mkdir("logs", 0755)

// Remove directory (must be empty)
err = mfs.Remove("logs")

// Remove directory tree
err = mfs.RemoveAll("app")

// Rename directory (moves all contents)
err = mfs.Rename("old_name", "new_name")

// Write file with create-if-missing option
mfs = mockfs.NewMockFS(nil, mockfs.WithCreateIfMissing(true))
err = mfs.WriteFile("new.txt", []byte("content"), 0644)
```

### 5.6. Shared Error Injector

*v2* allows sharing error injection rules across multiple filesystems or files.
```go
// Create shared injector
injector := mockfs.NewErrorInjector()
injector.AddGlob(mockfs.OpRead, "*.log", io.EOF, mockfs.ErrorModeAlways, 0)
injector.AddExact(mockfs.OpOpen, "config.json", fs.ErrPermission, mockfs.ErrorModeAlways, 0)

// Use with multiple filesystems
mfs1 := mockfs.NewMockFS(nil, mockfs.WithErrorInjector(injector))
mfs2 := mockfs.NewMockFS(nil, mockfs.WithErrorInjector(injector))

// Use with standalone files
file := mockfs.NewMockFileFromString("test.log", "data",
    mockfs.WithFileErrorInjector(injector),
)

// All share the same error rules
// Modifying injector affects all consumers
injector.Clear()
```

### 5.7. Standalone MockFile Constructors

*v2* provides constructors for creating `MockFile` instances without requiring a `MockFS` context, enabling easier unit testing of functions that accept `io.Reader`, `io.Writer`, or `io.ReadWriter`.
```go
// From string content
file := mockfs.NewMockFileFromString("test.txt", "content")

// From byte slice
file = mockfs.NewMockFileFromBytes("data.bin", []byte{0x01, 0x02, 0x03})

// With options
file = mockfs.NewMockFileFromString("test.txt", "content",
    mockfs.WithFileLatency(10*time.Millisecond),
    mockfs.WithFileReadOnly(),
    mockfs.WithFileErrorInjector(injector),
)

// Create directory with ReadDir handler
entries := []fs.DirEntry{ /* ... */ }
handler := mockfs.NewDirHandler(entries)
dir := mockfs.NewMockDirectory("mydir", handler,
    mockfs.WithFileLatency(5*time.Millisecond),
)

// Test functions that accept file interfaces
func ProcessReader(r io.Reader) error { /* ... */ }
err := ProcessReader(file)
```

### 5.8. SubFS Support

*v2* provides full `fs.SubFS` implementation with automatic path adjustment for error rules.
```go
mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
    "app/config/dev.json":  {Data: []byte("{}"), Mode: 0644},
    "app/config/prod.json": {Data: []byte("{}"), Mode: 0644},
    "app/logs/app.log":     {Data: []byte(""), Mode: 0644},
})

// Configure error for paths in parent
mfs.ErrorInjector().AddGlob(mockfs.OpRead, "app/config/*.json", io.EOF, mockfs.ErrorModeAlways, 0)

// Create sub-filesystem
subFS, err := mfs.Sub("app/config")

// Error rules automatically adjusted for sub-filesystem paths
// "app/config/*.json" becomes "*.json" in subFS context
data, err := fs.ReadFile(subFS, "dev.json") // Error injected

// Sub-filesystem has independent stats
subMockFS := subFS.(*mockfs.MockFS)
stats := subMockFS.Stats()
```

## 6. Common Migration Patterns<a id="common-migration-patterns"></a>

### 6.1. Pattern 1: Testing Read Errors

**Before (v1)**:
```go
func TestReadError(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "data.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
    })
    mfs.AddReadError("data.txt", io.ErrUnexpectedEOF)

    _, err := mfs.ReadFile("data.txt")
    if err != io.ErrUnexpectedEOF {
        t.Errorf("expected ErrUnexpectedEOF, got %v", err)
    }
}
```

**After (v2)**:
```go
func TestReadError(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "data.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
    })
    mfs.FailRead("data.txt", io.ErrUnexpectedEOF)

    _, err := mfs.ReadFile("data.txt")
    if err != io.ErrUnexpectedEOF {
        t.Errorf("expected ErrUnexpectedEOF, got %v", err)
    }
}
```

### 6.2. Pattern 2: Testing with Latency

**Before (v1)**:
```go
func TestTimeout(t *testing.T) {
    mfs := mockfs.NewMockFS(nil, mockfs.WithLatency(2*time.Second))

    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    // Test with context
    err := YourFunctionWithContext(ctx, mfs)
    if err != context.DeadlineExceeded {
        t.Error("expected timeout")
    }
}
```

**After (v2)**:
```go
func TestTimeout(t *testing.T) {
    mfs := mockfs.NewMockFS(nil, mockfs.WithLatency(2*time.Second))

    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()

    // Same test logic
    err := YourFunctionWithContext(ctx, mfs)
    if err != context.DeadlineExceeded {
        t.Error("expected timeout")
    }
}
```

### 6.3. Pattern 3: Testing Statistics

**Before (v1)**:
```go
func TestOperationCounts(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "file.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
    })

    // Function under test
    file, _ := mfs.Open("file.txt")
    buf := make([]byte, 100)
    file.Read(buf)
    file.Close()

    // All operations in single stats struct
    stats := mfs.GetStats()
    if stats.OpenCalls != 1 {
        t.Errorf("expected 1 open, got %d", stats.OpenCalls)
    }
    if stats.ReadCalls != 1 {
        t.Errorf("expected 1 read, got %d", stats.ReadCalls)
    }
    if stats.CloseCalls != 1 {
        t.Errorf("expected 1 close, got %d", stats.CloseCalls)
    }
}
```

**After (v2)**:
```go
func TestOperationCounts(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "file.txt": {Data: []byte("content"), Mode: 0644, ModTime: time.Now()},
    })

    // Function under test
    file, _ := mfs.Open("file.txt")
    buf := make([]byte, 100)
    file.Read(buf)
    file.Close()

    // Stats now split: MockFS for filesystem ops, MockFile for file-handle ops
    // Use Stats interface methods instead of struct fields

    // Filesystem-level operations
    fsStats := mfs.Stats()
    if fsStats.Count(mockfs.OpOpen) != 1 {
        t.Errorf("expected 1 open, got %d", fsStats.Count(mockfs.OpOpen))
    }

    // File-handle operations
    mockFile := file.(mockfs.MockFile)
    fileStats := mockFile.Stats()
    if fileStats.Count(mockfs.OpRead) != 1 {
        t.Errorf("expected 1 read, got %d", fileStats.Count(mockfs.OpRead))
    }
    if fileStats.Count(mockfs.OpClose) != 1 {
        t.Errorf("expected 1 close, got %d", fileStats.Count(mockfs.OpClose))
    }

    // New: Check for failures
    if fsStats.HasFailures() || fileStats.HasFailures() {
        t.Errorf("unexpected failures")
    }

    // New: Check byte counters
    if fileStats.BytesRead() == 0 {
        t.Error("expected bytes to be read")
    }
}
```

### 6.4. Pattern 4: Testing File Operations Independently

**Before (v1)**:
```go
func TestFileReader(t *testing.T) {
    // Must use MockFS to create file
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "test.txt": {Data: []byte("test data"), Mode: 0644, ModTime: time.Now()},
    })
    file, _ := mfs.Open("test.txt")

    result := YourFunctionThatReads(file)

    // Stats tracked in MockFS
    stats := mfs.GetStats()
    if stats.ReadCalls == 0 {
        t.Error("expected at least one read")
    }
}
```

**After (v2)**:
```go
func TestFileReader(t *testing.T) {
    // Test a function that accepts io.Reader
    // Create standalone file without MockFS
    file := mockfs.NewMockFileFromString("test.txt", "test data")

    result := YourFunctionThatReads(file)

    // Verify read statistics on file handle
    stats := file.Stats()
    if stats.Count(mockfs.OpRead) == 0 {
        t.Error("expected at least one read")
    }
    if stats.BytesRead() == 0 {
        t.Error("expected bytes to be read")
    }
}
```

### 6.5. Pattern 5: Advanced Error Scenarios

**Before (v1)**:
```go
func TestIntermittentErrors(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "flaky.txt": {Data: []byte("datadatadatadata"), Mode: 0644, ModTime: time.Now()},
    })

    // Error after 3 successes
    mfs.AddReadErrorAfterN("flaky.txt", io.EOF, 3)

    f, _ := mfs.Open("flaky.txt")
    buf := make([]byte, 1)

    // First 3 reads succeed
    for i := 0; i < 3; i++ {
        _, err := f.Read(buf)
        if err != nil {
            t.Errorf("read %d failed unexpectedly: %v", i, err)
        }
    }

    // 4th read fails
    _, err := f.Read(buf)
    if err != io.EOF {
        t.Errorf("expected EOF, got %v", err)
    }
}
```

**After (v2)**:
```go
func TestIntermittentErrors(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "flaky.txt": {Data: []byte("datadatadatadata"), Mode: 0644, ModTime: time.Now()},
    })

    // Error after 3 successes
    mfs.FailReadAfter("flaky.txt", io.EOF, 3)

    f, _ := mfs.Open("flaky.txt")
    buf := make([]byte, 1)

    // First 3 reads succeed
    for i := 0; i < 3; i++ {
        _, err := f.Read(buf)
        if err != nil {
            t.Errorf("read %d failed unexpectedly: %v", i, err)
        }
    }

    // 4th read fails
    _, err := f.Read(buf)
    if err != io.EOF {
        t.Errorf("expected EOF, got %v", err)
    }

    // New: Verify stats tracked both successes and failure
    mockFile := f.(mockfs.MockFile)
    stats := mockFile.Stats()
    if stats.CountSuccess(mockfs.OpRead) != 3 {
        t.Errorf("expected 3 successful reads, got %d", stats.CountSuccess(mockfs.OpRead))
    }
    if stats.CountFailure(mockfs.OpRead) != 1 {
        t.Errorf("expected 1 failed read, got %d", stats.CountFailure(mockfs.OpRead))
    }
}
```

### 6.6. Pattern 6: Testing with Glob Patterns (New in *v2*)

*v2* introduces glob patterns for easier path matching.
```go
func TestGlobPatternErrors(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "logs/app.log":   {Data: []byte("log"), Mode: 0644, ModTime: time.Now()},
        "logs/error.log": {Data: []byte("log"), Mode: 0644, ModTime: time.Now()},
        "data/file.txt":  {Data: []byte("data"), Mode: 0644, ModTime: time.Now()},
    })

    // Apply error to all .log files
    mfs.ErrorInjector().AddGlob(mockfs.OpRead, "logs/*.log", io.ErrUnexpectedEOF, mockfs.ErrorModeAlways, 0)

    // Reading .log files fails
    _, err := mfs.ReadFile("logs/app.log")
    if err != io.ErrUnexpectedEOF {
        t.Errorf("expected error for .log file, got %v", err)
    }

    // Reading .txt file succeeds
    _, err = mfs.ReadFile("data/file.txt")
    if err != nil {
        t.Errorf("unexpected error for .txt file: %v", err)
    }
}
```

### 6.7. Pattern 7: Testing SubFS (New in *v2*)

*v2* provides full `fs.SubFS` support.
```go
func TestSubFilesystem(t *testing.T) {
    mfs := mockfs.NewMockFS(map[string]*mockfs.MapFile{
        "app/config/dev.json":  {Data: []byte("{}"), Mode: 0644, ModTime: time.Now()},
        "app/config/prod.json": {Data: []byte("{}"), Mode: 0644, ModTime: time.Now()},
        "app/data/file.txt":    {Data: []byte("data"), Mode: 0644, ModTime: time.Now()},
    })

    // Configure error in parent filesystem
    mfs.FailRead("app/config/prod.json", fs.ErrPermission)

    // Create sub-filesystem
    subFS, err := mfs.Sub("app/config")
    if err != nil {
        t.Fatalf("Sub failed: %v", err)
    }

    // Error rules automatically adjusted
    _, err = fs.ReadFile(subFS, "prod.json") // Path relative to sub-filesystem
    if err != fs.ErrPermission {
        t.Errorf("expected permission error, got %v", err)
    }

    // File not in sub-filesystem
    _, err = fs.ReadFile(subFS, "../data/file.txt")
    if err == nil {
        t.Error("expected error for path outside sub-filesystem")
    }
}
```

## 7. Migration Checklist<a id="migration-checklist"></a>

### 7.1. Core API Changes

- [ ] Replace `GetStats()` with `Stats()` throughout codebase
- [ ] Update statistics access from struct fields to interface methods:
  - [ ] `stats.OpenCalls` → `stats.Count(mockfs.OpOpen)`
  - [ ] `stats.StatCalls` → `stats.Count(mockfs.OpStat)`
  - [ ] `stats.ReadDirCalls` → `stats.Count(mockfs.OpReadDir)`
  - [ ] `stats.ReadCalls` → `fileStats.Count(mockfs.OpRead)` (note: file-handle stats)
  - [ ] `stats.WriteCalls` → `fileStats.Count(mockfs.OpWrite)` (note: file-handle stats)
  - [ ] `stats.CloseCalls` → `fileStats.Count(mockfs.OpClose)` (note: file-handle stats)
- [ ] Understand statistics split: `MockFS.Stats()` tracks filesystem ops, `MockFile.Stats()` tracks file-handle ops
- [ ] Update code that accesses file-handle operation counts to use `MockFile.Stats()` instead of `MockFS.Stats()`

### 7.2. File and Directory Management

- [ ] Rename file/directory management methods:
  - [ ] `AddFileString()` → `AddFile()` (returns error)
  - [ ] `AddDirectory()` → `AddDir()` (returns error)
- [ ] Add error handling for file/directory operations that now return errors:
  - [ ] `AddFile()`, `AddFileBytes()`, `AddDir()`, `RemovePath()`

### 7.3. Error Injection - Convenience Methods

- [ ] Rename error injection convenience methods:
  - [ ] `AddStatError()` → `FailStat()`
  - [ ] `AddOpenError()` → `FailOpen()`
  - [ ] `AddReadError()` → `FailRead()`
  - [ ] `AddWriteError()` → `FailWrite()`
  - [ ] `AddReadDirError()` → `FailReadDir()`
  - [ ] `AddCloseError()` → `FailClose()`
  - [ ] `AddOpenErrorOnce()` → `FailOpenOnce()`
  - [ ] `AddReadErrorAfterN()` → `FailReadAfter()`

### 7.4. Error Injection - Advanced

- [ ] Replace pattern-based error injection:
  - [ ] `AddErrorPattern()` → `ErrorInjector().AddRegexp()`
  - [ ] `AddErrorExactMatch()` → `ErrorInjector().AddExact()`
  - [ ] `AddPathError()` → `ErrorInjector().AddExactForAllOps()`
  - [ ] `AddPathErrorPattern()` → `ErrorInjector().AddRegexpForAllOps()`
- [ ] Replace `MarkDirectoryNonExistent()` with combination of `RemoveAll()` and error injection

### 7.5. Write Operations

- [ ] Replace `WithWritesEnabled()` option with explicit write mode:
  - [ ] `WithOverwrite()` (default behavior)
  - [ ] `WithAppend()` (if appending is needed)
  - [ ] `WithReadOnly()` (if read-only is needed)
- [ ] Replace `WithWriteCallback()` with `WritableFS` interface methods:
  - [ ] Use `WriteFile()` for writing files
  - [ ] Use `Mkdir()` / `MkdirAll()` for creating directories
  - [ ] Use `Remove()` / `RemoveAll()` for removing paths
  - [ ] Use `Rename()` for renaming/moving paths
- [ ] Add `WithCreateIfMissing(true)` option if writes should create non-existent files

### 7.6. Operation Constants

- [ ] Verify all `Operation` constant references - numbering changed:
  - [ ] `OpUnknown` added as 0
  - [ ] `OpStat` changed from 0 to 1
  - [ ] `OpReadDir` moved in sequence
  - [ ] New operations added: `OpMkdir`, `OpMkdirAll`, `OpRemove`, `OpRemoveAll`, `OpRename`
- [ ] Update any switch statements or arrays indexed by `Operation`

### 7.7. New Features to Consider

- [ ] Consider using standalone `MockFile` constructors for file-specific tests:
  - [ ] `NewMockFileFromString()` for text files
  - [ ] `NewMockFileFromBytes()` for binary files
  - [ ] `NewMockDirectory()` for directory testing
- [ ] Consider per-operation latency with `WithPerOperationLatency()` if different operations need different delays
- [ ] Consider glob patterns (`AddGlob()`) for simpler path matching instead of regex
- [ ] Consider `fs.SubFS` testing with automatic error rule adjustment
- [ ] Consider new stats methods:
  - [ ] `CountSuccess()` / `CountFailure()` for success/failure tracking
  - [ ] `BytesRead()` / `BytesWritten()` for I/O volume tracking
  - [ ] `Delta()` for comparing stats snapshots
  - [ ] `HasFailures()` for quick error detection

### 7.8. Testing and Validation

- [ ] Run `go mod tidy` to update dependencies
- [ ] Run tests and fix compilation errors
- [ ] Verify test behavior matches expectations:
  - [ ] Stats assertions correctly distinguish filesystem vs file-handle operations
  - [ ] Error injection still triggers at expected points
  - [ ] Latency simulation still provides expected delays
- [ ] Update test assertions that relied on v1.0.2 specific behavior:
  - [ ] File-handle operation counts now in `MockFile.Stats()` not `MockFS.Stats()`
  - [ ] Stats snapshots are immutable - no need to copy before comparison

### 7.9. Documentation

- [ ] Update code comments referencing old API names
- [ ] Update examples in documentation
- [ ] Note any breaking changes in CHANGELOG or release notes

## 8. Getting Help<a id="getting-help"></a>

- Review test files (`*_test.go`) in the repository for comprehensive examples
- Check GoDoc at `pkg.go.dev/github.com/balinomad/go-mockfs`
- File issues at `github.com/balinomad/go-mockfs/issues`