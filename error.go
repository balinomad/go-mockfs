package mockfs

import (
	"errors"
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
	ErrInvalid        = fs.ErrInvalid    // "invalid argument"
	ErrPermission     = fs.ErrPermission // "permission denied"
	ErrExist          = fs.ErrExist      // "file already exists"
	ErrNotExist       = fs.ErrNotExist   // "file does not exist"
	ErrClosed         = fs.ErrClosed     // "file already closed"
	ErrDiskFull       = errors.New("disk full")
	ErrTimeout        = errors.New("operation timeout")
	ErrCorrupted      = errors.New("corrupted data")
	ErrTooManyHandles = errors.New("too many open files")
	ErrNotDir         = errors.New("not a directory")
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
func NewErrorRule(err error, mode ErrorMode, after uint64, matchers ...PathMatcher) *ErrorRule {
	return &ErrorRule{
		Err:      err,
		Mode:     mode,
		AfterN:   after,
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

	return NewErrorRule(r.Err, r.Mode, r.AfterN, newMatchers...)
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

// String returns a human-readable string representation of the operation.
// This is used for logging and testing purposes.
func (op Operation) String() string {
	if op < 0 || op >= NumOperations {
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
	// Add adds an error rule to the injector.
	Add(op Operation, rule *ErrorRule)

	// AddExact adds an error rule for a specific path.
	// This is a shortcut for Add(op, NewErrorRule(err, mode, after, ExactMatcher{path: path})).
	AddExact(op Operation, path string, err error, mode ErrorMode, after uint64)

	// AddPattern adds an error rule for a pattern match.
	// This is a shortcut for Add(op, NewErrorRule(err, mode, after, PatternMatcher{pattern: pattern})).
	// The pattern is a regular expression.
	AddPattern(op Operation, pattern string, err error, mode ErrorMode, after uint64) error

	// AddForPathAllOps adds an error rule for all operations on a specific path.
	AddForPathAllOps(path string, err error, mode ErrorMode, after uint64)

	// Clear clears all error rules.
	Clear()

	// CheckAndApply checks for and applies error rules for the given operation and path.
	CheckAndApply(op Operation, path string) error

	// CloneForSub returns a clone of the injector adjusted for a sub-namespace (used by SubFS).
	CloneForSub(prefix string) ErrorInjector

	// GetAll returns all error rules.
	GetAll() map[Operation][]*ErrorRule // for introspection & tests
}

// ErrorManager implements ErrorInjector.
type ErrorManager struct {
	mu      sync.RWMutex
	configs map[Operation][]*ErrorRule
}

// Ensure ErrorManager implements ErrorInjector.
var _ ErrorInjector = (*ErrorManager)(nil)

// NewErrorManager returns a new ErrorManager.
func NewErrorManager() *ErrorManager {
	return &ErrorManager{
		configs: make(map[Operation][]*ErrorRule),
	}
}

// Add adds an error rule to the injector.
func (em *ErrorManager) Add(op Operation, rule *ErrorRule) {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.configs[op] = append(em.configs[op], rule)
}

// AddExact adds an error rule for a specific path.
func (em *ErrorManager) AddExact(op Operation, path string, err error, mode ErrorMode, after uint64) {
	em.Add(op, NewErrorRule(err, mode, after, NewExactMatcher(path)))
}

// AddPattern adds an error rule for a pattern match.
// The pattern is a regular expression.
func (em *ErrorManager) AddPattern(op Operation, pattern string, err error, mode ErrorMode, after uint64) error {
	m, errRule := NewRegexpMatcher(pattern)
	if errRule != nil {
		return errRule
	}
	em.Add(op, NewErrorRule(err, mode, after, m))
	return nil
}

// AddForPathAllOps adds an error rule for all operations on a specific path.
func (em *ErrorManager) AddForPathAllOps(path string, err error, mode ErrorMode, after uint64) {
	for op := OpStat; op < NumOperations; op++ {
		em.AddExact(op, path, err, mode, after)
	}
}

// Clear clears all error rules.
func (em *ErrorManager) Clear() {
	em.mu.Lock()
	defer em.mu.Unlock()
	em.configs = make(map[Operation][]*ErrorRule)
}

// GetAll returns all error rules.
func (em *ErrorManager) GetAll() map[Operation][]*ErrorRule {
	em.mu.RLock()
	defer em.mu.RUnlock()
	out := make(map[Operation][]*ErrorRule, len(em.configs))
	for op, arr := range em.configs {
		cop := make([]*ErrorRule, len(arr))
		copy(cop, arr)
		out[op] = cop
	}
	return out
}

// CloneForSub returns a clone of the injector adjusted for a sub-namespace (used by SubFS).
func (em *ErrorManager) CloneForSub(prefix string) ErrorInjector {
	em.mu.RLock()
	defer em.mu.RUnlock()
	clone := NewErrorManager()
	for op, arr := range em.configs {
		for _, r := range arr {
			clone.Add(op, r.CloneForSub(prefix))
		}
	}
	return clone
}

// CheckAndApply tries rules in insertion order; notionally we could add priorities.
// It owns locking and mutates rule state (shouldReturnError uses atomics for some modes).
func (em *ErrorManager) CheckAndApply(op Operation, path string) error {
	em.mu.RLock()
	defer em.mu.RUnlock()

	// First check op-specific rules
	if arr, ok := em.configs[op]; ok {
		for _, r := range arr {
			if r.matches(path) {
				if r.shouldReturnError() {
					return r.Err
				}
			}
		}
	}
	// Then optionally check any global/wildcard rules (OpUnknown)
	if arr, ok := em.configs[OpUnknown]; ok {
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
