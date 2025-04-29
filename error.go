package mockfs

import (
	"errors"
	"io/fs"
	"regexp"
	"sync/atomic"
)

// ErrorMode defines how an error is applied (always, once, etc.)
type ErrorMode int

const (
	// ErrorModeAlways means the error is returned every time
	ErrorModeAlways ErrorMode = iota
	// ErrorModeOnce means the error is returned once, then cleared
	ErrorModeOnce
	// ErrorModeAfterSuccesses means the error is returned after N successful calls
	ErrorModeAfterSuccesses
)

// Common errors that can be used with the mock
var (
	// Standard fs errors
	ErrInvalid    = fs.ErrInvalid    // "invalid argument"
	ErrPermission = fs.ErrPermission // "permission denied"
	ErrExist      = fs.ErrExist      // "file already exists"
	ErrNotExist   = fs.ErrNotExist   // "file does not exist"
	ErrClosed     = fs.ErrClosed     // "file already closed"

	// Additional custom errors
	ErrDiskFull       = errors.New("disk full")
	ErrTimeout        = errors.New("operation timeout")
	ErrCorrupted      = errors.New("corrupted data")
	ErrTooManyHandles = errors.New("too many open files")
	ErrNotDir         = errors.New("not a directory")
)

// ErrorConfig captures the settings for an error to be injected.
type ErrorConfig struct {
	Error    error            // The error to return
	Mode     ErrorMode        // How the error is applied
	Counter  atomic.Int64     // Counter for behaviors like ErrorModeOnce and ErrorModeAfterSuccesses (using atomic for safety)
	Matches  []string         // Exact path matches
	Patterns []*regexp.Regexp // Pattern matches
	used     atomic.Bool      // Flag for ErrorModeOnce
}

// NewErrorConfig creates a new error configuration pointer.
// For ErrorModeAfterSuccesses, 'counter' is the number of successes *before* the error.
// For ErrorModeOnce, 'counter' is ignored.
func NewErrorConfig(err error, mode ErrorMode, counter int, matches []string, patterns []*regexp.Regexp) *ErrorConfig {
	ec := &ErrorConfig{
		Error:    err,
		Mode:     mode,
		Matches:  matches,
		Patterns: patterns,
	}
	if mode == ErrorModeAfterSuccesses {
		ec.Counter.Store(int64(counter)) // Store initial success count
	}
	return ec
}

// shouldApply checks if the path matches the configuration.
// This part doesn't modify state, safe for concurrent checks.
func (c *ErrorConfig) shouldApply(path string) bool {
	// Check exact matches
	for _, m := range c.Matches {
		if m == path {
			return true
		}
	}

	// Check patterns
	for _, p := range c.Patterns {
		if p.MatchString(path) {
			return true
		}
	}

	return false
}

// Clone creates a deep copy of the ErrorConfig.
// It ensures atomic values are copied correctly and slices are handled appropriately.
func (c *ErrorConfig) Clone() *ErrorConfig {
	newC := &ErrorConfig{
		Error: c.Error, // Errors are typically immutable or pointers
		Mode:  c.Mode,
		// Shallow copy slices as Matches/Patterns are read-only after config creation
		Matches:  append([]string(nil), c.Matches...),
		Patterns: append([]*regexp.Regexp(nil), c.Patterns...),
	}

	// Copy atomic values safely
	newC.Counter.Store(c.Counter.Load())
	newC.used.Store(c.used.Load())

	return newC
}

// use checks if the error should be returned *now* and updates state if necessary.
// This MUST be called within a write lock on the owning MockFS.
func (c *ErrorConfig) use() bool {
	switch c.Mode {
	case ErrorModeAlways:
		return true // Always return the error
	case ErrorModeOnce:
		// Attempt to set 'used' from false to true.
		// If successful (it was false), return the error this one time.
		// If it fails (it was already true), don't return the error.
		return !c.used.Swap(true)
	case ErrorModeAfterSuccesses:
		// Decrement the success counter. If it reaches zero or below,
		// it's time to return the error.
		remainingSuccesses := c.Counter.Add(-1)
		return remainingSuccesses < 0 // Return error *after* successes are used up
	default:
		return true // Should not happen
	}
}
