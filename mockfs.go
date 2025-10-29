// Package mockfs provides a mock filesystem implementation based on [testing/fstest.MapFS],
// allowing for controlled error injection and latency simulation for testing purposes.
package mockfs

import (
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"testing/fstest"
	"time"
)

// A MapFile describes a single file in a MapFS.
// See [testing/fstest.MapFile].
type MapFile = fstest.MapFile

// WritableFS is an extension of fs.FS that supports write operations.
// It mirrors the mutating side of the [os] standard package.
type WritableFS interface {
	fs.FS

	// Mkdir creates a directory in the filesystem.
	Mkdir(path string, perm fs.FileMode) error

	// MkdirAll creates a directory path and all parents if needed.
	MkdirAll(path string, perm fs.FileMode) error

	// Remove removes a file or directory from the filesystem.
	Remove(path string) error

	// RemoveAll removes a path and any children recursively.
	RemoveAll(path string) error

	// Rename renames a file or directory in the filesystem.
	// If the destination already exists, it will be overwritten.
	Rename(oldpath, newpath string) error

	// WriteFile writes data to a file in the filesystem.
	WriteFile(path string, data []byte, perm fs.FileMode) error
}

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

// MockFSOption is a function type for configuring MockFS.
type MockFSOption func(*MockFS)

// WithErrorInjector sets the error injector for the MockFS.
func WithErrorInjector(injector ErrorInjector) MockFSOption {
	return func(m *MockFS) {
		if injector != nil {
			m.injector = injector
		}
	}
}

// WithCreateIfMissing sets whether writes should create files when missing.
// Default behavior is to return an error.
func WithCreateIfMissing(create bool) MockFSOption {
	return func(m *MockFS) {
		m.createIfMissing = create
	}
}

// WithReadOnly explicitly marks the filesystem as read-only.
func WithReadOnly() MockFSOption {
	return func(m *MockFS) {
		m.writeMode = writeModeReadOnly
	}
}

// WithOverwrite sets the write policy to overwrite existing contents.
// The existing contents will be replaced by the new data.
func WithOverwrite() MockFSOption {
	return func(m *MockFS) {
		m.writeMode = writeModeOverwrite
	}
}

// WithAppend sets the write policy to append data to existing contents.
// The new data will be appended to the existing contents.
func WithAppend() MockFSOption {
	return func(m *MockFS) {
		m.writeMode = writeModeAppend
	}
}

// WithLatency sets the simulated latency for all operations.
func WithLatency(duration time.Duration) MockFSOption {
	return func(m *MockFS) {
		m.latency = NewLatencySimulator(duration)
	}
}

// WithLatencySimulator sets a custom latency simulator for operations.
func WithLatencySimulator(sim LatencySimulator) MockFSOption {
	return func(m *MockFS) {
		if sim != nil {
			m.latency = sim
		}
	}
}

// WithPerOperationLatency sets different latencies for different operations.
func WithPerOperationLatency(durations map[Operation]time.Duration) MockFSOption {
	return func(m *MockFS) {
		m.latency = NewLatencySimulatorPerOp(durations)
	}
}

// NewMockFS creates a new MockFS with the given MapFile data and options.
// Nil MapFile entries in the initial map are ignored.
// If no root directory (".") is provided, one is created automatically.
func NewMockFS(initial map[string]*MapFile, opts ...MockFSOption) *MockFS {
	files := make(map[string]*fstest.MapFile)

	// Create a deep copy of the input map to avoid external modifications
	for p, file := range initial {
		if file == nil {
			continue // Skip nil entries
		}
		cleanPath := cleanPath(p)
		newFile := *file
		if file.Data != nil {
			newFile.Data = append([]byte(nil), file.Data...)
		}
		files[cleanPath] = &newFile
	}

	// Ensure root directory exists if not explicitly provided
	if _, exists := files["."]; !exists {
		files["."] = &fstest.MapFile{
			Mode:    fs.ModeDir | 0755,
			ModTime: time.Now(),
		}
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
		opt(m)
	}

	return m
}

// ErrorInjector returns the error injector for advanced configuration.
func (m *MockFS) ErrorInjector() ErrorInjector {
	return m.injector
}

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
		err = &fs.PathError{Op: "Stat", Path: name, Err: fs.ErrNotExist}
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
		err = &fs.PathError{Op: "Open", Path: name, Err: fs.ErrNotExist}
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

// createReadDirHandler generates a ReadDir handler for a directory.
// The handler returns fs.DirEntry implementations that delegate to MockFile.Stat().
func (m *MockFS) createReadDirHandler(dirPath string) func(int) ([]fs.DirEntry, error) {
	m.mu.RLock()

	var entries []fs.DirEntry
	prefix := dirPrefix(dirPath)
	seen := make(map[string]bool)

	// Collect immediate children only
	for p, file := range m.files {
		if p == dirPath {
			continue // Skip the directory itself
		}

		var relative string
		if prefix == "" {
			relative = p
		} else if strings.HasPrefix(p, prefix) {
			relative = p[len(prefix):]
		} else {
			continue
		}

		// Only immediate children (no subdirectories)
		if idx := strings.Index(relative, "/"); idx != -1 {
			// This is in a subdirectory, only add the subdirectory once
			subdirName := relative[:idx]
			if !seen[subdirName] {
				seen[subdirName] = true
				// Look up the actual subdirectory entry
				subdirPath := joinPath(dirPath, subdirName)
				if subdir, exists := m.files[subdirPath]; exists {
					entries = append(entries, &FileInfo{
						name:    subdirName,
						mode:    subdir.Mode,
						modTime: subdir.ModTime,
						size:    0,
					})
				}
			}
			continue
		}

		// Direct child file
		if !seen[relative] {
			seen[relative] = true
			entries = append(entries, &FileInfo{
				name:    relative,
				mode:    file.Mode,
				modTime: file.ModTime,
				size:    int64(len(file.Data)),
			})
		}
	}
	m.mu.RUnlock()

	// Sort entries by name for deterministic output
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// Return the stateful handler
	var offset int
	return func(n int) ([]fs.DirEntry, error) {
		// Check if we are already at the end
		if offset >= len(entries) {
			if n > 0 {
				return []fs.DirEntry{}, io.EOF
			}
			return []fs.DirEntry{}, nil
		}

		// Calculate the end of the batch
		end := offset + n
		if n <= 0 {
			end = len(entries)
		}
		if end > len(entries) {
			end = len(entries)
		}

		// Return the batch
		batch := entries[offset:end]
		offset = end

		// Handle EOF
		if n > 0 && offset >= len(entries) {
			return batch, io.EOF
		}

		return batch, nil
	}
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
		err = &fs.PathError{Op: "ReadDir", Path: name, Err: fs.ErrNotExist}
		return nil, err
	}

	if !mapFile.Mode.IsDir() {
		err = &fs.PathError{Op: "ReadDir", Path: name, Err: ErrNotDir}
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

// validateSubdir cleans and validates the target directory path for Sub.
func (m *MockFS) validateSubdir(dir string) (cleanDir string, info fs.FileInfo, err error) {
	// Special case: fs.Sub() has specific behavior for ".", which we must replicate
	// by disallowing it here, as it doesn't make sense for our mock SubFS
	if dir == "." || dir == "/" || !fs.ValidPath(dir) {
		return "", nil, &fs.PathError{Op: "Sub", Path: dir, Err: fs.ErrInvalid}
	}
	cleanDir = cleanPath(dir)

	// Check if directory exists
	m.mu.RLock()
	mapFile, exists := m.files[cleanDir]
	m.mu.RUnlock()

	if !exists {
		return "", nil, &fs.PathError{Op: "Sub", Path: dir, Err: fs.ErrNotExist}
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
		Mode:    dirInfo.Mode() | fs.ModeDir,
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

// --- File/Directory Management ---

// AddFile adds a text file to the mock filesystem.
// If the file already exists, it is overwritten.
// Returns an error if the path is invalid.
func (m *MockFS) AddFile(filePath string, content string, mode fs.FileMode) error {
	return m.addFile(filePath, []byte(content), mode)
}

// AddFileBytes adds a binary file to the mock filesystem.
// If the file already exists, it is overwritten.
// Returns an error if the path is invalid.
func (m *MockFS) AddFileBytes(filePath string, content []byte, mode fs.FileMode) error {
	return m.addFile(filePath, content, mode)
}

// addFile is the internal implementation for adding files.
func (m *MockFS) addFile(filePath string, content []byte, mode fs.FileMode) error {
	// Validate original path first; it should not have a trailing slash
	if !fs.ValidPath(filePath) || strings.HasSuffix(filePath, "/") {
		return &fs.PathError{Op: "AddFile", Path: filePath, Err: fs.ErrInvalid}
	}

	cleanPath := cleanPath(filePath)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.files[cleanPath] = &fstest.MapFile{
		Data:    append([]byte(nil), content...), // Deep copy
		Mode:    mode &^ fs.ModeDir,
		ModTime: time.Now(),
	}
	return nil
}

// AddDir adds a directory to the mock filesystem.
// If the directory already exists, it is overwritten.
// Returns an error if the path is invalid.
func (m *MockFS) AddDir(dirPath string, mode fs.FileMode) error {
	// Disallow adding "." as a directory explicitly
	if !fs.ValidPath(dirPath) || dirPath == "." {
		return &fs.PathError{Op: "AddDir", Path: dirPath, Err: fs.ErrInvalid}
	}

	cleanPath := cleanPath(dirPath)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.files[cleanPath] = &fstest.MapFile{
		Mode:    (mode & fs.ModePerm) | fs.ModeDir,
		ModTime: time.Now(),
		// Data should be nil for directory
	}
	return nil
}

// RemovePath removes a file or directory from the mock filesystem.
// Note: This does not recursively remove directory contents.
// Returns an error if the path is invalid.
func (m *MockFS) RemovePath(filePath string) error {
	if !fs.ValidPath(filePath) {
		return &fs.PathError{Op: "RemovePath", Path: filePath, Err: fs.ErrInvalid}
	}

	cleanPath := cleanPath(filePath)

	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.files, cleanPath)
	return nil
}

// --- Error Injection Configuration ---

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
		cleanPath := cleanPath(p)
		_ = m.RemovePath(cleanPath) // Remove from map first
		m.injector.AddGlobForAllOps(cleanPath, fs.ErrNotExist, ErrorModeAlways, 0)
	}
}

// ClearErrors removes all configured error injection rules for all operations.
func (m *MockFS) ClearErrors() {
	m.injector.Clear()
}

// --- WritableFS Implementation ---

// Mkdir creates a directory in the filesystem.
func (m *MockFS) Mkdir(dirPath string, perm fs.FileMode) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpMkdir, 0, err) }()

	cleanPath, err := m.validateAndCleanPath(dirPath, OpMkdir)
	if err != nil {
		return err
	}

	if err = m.injector.CheckAndApply(OpMkdir, cleanPath); err != nil {
		return err
	}

	m.latency.Simulate(OpMkdir)

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.files[cleanPath]; exists {
		err = &fs.PathError{Op: "Mkdir", Path: dirPath, Err: fs.ErrExist}
		return err
	}

	// Check parent exists and is a directory
	parent := path.Dir(cleanPath)
	if parent != "." && parent != cleanPath {
		parentFile, exists := m.files[parent]
		if !exists {
			err = &fs.PathError{Op: "Mkdir", Path: dirPath, Err: fs.ErrNotExist}
			return err
		}
		if !parentFile.Mode.IsDir() {
			err = &fs.PathError{Op: "Mkdir", Path: dirPath, Err: ErrNotDir}
			return err
		}
	}

	m.files[cleanPath] = &fstest.MapFile{
		Mode:    (perm & fs.ModePerm) | fs.ModeDir,
		ModTime: time.Now(),
	}
	return nil
}

// MkdirAll creates a directory path and all parents if needed.
func (m *MockFS) MkdirAll(dirPath string, perm fs.FileMode) (err error) {
	// Record the result of this operation on exit
	defer func() { m.stats.Record(OpMkdirAll, 0, err) }()

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

	// Check if already exists
	if existing, exists := m.files[cleanPath]; exists {
		if !existing.Mode.IsDir() {
			err = &fs.PathError{Op: "MkdirAll", Path: dirPath, Err: ErrNotDir}
			return err
		}
		return nil
	}

	// Create all parent directories
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

		if _, exists := m.files[currentPath]; !exists {
			m.files[currentPath] = &fstest.MapFile{
				Mode:    (perm & fs.ModePerm) | fs.ModeDir,
				ModTime: time.Now(),
			}
		} else {
			if !m.files[currentPath].Mode.IsDir() {
				err = &fs.PathError{Op: "MkdirAll", Path: dirPath, Err: ErrNotDir}
				return err
			}
		}
	}

	return nil
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
		err = &fs.PathError{Op: "Remove", Path: filePath, Err: fs.ErrNotExist}
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
		err = &fs.PathError{Op: "Rename", Path: oldpath, Err: fs.ErrNotExist}
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
func (m *MockFS) WriteFile(filePath string, data []byte, perm fs.FileMode) (err error) {
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
			Mode:    perm &^ fs.ModeDir,
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

// validateAndCleanPath validates and cleans the path, returning an error if invalid.
// Path validation happens before any other operation (including error injection).
func (m *MockFS) validateAndCleanPath(p string, op Operation) (string, error) {
	if !fs.ValidPath(p) {
		return "", &fs.PathError{Op: op.String(), Path: p, Err: fs.ErrInvalid}
	}
	return cleanPath(p), nil
}

// cleanPath returns the shortest path name equivalent to path by purely lexical processing.
func cleanPath(p string) string {
	return path.Clean(p)
}

// dirPrefix returns the prefix for directory filtering.
func dirPrefix(dirPath string) string {
	if dirPath == "." {
		return ""
	}
	return dirPath + "/"
}

// joinPath joins directory and name, handling "." correctly.
func joinPath(dirPath, name string) string {
	if dirPath == "." {
		return name
	}
	return dirPath + "/" + name
}
