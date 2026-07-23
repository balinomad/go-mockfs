package mockfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrorMode defines how an error is applied (always, once, etc.)
type ErrorMode int

const (
	// ErrorModeAlways means the error is returned every time.
	ErrorModeAlways ErrorMode = iota
	// ErrorModeOnce means the error is returned once, then cleared.
	ErrorModeOnce
	// ErrorModeAfterSuccesses means the error is returned after N successful calls.
	ErrorModeAfterSuccesses
	// ErrorModeNext means the error is returned next N times, then cleared.
	ErrorModeNext
)

// IsValid returns true if mode is one of the defined ErrorMode constants.
func (m ErrorMode) IsValid() bool {
	return m >= ErrorModeAlways && m <= ErrorModeNext
}

// ErrUsage indicates that mockfs itself was misconfigured or misused by the
// caller — an invalid FsOption, a nil MapFile, a negative duration, a
// malformed FileInfo, or a garbage ErrorMode — as opposed to an error
// injected via ErrorInjector/FailX, which simulates a filesystem failure
// and is returned verbatim, unwrapped. Distinguish with:
//
//	errors.Is(err, mockfs.ErrUsage)
var ErrUsage = errors.New("usage error")

// Generic file system errors.
// Errors returned by file systems can be tested against these errors using [errors.Is].
var (
	// ErrInvalid indicates an invalid argument.
	ErrInvalid = fs.ErrInvalid

	// ErrPermission indicates a permission error.
	ErrPermission = fs.ErrPermission

	// ErrExist indicates that a file already exists.
	ErrExist = fs.ErrExist

	// ErrNotExist indicates that a file does not exist.
	ErrNotExist = fs.ErrNotExist

	// ErrClosed indicates that a file is closed.
	ErrClosed = fs.ErrClosed

	// ErrUnexpectedEOF indicates an unexpected end of file.
	ErrUnexpectedEOF = io.ErrUnexpectedEOF

	// ErrDiskFull indicates that the disk is full.
	ErrDiskFull = errors.New("disk full")

	// ErrTimeout indicates that the operation timed out.
	ErrTimeout = errors.New("operation timeout")

	// ErrCorrupted indicates that the data is corrupted.
	ErrCorrupted = errors.New("corrupted data")

	// ErrTooManyHandles indicates that too many handles are open.
	ErrTooManyHandles = errors.New("too many open files")

	// ErrNotDir indicates that a path component is not a directory.
	ErrNotDir = errors.New("not a directory")

	// ErrIsDir indicates that a path component is a directory.
	ErrIsDir = errors.New("it is a directory")

	// ErrNotEmpty indicates that a directory is not empty.
	ErrNotEmpty = errors.New("directory not empty")

	// ErrNegativeOffset indicates that the offset is negative.
	ErrNegativeOffset = errors.New("negative offset")
)

// ErrorRule captures the settings for an error to be injected.
type ErrorRule struct {
	Err      error         // Err is the error to return.
	Mode     ErrorMode     // Mode specifies how the error is applied.
	AfterN   uint64        // AfterN is used only for ErrorModeAfterSuccesses and ErrorModeNext.
	matchers []PathMatcher // Matchers for paths.
	usedOnce atomic.Bool   // Used only for ErrorModeOnce.
	hits     atomic.Uint64 // Number of hits observed.
}

// NewErrorRule creates a new error rule.
//
// Parameters:
//   - err - the error to return.
//   - mode - one of ErrorModeAlways, ErrorModeOnce, ErrorModeAfterSuccesses, or ErrorModeNext.
//     Returns an error wrapping ErrUsage if mode is not one of these.
//   - after - used only for ErrorModeAfterSuccesses and ErrorModeNext; specifies the number of
//     calls before the error behaviour activates. Returns an error wrapping ErrUsage if after is
//     negative for these modes. Ignored for ErrorModeAlways and ErrorModeOnce.
//   - matchers - an optional list of path matchers. If not provided, the rule applies to no paths.
func NewErrorRule(err error, mode ErrorMode, after int, matchers ...PathMatcher) (*ErrorRule, error) {
	if !mode.IsValid() {
		return nil, fmt.Errorf("mockfs: %w: invalid ErrorMode: %d", ErrUsage, mode)
	}

	afterN, validationErr := validateAfter(after, mode)
	if validationErr != nil {
		return nil, validationErr
	}

	return newValidatedErrorRule(err, mode, afterN, matchers...), nil
}

// newValidatedErrorRule constructs an ErrorRule directly from an already-validated afterN.
// Callers must ensure afterN was produced by validateAfter for the given mode.
func newValidatedErrorRule(err error, mode ErrorMode, afterN uint64, matchers ...PathMatcher) *ErrorRule {
	return &ErrorRule{
		Err:      err,
		Mode:     mode,
		AfterN:   afterN,
		matchers: matchers,
	}
}

// validateAfter checks that after is valid for the given mode and converts it to uint64.
// Returns an error if after is negative and the mode reads the after value.
// For ErrorModeAlways and ErrorModeOnce, after is ignored and normalised to 0.
func validateAfter(after int, mode ErrorMode) (uint64, error) {
	switch mode {
	case ErrorModeAfterSuccesses, ErrorModeNext:
		if after < 0 {
			return 0, fmt.Errorf("mockfs: %w: invalid after value %d for mode %v — must be >= 0", ErrUsage, after, mode)
		}
		return uint64(after), nil
	default:
		return 0, nil
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
// For ErrorModeAfterSuccesses and ErrorModeNext, it increments the hit counter.
func (r *ErrorRule) shouldReturnError() bool {
	switch r.Mode {
	case ErrorModeAlways:
		return true
	case ErrorModeOnce:
		return r.usedOnce.CompareAndSwap(false, true)
	case ErrorModeAfterSuccesses:
		hits := r.hits.Add(1)
		return hits > r.AfterN
	case ErrorModeNext:
		hits := r.hits.Add(1)
		return hits <= r.AfterN
	default:
		//nolint:forbidigo // Panic is intentional here to mark incorrect use
		panic(fmt.Sprintf("mockfs: invalid ErrorMode: %d", r.Mode))
	}
}

// CloneForSub returns a clone of the rule adjusted for a sub-namespace (used by SubFS).
func (r *ErrorRule) CloneForSub(prefix string) *ErrorRule {
	newMatchers := make([]PathMatcher, 0, len(r.matchers))
	for _, m := range r.matchers {
		newMatchers = append(newMatchers, m.CloneForSub(prefix))
	}

	// AfterN was validated when the original rule was created; no re-validation needed.
	return newValidatedErrorRule(r.Err, r.Mode, r.AfterN, newMatchers...)
}

// Operation defines the type of filesystem operation for error injection context.
type Operation int

const (
	// InvalidOperation is an invalid operation.
	InvalidOperation Operation = iota - 1
	// OpUnknown represents the unknown operation.
	OpUnknown
	// OpStat represents the Stat operation.
	OpStat
	// OpOpen represents the Open operation.
	OpOpen
	// OpRead represents the Read operation.
	OpRead
	// OpWrite represents the Write operation.
	OpWrite
	// OpSeek represents the Seek operation.
	OpSeek
	// OpClose represents the Close operation.
	OpClose
	// OpReadDir represents the ReadDir operation.
	OpReadDir
	// OpMkdir represents the Mkdir operation.
	OpMkdir
	// OpMkdirAll represents the MkdirAll operation.
	OpMkdirAll
	// OpRemove represents the Remove operation.
	OpRemove
	// OpRemoveAll represents the RemoveAll operation.
	OpRemoveAll
	// OpRename represents the Rename operation.
	OpRename

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
	OpSeek:           "Seek",
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
	return op > OpUnknown && op < NumOperations
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
	for op := range NumOperations {
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
	// Returns an error if after is negative and mode is ErrorModeAfterSuccesses or ErrorModeNext.
	AddExact(op Operation, path string, err error, mode ErrorMode, after int) error

	// AddGlob adds an error rule for paths matching a glob pattern (e.g., "dir/*.txt").
	// This uses [path.Match] semantics.
	// Returns an error if the pattern is malformed or if after is negative for
	// ErrorModeAfterSuccesses or ErrorModeNext.
	AddGlob(op Operation, pattern string, err error, mode ErrorMode, after int) error

	// AddRegexp adds an error rule for paths matching a regular expression.
	// This uses [regexp.Compile] semantics.
	// Returns an error if the regular expression fails to compile or if after is negative for
	// ErrorModeAfterSuccesses or ErrorModeNext.
	AddRegexp(op Operation, pattern string, err error, mode ErrorMode, after int) error

	// AddAll adds an error rule that matches all paths for the given operation.
	// Returns an error if after is negative and mode is ErrorModeAfterSuccesses or ErrorModeNext.
	AddAll(op Operation, err error, mode ErrorMode, after int) error

	// AddExactForAllOps adds an error rule for a specific, exact path that applies
	// to all filesystem operations (Stat, Open, Remove, Mkdir, etc.).
	// A new rule is created for each operation to ensure independent state.
	// Returns an error if after is negative and mode is ErrorModeAfterSuccesses or ErrorModeNext.
	AddExactForAllOps(path string, err error, mode ErrorMode, after int) error

	// AddGlobForAllOps adds an error rule for a glob pattern that applies
	// to all filesystem operations.
	// A new rule is created for each operation to ensure independent state.
	// Returns an error if the pattern is malformed or if after is negative for
	// ErrorModeAfterSuccesses or ErrorModeNext.
	AddGlobForAllOps(pattern string, err error, mode ErrorMode, after int) error

	// AddRegexpForAllOps adds an error rule for a regular expression that applies
	// to all filesystem operations.
	// A new rule is created for each operation to ensure independent state.
	// Returns an error if the regular expression fails to compile or if after is negative for
	// ErrorModeAfterSuccesses or ErrorModeNext.
	AddRegexpForAllOps(pattern string, err error, mode ErrorMode, after int) error

	// AddAllForAllOps adds an error rule that matches all paths AND all operations.
	// A new rule is created for each operation to ensure independent state.
	// Returns an error if after is negative and mode is ErrorModeAfterSuccesses or ErrorModeNext.
	AddAllForAllOps(err error, mode ErrorMode, after int) error

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
func (ei *errorInjector) AddExact(op Operation, path string, err error, mode ErrorMode, after int) error {
	rule, validationErr := NewErrorRule(err, mode, after, NewExactMatcher(path))
	if validationErr != nil {
		return validationErr
	}

	ei.Add(op, rule)

	return nil
}

// AddGlob adds an error rule for paths matching a glob pattern (e.g., "dir/*.txt").
func (ei *errorInjector) AddGlob(op Operation, pattern string, err error, mode ErrorMode, after int) error {
	m, errGlob := NewGlobMatcher(pattern)
	if errGlob != nil {
		return errGlob
	}

	rule, errRule := NewErrorRule(err, mode, after, m)
	if errRule != nil {
		return errRule
	}

	ei.Add(op, rule)

	return nil
}

// AddRegexp adds an error rule for paths matching a regular expression.
func (ei *errorInjector) AddRegexp(op Operation, pattern string, err error, mode ErrorMode, after int) error {
	m, errRegexp := NewRegexpMatcher(pattern)
	if errRegexp != nil {
		return errRegexp
	}

	rule, errRule := NewErrorRule(err, mode, after, m)
	if errRule != nil {
		return errRule
	}

	ei.Add(op, rule)

	return nil
}

// AddAll adds an error rule that matches all paths for the given operation.
func (ei *errorInjector) AddAll(op Operation, err error, mode ErrorMode, after int) error {
	rule, validationErr := NewErrorRule(err, mode, after, NewWildcardMatcher())
	if validationErr != nil {
		return validationErr
	}

	ei.Add(op, rule)

	return nil
}

// AddExactForAllOps adds an error rule for a specific, exact path that applies
// to all filesystem operations.
func (ei *errorInjector) AddExactForAllOps(path string, err error, mode ErrorMode, after int) error {
	afterN, validationErr := validateAfter(after, mode)
	if validationErr != nil {
		return validationErr
	}

	ei.mu.Lock()
	defer ei.mu.Unlock()

	for op := OpStat; op < NumOperations; op++ {
		// Create a new rule for each operation to track state (hits, once) independently.
		ei.configs[op] = append(ei.configs[op], newValidatedErrorRule(err, mode, afterN, NewExactMatcher(path)))
	}

	return nil
}

// AddGlobForAllOps adds an error rule for a glob pattern that applies
// to all filesystem operations.
func (ei *errorInjector) AddGlobForAllOps(pattern string, err error, mode ErrorMode, after int) error {
	m, errGlob := NewGlobMatcher(pattern)
	if errGlob != nil {
		return errGlob
	}

	afterN, validationErr := validateAfter(after, mode)
	if validationErr != nil {
		return validationErr
	}

	ei.mu.Lock()
	defer ei.mu.Unlock()

	for op := OpStat; op < NumOperations; op++ {
		// Re-use the immutable matcher, but create a new rule for each op.
		ei.configs[op] = append(ei.configs[op], newValidatedErrorRule(err, mode, afterN, m))
	}

	return nil
}

// AddRegexpForAllOps adds an error rule for a regular expression that applies
// to all filesystem operations.
func (ei *errorInjector) AddRegexpForAllOps(pattern string, err error, mode ErrorMode, after int) error {
	m, errRegexp := NewRegexpMatcher(pattern)
	if errRegexp != nil {
		return errRegexp
	}

	afterN, validationErr := validateAfter(after, mode)
	if validationErr != nil {
		return validationErr
	}

	ei.mu.Lock()
	defer ei.mu.Unlock()

	for op := OpStat; op < NumOperations; op++ {
		// Re-use the immutable matcher, but create a new rule for each op.
		ei.configs[op] = append(ei.configs[op], newValidatedErrorRule(err, mode, afterN, m))
	}

	return nil
}

// AddAllForAllOps adds an error rule that matches all paths AND all operations.
func (ei *errorInjector) AddAllForAllOps(err error, mode ErrorMode, after int) error {
	afterN, validationErr := validateAfter(after, mode)
	if validationErr != nil {
		return validationErr
	}

	matcher := NewWildcardMatcher()

	ei.mu.Lock()
	defer ei.mu.Unlock()

	for op := OpStat; op < NumOperations; op++ {
		// Re-use the immutable matcher, but create a new rule for each op.
		ei.configs[op] = append(ei.configs[op], newValidatedErrorRule(err, mode, afterN, matcher))
	}

	return nil
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
