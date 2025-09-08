package mockfs

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing/fstest"
	"time"
)

// Operation defines the type of filesystem operation for error injection context.
type Operation int

const (
	OpStat Operation = iota
	OpOpen
	OpRead
	OpWrite
	OpReadDir
	OpClose
	opSentinel // Marks the end of valid operations
)

// MockFS wraps fstest.MapFS to inject errors for specific paths and operations.
type MockFS struct {
	fsys          fstest.MapFS
	mu            sync.RWMutex
	errorConfigs  map[Operation][]*ErrorConfig         // Map operations to their error configs
	stats         Stats                                // Internal stats struct
	latency       time.Duration                        // Simulated latency
	allowWrites   bool                                 // Flag to enable simulated writes
	writeCallback func(path string, data []byte) error // Optional callback for writes
}

// Ensure interface implementations
var (
	_ fs.FS         = (*MockFS)(nil)
	_ fs.ReadDirFS  = (*MockFS)(nil)
	_ fs.ReadFileFS = (*MockFS)(nil)
	_ fs.StatFS     = (*MockFS)(nil)
	_ fs.SubFS      = (*MockFS)(nil)
)

// Option is a function type for configuring MockFS.
type Option func(*MockFS)

// WithLatency sets the simulated latency for operations.
func WithLatency(d time.Duration) Option {
	return func(m *MockFS) {
		m.latency = d
	}
}

// WithWriteCallback sets a callback function to handle writes.
// This allows simulating writes by updating external state or the internal map.
func WithWriteCallback(callback func(path string, data []byte) error) Option {
	return func(m *MockFS) {
		m.allowWrites = true // Enable writes if a callback is provided
		m.writeCallback = callback
	}
}

// WithWritesEnabled allows write operations, simulating them by modifying the internal MapFS.
// Note: This modifies the underlying map, use with caution if the original map data is important.
func WithWritesEnabled() Option {
	return func(m *MockFS) {
		m.allowWrites = true
		// Default write callback updates the internal fstest.MapFS
		m.writeCallback = func(path string, data []byte) error {
			m.mu.Lock() // Lock needed to modify the map
			defer m.mu.Unlock()

			cleanPath := filepath.Clean(path) // Clean the path

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
	}
}

// NewMockFS creates a new MockFS with the given MapFS data and options.
func NewMockFS(data map[string]*MapFile, opts ...Option) *MockFS {
	mapFS := make(fstest.MapFS)

	// Create a copy of the input map to avoid external modifications
	for path, file := range data {
		cleanPath := filepath.Clean(path) // Clean path keys
		// Create a copy of the MapFile as well
		if file != nil {
			newFile := *file
			newFile.Data = append([]byte(nil), file.Data...)
			mapFS[cleanPath] = &newFile
		} else {
			mapFS[cleanPath] = nil
		}
	}

	m := &MockFS{
		fsys:         mapFS,
		errorConfigs: make(map[Operation][]*ErrorConfig),
	}

	for _, opt := range opts {
		opt(m)
	}

	return m
}

// GetStats returns the current operation counts.
func (m *MockFS) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return a copy to avoid race conditions if the caller modifies it
	statsCopy := m.stats
	return statsCopy
}

// ResetStats resets all operation counters to zero.
func (m *MockFS) ResetStats() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stats = Stats{} // Reset to zero values
}

// Stat intercepts the Stat call and returns configured errors for paths.
// It implements the fs.StatFS interface.
func (m *MockFS) Stat(name string) (fs.FileInfo, error) {
	cleanName := filepath.Clean(name) // Clean name
	err := m.checkForError(OpStat, cleanName)
	if err != nil {
		return nil, err
	}
	m.simulateLatency()
	return m.fsys.Stat(cleanName)
}

// Open intercepts the Open call and returns configured errors for paths.
// It implements the fs.FS interface.
func (m *MockFS) Open(name string) (fs.File, error) {
	cleanName := filepath.Clean(name) // Clean name early
	err := m.checkForError(OpOpen, cleanName)
	if err != nil {
		return nil, err
	}
	m.simulateLatency()

	file, openErr := m.fsys.Open(cleanName)
	if openErr != nil {
		return nil, openErr
	}

	mockFile := &MockFile{
		file:   file,
		name:   cleanName, // Store the cleaned name used to open
		mockFS: m,         // Reference THIS MockFS instance
		closed: false,
	}

	// Check if the underlying file supports writing *if* writes are enabled
	if m.allowWrites {
		// Check for io.Writer support on the concrete file type
		if writer, ok := file.(io.Writer); ok {
			mockFile.writeFile = writer
		}
		// MockFile.Write will handle the case where writeFile is nil
		// but m.writeCallback is not, allowing writes via callback.
	}

	return mockFile, nil
}

// ReadFile implements the fs.ReadFileFS interface.
// This implementation tries to use the mock's own methods for consistency.
func (m *MockFS) ReadFile(name string) ([]byte, error) {
	cleanName := filepath.Clean(name) // Clean name
	// Note: This sequence inherently calls Open, Read (potentially multiple), and Close.
	// We rely on those methods to check errors and update stats correctly.
	file, err := m.Open(cleanName) // Uses mock Open, handles OpOpen errors/stats
	if err != nil {
		return nil, err
	}
	// Ensure file is closed even if ReadAll fails.
	defer func() { _ = file.Close() }() // Uses mock Close, handles OpClose errors/stats

	// io.ReadAll will call file.Read repeatedly.
	// file.Read (our MockFile.Read) handles OpRead errors/stats.
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// ReadDir implements the fs.ReadDirFS interface.
func (m *MockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	cleanName := filepath.Clean(name) // Clean name
	err := m.checkForError(OpReadDir, cleanName)
	if err != nil {
		return nil, err
	}
	m.simulateLatency()
	return m.fsys.ReadDir(cleanName)
}

// Sub implements fs.SubFS to return a sub-filesystem.
func (m *MockFS) Sub(dir string) (fs.FS, error) {
	// Validate the directory path and get its info.
	cleanDir, info, err := m.validateSubdir(dir)
	if err != nil {
		return nil, err
	}

	// Create the new MockFS for the subdirectory
	subFS := NewMockFS(nil)
	subFS.latency = m.latency
	subFS.allowWrites = m.allowWrites
	subFS.writeCallback = m.writeCallback

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Populate the sub-filesystem with the relevant files and directories.
	m.copyFilesToSubFS(subFS, cleanDir, info)

	// Copy and adjust relevant error injection configurations.
	m.copyErrorConfigsToSubFS(subFS, cleanDir)

	return subFS, nil
}

// validateSubdir cleans and validates the target directory path for Sub.
// It ensures the path is a valid, existing directory.
func (m *MockFS) validateSubdir(dir string) (cleanDir string, info fs.FileInfo, err error) {
	cleanDir = filepath.Clean(dir)

	// Validate the directory itself using the *parent* filesystem's Stat wrapper.
	// This ensures injected Stat errors on the dir are checked first.
	info, err = m.Stat(cleanDir) // Use wrapped Stat
	if err != nil {
		// If Stat fails (either injected or underlying), Sub fails.
		return "", nil, &fs.PathError{Op: "sub", Path: dir, Err: err}
	}
	if !info.IsDir() {
		return "", nil, &fs.PathError{Op: "sub", Path: dir, Err: ErrNotDir}
	}

	// Although Stat would likely fail for these cases, this check adds robustness.
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

	// Map the directory itself to "." in the sub-filesystem.
	subFS.fsys["."] = &fstest.MapFile{
		Mode:    dirInfo.Mode() | fs.ModeDir, // Ensure ModeDir is set
		ModTime: dirInfo.ModTime(),
		Sys:     dirInfo.Sys(),
	}

	// Copy all descendant files from the parent's underlying fsys.
	for path, file := range m.fsys {
		// Path is already clean due to how keys are stored in NewMockFS/Add*.
		if strings.HasPrefix(path, prefix) {
			subPath := path[len(prefix):] // Get path relative to subDir

			// Copy the MapFile to the sub filesystem, ensuring data is a deep copy.
			if file != nil {
				newFile := *file
				newFile.Data = append([]byte(nil), file.Data...) // Deep copy of data slice
				subFS.fsys[subPath] = &newFile
			}
		}
	}
}

// copyErrorConfigsToSubFS copies and adjusts error injection rules for the sub-filesystem.
// It assumes the caller has acquired the necessary read lock on the parent MockFS.
func (m *MockFS) copyErrorConfigsToSubFS(subFS *MockFS, cleanDir string) {
	for op, configs := range m.errorConfigs {
		subConfigs := make([]*ErrorConfig, 0, len(configs))
		for _, cfg := range configs {
			if cfg == nil {
				continue
			}

			subCfg := cfg.Clone()
			// Adjust exact path matches to be relative to the new sub-filesystem root.
			subCfg.Matches = adjustPathsForSub(subCfg.Matches, cleanDir)
			// Patterns are assumed relative or simple, copied as-is via Clone.

			// Only add the config to subFS if it's still potentially relevant
			// (i.e., it has remaining matches or patterns after adjustment).
			if len(subCfg.Matches) > 0 || len(subCfg.Patterns) > 0 {
				subConfigs = append(subConfigs, subCfg)
			}
		}
		if len(subConfigs) > 0 {
			subFS.errorConfigs[op] = subConfigs
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
		return // Ignore invalid paths
	}
	m.fsys[cleanPath] = &fstest.MapFile{
		Data:    []byte(content),
		Mode:    mode &^ fs.ModeDir, // Ensure not a directory mode
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
	// Copy content to avoid external modification
	dataCopy := append([]byte(nil), content...)
	m.fsys[cleanPath] = &fstest.MapFile{
		Data:    dataCopy,
		Mode:    mode &^ fs.ModeDir, // Ensure not a directory mode
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
		Mode:    (mode & fs.ModePerm) | fs.ModeDir, // Ensure dir flag, mask non-perm bits
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

// --- Error Injection Configuration ---

// AddError adds an error configuration pointer for a specific operation.
func (m *MockFS) AddError(op Operation, config *ErrorConfig) {
	if config == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.errorConfigs[op] = append(m.errorConfigs[op], config)
}

// AddErrorExactMatch adds an error for exact path matches on a specific operation.
func (m *MockFS) AddErrorExactMatch(op Operation, path string, err error, mode ErrorMode, successes int) {
	cleanPath := filepath.Clean(path)
	cfg := NewErrorConfig(err, mode, successes, []string{cleanPath}, nil)
	m.AddError(op, cfg)
}

// AddErrorPattern adds an error for pattern matches on a specific operation.
func (m *MockFS) AddErrorPattern(op Operation, pattern string, err error, mode ErrorMode, successes int) error {
	regex, regexErr := regexp.Compile(pattern)
	if regexErr != nil {
		return fmt.Errorf("invalid pattern %q: %w", pattern, regexErr)
	}
	cfg := NewErrorConfig(err, mode, successes, nil, []*regexp.Regexp{regex})
	m.AddError(op, cfg)
	return nil
}

// AddPathError adds the same error config for *all* operations on an exact path.
func (m *MockFS) AddPathError(path string, err error, mode ErrorMode, successes int) {
	cleanPath := filepath.Clean(path)
	baseCfg := NewErrorConfig(err, mode, successes, []string{cleanPath}, nil)
	for op := range AllOperations() {
		// Need to clone the config for each operation if stateful modes are used,
		// otherwise ErrorModeOnce/AfterSuccesses state is shared across ops.
		cfgForOp := baseCfg.Clone()
		m.AddError(op, cfgForOp)
	}
}

// AddPathErrorPattern adds the same error config for *all* operations matching a pattern.
func (m *MockFS) AddPathErrorPattern(pattern string, err error, mode ErrorMode, successes int) error {
	regex, regexErr := regexp.Compile(pattern)
	if regexErr != nil {
		return fmt.Errorf("invalid pattern %q: %w", pattern, regexErr)
	}
	baseCfg := NewErrorConfig(err, mode, successes, nil, []*regexp.Regexp{regex})
	for op := range AllOperations() {
		// Clone for each operation type to ensure independent state
		cfgForOp := baseCfg.Clone()
		m.AddError(op, cfgForOp)
	}

	return nil
}

// AddStatError configures a path to return the specified error on Stat.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddStatError(path string, err error) {
	m.AddErrorExactMatch(OpStat, path, err, ErrorModeAlways, 0)
}

// AddOpenError configures a path to return the specified error on Open calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddOpenError(path string, err error) {
	m.AddErrorExactMatch(OpOpen, path, err, ErrorModeAlways, 0)
}

// AddReadError configures a path to return the specified error on Read calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddReadError(path string, err error) {
	m.AddErrorExactMatch(OpRead, path, err, ErrorModeAlways, 0)
}

// AddWriteError configures a path to return the specified error on Write calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddWriteError(path string, err error) {
	m.AddErrorExactMatch(OpWrite, path, err, ErrorModeAlways, 0)
}

// AddReadDirError configures a path to return the specified error on ReadDir calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddReadDirError(path string, err error) {
	m.AddErrorExactMatch(OpReadDir, path, err, ErrorModeAlways, 0)
}

// AddCloseError configures a path to return the specified error on Close calls.
// It is a convenience method for AddErrorExactMatch with ErrorModeAlways mode.
func (m *MockFS) AddCloseError(path string, err error) {
	m.AddErrorExactMatch(OpClose, path, err, ErrorModeAlways, 0)
}

// AddOpenErrorOnce configures a path to return the specified error on Open once.
// It is a convenience method for AddErrorExactMatch with ErrorModeOnce mode.
func (m *MockFS) AddOpenErrorOnce(path string, err error) {
	m.AddErrorExactMatch(OpOpen, path, err, ErrorModeOnce, 0)
}

// AddReadErrorAfterN configures a read error after N successful reads.
// It is a convenience method for AddErrorExactMatch with ErrorModeAfterSuccesses mode.
func (m *MockFS) AddReadErrorAfterN(path string, err error, successes int) {
	m.AddErrorExactMatch(OpRead, path, err, ErrorModeAfterSuccesses, successes)
}

// MarkNonExistent injects ErrNotExist for all operations on the given paths.
func (m *MockFS) MarkNonExistent(paths ...string) {
	for _, path := range paths {
		cleanPath := filepath.Clean(path)
		m.RemovePath(cleanPath) // Remove from map first
		m.AddPathError(cleanPath, fs.ErrNotExist, ErrorModeAlways, 0)
	}
}

// MarkDirectoryNonExistent removes a directory and all its contents from the map
// and injects ErrNotExist errors for the directory path and a pattern matching its contents.
func (m *MockFS) MarkDirectoryNonExistent(dirPath string) {
	cleanDirPath := filepath.Clean(dirPath)
	if cleanDirPath == "." || cleanDirPath == "/" {
		return // Avoid removing root
	}

	// Remove entries from the map
	prefix := cleanDirPath + "/"
	m.mu.Lock()
	for path := range m.fsys {
		// Check if path is the dir itself or is inside the dir
		if path == cleanDirPath || strings.HasPrefix(path, prefix) {
			delete(m.fsys, path)
		}
	}
	m.mu.Unlock()

	// Add errors (these methods handle their own locking)
	m.AddPathError(cleanDirPath, fs.ErrNotExist, ErrorModeAlways, 0)
	pattern := regexp.QuoteMeta(cleanDirPath) + "/.*"
	// Ignore compile error, unlikely for quoted path
	_ = m.AddPathErrorPattern(pattern, fs.ErrNotExist, ErrorModeAlways, 0)
}

// ClearErrors removes all configured errors for all operations.
func (m *MockFS) ClearErrors() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.errorConfigs = make(map[Operation][]*ErrorConfig)
}

// --- Internal Helpers ---

// simulateLatency introduces an artificial delay if configured.
// Assumes lock is already held or not needed.
func (m *MockFS) simulateLatency() {
	if m.latency > 0 {
		// No lock needed for time.Sleep
		time.Sleep(m.latency)
	}
}

// checkForError checks if an error should be injected for the operation and path.
// It handles locking, updates statistics, and returns the error if applicable.
// It does NOT simulate latency; latency should be simulated *after* this check passes.
func (m *MockFS) checkForError(op Operation, path string) error {
	cleanPath := filepath.Clean(path)
	if !fs.ValidPath(cleanPath) {
		// Should we return ErrInvalid here? Or let the underlying fsys handle it?
		// Let's assume underlying handles invalid paths, focus on injection.
		// But we need path for checking configs.
		// If path is invalid, it likely won't match any config anyway.
	}

	// Use write lock because ErrorConfig.use() modifies atomic state
	m.mu.Lock()
	defer m.mu.Unlock()

	// Increment stats for the operation attempt
	m.incrementStat(op)

	configs := m.errorConfigs[op]
	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		// Check if path matches this config
		if cfg.shouldApply(cleanPath) {
			// Check if the error mode dictates returning an error *now*
			if cfg.use() {
				return cfg.Error // Return the injected error
			}
			// If use() returned false (e.g., ErrorModeOnce used), continue checking others
		}
	}

	return nil // No injected error found
}

// incrementStat safely increments the counter for the given operation.
// Assumes write lock is held.
func (m *MockFS) incrementStat(op Operation) {
	switch op {
	case OpStat:
		m.stats.StatCalls++
	case OpOpen:
		m.stats.OpenCalls++
	case OpRead:
		m.stats.ReadCalls++
	case OpWrite:
		m.stats.WriteCalls++
	case OpReadDir:
		m.stats.ReadDirCalls++
	case OpClose:
		m.stats.CloseCalls++
	}
}

// adjustPathsForSub adjusts exact match paths relative to a new root.
func adjustPathsForSub(paths []string, subDir string) []string {
	if len(paths) == 0 {
		return nil
	}

	var result []string
	prefix := subDir
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	for _, p := range paths {
		// p should also be clean if added via helper methods
		cleanP := filepath.Clean(p) // Ensure clean just in case
		if cleanP == subDir {
			result = append(result, ".") // Root of the original dir becomes "."
		} else if strings.HasPrefix(cleanP, prefix) {
			// Strip prefix and clean result to handle potential "." or ".." issues if any
			result = append(result, filepath.Clean(cleanP[len(prefix):]))
		}
		// Paths outside the subDir are implicitly excluded as they won't match HasPrefix
	}
	// Return nil if result slice is empty, consistent with input behavior
	if len(result) == 0 {
		return nil
	}
	return result
}

// AllOperations returns a map representing all known Operation types. Used for iteration.
func AllOperations() map[Operation]struct{} {
	m := make(map[Operation]struct{}, opSentinel)
	for i := Operation(0); i < opSentinel; i++ {
		m[i] = struct{}{}
	}
	return m
}
