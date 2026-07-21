package mockfs

import (
	"bytes"
	"encoding"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"sync"
	"testing/fstest"
	"time"
)

// A FileMode represents a file's mode and permission bits.
// See [io/fs.FileMode].
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

// FsOption configures the MockFS or adds entries to it.
type FsOption func(fs *MockFS) error

// File adds a file at the current context path.
// The content will be converted to a byte slice.
// The mode is optional, defaulting to 0644.
//
// Note: File does not create parent directories.
// The hierarchy must be built explicitly using Dir().
func File(name string, content any, mode ...FileMode) FsOption {
	opName := "File"

	return func(m *MockFS) error {
		if name == "" {
			return fmt.Errorf("%s: empty file name", opName)
		}
		// Strict hierarchical validation: name must be a single segment
		if strings.ContainsRune(name, '/') || name == "." || name == ".." {
			return fmt.Errorf("%s: invalid name %q (must be a single path segment)", opName, name)
		}

		fullPath := name
		if m.buildCtx != "." {
			fullPath = path.Join(m.buildCtx, name)
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
// Mixed arguments are supported: FileMode sets permissions, FsOption adds children.
//
// Note: Dir does not create parent directories.
// The hierarchy must be built explicitly using nested Dir() calls.
func Dir(name string, args ...any) FsOption {
	opName := "Dir"

	return func(m *MockFS) error {
		if name == "" {
			return fmt.Errorf("%s: empty directory name", opName)
		}
		// Strict hierarchical validation
		if strings.ContainsRune(name, '/') || name == "." || name == ".." {
			return fmt.Errorf("%s: invalid name %q (must be a single path segment)", opName, name)
		}

		callerCtx := m.buildCtx
		fullPath := name
		if callerCtx != "." {
			fullPath = path.Join(callerCtx, name)
		}
		cleanPath := path.Clean(fullPath)

		perm := defaultDirPerm
		var children []FsOption

		// Argument parsing
		for _, arg := range args {
			switch v := arg.(type) {
			case FileMode:
				perm = v
			case FsOption:
				children = append(children, v)
			default:
				return fmt.Errorf("%s: invalid argument for %s: %T", opName, cleanPath, v)
			}
		}

		// Create the directory entry itself
		m.files[cleanPath] = &fstest.MapFile{
			Mode:    (perm & ModePerm) | ModeDir,
			ModTime: time.Now(),
		}

		// Apply children with cleanPath as their context; restore the
		// caller's context on return so sibling options in the parent
		// Dir (if any) see their own context, not this one's.
		m.buildCtx = cleanPath
		defer func() { m.buildCtx = callerCtx }()

		for _, child := range children {
			if err := child(m); err != nil {
				return err
			}
		}
		return nil
	}
}

// WithErrorInjector sets the error injector for the MockFS.
func WithErrorInjector(injector ErrorInjector) FsOption {
	return func(m *MockFS) error {
		if injector != nil {
			m.injector = injector
		}
		return nil
	}
}

// WithCreateIfMissing sets whether writes should create files when missing.
// Default behavior is to return an error.
func WithCreateIfMissing(create bool) FsOption {
	return func(m *MockFS) error {
		m.createIfMissing = create
		return nil
	}
}

// WithReadOnly explicitly marks the filesystem as read-only.
func WithReadOnly() FsOption {
	return func(m *MockFS) error {
		m.writeMode = writeModeReadOnly
		return nil
	}
}

// WithOverwrite sets the write policy to overwrite existing contents.
// The existing contents will be replaced by the new data.
func WithOverwrite() FsOption {
	return func(m *MockFS) error {
		m.writeMode = writeModeOverwrite
		return nil
	}
}

// WithAppend sets the write policy to append data to existing contents.
// The new data will be appended to the existing contents.
func WithAppend() FsOption {
	return func(m *MockFS) error {
		m.writeMode = writeModeAppend
		return nil
	}
}

// WithLatency sets the simulated latency for all operations.
func WithLatency(duration time.Duration) FsOption {
	return func(m *MockFS) error {
		m.latency = NewLatencySimulator(duration)
		return nil
	}
}

// WithLatencySimulator sets a custom latency simulator for operations.
func WithLatencySimulator(sim LatencySimulator) FsOption {
	return func(m *MockFS) error {
		if sim != nil {
			m.latency = sim
		}
		return nil
	}
}

// WithPerOperationLatency sets different latencies for different operations.
func WithPerOperationLatency(durations map[Operation]time.Duration) FsOption {
	return func(m *MockFS) error {
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
	buildCtx        string                     // Current path context for File()/Dir() during NewMockFS; the value held after NewMockFS returns has no further meaning.
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
//
// Panics if any option returns an error (e.g. an invalid path passed to
// File() or Dir()). These represent programmer errors in test setup code.
func NewMockFS(opts ...FsOption) *MockFS {
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
		buildCtx:        ".",
	}

	// Apply options
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(m); err != nil {
			//nolint:forbidigo // Panic is intentional here to mark incorrect use
			panic(fmt.Sprintf("mockfs: failed to apply option %v", err))
		}
	}

	return m
}

// Stat returns file information for the given path.
// It implements the fs.StatFS interface.
// This is a filesystem-level operation that does not open the file.
//
//nolint:nonamedreturns // Deferred function is using the named returns.
func (m *MockFS) Stat(name string) (fi fs.FileInfo, err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpStat, 0, err) }()

	cleanName, err := m.validateAndCleanPath(name, OpStat)
	if err != nil {
		return nil, err
	}

	basename := cleanName
	if idx := strings.LastIndexByte(cleanName, '/'); idx >= 0 {
		basename = cleanName[idx+1:]
	}

	if err := m.injector.CheckAndApply(OpStat, cleanName); err != nil {
		return nil, fmt.Errorf("mockfs:  %w", err)
	}

	m.latency.Simulate(OpStat)

	m.mu.RLock()
	mapFile, exists := m.files[cleanName]
	m.mu.RUnlock()

	if !exists {
		return nil, &fs.PathError{Op: OpStat.String(), Path: name, Err: fs.ErrNotExist}
	}

	// Build FileInfo from MapFile
	return &FileInfo{
		name:    basename,
		size:    int64(len(mapFile.Data)),
		mode:    mapFile.Mode,
		modTime: mapFile.ModTime,
	}, nil
}

// Open opens the named file and returns a MockFile.
// It implements the fs.FS interface.
// This is a filesystem-level operation. The returned MockFile handles file-level operations.
// Use OpenMockFile to obtain the concrete *MockFile directly without a type assertion.
//
//nolint:nonamedreturns // Deferred function is using the named returns.
func (m *MockFS) Open(name string) (f fs.File, err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpOpen, 0, err) }()

	cleanName, err := m.validateAndCleanPath(name, OpOpen)
	if err != nil {
		return nil, err
	}

	if err := m.injector.CheckAndApply(OpOpen, cleanName); err != nil {
		return nil, fmt.Errorf("mockfs:  %w", err)
	}

	m.latency.Simulate(OpOpen)

	m.mu.RLock()
	mapFile, exists := m.files[cleanName]
	m.mu.RUnlock()

	if !exists {
		return nil, &fs.PathError{Op: OpOpen.String(), Path: name, Err: ErrNotExist}
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

// OpenMockFile opens the named file and returns the concrete *MockFile directly.
// This avoids the type assertion required when using Open and is the preferred
// method when access to Stats, ErrorInjector, or LatencySimulator is needed on
// the returned file handle.
func (m *MockFS) OpenMockFile(name string) (*MockFile, error) {
	f, err := m.Open(name)
	if err != nil {
		return nil, err
	}
	mf, ok := f.(*MockFile)
	if !ok {
		return nil, fmt.Errorf("mockfs: OpenMockFile: Open returned unexpected type %T for %q", f, name)
	}
	return mf, nil
}

// ReadFile implements the fs.ReadFileFS interface.
// It opens the file, reads it, and closes it.
// Note: OpOpen is recorded by Open(), OpRead and OpClose by MockFile.
//
//nolint:nonamedreturns // Deferred function is using the named returns.
func (m *MockFS) ReadFile(name string) (data []byte, err error) {
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
	defer func() {
		closeErr := file.Close()
		if closeErr == nil {
			return
		}
		if err != nil {
			err = errors.Join(err, closeErr)
		} else {
			err = closeErr
		}
	}()

	data, err = io.ReadAll(file)
	return data, fmt.Errorf("mockfs:  %w", err)
}

// ReadDir implements the fs.ReadDirFS interface.
// This is a filesystem-level operation.
//
//nolint:nonamedreturns // Deferred function is using the named returns.
func (m *MockFS) ReadDir(name string) (de []fs.DirEntry, err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpReadDir, 0, err) }()

	cleanName, err := m.validateAndCleanPath(name, OpReadDir)
	if err != nil {
		return nil, err
	}

	if err := m.injector.CheckAndApply(OpReadDir, cleanName); err != nil {
		return nil, fmt.Errorf("mockfs:  %w", err)
	}

	m.latency.Simulate(OpReadDir)

	m.mu.RLock()
	mapFile, exists := m.files[cleanName]
	m.mu.RUnlock()

	if !exists {
		return nil, &fs.PathError{Op: OpReadDir.String(), Path: name, Err: ErrNotExist}
	}

	if !mapFile.Mode.IsDir() {
		return nil, &fs.PathError{Op: OpReadDir.String(), Path: name, Err: ErrNotDir}
	}

	// Use the same handler logic
	handler := m.createReadDirHandler(cleanName)
	return handler(-1)
}

// Sub implements fs.SubFS to return a sub-filesystem.
// Passing "." returns the receiver unchanged, matching stdlib fs.Sub behaviour.
func (m *MockFS) Sub(dir string) (fs.FS, error) {
	if dir == "." {
		return m, nil
	}

	cleanDir, info, err := m.validateSubdir(dir)
	if err != nil {
		return nil, err
	}

	subFS := NewMockFS()
	subFS.latency = m.latency.Clone()
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
		Data:    data, // already deep copied by toBytes
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

// failExact wraps injector.AddExact with exact-path matching and consistent
// error wrapping. All FailX/FailXOnce/FailReadAfter methods delegate here.
func (m *MockFS) failExact(op Operation, filepath string, err error, mode ErrorMode, after int) error {
	if ruleErr := m.injector.AddExact(op, filepath, err, mode, after); ruleErr != nil {
		return fmt.Errorf("mockfs:  %w", ruleErr)
	}
	return nil
}

// FailStat configures a path to return the specified error on Stat operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailStat(filepath string, err error) error {
	return m.failExact(OpStat, filepath, err, ErrorModeAlways, 0)
}

// FailStatOnce configures a path to return the specified error once on Stat operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailStatOnce(filepath string, err error) error {
	return m.failExact(OpStat, filepath, err, ErrorModeOnce, 0)
}

// FailOpen configures a path to return the specified error on Open operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailOpen(filepath string, err error) error {
	return m.failExact(OpOpen, filepath, err, ErrorModeAlways, 0)
}

// FailOpenOnce configures a path to return the specified error once on Open operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailOpenOnce(filepath string, err error) error {
	return m.failExact(OpOpen, filepath, err, ErrorModeOnce, 0)
}

// FailRead configures a path to return the specified error on Read operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailRead(filepath string, err error) error {
	return m.failExact(OpRead, filepath, err, ErrorModeAlways, 0)
}

// FailReadOnce configures a path to return the specified error once on Read operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailReadOnce(filepath string, err error) error {
	return m.failExact(OpRead, filepath, err, ErrorModeOnce, 0)
}

// FailReadAfter configures a read error after N successful reads.
// If successes=0, the error is returned immediately (no successful reads allowed).
// If successes=3, the first 3 reads succeed, the 4th read fails.
// Returns an error if successes is negative.
func (m *MockFS) FailReadAfter(filepath string, err error, successes int) error {
	return m.failExact(OpRead, filepath, err, ErrorModeAfterSuccesses, successes)
}

// FailReadNext configures the next N read operations to fail, then succeed.
// Returns an error if count is negative.
func (m *MockFS) FailReadNext(filepath string, err error, count int) error {
	rule, ruleErr := NewErrorRule(err, ErrorModeNext, count, NewExactMatcher(filepath))
	if ruleErr != nil {
		return ruleErr
	}
	// Special handling: negative 'after' means "fail next N, then succeed"
	m.injector.Add(OpRead, rule)
	return nil
}

// FailWrite configures a path to return the specified error on Write operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailWrite(filepath string, err error) error {
	return m.failExact(OpWrite, filepath, err, ErrorModeAlways, 0)
}

// FailWriteOnce configures a path to return the specified error once on Write operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailWriteOnce(filepath string, err error) error {
	return m.failExact(OpWrite, filepath, err, ErrorModeOnce, 0)
}

// FailReadDir configures a path to return the specified error on ReadDir operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailReadDir(filepath string, err error) error {
	return m.failExact(OpReadDir, filepath, err, ErrorModeAlways, 0)
}

// FailReadDirOnce configures a path to return the specified error once on ReadDir operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailReadDirOnce(filepath string, err error) error {
	return m.failExact(OpReadDir, filepath, err, ErrorModeOnce, 0)
}

// FailClose configures a path to return the specified error on Close operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailClose(filepath string, err error) error {
	return m.failExact(OpClose, filepath, err, ErrorModeAlways, 0)
}

// FailCloseOnce configures a path to return the specified error once on Close operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailCloseOnce(filepath string, err error) error {
	return m.failExact(OpClose, filepath, err, ErrorModeOnce, 0)
}

// FailMkdir configures a path to return the specified error on Mkdir operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailMkdir(filepath string, err error) error {
	return m.failExact(OpMkdir, filepath, err, ErrorModeAlways, 0)
}

// FailMkdirOnce configures a path to return the specified error once on Mkdir operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailMkdirOnce(filepath string, err error) error {
	return m.failExact(OpMkdir, filepath, err, ErrorModeOnce, 0)
}

// FailMkdirAll configures a path to return the specified error on MkdirAll operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailMkdirAll(filepath string, err error) error {
	return m.failExact(OpMkdirAll, filepath, err, ErrorModeAlways, 0)
}

// FailMkdirAllOnce configures a path to return the specified error once on MkdirAll operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailMkdirAllOnce(filepath string, err error) error {
	return m.failExact(OpMkdirAll, filepath, err, ErrorModeOnce, 0)
}

// FailRemove configures a path to return the specified error on Remove operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailRemove(filepath string, err error) error {
	return m.failExact(OpRemove, filepath, err, ErrorModeAlways, 0)
}

// FailRemoveOnce configures a path to return the specified error once on Remove operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailRemoveOnce(filepath string, err error) error {
	return m.failExact(OpRemove, filepath, err, ErrorModeOnce, 0)
}

// FailRemoveAll configures a path to return the specified error on RemoveAll operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailRemoveAll(filepath string, err error) error {
	return m.failExact(OpRemoveAll, filepath, err, ErrorModeAlways, 0)
}

// FailRemoveAllOnce configures a path to return the specified error once on RemoveAll operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailRemoveAllOnce(filepath string, err error) error {
	return m.failExact(OpRemoveAll, filepath, err, ErrorModeOnce, 0)
}

// FailRename configures a path to return the specified error on Rename operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailRename(filepath string, err error) error {
	return m.failExact(OpRename, filepath, err, ErrorModeAlways, 0)
}

// FailRenameOnce configures a path to return the specified error once on Rename operations.
// It returns an error only if the underlying rule configuration is invalid;
// for this fixed-mode call, that is unreachable.
func (m *MockFS) FailRenameOnce(filepath string, err error) error {
	return m.failExact(OpRename, filepath, err, ErrorModeOnce, 0)
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
		//nolint:errcheck // RemoveEntry only errors on an invalid fs path; MarkNonExistent has no error return to surface it through.
		_ = m.RemoveEntry(cleanPath) // Remove from map first
		//nolint:errcheck // AddGlobForAllOps only errors on a malformed glob pattern; MarkNonExistent has no error return to surface it through.
		_ = m.injector.AddGlobForAllOps(cleanPath, ErrNotExist, ErrorModeAlways, 0)
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
//
//nolint:nonamedreturns // Deferred function is using the named returns.
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
	if err := m.injector.CheckAndApply(OpMkdir, cleanPath); err != nil {
		return fmt.Errorf("mockfs:  %w", err)
	}
	m.latency.Simulate(OpMkdir)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Logic Layer
	return m.mkdir(OpMkdir.String(), cleanPath, perm)
}

// MkdirAll creates a directory path and all parents if needed.
//
//nolint:nonamedreturns // Deferred function is using the named returns.
func (m *MockFS) MkdirAll(dirPath string, perm FileMode) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpMkdirAll, 0, err) }()

	// Simulation Layer
	cleanPath, err := m.validateAndCleanPath(dirPath, OpMkdirAll)
	if err != nil {
		return err
	}
	if err := m.injector.CheckAndApply(OpMkdirAll, cleanPath); err != nil {
		return fmt.Errorf("mockfs:  %w", err)
	}
	m.latency.Simulate(OpMkdirAll)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Logic Layer
	return m.mkdirAll(OpMkdirAll.String(), cleanPath, perm)
}

// Remove removes a file or directory from the filesystem.
// Directories must be empty to be removed.
//
//nolint:nonamedreturns // Deferred function is using the named returns.
func (m *MockFS) Remove(filePath string) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpRemove, 0, err) }()

	cleanPath, err := m.validateAndCleanPath(filePath, OpRemove)
	if err != nil {
		return err
	}

	if err := m.injector.CheckAndApply(OpRemove, cleanPath); err != nil {
		return fmt.Errorf("mockfs:  %w", err)
	}

	m.latency.Simulate(OpRemove)

	m.mu.Lock()
	defer m.mu.Unlock()

	file, exists := m.files[cleanPath]
	if !exists {
		return &fs.PathError{Op: "Remove", Path: filePath, Err: ErrNotExist}
	}

	// If it's a directory, check it's empty
	if file.Mode.IsDir() {
		prefix := cleanPath + "/"
		for p := range m.files {
			if strings.HasPrefix(p, prefix) {
				return &fs.PathError{Op: "Remove", Path: filePath, Err: ErrNotEmpty}
			}
		}
	}

	delete(m.files, cleanPath)
	return nil
}

// RemoveAll removes a path and any children recursively.
//
//nolint:nonamedreturns // Deferred function is using the named returns.
func (m *MockFS) RemoveAll(filePath string) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpRemoveAll, 0, err) }()

	cleanPath, err := m.validateAndCleanPath(filePath, OpRemoveAll)
	if err != nil {
		return err
	}

	if err := m.injector.CheckAndApply(OpRemoveAll, cleanPath); err != nil {
		return fmt.Errorf("mockfs:  %w", err)
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
//
//nolint:nonamedreturns // Deferred function is using the named returns.
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

	if err := m.injector.CheckAndApply(OpRename, cleanOld); err != nil {
		return fmt.Errorf("mockfs:  %w", err)
	}

	m.latency.Simulate(OpRename)

	m.mu.Lock()
	defer m.mu.Unlock()

	oldFile, exists := m.files[cleanOld]
	if !exists {
		return &fs.PathError{Op: "Rename", Path: oldpath, Err: ErrNotExist}
	}

	// Copy to new location
	newFile := *oldFile
	newFile.Data = bytes.Clone(oldFile.Data)
	newFile.ModTime = time.Now()
	m.files[cleanNew] = &newFile

	// If directory, rename all children
	if oldFile.Mode.IsDir() {
		oldPrefix := cleanOld + "/"
		newPrefix := cleanNew + "/"
		for p, f := range m.files {
			if !strings.HasPrefix(p, oldPrefix) {
				continue
			}
			newP := newPrefix + p[len(oldPrefix):]
			childFile := *f
			childFile.Data = bytes.Clone(f.Data)
			m.files[newP] = &childFile
			delete(m.files, p)
		}
	}

	// Remove old location
	delete(m.files, cleanOld)
	return nil
}

// WriteFile writes data to a file in the filesystem.
//
//nolint:nonamedreturns // Deferred function is using the named returns.
func (m *MockFS) WriteFile(filePath string, data []byte, perm FileMode) (err error) {
	// Record the result of this operation on exit
	defer func() {
		written := 0
		if err == nil {
			written = len(data)
		}
		m.stats.Record(OpWrite, written, err)
	}()

	cleanPath, err := m.validateAndCleanPath(filePath, OpWrite)
	if err != nil {
		return err
	}

	if err := m.injector.CheckAndApply(OpWrite, cleanPath); err != nil {
		return fmt.Errorf("mockfs:  %w", err)
	}

	m.latency.Simulate(OpWrite)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check write mode restrictions
	if m.writeMode == writeModeReadOnly {
		return &fs.PathError{Op: "Write", Path: filePath, Err: ErrPermission}
	}

	// Find the existing file or create if it doesn't exist
	existing, ok := m.files[cleanPath]
	if !ok {
		if !m.createIfMissing {
			return &fs.PathError{Op: "Write", Path: filePath, Err: ErrNotExist}
		}

		m.files[cleanPath] = &fstest.MapFile{
			Data:    bytes.Clone(data),
			Mode:    perm &^ ModeDir,
			ModTime: time.Now(),
		}
		return nil
	}

	// Apply write mode
	switch m.writeMode {
	case writeModeAppend:
		existing.Data = append(existing.Data, data...)
		existing.ModTime = time.Now()
		return nil

	case writeModeOverwrite:
		existing.Data = bytes.Clone(data)
		existing.ModTime = time.Now()
		return nil

	default:
		//nolint:forbidigo // Panic is intentional here to mark incorrect use
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
	slices.SortFunc(entries, func(a, b fs.DirEntry) int {
		return strings.Compare(a.Name(), b.Name())
	})

	return createReadDirClosure(entries)
}

// collectDirEntries returns all directory entries for a directory.
func (m *MockFS) collectDirEntries(dirPath, prefix string) []fs.DirEntry {
	seen := make(map[string]bool)
	entries := make([]fs.DirEntry, 0, 16)

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
		if before, _, ok := strings.Cut(rel, "/"); ok {
			// This is in a subdirectory, only add the subdirectory once
			sub := before
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
				return []fs.DirEntry{}, io.EOF
			}
			return []fs.DirEntry{}, nil
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
func (m *MockFS) validateSubdir(dir string) (string, fs.FileInfo, error) {
	if !fs.ValidPath(dir) {
		return "", nil, &fs.PathError{Op: "Sub", Path: dir, Err: ErrInvalid}
	}
	cleanDir := path.Clean(dir)

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

	info := &FileInfo{
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

			if file != nil {
				newFile := *file
				newFile.Data = bytes.Clone(file.Data)
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
	if cleanPath == "" {
		return nil
	}

	for i := 1; i <= len(cleanPath); i++ {
		if i != len(cleanPath) && cleanPath[i] != '/' {
			continue
		}
		// Trigger on separators or the end of the string
		currentPath := cleanPath[:i]

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
//
//nolint:nonamedreturns // Named return values are used in the deferred function.
func toBytes(content any) (data []byte, err error) {
	// Recover from panics to wrap with type information before re-panicking in caller.
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
		data = bytes.Clone(v)
	case string:
		data = []byte(v)
	case io.Reader:
		if data, err = io.ReadAll(v); err != nil {
			err = fmt.Errorf("ReadAll failed: %w", err)
		}
	case encoding.BinaryMarshaler:
		if data, err = v.MarshalBinary(); err != nil {
			err = fmt.Errorf("MarshalBinary failed: %w", err)
		}
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
