package mockfs

import (
	"encoding"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"testing/fstest"
	"time"
)

// A FileMode represents a file's mode and permission bits.
// See [io/fs.FileMode]
type FileMode = fs.FileMode

// The defined file mode bits are the most significant bits of the FileMode.
const (
	ModeDir         FileMode = fs.ModeDir    // d: is a directory
	ModeAppend      FileMode = fs.ModeAppend // a: append-only
	ModePerm        FileMode = fs.ModePerm   // Unix permission bits
	defaultFilePerm FileMode = 0o644
	defaultDirPerm  FileMode = 0o755
)

// A MapFile describes a single file in a MapFS.
// See [testing/fstest.MapFile].
type MapFile = fstest.MapFile

// WritableFS is an extension of fs.FS that supports write operations.
// It mirrors the mutating side of the [os] standard package.
type WritableFS interface {
	fs.FS

	// Mkdir creates a directory in the filesystem.
	Mkdir(path string, perm FileMode) error

	// MkdirAll creates a directory path and all parents if needed.
	MkdirAll(path string, perm FileMode) error

	// Remove removes a file or directory from the filesystem.
	Remove(path string) error

	// RemoveAll removes a path and any children recursively.
	RemoveAll(path string) error

	// Rename renames a file or directory in the filesystem.
	// If the destination already exists, it will be overwritten.
	Rename(oldpath, newpath string) error

	// WriteFile writes data to a file in the filesystem.
	WriteFile(path string, data []byte, perm FileMode) error
}

// --- MockFS Options ---

// MockFSOption configures the MockFS or adds entries to it.
// The contextPath argument allows relative path resolution for nested structures.
type MockFSOption func(fs *MockFS, contextPath string) error

// File adds a file at the current context path.
// The content will be converted to a byte slice.
// The mode is optional, defaulting to 0644.
//
// Note: File does not create parent directories.
// The hierarchy must be built explicitly using Dir().
func File(name string, content any, mode ...FileMode) MockFSOption {
	opName := "File"

	return func(m *MockFS, contextPath string) error {
		if name == "" {
			return fmt.Errorf("%s: empty file name", opName)
		}
		// Strict hierarchical validation: name must be a single segment
		if strings.ContainsRune(name, '/') || name == "." || name == ".." {
			return fmt.Errorf("%s: invalid name %q (must be a single path segment)", opName, name)
		}

		fullPath := name
		if contextPath != "." {
			fullPath = path.Join(contextPath, name)
		}
		cleanPath := path.Clean(fullPath)

		data, err := toBytes(content)
		if err != nil {
			return fmt.Errorf("%s: invalid content in %s (%T): %w", opName, cleanPath, content, err)
		}

		perm := defaultFilePerm
		if len(mode) > 0 {
			perm = mode[0]
		}

		m.files[cleanPath] = &fstest.MapFile{
			Data:    data,
			Mode:    (perm & ModePerm) &^ ModeDir,
			ModTime: time.Now(),
		}
		return nil
	}
}

// Dir adds a directory and applies child options within its context.
// Mixed arguments are supported: FileMode sets permissions, MockFSOption adds children.
//
// Note: Dir does not create parent directories.
// The hierarchy must be built explicitly using nested Dir() calls.
func Dir(name string, args ...any) MockFSOption {
	opName := "Dir"

	return func(m *MockFS, contextPath string) error {
		if name == "" {
			return fmt.Errorf("%s: empty directory name", opName)
		}
		// Strict hierarchical validation
		if strings.ContainsRune(name, '/') || name == "." || name == ".." {
			return fmt.Errorf("%s: invalid name %q (must be a single path segment)", opName, name)
		}

		fullPath := name
		if contextPath != "." {
			fullPath = path.Join(contextPath, name)
		}
		cleanPath := path.Clean(fullPath)

		perm := defaultDirPerm
		var children []MockFSOption

		// Argument parsing
		for _, arg := range args {
			switch v := arg.(type) {
			case FileMode:
				perm = v
			case MockFSOption:
				children = append(children, v)
			default:
				return fmt.Errorf("%s: invalid argument %s: %T", opName, cleanPath, v)
			}
		}

		// Create the directory entry itself
		m.files[cleanPath] = &fstest.MapFile{
			Mode:    (perm & ModePerm) | ModeDir,
			ModTime: time.Now(),
		}

		// Apply children with the new cleanPath as context
		for _, child := range children {
			if err := child(m, cleanPath); err != nil {
				return err
			}
		}
		return nil
	}
}

// WithErrorInjector sets the error injector for the MockFS.
func WithErrorInjector(injector ErrorInjector) MockFSOption {
	return func(m *MockFS, _ string) error {
		if injector != nil {
			m.injector = injector
		}
		return nil
	}
}

// WithCreateIfMissing sets whether writes should create files when missing.
// Default behavior is to return an error.
func WithCreateIfMissing(create bool) MockFSOption {
	return func(m *MockFS, _ string) error {
		m.createIfMissing = create
		return nil
	}
}

// WithReadOnly explicitly marks the filesystem as read-only.
func WithReadOnly() MockFSOption {
	return func(m *MockFS, _ string) error {
		m.writeMode = writeModeReadOnly
		return nil
	}
}

// WithOverwrite sets the write policy to overwrite existing contents.
// The existing contents will be replaced by the new data.
func WithOverwrite() MockFSOption {
	return func(m *MockFS, _ string) error {
		m.writeMode = writeModeOverwrite
		return nil
	}
}

// WithAppend sets the write policy to append data to existing contents.
// The new data will be appended to the existing contents.
func WithAppend() MockFSOption {
	return func(m *MockFS, _ string) error {
		m.writeMode = writeModeAppend
		return nil
	}
}

// WithLatency sets the simulated latency for all operations.
func WithLatency(duration time.Duration) MockFSOption {
	return func(m *MockFS, _ string) error {
		m.latency = NewLatencySimulator(duration)
		return nil
	}
}

// WithLatencySimulator sets a custom latency simulator for operations.
func WithLatencySimulator(sim LatencySimulator) MockFSOption {
	return func(m *MockFS, _ string) error {
		if sim != nil {
			m.latency = sim
		}
		return nil
	}
}

// WithPerOperationLatency sets different latencies for different operations.
func WithPerOperationLatency(durations map[Operation]time.Duration) MockFSOption {
	return func(m *MockFS, _ string) error {
		m.latency = NewLatencySimulatorPerOp(durations)
		return nil
	}
}

// --- MockFS ---

// MockFS wraps a file map to inject errors for specific paths and operations.
type MockFS struct {
	files           map[string]*fstest.MapFile // Internal file storage.
	mu              sync.RWMutex               // Mutex for concurrent file structure operations.
	injector        ErrorInjector              // Shared error injector.
	stats           StatsRecorder              // Filesystem-level operation statistics.
	latency         LatencySimulator           // Shared latency simulator.
	createIfMissing bool                       // Whether to create files on write if missing.
	writeMode       writeMode                  // How to apply data to files.
}

// Ensure interface implementations.
var (
	_ fs.FS         = (*MockFS)(nil)
	_ fs.ReadDirFS  = (*MockFS)(nil)
	_ fs.ReadFileFS = (*MockFS)(nil)
	_ fs.StatFS     = (*MockFS)(nil)
	_ fs.SubFS      = (*MockFS)(nil)
	_ WritableFS    = (*MockFS)(nil)
)

// NewMockFS creates a new MockFS with the given MapFile data and options.
// Nil MapFile entries in the initial map are ignored.
// If no root directory (".") is provided, one is created automatically.
func NewMockFS(opts ...MockFSOption) *MockFS {
	// Ensure root directory exists
	files := map[string]*fstest.MapFile{
		".": {
			Mode:    ModeDir | defaultDirPerm,
			ModTime: time.Now(),
		},
	}

	m := &MockFS{
		files:           files,
		injector:        NewErrorInjector(),
		stats:           NewStatsRecorder(nil),
		latency:         NewNoopLatencySimulator(),
		createIfMissing: false,
		writeMode:       writeModeOverwrite,
	}

	// Apply options
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(m, "."); err != nil {
			// Panic here to surface API misuse immediately
			panic(fmt.Sprintf("mockfs: failed to apply option %v", err))
		}
	}

	return m
}

// Stat returns file information for the given path.
// It implements the fs.StatFS interface.
// This is a filesystem-level operation that does not open the file.
func (m *MockFS) Stat(name string) (info fs.FileInfo, err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpStat, 0, err) }()

	cleanName, err := m.validateAndCleanPath(name, OpStat)
	if err != nil {
		return nil, err
	}

	if err = m.injector.CheckAndApply(OpStat, cleanName); err != nil {
		return nil, err
	}

	m.latency.Simulate(OpStat)

	m.mu.RLock()
	mapFile, exists := m.files[cleanName]
	m.mu.RUnlock()

	if !exists {
		err = &fs.PathError{Op: OpStat.String(), Path: name, Err: fs.ErrNotExist}
		return nil, err
	}

	// Build FileInfo from MapFile
	return &FileInfo{
		name:    path.Base(cleanName),
		size:    int64(len(mapFile.Data)),
		mode:    mapFile.Mode,
		modTime: mapFile.ModTime,
	}, nil
}

// Open opens the named file and returns a MockFile.
// It implements the fs.FS interface.
// This is a filesystem-level operation. The returned MockFile handles file-level operations.
func (m *MockFS) Open(name string) (file fs.File, err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpOpen, 0, err) }()

	cleanName, err := m.validateAndCleanPath(name, OpOpen)
	if err != nil {
		return nil, err
	}

	if err = m.injector.CheckAndApply(OpOpen, cleanName); err != nil {
		return nil, err
	}

	m.latency.Simulate(OpOpen)

	m.mu.RLock()
	mapFile, exists := m.files[cleanName]
	m.mu.RUnlock()

	if !exists {
		err = &fs.PathError{Op: OpOpen.String(), Path: name, Err: ErrNotExist}
		return nil, err
	}

	// Create ReadDir handler for directories
	var readDirHandler func(int) ([]fs.DirEntry, error)
	if mapFile.Mode.IsDir() {
		readDirHandler = m.createReadDirHandler(cleanName)
	}

	// Clone latency simulator to give each file handle independent Once() state
	// while preserving duration configuration
	clonedLatency := m.latency.Clone()

	// Create MockFile with its own Stats for file-handle operations
	return newMockFile(
		mapFile,
		cleanName,
		m.writeMode,
		m.injector,    // Share error injector
		clonedLatency, // Independent per file
		readDirHandler,
		nil, // Each file gets its own Stats
	), nil
}

// ReadFile implements the fs.ReadFileFS interface.
// It opens the file, reads it, and closes it.
// Note: OpOpen is recorded by Open(), OpRead and OpClose by MockFile.
func (m *MockFS) ReadFile(name string) ([]byte, error) {
	cleanName, err := m.validateAndCleanPath(name, OpRead)
	if err != nil {
		return nil, err
	}

	// Open delegates to Open() which handles OpOpen tracking
	// MockFile.Read() and Close() handle their own tracking
	file, err := m.Open(cleanName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	return io.ReadAll(file)
}

// ReadDir implements the fs.ReadDirFS interface.
// This is a filesystem-level operation.
func (m *MockFS) ReadDir(name string) (entries []fs.DirEntry, err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpReadDir, 0, err) }()

	cleanName, err := m.validateAndCleanPath(name, OpReadDir)
	if err != nil {
		return nil, err
	}

	if err = m.injector.CheckAndApply(OpReadDir, cleanName); err != nil {
		return nil, err
	}

	m.latency.Simulate(OpReadDir)

	m.mu.RLock()
	mapFile, exists := m.files[cleanName]
	m.mu.RUnlock()

	if !exists {
		err = &fs.PathError{Op: OpReadDir.String(), Path: name, Err: ErrNotExist}
		return nil, err
	}

	if !mapFile.Mode.IsDir() {
		err = &fs.PathError{Op: OpReadDir.String(), Path: name, Err: ErrNotDir}
		return nil, err
	}

	// Use the same handler logic
	handler := m.createReadDirHandler(cleanName)
	return handler(-1)
}

// Sub implements fs.SubFS to return a sub-filesystem.
func (m *MockFS) Sub(dir string) (fs.FS, error) {
	cleanDir, info, err := m.validateSubdir(dir)
	if err != nil {
		return nil, err
	}

	subFS := NewMockFS(nil)
	subFS.latency = m.latency
	subFS.writeMode = m.writeMode
	subFS.createIfMissing = m.createIfMissing
	// Sub filesystem gets its own Stats (not shared with parent)

	m.mu.RLock()
	defer m.mu.RUnlock()

	m.copyFilesToSubFS(subFS, cleanDir, info)

	// Clone the injector with adjusted paths
	subFS.injector = m.injector.CloneForSub(cleanDir)

	return subFS, nil
}

// --- File/Directory Management ---

// AddFile adds a new file to the mock filesystem.
// If the file already exists, it is overwritten.
// The parent directories will be created implicitly if they don't exist.
// Returns an error if the path or content is invalid, or if a file blocks a parent directory.
func (m *MockFS) AddFile(filePath string, content any, mode ...FileMode) error {
	opName := "AddFile"

	// Validate original path first; it should not have a trailing slash
	if !fs.ValidPath(filePath) || strings.HasSuffix(filePath, "/") {
		return &fs.PathError{Op: opName, Path: filePath, Err: ErrInvalid}
	}

	cleanPath := path.Clean(filePath)

	data, err := toBytes(content)
	if err != nil {
		return fmt.Errorf("%s: invalid content in %s (%T): %w", opName, filePath, content, err)
	}

	perm := defaultFilePerm
	if len(mode) > 0 {
		perm = mode[0]
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Create parent directories implicitly
	if err := m.ensureParentDirs(opName, cleanPath); err != nil {
		return err
	}

	// Check if a directory exists at this path
	if existing, exists := m.files[cleanPath]; exists && existing.Mode.IsDir() {
		return &fs.PathError{Op: opName, Path: filePath, Err: ErrIsDir}
	}

	m.files[cleanPath] = &fstest.MapFile{
		Data:    data, // already deep copied
		Mode:    (perm & ModePerm) &^ ModeDir,
		ModTime: time.Now(),
	}

	return nil
}

// AddDir adds a directory to the mock filesystem.
// If the directory already exists, it is overwritten.
// Returns an error if the path is invalid or it already exists and is not a directory.
func (m *MockFS) AddDir(dirPath string, mode ...FileMode) error {
	opName := "AddDir"

	perm := defaultDirPerm
	if len(mode) > 0 {
		perm = mode[0]
	}
	if !fs.ValidPath(dirPath) {
		return &fs.PathError{Op: opName, Path: dirPath, Err: ErrInvalid}
	}

	cleanPath := path.Clean(dirPath)

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.mkdirAll(opName, cleanPath, perm)
}

// RemoveEntry removes a file or directory from the mock filesystem.
// Unlike Remove, it recursively removes directory contents (like RemoveAll) to prevent orphan entries,
// and it does not simulate latency or errors.
func (m *MockFS) RemoveEntry(fileOrDirPath string) error {
	opName := "RemoveEntry"
	if !fs.ValidPath(fileOrDirPath) {
		return &fs.PathError{Op: opName, Path: fileOrDirPath, Err: ErrInvalid}
	}

	cleanPath := path.Clean(fileOrDirPath)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Recursive removal to keep map state consistent
	prefix := cleanPath + "/"
	for p := range m.files {
		if p == cleanPath || strings.HasPrefix(p, prefix) {
			delete(m.files, p)
		}
	}

	return nil
}

// --- Error Injection Configuration ---

// ErrorInjector returns the error injector for advanced configuration.
func (m *MockFS) ErrorInjector() ErrorInjector {
	return m.injector
}

// FailStat configures a path to return the specified error on Stat operations.
func (m *MockFS) FailStat(path string, err error) {
	m.injector.AddExact(OpStat, path, err, ErrorModeAlways, 0)
}

// FailStatOnce configures a path to return the specified error once on Stat operations.
func (m *MockFS) FailStatOnce(path string, err error) {
	m.injector.AddExact(OpStat, path, err, ErrorModeOnce, 0)
}

// FailOpen configures a path to return the specified error on Open operations.
func (m *MockFS) FailOpen(path string, err error) {
	m.injector.AddExact(OpOpen, path, err, ErrorModeAlways, 0)
}

// FailOpenOnce configures a path to return the specified error once on Open operations.
func (m *MockFS) FailOpenOnce(path string, err error) {
	m.injector.AddExact(OpOpen, path, err, ErrorModeOnce, 0)
}

// FailRead configures a path to return the specified error on Read operations.
func (m *MockFS) FailRead(path string, err error) {
	m.injector.AddExact(OpRead, path, err, ErrorModeAlways, 0)
}

// FailReadOnce configures a path to return the specified error once on Read operations.
func (m *MockFS) FailReadOnce(path string, err error) {
	m.injector.AddExact(OpRead, path, err, ErrorModeOnce, 0)
}

// FailReadAfter configures a read error after N successful reads.
func (m *MockFS) FailReadAfter(path string, err error, successes int) {
	m.injector.AddExact(OpRead, path, err, ErrorModeAfterSuccesses, successes)
}

// FailWrite configures a path to return the specified error on Write operations.
func (m *MockFS) FailWrite(path string, err error) {
	m.injector.AddExact(OpWrite, path, err, ErrorModeAlways, 0)
}

// FailWriteOnce configures a path to return the specified error once on Write operations.
func (m *MockFS) FailWriteOnce(path string, err error) {
	m.injector.AddExact(OpWrite, path, err, ErrorModeOnce, 0)
}

// FailReadDir configures a path to return the specified error on ReadDir operations.
func (m *MockFS) FailReadDir(path string, err error) {
	m.injector.AddExact(OpReadDir, path, err, ErrorModeAlways, 0)
}

// FailReadDirOnce configures a path to return the specified error once on ReadDir operations.
func (m *MockFS) FailReadDirOnce(path string, err error) {
	m.injector.AddExact(OpReadDir, path, err, ErrorModeOnce, 0)
}

// FailClose configures a path to return the specified error on Close operations.
func (m *MockFS) FailClose(path string, err error) {
	m.injector.AddExact(OpClose, path, err, ErrorModeAlways, 0)
}

// FailCloseOnce configures a path to return the specified error once on Close operations.
func (m *MockFS) FailCloseOnce(path string, err error) {
	m.injector.AddExact(OpClose, path, err, ErrorModeOnce, 0)
}

// FailMkdir configures a path to return the specified error on Mkdir operations.
func (m *MockFS) FailMkdir(path string, err error) {
	m.injector.AddExact(OpMkdir, path, err, ErrorModeAlways, 0)
}

// FailMkdirOnce configures a path to return the specified error once on Mkdir operations.
func (m *MockFS) FailMkdirOnce(path string, err error) {
	m.injector.AddExact(OpMkdir, path, err, ErrorModeOnce, 0)
}

// FailMkdirAll configures a path to return the specified error on MkdirAll operations.
func (m *MockFS) FailMkdirAll(path string, err error) {
	m.injector.AddExact(OpMkdirAll, path, err, ErrorModeAlways, 0)
}

// FailMkdirAllOnce configures a path to return the specified error once on MkdirAll operations.
func (m *MockFS) FailMkdirAllOnce(path string, err error) {
	m.injector.AddExact(OpMkdirAll, path, err, ErrorModeOnce, 0)
}

// FailRemove configures a path to return the specified error on Remove operations.
func (m *MockFS) FailRemove(path string, err error) {
	m.injector.AddExact(OpRemove, path, err, ErrorModeAlways, 0)
}

// FailRemoveOnce configures a path to return the specified error once on Remove operations.
func (m *MockFS) FailRemoveOnce(path string, err error) {
	m.injector.AddExact(OpRemove, path, err, ErrorModeOnce, 0)
}

// FailRemoveAll configures a path to return the specified error on RemoveAll operations.
func (m *MockFS) FailRemoveAll(path string, err error) {
	m.injector.AddExact(OpRemoveAll, path, err, ErrorModeAlways, 0)
}

// FailRemoveAllOnce configures a path to return the specified error once on RemoveAll operations.
func (m *MockFS) FailRemoveAllOnce(path string, err error) {
	m.injector.AddExact(OpRemoveAll, path, err, ErrorModeOnce, 0)
}

// FailRename configures a path to return the specified error on Rename operations.
func (m *MockFS) FailRename(path string, err error) {
	m.injector.AddExact(OpRename, path, err, ErrorModeAlways, 0)
}

// FailRenameOnce configures a path to return the specified error once on Rename operations.
func (m *MockFS) FailRenameOnce(path string, err error) {
	m.injector.AddExact(OpRename, path, err, ErrorModeOnce, 0)
}

// MarkNonExistent configures paths to return ErrNotExist for all operations.
// This removes the paths from the internal map and injects errors.
//
// Note: If you later add files at these paths using AddFile/AddFileBytes,
// the file will exist in the map but error injection will still apply.
// Use ClearErrors or remove specific injection rules if needed.
func (m *MockFS) MarkNonExistent(paths ...string) {
	for _, p := range paths {
		cleanPath := path.Clean(p)
		_ = m.RemoveEntry(cleanPath) // Remove from map first
		m.injector.AddGlobForAllOps(cleanPath, ErrNotExist, ErrorModeAlways, 0)
	}
}

// ClearErrors removes all configured error injection rules for all operations.
func (m *MockFS) ClearErrors() {
	m.injector.Clear()
}

// --- Operation Statistics ---

// Stats returns a snapshot of filesystem-level operation statistics.
// This includes operations like Open, Stat, ReadDir, Mkdir, Remove, Rename, and WriteFile.
// It does NOT include file-handle operations (Read, Write, Close on open files).
// Use MockFile.Stats() to inspect per-file-handle operations.
func (m *MockFS) Stats() Stats {
	return m.stats.Snapshot()
}

// ResetStats resets all operation statistics to zero.
func (m *MockFS) ResetStats() {
	m.stats.Reset()
}

// --- WritableFS Implementation ---

// Mkdir creates a directory in the filesystem.
func (m *MockFS) Mkdir(dirPath string, perm FileMode) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpMkdir, 0, err) }()

	// Simulation Layer: Validation, Injection, Latency
	cleanPath, err := m.validateAndCleanPath(dirPath, OpMkdir)
	if err != nil {
		return err
	}
	// Disallow "." for explicit Mkdir
	if cleanPath == "." {
		return &fs.PathError{Op: OpMkdir.String(), Path: dirPath, Err: ErrInvalid}
	}
	if err = m.injector.CheckAndApply(OpMkdir, cleanPath); err != nil {
		return err
	}
	m.latency.Simulate(OpMkdir)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Logic Layer
	return m.mkdir(OpMkdir.String(), cleanPath, perm)
}

// MkdirAll creates a directory path and all parents if needed.
func (m *MockFS) MkdirAll(dirPath string, perm FileMode) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpMkdirAll, 0, err) }()

	// Simulation Layer
	cleanPath, err := m.validateAndCleanPath(dirPath, OpMkdirAll)
	if err != nil {
		return err
	}
	if err = m.injector.CheckAndApply(OpMkdirAll, cleanPath); err != nil {
		return err
	}
	m.latency.Simulate(OpMkdirAll)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Logic Layer
	return m.mkdirAll(OpMkdirAll.String(), cleanPath, perm)
}

// Remove removes a file or directory from the filesystem.
// Directories must be empty to be removed.
func (m *MockFS) Remove(filePath string) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpRemove, 0, err) }()

	cleanPath, err := m.validateAndCleanPath(filePath, OpRemove)
	if err != nil {
		return err
	}

	if err = m.injector.CheckAndApply(OpRemove, cleanPath); err != nil {
		return err
	}

	m.latency.Simulate(OpRemove)

	m.mu.Lock()
	defer m.mu.Unlock()

	file, exists := m.files[cleanPath]
	if !exists {
		err = &fs.PathError{Op: "Remove", Path: filePath, Err: ErrNotExist}
		return err
	}

	// If it's a directory, check it's empty
	if file.Mode.IsDir() {
		prefix := cleanPath + "/"
		for p := range m.files {
			if strings.HasPrefix(p, prefix) {
				err = &fs.PathError{Op: "Remove", Path: filePath, Err: ErrNotEmpty}
				return err
			}
		}
	}

	delete(m.files, cleanPath)
	return nil
}

// RemoveAll removes a path and any children recursively.
func (m *MockFS) RemoveAll(filePath string) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpRemoveAll, 0, err) }()

	cleanPath, err := m.validateAndCleanPath(filePath, OpRemoveAll)
	if err != nil {
		return err
	}

	if err = m.injector.CheckAndApply(OpRemoveAll, cleanPath); err != nil {
		return err
	}

	m.latency.Simulate(OpRemoveAll)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove the path itself and all children
	prefix := cleanPath + "/"
	for p := range m.files {
		if p == cleanPath || strings.HasPrefix(p, prefix) {
			delete(m.files, p)
		}
	}

	return nil
}

// Rename renames a file or directory in the filesystem.
// If the destination already exists, it will be overwritten.
func (m *MockFS) Rename(oldpath, newpath string) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpRename, 0, err) }()

	cleanOld, err := m.validateAndCleanPath(oldpath, OpRename)
	if err != nil {
		return err
	}

	cleanNew, err := m.validateAndCleanPath(newpath, OpRename)
	if err != nil {
		return err
	}

	if err = m.injector.CheckAndApply(OpRename, cleanOld); err != nil {
		return err
	}

	m.latency.Simulate(OpRename)

	m.mu.Lock()
	defer m.mu.Unlock()

	oldFile, exists := m.files[cleanOld]
	if !exists {
		err = &fs.PathError{Op: "Rename", Path: oldpath, Err: ErrNotExist}
		return err
	}

	// Copy to new location
	newFile := *oldFile
	if oldFile.Data != nil {
		newFile.Data = append([]byte(nil), oldFile.Data...)
	}
	newFile.ModTime = time.Now()
	m.files[cleanNew] = &newFile

	// If directory, rename all children
	if oldFile.Mode.IsDir() {
		oldPrefix := cleanOld + "/"
		newPrefix := cleanNew + "/"
		for p, f := range m.files {
			if strings.HasPrefix(p, oldPrefix) {
				newP := newPrefix + p[len(oldPrefix):]
				childFile := *f
				if f.Data != nil {
					childFile.Data = append([]byte(nil), f.Data...)
				}
				m.files[newP] = &childFile
				delete(m.files, p)
			}
		}
	}

	// Remove old location
	delete(m.files, cleanOld)
	return nil
}

// WriteFile writes data to a file in the filesystem.
func (m *MockFS) WriteFile(filePath string, data []byte, perm FileMode) (err error) {
	// Record the result of this operation on exit
	// We record len(data) as bytes written, assuming a full write or failure
	defer func() { m.stats.Record(OpWrite, len(data), err) }()

	cleanPath, err := m.validateAndCleanPath(filePath, OpWrite)
	if err != nil {
		return err
	}

	if err = m.injector.CheckAndApply(OpWrite, cleanPath); err != nil {
		return err
	}

	m.latency.Simulate(OpWrite)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check write mode restrictions
	if m.writeMode == writeModeReadOnly {
		err = &fs.PathError{Op: "Write", Path: filePath, Err: ErrPermission}
		return err
	}

	// Find the existing file or create if it doesn't exist
	existing, ok := m.files[cleanPath]
	if !ok {
		if !m.createIfMissing {
			err = &fs.PathError{Op: "Write", Path: filePath, Err: ErrNotExist}
			return err
		}

		// Create a new MapFile entry
		m.files[cleanPath] = &fstest.MapFile{
			Data:    append([]byte(nil), data...), // Copy data
			Mode:    perm &^ ModeDir,
			ModTime: time.Now(),
		}
		return nil
	}

	// Apply write mode
	switch m.writeMode {
	case writeModeAppend:
		// Append data to existing file
		existing.Data = append(existing.Data, data...)
		existing.ModTime = time.Now()
		return nil

	case writeModeOverwrite:
		// Overwrite data for existing file
		existing.Data = append([]byte(nil), data...) // Make a copy
		existing.ModTime = time.Now()
		return nil

	default:
		panic("mockfs: invalid writeMode state")
	}
}

// --- Internal Helpers ---

// createReadDirHandler generates a ReadDir handler for a directory.
// The handler returns fs.DirEntry implementations that delegate to MockFile.Stat().
func (m *MockFS) createReadDirHandler(dirPath string) func(int) ([]fs.DirEntry, error) {
	prefix := ""
	if dirPath != "." {
		prefix = dirPath + "/"
	}

	entries := m.collectDirEntries(dirPath, prefix)

	// Sort for deterministic output
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	return createReadDirClosure(entries)
}

// collectDirEntries returns all directory entries for a directory.
func (m *MockFS) collectDirEntries(dirPath, prefix string) []fs.DirEntry {
	seen := make(map[string]bool)
	var entries []fs.DirEntry

	m.mu.RLock()
	defer m.mu.RUnlock()

	for p, file := range m.files {
		if p == dirPath {
			continue // Skip the directory itself
		}

		// Determine relative path
		var rel string
		switch {
		case prefix == "":
			rel = p
		case strings.HasPrefix(p, prefix):
			rel = p[len(prefix):]
		default:
			continue
		}

		// Only immediate children (no subdirectories)
		if idx := strings.IndexByte(rel, '/'); idx >= 0 {
			// This is in a subdirectory, only add the subdirectory once
			sub := rel[:idx]
			if seen[sub] {
				continue
			}

			seen[sub] = true

			subPath := sub
			if dirPath != "." {
				subPath = path.Join(dirPath, sub)
			}

			// Look up the actual subdirectory entry
			if subdir, exists := m.files[subPath]; exists {
				entries = append(entries, &FileInfo{
					name:    sub,
					mode:    subdir.Mode,
					modTime: subdir.ModTime,
					size:    0,
				})
			}
			continue
		}

		// Direct child file
		if !seen[rel] {
			seen[rel] = true
			entries = append(entries, &FileInfo{
				name:    rel,
				mode:    file.Mode,
				modTime: file.ModTime,
				size:    int64(len(file.Data)),
			})
		}
	}

	return entries
}

// createReadDirClosure generates a ReadDir handler for a directory.
// The handler returns fs.DirEntry implementations that delegate to MockFile.Stat().
func createReadDirClosure(entries []fs.DirEntry) func(int) ([]fs.DirEntry, error) {
	var offset int
	entryCount := len(entries)

	return func(n int) ([]fs.DirEntry, error) {
		// Check if we are already at the end
		if offset >= entryCount {
			if n > 0 {
				return nil, io.EOF
			}
			return nil, nil
		}

		// Calculate the end of the batch
		end := entryCount
		next := offset + n
		if n > 0 && next < entryCount {
			end = offset + n
		}

		// Return the batch
		batch := entries[offset:end]
		offset = end

		// Handle EOF
		if n > 0 && offset >= entryCount {
			return batch, io.EOF
		}

		return batch, nil
	}
}

// validateSubdir cleans and validates the target directory path for Sub.
func (m *MockFS) validateSubdir(dir string) (cleanDir string, info fs.FileInfo, err error) {
	// Special case: fs.Sub() has specific behavior for ".", which we must replicate
	// by disallowing it here, as it doesn't make sense for our mock SubFS
	if dir == "." || dir == "/" || !fs.ValidPath(dir) {
		return "", nil, &fs.PathError{Op: "Sub", Path: dir, Err: ErrInvalid}
	}
	cleanDir = path.Clean(dir)

	// Check if directory exists
	m.mu.RLock()
	mapFile, exists := m.files[cleanDir]
	m.mu.RUnlock()

	if !exists {
		return "", nil, &fs.PathError{Op: "Sub", Path: dir, Err: ErrNotExist}
	}

	if !mapFile.Mode.IsDir() {
		return "", nil, &fs.PathError{Op: "Sub", Path: dir, Err: ErrNotDir}
	}

	info = &FileInfo{
		name:    path.Base(cleanDir),
		size:    0,
		mode:    mapFile.Mode,
		modTime: mapFile.ModTime,
	}

	return cleanDir, info, nil
}

// copyFilesToSubFS populates the sub-filesystem's file map from the parent.
// It assumes the caller has acquired the necessary read lock on the parent MockFS.
func (m *MockFS) copyFilesToSubFS(subFS *MockFS, cleanDir string, dirInfo fs.FileInfo) {
	prefix := cleanDir + "/"

	// Map the directory itself to "." in the sub-filesystem
	subFS.files["."] = &fstest.MapFile{
		Mode:    dirInfo.Mode() | ModeDir,
		ModTime: dirInfo.ModTime(),
	}

	// Copy all descendant files
	for p, file := range m.files {
		if strings.HasPrefix(p, prefix) {
			// Get path relative to subDir
			subPath := p[len(prefix):]

			// Deep copy the MapFile to the sub filesystem
			if file != nil {
				newFile := *file
				if file.Data != nil {
					newFile.Data = append([]byte(nil), file.Data...)
				}
				subFS.files[subPath] = &newFile
			}
		}
	}
}

// ensureParentDirs creates all parent directories for a path if they don't exist.
// Caller must hold the mutex and have validated the path.
func (m *MockFS) ensureParentDirs(opName, pathStr string) error {
	dir := path.Dir(pathStr)
	if dir == "." || dir == "/" {
		return nil
	}

	// Use the core recursive creation logic up to the parent directory
	return m.mkdirAll(opName, dir, defaultDirPerm)
}

// mkdir is the core logic for single-directory creation.
// It assumes the lock is held and path is cleaned/validated.
func (m *MockFS) mkdir(opName, cleanPath string, perm FileMode) error {
	// Check if already exists
	if _, exists := m.files[cleanPath]; exists {
		return &fs.PathError{Op: opName, Path: cleanPath, Err: fs.ErrExist}
	}

	// Check parent exists and is a directory
	parent := path.Dir(cleanPath)
	if parent != "." {
		parentFile, exists := m.files[parent]
		if !exists {
			return &fs.PathError{Op: opName, Path: cleanPath, Err: ErrNotExist}
		}
		if !parentFile.Mode.IsDir() {
			return &fs.PathError{Op: opName, Path: cleanPath, Err: ErrNotDir}
		}
	}

	// Create the directory
	m.files[cleanPath] = &fstest.MapFile{
		Mode:    (perm & ModePerm) | ModeDir,
		ModTime: time.Now(),
	}

	return nil
}

// mkdirAll is the core logic for recursive directory creation.
// It assumes the lock is held and path is cleaned/validated.
func (m *MockFS) mkdirAll(opName, cleanPath string, perm FileMode) error {
	parts := strings.Split(cleanPath, "/")
	currentPath := ""

	for i, part := range parts {
		if part == "" {
			continue
		}

		if i > 0 {
			currentPath += "/"
		}
		currentPath += part

		if existing, exists := m.files[currentPath]; exists {
			// Check if it's a file blocking the path
			if !existing.Mode.IsDir() {
				// ErrNotDir means a file exists where a directory should be
				return &fs.PathError{Op: opName, Path: cleanPath, Err: ErrNotDir}
			}
			continue
		}

		// Create it
		m.files[currentPath] = &fstest.MapFile{
			Mode:    (perm & ModePerm) | ModeDir,
			ModTime: time.Now(),
		}
	}

	return nil
}

// validateAndCleanPath validates and cleans the path, returning an error if invalid.
// Path validation happens before any other operation (including error injection).
func (m *MockFS) validateAndCleanPath(p string, op Operation) (string, error) {
	if !fs.ValidPath(p) {
		return "", &fs.PathError{Op: op.String(), Path: p, Err: ErrInvalid}
	}
	return path.Clean(p), nil
}

// toBytes converts a variety of input types into a byte slice.
// It never panics and never returns nil if error is nil, but may return an empty slice.
func toBytes(content any) (data []byte, err error) {
	// Recover from panics caused by typed nils or buggy third-party methods.
	defer func() {
		if r := recover(); r != nil {
			data = []byte{}
			err = fmt.Errorf("panic converting %T: %v", content, r)
		}
	}()

	if content == nil {
		return []byte{}, nil
	}

	switch v := content.(type) {
	case []byte:
		// Clone to ensure immutability and return non-nil slice.
		data = make([]byte, len(v))
		copy(data, v)
	case string:
		data = []byte(v)
	case io.Reader:
		data, err = io.ReadAll(v)
	case encoding.BinaryMarshaler:
		data, err = v.MarshalBinary()
	case fmt.Stringer:
		data = []byte(v.String())
	default:
		data = fmt.Append([]byte{}, content)
	}

	if err != nil {
		return []byte{}, err
	}

	return data, nil
}
