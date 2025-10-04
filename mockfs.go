// Package mockfs provides a mock filesystem implementation based on [testing/fstest.MapFS],
// allowing for controlled error injection and latency simulation for testing purposes.
package mockfs

import (
	"io"
	"io/fs"
	"path/filepath"
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

// MockFS wraps fstest.MapFS to inject errors for specific paths and operations.
type MockFS struct {
	fsys         fstest.MapFS                         // Internal MapFS.
	mu           sync.RWMutex                         // Mutex for concurrent file structure operations.
	injector     ErrorInjector                        // Error injector.
	counters     *Counters                            // Internal statistics.
	latency      time.Duration                        // Simulated latency.
	allowWrites  bool                                 // Flag to enable simulated writes.
	writeHandler func(path string, data []byte) error // Callback for write operations.
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

// Option is a function type for configuring MockFS.
type Option func(*MockFS)

// WithInjector sets the error injector for the MockFS.
func WithInjector(i ErrorInjector) Option {
	return func(m *MockFS) {
		if i != nil {
			m.injector = i
		}
	}
}

// WithWritesEnabled allows write operations, simulating them by modifying the internal MapFS.
// If handler is nil, a default handler that updates the internal map is used.
//
// Note: This modifies the underlying map, use with caution if the original map data is important.
func WithWritesEnabled(handler func(path string, data []byte) error) Option {
	return func(m *MockFS) {
		m.allowWrites = true

		if handler == nil {
			m.writeHandler = m.defaultWriteHandler
		} else {
			m.writeHandler = handler
		}
	}
}

// WithLatency sets the simulated latency for operations.
func WithLatency(d time.Duration) Option {
	return func(m *MockFS) {
		m.latency = d
	}
}

// NewMockFS creates a new MockFS with the given MapFS data and options.
func NewMockFS(initial map[string]*MapFile, opts ...Option) *MockFS {
	mapFS := make(fstest.MapFS)

	// Create a copy of the input map to avoid external modifications
	for path, file := range initial {
		cleanPath := filepath.Clean(path)
		if file != nil {
			newFile := *file
			newFile.Data = append([]byte(nil), file.Data...)
			mapFS[cleanPath] = &newFile
		} else {
			mapFS[cleanPath] = nil
		}
	}

	m := &MockFS{
		fsys:     mapFS,
		injector: NewErrorManager(),
		counters: NewCounters(),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GetInjector returns the error injector for advanced configuration.
func (m *MockFS) GetInjector() ErrorInjector {
	return m.injector
}

// GetStats returns the current operation counts.
func (m *MockFS) GetStats() *Counters {
	return m.counters.Clone()
}

// ResetStats resets all operation counters to zero.
func (m *MockFS) ResetStats() {
	m.counters.ResetAll()
}

// Stat intercepts the Stat call and returns configured errors for paths.
// It implements the fs.StatFS interface.
func (m *MockFS) Stat(name string) (fs.FileInfo, error) {
	cleanName := filepath.Clean(name)

	m.counters.inc(OpStat)

	if err := m.checkAndInjectError(OpStat, cleanName); err != nil {
		return nil, err
	}

	m.simulateLatency()

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.fsys.Stat(cleanName)
}

// Open intercepts the Open call and returns configured errors for paths.
// It implements the fs.FS interface.
func (m *MockFS) Open(name string) (fs.File, error) {
	cleanName := filepath.Clean(name)

	m.counters.inc(OpOpen)
	if err := m.checkAndInjectError(OpOpen, cleanName); err != nil {
		return nil, err
	}

	m.simulateLatency()

	m.mu.RLock()
	file, openErr := m.fsys.Open(cleanName)
	m.mu.RUnlock()
	if openErr != nil {
		return nil, openErr
	}

	var writeHandler func([]byte) (int, error)
	if m.allowWrites {
		// If the underlying file supports io.Writer, use it.
		if writer, ok := file.(io.Writer); ok {
			writeHandler = writer.Write
		} else if m.writeHandler != nil {
			// Capture cleanName in closure
			writeHandler = func(data []byte) (int, error) {
				if err := m.writeHandler(cleanName, data); err != nil {
					return 0, err
				}
				return len(data), nil
			}
		}
	}

	return NewMockFile(
		file,
		cleanName,
		m.checkAndInjectError,
		m.simulateLatency,
		m.counters.inc,
		writeHandler,
	), nil
}

// ReadFile implements the fs.ReadFileFS interface.
func (m *MockFS) ReadFile(name string) ([]byte, error) {
	cleanName := filepath.Clean(name)
	file, err := m.Open(cleanName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// ReadDir implements the fs.ReadDirFS interface.
func (m *MockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	cleanName := filepath.Clean(name)

	m.counters.inc(OpReadDir)

	if err := m.checkAndInjectError(OpReadDir, cleanName); err != nil {
		return nil, err
	}

	m.simulateLatency()

	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.fsys.ReadDir(cleanName)
}

// Sub implements fs.SubFS to return a sub-filesystem.
func (m *MockFS) Sub(dir string) (fs.FS, error) {
	cleanDir, info, err := m.validateSubdir(dir)
	if err != nil {
		return nil, err
	}

	subFS := NewMockFS(nil)
	subFS.latency = m.latency
	subFS.allowWrites = m.allowWrites
	subFS.writeHandler = m.writeHandler

	m.mu.RLock()
	defer m.mu.RUnlock()

	m.copyFilesToSubFS(subFS, cleanDir, info)

	// Clone the injector with adjusted paths
	subFS.injector = m.injector.CloneForSub(cleanDir)

	return subFS, nil
}

// validateSubdir cleans and validates the target directory path for Sub.
func (m *MockFS) validateSubdir(dir string) (cleanDir string, info fs.FileInfo, err error) {
	cleanDir = filepath.Clean(dir)

	// Validate the directory itself using the parent filesystem's Stat wrapper
	info, err = m.Stat(cleanDir)
	if err != nil {
		return "", nil, &fs.PathError{Op: "sub", Path: dir, Err: err}
	}
	if !info.IsDir() {
		return "", nil, &fs.PathError{Op: "sub", Path: dir, Err: ErrNotDir}
	}

	// Although Stat would likely fail for these cases, we check them here as well
	if !fs.ValidPath(cleanDir) || cleanDir == "." || cleanDir == "/" {
		return "", nil, &fs.PathError{Op: "sub", Path: dir, Err: fs.ErrInvalid}
	}

	return cleanDir, info, nil
}

// copyFilesToSubFS populates the sub-filesystem's file map from the parent.
// It assumes the caller has acquired the necessary read lock on the parent MockFS.
func (m *MockFS) copyFilesToSubFS(subFS *MockFS, cleanDir string, dirInfo fs.FileInfo) {
	prefix := cleanDir
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	// Map the directory itself to "." in the sub-filesystem
	subFS.fsys["."] = &fstest.MapFile{
		Mode:    dirInfo.Mode() | fs.ModeDir,
		ModTime: dirInfo.ModTime(),
		Sys:     dirInfo.Sys(),
	}

	// Copy all descendant files from the parent's underlying fsys
	for path, file := range m.fsys {
		if strings.HasPrefix(path, prefix) {
			// Get path relative to subDir
			subPath := path[len(prefix):]

			// Copy the MapFile to the sub filesystem, ensuring data is a deep copy
			if file != nil {
				newFile := *file
				newFile.Data = append([]byte(nil), file.Data...)
				subFS.fsys[subPath] = &newFile
			}
		}
	}
}

// --- File/Directory Management ---

// AddFileString adds a text file to the mock filesystem. Overwrites if exists.
func (m *MockFS) AddFileString(path string, content string, mode fs.FileMode) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)
	if !fs.ValidPath(cleanPath) || strings.HasSuffix(cleanPath, "/") {
		return
	}
	m.fsys[cleanPath] = &fstest.MapFile{
		Data:    []byte(content),
		Mode:    mode &^ fs.ModeDir,
		ModTime: time.Now(),
	}
}

// AddFileBytes adds a binary file to the mock filesystem. Overwrites if exists.
func (m *MockFS) AddFileBytes(path string, content []byte, mode fs.FileMode) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)
	if !fs.ValidPath(cleanPath) || strings.HasSuffix(cleanPath, "/") {
		return
	}
	dataCopy := append([]byte(nil), content...)
	m.fsys[cleanPath] = &fstest.MapFile{
		Data:    dataCopy,
		Mode:    mode &^ fs.ModeDir,
		ModTime: time.Now(),
	}
}

// AddDirectory adds a directory to the mock filesystem. Overwrites if exists.
func (m *MockFS) AddDirectory(path string, mode fs.FileMode) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)
	if !fs.ValidPath(cleanPath) || cleanPath == "." {
		return
	}
	m.fsys[cleanPath] = &fstest.MapFile{
		Mode:    (mode & fs.ModePerm) | fs.ModeDir,
		ModTime: time.Now(),
		// Data should be nil for directory
	}
}

// RemovePath removes a file or directory from the mock filesystem.
// Note: This does not recursively remove directory contents.
func (m *MockFS) RemovePath(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)
	delete(m.fsys, cleanPath)
}

// --- Error Injection Configuration (Convenience Methods) ---

// AddStatError configures a path to return the specified error on Stat.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddStatError(path string, err error) {
	m.injector.AddExact(OpStat, path, err, ErrorModeAlways, 0)
}

// AddOpenError configures a path to return the specified error on Open calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddOpenError(path string, err error) {
	m.injector.AddExact(OpOpen, path, err, ErrorModeAlways, 0)
}

// AddReadError configures a path to return the specified error on Read calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddReadError(path string, err error) {
	m.injector.AddExact(OpRead, path, err, ErrorModeAlways, 0)
}

// AddWriteError configures a path to return the specified error on Write calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddWriteError(path string, err error) {
	m.injector.AddExact(OpWrite, path, err, ErrorModeAlways, 0)
}

// AddReadDirError configures a path to return the specified error on ReadDir calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddReadDirError(path string, err error) {
	m.injector.AddExact(OpReadDir, path, err, ErrorModeAlways, 0)
}

// AddCloseError configures a path to return the specified error on Close calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddCloseError(path string, err error) {
	m.injector.AddExact(OpClose, path, err, ErrorModeAlways, 0)
}

// AddOpenErrorOnce configures a path to return the specified error once on Open calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeOnce mode.
func (m *MockFS) AddOpenErrorOnce(path string, err error) {
	m.injector.AddExact(OpOpen, path, err, ErrorModeOnce, 0)
}

// AddReadErrorAfterN configures a read error after N successful reads.
// It is a convenience method for AddErrorExactMatch with ErrorModeAfterSuccesses mode.
func (m *MockFS) AddReadErrorAfterN(path string, err error, successes uint64) {
	m.injector.AddExact(OpRead, path, err, ErrorModeAfterSuccesses, successes)
}

// MarkNonExistent injects ErrNotExist for all operations on the given paths.
func (m *MockFS) MarkNonExistent(paths ...string) {
	for _, path := range paths {
		cleanPath := filepath.Clean(path)
		m.RemovePath(cleanPath) // Remove from map first
		m.injector.AddForPathAllOps(cleanPath, fs.ErrNotExist, ErrorModeAlways, 0)
	}
}

// ClearErrors removes all configured errors for all operations.
func (m *MockFS) ClearErrors() {
	m.injector.Clear()
}

// --- WritableFS Implementation ---

// Mkdir creates a directory in the filesystem.
func (m *MockFS) Mkdir(path string, perm fs.FileMode) error {
	cleanPath := filepath.Clean(path)

	m.counters.inc(OpMkdir)

	if err := m.checkAndInjectError(OpMkdir, cleanPath); err != nil {
		return err
	}

	m.simulateLatency()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if _, exists := m.fsys[cleanPath]; exists {
		return &fs.PathError{Op: "mkdir", Path: path, Err: fs.ErrExist}
	}

	// Check parent exists and is a directory
	parent := filepath.Dir(cleanPath)
	if parent != "." {
		parentFile, exists := m.fsys[parent]
		if !exists {
			return &fs.PathError{Op: "mkdir", Path: path, Err: fs.ErrNotExist}
		}
		if !parentFile.Mode.IsDir() {
			return &fs.PathError{Op: "mkdir", Path: path, Err: ErrNotDir}
		}
	}

	m.fsys[cleanPath] = &fstest.MapFile{
		Mode:    (perm & fs.ModePerm) | fs.ModeDir,
		ModTime: time.Now(),
	}
	return nil
}

// MkdirAll creates a directory path and all parents if needed.
func (m *MockFS) MkdirAll(path string, perm fs.FileMode) error {
	cleanPath := filepath.Clean(path)

	m.counters.inc(OpMkdirAll)

	if err := m.checkAndInjectError(OpMkdirAll, cleanPath); err != nil {
		return err
	}

	m.simulateLatency()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if already exists
	if existing, exists := m.fsys[cleanPath]; exists {
		if !existing.Mode.IsDir() {
			return &fs.PathError{Op: "mkdir", Path: path, Err: ErrNotDir}
		}
		return nil
	}

	// Create all parent directories
	parts := strings.Split(cleanPath, string(filepath.Separator))
	currentPath := ""
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = filepath.Join(currentPath, part)
		}

		if _, exists := m.fsys[currentPath]; !exists {
			m.fsys[currentPath] = &fstest.MapFile{
				Mode:    (perm & fs.ModePerm) | fs.ModeDir,
				ModTime: time.Now(),
			}
		}
	}

	return nil
}

// Remove removes a file or directory from the filesystem.
func (m *MockFS) Remove(path string) error {
	cleanPath := filepath.Clean(path)

	m.counters.inc(OpRemove)

	if err := m.checkAndInjectError(OpRemove, cleanPath); err != nil {
		return err
	}

	m.simulateLatency()

	m.mu.Lock()
	defer m.mu.Unlock()

	file, exists := m.fsys[cleanPath]
	if !exists {
		return &fs.PathError{Op: "remove", Path: path, Err: fs.ErrNotExist}
	}

	// If it's a directory, check it's empty
	if file.Mode.IsDir() {
		prefix := cleanPath + "/"
		for p := range m.fsys {
			if strings.HasPrefix(p, prefix) {
				return &fs.PathError{Op: "remove", Path: path, Err: ErrNotDir}
			}
		}
	}

	delete(m.fsys, cleanPath)
	return nil
}

// RemoveAll removes a path and any children recursively.
func (m *MockFS) RemoveAll(path string) error {
	cleanPath := filepath.Clean(path)

	m.counters.inc(OpRemoveAll)

	if err := m.checkAndInjectError(OpRemoveAll, cleanPath); err != nil {
		return err
	}

	m.simulateLatency()

	m.mu.Lock()
	defer m.mu.Unlock()

	// Remove the path itself and all children
	prefix := cleanPath + "/"
	for p := range m.fsys {
		if p == cleanPath || strings.HasPrefix(p, prefix) {
			delete(m.fsys, p)
		}
	}

	return nil
}

// Rename renames a file or directory in the filesystem.
func (m *MockFS) Rename(oldpath, newpath string) error {
	cleanOld := filepath.Clean(oldpath)
	cleanNew := filepath.Clean(newpath)

	m.counters.inc(OpRename)

	if err := m.checkAndInjectError(OpRename, cleanOld); err != nil {
		return err
	}

	m.simulateLatency()

	m.mu.Lock()
	defer m.mu.Unlock()

	oldFile, exists := m.fsys[cleanOld]
	if !exists {
		return &fs.PathError{Op: "rename", Path: oldpath, Err: fs.ErrNotExist}
	}

	// Copy to new location
	newFile := *oldFile
	if oldFile.Data != nil {
		newFile.Data = append([]byte(nil), oldFile.Data...)
	}
	m.fsys[cleanNew] = &newFile

	// If directory, rename all children
	if oldFile.Mode.IsDir() {
		oldPrefix := cleanOld + "/"
		newPrefix := cleanNew + "/"
		for p, f := range m.fsys {
			if strings.HasPrefix(p, oldPrefix) {
				newPath := newPrefix + p[len(oldPrefix):]
				childFile := *f
				if f.Data != nil {
					childFile.Data = append([]byte(nil), f.Data...)
				}
				m.fsys[newPath] = &childFile
				delete(m.fsys, p)
			}
		}
	}

	// Remove old location
	delete(m.fsys, cleanOld)
	return nil
}

// WriteFile writes data to a file in the filesystem.
func (m *MockFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	if !m.allowWrites {
		return &fs.PathError{Op: "writefile", Path: path, Err: fs.ErrInvalid}
	}

	cleanPath := filepath.Clean(path)

	m.counters.inc(OpWrite)

	if err := m.checkAndInjectError(OpWrite, cleanPath); err != nil {
		return err
	}

	m.simulateLatency()

	return m.writeHandler(cleanPath, data)
}

// --- Internal Helpers ---

// simulateLatency introduces an artificial delay if configured.
func (m *MockFS) simulateLatency() {
	if m.latency > 0 {
		time.Sleep(m.latency)
	}
}

// checkAndInjectError checks if an error should be injected for the operation and path.
// It returns the error if applicable.
func (m *MockFS) checkAndInjectError(op Operation, path string) error {
	return m.injector.CheckAndApply(op, path)
}

// defaultWriteHandler is the default write callback for MockFS.
// It creates new files or overwrites existing ones with the provided data.
func (m *MockFS) defaultWriteHandler(path string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cleanPath := filepath.Clean(path)

	// Find the existing file or create if it doesn't exist
	existing, ok := m.fsys[cleanPath]
	if !ok {
		// Create a new MapFile entry.
		m.fsys[cleanPath] = &fstest.MapFile{
			Data:    append([]byte(nil), data...), // Copy data
			Mode:    0644,                         // Default mode
			ModTime: time.Now(),
		}
		return nil
	}

	// Overwrite data for existing file
	existing.Data = append([]byte(nil), data...) // Make a copy
	existing.ModTime = time.Now()

	return nil
}
