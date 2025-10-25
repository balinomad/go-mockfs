package mockfs

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrorMode defines how an error is applied (always, once, etc.)
type ErrorMode int

const (
	ErrorModeAlways         ErrorMode = iota // ErrorModeAlways means the error is returned every time.
	ErrorModeOnce                            // ErrorModeOnce means the error is returned once, then cleared.
	ErrorModeAfterSuccesses                  // ErrorModeAfterSuccesses means the error is returned after N successful calls.
)

// Generic file system errors.
// Errors returned by file systems can be tested against these errors using [errors.Is].
var (
	ErrInvalid        = fs.ErrInvalid                     // ErrInvalid indicates an invalid argument.
	ErrPermission     = fs.ErrPermission                  // ErrPermission indicates a permission error.
	ErrExist          = fs.ErrExist                       // ErrExist indicates that a file already exists.
	ErrNotExist       = fs.ErrNotExist                    // ErrNotExist indicates that a file does not exist.
	ErrClosed         = fs.ErrClosed                      // ErrClosed indicates that a file is closed.
	ErrDiskFull       = errors.New("disk full")           // ErrDiskFull indicates that the disk is full.
	ErrTimeout        = errors.New("operation timeout")   // ErrTimeout indicates that the operation timed out.
	ErrCorrupted      = errors.New("corrupted data")      // ErrCorrupted indicates that the data is corrupted.
	ErrTooManyHandles = errors.New("too many open files") // ErrTooManyHandles indicates that too many handles are open.
	ErrNotDir         = errors.New("not a directory")     // ErrNotDir indicates that a path component is not a directory.
	ErrNotEmpty       = errors.New("directory not empty") // ErrNotEmpty indicates that a directory is not empty.
)

// ErrorRule captures the settings for an error to be injected.
type ErrorRule struct {
	Err      error         // Err is the error to return.
	Mode     ErrorMode     // Mode specifies how the error is applied.
	AfterN   uint64        // AfterN is used only for ErrorModeAfterSuccesses.
	matchers []PathMatcher // Matchers for paths.
	usedOnce atomic.Bool   // Used only for ErrorModeOnce.
	hits     atomic.Uint64 // Number of hits observed.
}

// NewErrorRule creates a new error rule.
//
// Parameters:
//   - err - the error to return.
//   - mode - one of ErrorModeAlways, ErrorModeOnce, or ErrorModeAfterSuccesses.
//   - after - used only for ErrorModeAfterSuccesses and specifies the number of successful calls
//     before the error is returned.
//   - matchers - an optional list of path matchers. If not provided, the rule applies to all paths.
func NewErrorRule(err error, mode ErrorMode, after int, matchers ...PathMatcher) *ErrorRule {
	return &ErrorRule{
		Err:      err,
		Mode:     mode,
		AfterN:   mustAfter(after),
		matchers: matchers,
	}
}

// matches returns true if the rule applies to the path.
// This method doesn't modify state, safe for concurrent checks.
func (r *ErrorRule) matches(path string) bool {
	// no matchers means match nothing (use WildcardMatcher to match all)
	if len(r.matchers) == 0 {
		return false
	}

	for _, m := range r.matchers {
		if m.Matches(path) {
			return true
		}
	}

	return false
}

// shouldReturnError returns true if the error should be returned.
// For ErrorModeAfterSuccesses, it increments the hit counter.
func (r *ErrorRule) shouldReturnError() bool {
	switch r.Mode {
	case ErrorModeAlways:
		return true
	case ErrorModeOnce:
		return r.usedOnce.CompareAndSwap(false, true)
	case ErrorModeAfterSuccesses:
		hits := r.hits.Add(1)
		return hits > r.AfterN
	default:
		return false
	}
}

// CloneForSub returns a clone of the rule adjusted for a sub-namespace (used by SubFS).
func (r *ErrorRule) CloneForSub(prefix string) *ErrorRule {
	newMatchers := make([]PathMatcher, 0, len(r.matchers))
	for _, m := range r.matchers {
		newMatchers = append(newMatchers, m.CloneForSub(prefix))
	}

	return NewErrorRule(r.Err, r.Mode, int(r.AfterN), newMatchers...)
}

// Operation defines the type of filesystem operation for error injection context.
type Operation int

const (
	// InvalidOperation is an invalid operation.
	InvalidOperation Operation = iota - 1

	OpUnknown // OpUnknown represents the unknown operation.

	OpStat      // OpStat represents the Stat operation.
	OpOpen      // OpOpen represents the Open operation.
	OpRead      // OpRead represents the Read operation.
	OpWrite     // OpWrite represents the Write operation.
	OpClose     // OpClose represents the Close operation.
	OpReadDir   // OpReadDir represents the ReadDir operation.
	OpMkdir     // OpMkdir represents the Mkdir operation.
	OpMkdirAll  // OpMkdirAll represents the MkdirAll operation.
	OpRemove    // OpRemove represents the Remove operation.
	OpRemoveAll // OpRemoveAll represents the RemoveAll operation.
	OpRename    // OpRename represents the Rename operation.

	// NumOperations is the number of available operations.
	NumOperations
)

// operationNames maps each operation to a human-readable string.
var operationNames = map[Operation]string{
	InvalidOperation: "Invalid",
	OpUnknown:        "Unknown",
	OpStat:           "Stat",
	OpOpen:           "Open",
	OpRead:           "Read",
	OpWrite:          "Write",
	OpClose:          "Close",
	OpReadDir:        "ReadDir",
	OpMkdir:          "Mkdir",
	OpMkdirAll:       "MkdirAll",
	OpRemove:         "Remove",
	OpRemoveAll:      "RemoveAll",
	OpRename:         "Rename",
}

// IsValid returns true if the operation is valid.
func (op Operation) IsValid() bool {
	return op > 0 && op < NumOperations
}

// String returns a human-readable string representation of the operation.
// This is used for logging and testing purposes.
func (op Operation) String() string {
	if !op.IsValid() && op != OpUnknown {
		return operationNames[InvalidOperation]
	}

	return operationNames[op]
}

// StringToOperation converts a string to an Operation.
// It returns an invalid operation if the string does not match a valid operation.
func StringToOperation(s string) Operation {
	for op := Operation(0); op < NumOperations; op++ {
		if strings.EqualFold(operationNames[op], s) {
			return op
		}
	}

	return InvalidOperation
}

// ErrorInjector defines the interface for error injection in filesystem operations.
type ErrorInjector interface {
	// Add adds a custom, pre-configured error rule to the injector.
	// All other Add* helpers call this method.
	Add(op Operation, rule *ErrorRule)

	// AddExact adds an error rule for a specific, exact path.
	AddExact(op Operation, path string, err error, mode ErrorMode, after int)

	// AddGlob adds an error rule for paths matching a glob pattern (e.g., "dir/*.txt").
	// This uses [path.Match] semantics.
	// Returns path.ErrBadPattern if the pattern is malformed.
	AddGlob(op Operation, pattern string, err error, mode ErrorMode, after int) error

	// AddRegexp adds an error rule for paths matching a regular expression.
	// This uses [regexp.Compile] semantics.
	// Returns an error if the regular expression fails to compile.
	AddRegexp(op Operation, pattern string, err error, mode ErrorMode, after int) error

	// AddAll adds an error rule that matches all paths for the given operation.
	// This is a helper for NewErrorRule(..., NewWildcardMatcher()).
	AddAll(op Operation, err error, mode ErrorMode, after int)

	// AddExactForAllOps adds an error rule for a specific, exact path that applies
	// to all filesystem operations (Stat, Open, Remove, Mkdir, etc.).
	// A new rule is created for each operation to ensure independent state.
	AddExactForAllOps(path string, err error, mode ErrorMode, after int)

	// AddGlobForAllOps adds an error rule for a glob pattern that applies
	// to all filesystem operations.
	// A new rule is created for each operation to ensure independent state.
	// Returns path.ErrBadPattern if the pattern is malformed.
	AddGlobForAllOps(pattern string, err error, mode ErrorMode, after int) error

	// AddRegexpForAllOps adds an error rule for a regular expression that applies
	// to all filesystem operations.
	// A new rule is created for each operation to ensure independent state.
	// Returns an error if the regular expression fails to compile.
	AddRegexpForAllOps(pattern string, err error, mode ErrorMode, after int) error

	// AddAllForAllOps adds an error rule that matches all paths AND all operations.
	// A new rule is created for each operation to ensure independent state.
	AddAllForAllOps(err error, mode ErrorMode, after int)

	// Clear clears all error rules from the injector.
	Clear()

	// CheckAndApply checks for and applies error rules for the given operation and path.
	// This is intended for internal use by MockFS and MockFile.
	CheckAndApply(op Operation, path string) error

	// CloneForSub returns a clone of the injector adjusted for a sub-namespace.
	CloneForSub(prefix string) ErrorInjector

	// GetAll returns a map of all configured error rules for introspection.
	GetAll() map[Operation][]*ErrorRule
}

// errorInjector implements ErrorInjector.
type errorInjector struct {
	mu      sync.RWMutex
	configs map[Operation][]*ErrorRule
}

// Ensure errorInjector implements ErrorInjector.
var _ ErrorInjector = (*errorInjector)(nil)

// NewErrorInjector returns a new ErrorInjector.
func NewErrorInjector() ErrorInjector {
	return &errorInjector{
		configs: make(map[Operation][]*ErrorRule),
	}
}

// Add adds a custom, pre-configured error rule to the injector.
func (ei *errorInjector) Add(op Operation, rule *ErrorRule) {
	ei.mu.Lock()
	defer ei.mu.Unlock()

	ei.configs[op] = append(ei.configs[op], rule)
}

// AddExact adds an error rule for a specific path.
func (ei *errorInjector) AddExact(op Operation, path string, err error, mode ErrorMode, after int) {
	ei.Add(op, NewErrorRule(err, mode, after, NewExactMatcher(path)))
}

// AddGlob adds an error rule for paths matching a glob pattern (e.g., "dir/*.txt").
func (ei *errorInjector) AddGlob(op Operation, pattern string, err error, mode ErrorMode, after int) error {
	m, errRule := NewGlobMatcher(pattern)
	if errRule != nil {
		return errRule
	}

	ei.Add(op, NewErrorRule(err, mode, after, m))

	return nil
}

// AddRegexp adds an error rule for paths matching a regular expression.
func (ei *errorInjector) AddRegexp(op Operation, pattern string, err error, mode ErrorMode, after int) error {
	m, errRule := NewRegexpMatcher(pattern)
	if errRule != nil {
		return errRule
	}

	ei.Add(op, NewErrorRule(err, mode, after, m))

	return nil
}

// AddAll adds an error rule that matches all paths for the given operation.
func (ei *errorInjector) AddAll(op Operation, err error, mode ErrorMode, after int) {
	ei.Add(op, NewErrorRule(err, mode, after, NewWildcardMatcher()))
}

// AddExactForAllOps adds an error rule for a specific, exact path that applies
// to all filesystem operations.
func (ei *errorInjector) AddExactForAllOps(path string, err error, mode ErrorMode, after int) {
	ei.mu.Lock()
	defer ei.mu.Unlock()

	for op := OpStat; op < NumOperations; op++ {
		// Create a new rule for each operation to track state (hits, once) independently
		ei.configs[op] = append(ei.configs[op], NewErrorRule(err, mode, after, NewExactMatcher(path)))
	}
}

// AddGlobForAllOps adds an error rule for a glob pattern that applies
// to all filesystem operations.
func (ei *errorInjector) AddGlobForAllOps(pattern string, err error, mode ErrorMode, after int) error {
	// Validate pattern once
	m, errRule := NewGlobMatcher(pattern)
	if errRule != nil {
		return errRule
	}

	ei.mu.Lock()
	defer ei.mu.Unlock()

	for op := OpStat; op < NumOperations; op++ {
		// Re-use the immutable matcher, but create a new rule for each op
		ei.configs[op] = append(ei.configs[op], NewErrorRule(err, mode, after, m))
	}

	return nil
}

// AddRegexpForAllOps adds an error rule for a regular expression that applies
// to all filesystem operations.
func (ei *errorInjector) AddRegexpForAllOps(pattern string, err error, mode ErrorMode, after int) error {
	// Validate pattern once
	m, errRule := NewRegexpMatcher(pattern)
	if errRule != nil {
		return errRule
	}

	ei.mu.Lock()
	defer ei.mu.Unlock()

	for op := OpStat; op < NumOperations; op++ {
		// Re-use the immutable matcher, but create a new rule for each op
		ei.configs[op] = append(ei.configs[op], NewErrorRule(err, mode, after, m))
	}

	return nil
}

// AddAllForAllOps adds an error rule that matches all paths AND all operations.
func (ei *errorInjector) AddAllForAllOps(err error, mode ErrorMode, after int) {
	matcher := NewWildcardMatcher()

	ei.mu.Lock()
	defer ei.mu.Unlock()

	for op := OpStat; op < NumOperations; op++ {
		// Re-use the immutable matcher, but create a new rule for each op
		ei.configs[op] = append(ei.configs[op], NewErrorRule(err, mode, after, matcher))
	}
}

// Clear clears all error rules.
func (ei *errorInjector) Clear() {
	ei.mu.Lock()
	defer ei.mu.Unlock()

	ei.configs = make(map[Operation][]*ErrorRule)
}

// GetAll returns all error rules.
func (ei *errorInjector) GetAll() map[Operation][]*ErrorRule {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	out := make(map[Operation][]*ErrorRule, len(ei.configs))
	for op, arr := range ei.configs {
		cop := make([]*ErrorRule, len(arr))
		copy(cop, arr)
		out[op] = cop
	}

	return out
}

// CloneForSub returns a clone of the injector adjusted for a sub-namespace (used by SubFS).
func (ei *errorInjector) CloneForSub(prefix string) ErrorInjector {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	clone := NewErrorInjector()
	for op, arr := range ei.configs {
		for _, r := range arr {
			clone.Add(op, r.CloneForSub(prefix))
		}
	}

	return clone
}

// CheckAndApply tries rules in insertion order; notionally we could add priorities.
// It owns locking and mutates rule state (shouldReturnError uses atomics for some modes).
func (ei *errorInjector) CheckAndApply(op Operation, path string) error {
	ei.mu.RLock()
	defer ei.mu.RUnlock()

	// First check op-specific rules
	if arr, ok := ei.configs[op]; ok {
		for _, r := range arr {
			if r.matches(path) {
				if r.shouldReturnError() {
					return r.Err
				}
			}
		}
	}
	// Then optionally check any global/wildcard rules (OpUnknown)
	if arr, ok := ei.configs[OpUnknown]; ok {
		for _, r := range arr {
			if r.matches(path) {
				if r.shouldReturnError() {
					return r.Err
				}
			}
		}
	}
	return nil
}

// mustAfter converts a public int 'after' to internal uint64 and panics on invalid input.
func mustAfter(after int) uint64 {
	if after < 0 {
		panic(fmt.Sprintf("mockfs: invalid after value %d — must be >= 0", after))
	}
	return uint64(after)
}
