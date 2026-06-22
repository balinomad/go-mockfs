package mockfs_test

import (
	"errors"
	"io/fs"
	"testing"
	"time"
)

const (
	testDurationShort = 25 * time.Millisecond
	testDuration      = 50 * time.Millisecond
	testDurationLong  = 100 * time.Millisecond
	tolerance         = 20 * time.Millisecond // Timing tolerance for test flakiness
)

// --- Require helpers use Fatal for immediate failure ---

// requireNoError fails test immediately if err is non-nil.
func requireNoError(tb testing.TB, err error, name ...string) {
	tb.Helper()

	if err != nil {
		tb.Fatalf("%sexpected no error, got %q", prefix(name...), err)
	}
}

// requirePanic fails test immediately if fn does not panic.
func requirePanic(tb testing.TB, fn func(), name ...string) {
	tb.Helper()

	defer func() {
		if r := recover(); r == nil {
			tb.Fatalf("%sexpected panic, but none occurred", prefix(name...))
		}
	}()

	fn()
}

// --- Assert helpers use Error for deferred failure ---

// assertPanic verifies that fn panics; fails test if no panic occurs.
func assertPanic(tb testing.TB, fn func(), name ...string) {
	tb.Helper()

	defer func() {
		if r := recover(); r == nil {
			tb.Errorf("%sexpected panic, but none occurred", prefix(name...))
		}
	}()

	fn()
}

// assertError asserts that got matches want. Handles fs.PathError wrapping.
func assertError(tb testing.TB, got, want error, name ...string) {
	tb.Helper()

	if got == nil {
		if want != nil {
			tb.Errorf("%sexpected error %q, got nil", prefix(name...), want)
		}
		return
	}

	if want == nil {
		tb.Errorf("%sexpected nil, got error %q", prefix(name...), got)
		return
	}

	if !errors.Is(got, want) {
		var pathErr *fs.PathError
		// If 'got' is a PathError, check if its internal error is 'want'.
		if errors.As(got, &pathErr) && errors.Is(pathErr.Err, want) {
			return
		}
		tb.Errorf("%sexpected error %q, got %q", prefix(name...), want, got)
	}
}

// assertNoError reports a non-fatal test error if err is non-nil.
func assertNoError(tb testing.TB, err error, name ...string) {
	tb.Helper()

	if err != nil {
		tb.Errorf("%sexpected no error, got %q", prefix(name...), err)
	}
}

// assertAnyError reports a non-fatal test error if err is nil.
func assertAnyError(tb testing.TB, err error, name ...string) {
	tb.Helper()

	if err == nil {
		tb.Errorf("%sexpected error, got nil", prefix(name...))
	}
}

// assertErrorWant asserts that got records an error if wantErr is true, and no error if wantErr is false.
// If expected is not nil, it is used as the expected error. Otherwise an error is expected if wantErr is true.
// Handles fs.PathError wrapping.
func assertErrorWant(tb testing.TB, got error, wantErr bool, expected error, name ...string) {
	tb.Helper()

	// No error returned
	if got == nil {
		if wantErr {
			// Expected an error but got nil
			if expected != nil {
				tb.Errorf("%sexpected error %q, but none occurred", prefix(name...), expected)
			} else {
				tb.Errorf("%sexpected error, but none occurred", prefix(name...))
			}
		}
		return
	}

	// Error returned but we didn't want any
	if !wantErr {
		assertNoError(tb, got, name...)
		return
	}

	// Error returned AND we wanted one; compare if expected is provided
	if expected != nil {
		assertError(tb, got, expected, name...)
	}
}

// assertDuration checks if elapsed time is within expected range.
func assertDuration(tb testing.TB, start time.Time, expected time.Duration, name ...string) {
	tb.Helper()

	elapsed := time.Since(start)
	if elapsed < expected-tolerance || elapsed > expected+tolerance {
		tb.Errorf("%sexpected duration %v (±%v), got %v", prefix(name...), expected, tolerance, elapsed)
	}
}

// assertNoDuration checks that operation completed quickly (no sleep).
func assertNoDuration(tb testing.TB, start time.Time, name ...string) {
	tb.Helper()

	elapsed := time.Since(start)
	if elapsed > tolerance {
		tb.Errorf("%sexpected duration < %v, got %v", prefix(name...), tolerance, elapsed)
	}
}

// mustReadFile reads the entire file or fails the test.
func mustReadFile(tb testing.TB, fsys fs.ReadFileFS, name string) []byte {
	tb.Helper()

	data, err := fsys.ReadFile(name)
	if err != nil {
		tb.Fatalf("ReadFile(%q) failed: %v", name, err)
	}

	return data
}

// prefix is a helper that returns the prefix for a test name.
func prefix(name ...string) string {
	if len(name) == 0 || name[0] == "" {
		return ""
	}
	return name[0] + ": "
}
