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
func requireNoError(t testing.TB, err error, name ...string) {
	t.Helper()

	if err != nil {
		t.Fatalf("%sexpected no error, got %q", prefix(name...), err)
	}
}

// requirePanic fails test immediately if fn does not panic.
func requirePanic(t testing.TB, fn func(), name ...string) {
	t.Helper()

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("%sexpected panic, but none occurred", prefix(name...))
		}
	}()

	fn()
}

// --- Assert helpers use Error for deferred failure ---

// assertPanic verifies that fn panics; fails test if no panic occurs.
func assertPanic(t testing.TB, fn func(), name ...string) {
	t.Helper()

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%sexpected panic, but none occurred", prefix(name...))
		}
	}()

	fn()
}

// assertError asserts that got matches want. Handles fs.PathError wrapping.
func assertError(t testing.TB, got error, want error, name ...string) {
	t.Helper()

	if got == nil {
		if want != nil {
			t.Errorf("%sexpected error %q, got nil", prefix(name...), want)
		}
		return
	}

	if want == nil {
		t.Errorf("%sexpected nil, got error %q", prefix(name...), got)
		return
	}

	if !errors.Is(got, want) {
		var pathErr *fs.PathError
		// If 'got' is a PathError, check if its internal error is 'want'.
		if errors.As(got, &pathErr) && errors.Is(pathErr.Err, want) {
			return
		}
		t.Errorf("%sexpected error %q, got %q", prefix(name...), want, got)
	}
}

// assertNoError reports a non-fatal test error if err is non-nil.
func assertNoError(t testing.TB, err error, name ...string) {
	t.Helper()

	if err != nil {
		t.Errorf("%sexpected no error, got %q", prefix(name...), err)
	}
}

// assertAnyError reports a non-fatal test error if err is nil.
func assertAnyError(t testing.TB, err error, name ...string) {
	t.Helper()

	if err == nil {
		t.Errorf("%sexpected error, got nil", prefix(name...))
	}
}

// assertErrorWant asserts that got records an error if wantErr is true, and no error if wantErr is false.
// If expected is not nil, it is used as the expected error. Otherwise an error is expected if wantErr is true.
// Handles fs.PathError wrapping.
func assertErrorWant(t testing.TB, got error, wantErr bool, expected error, name ...string) {
	t.Helper()

	// No error returned
	if got == nil {
		if wantErr {
			// Expected an error but got nil
			if expected != nil {
				t.Errorf("%sexpected error %q, but none occurred", prefix(name...), expected)
			} else {
				t.Errorf("%sexpected error, but none occurred", prefix(name...))
			}
		}
		return
	}

	// Error returned but we didn't want any
	if !wantErr {
		assertNoError(t, got, name...)
		return
	}

	// Error returned AND we wanted one; compare if expected is provided
	if expected != nil {
		assertError(t, got, expected, name...)
	}
}

// assertDuration checks if elapsed time is within expected range.
func assertDuration(t testing.TB, start time.Time, expected time.Duration, name ...string) {
	t.Helper()

	elapsed := time.Since(start)
	if elapsed < expected-tolerance || elapsed > expected+tolerance {
		t.Errorf("%sexpected duration %v (±%v), got %v", prefix(name...), expected, tolerance, elapsed)
	}
}

// assertNoDuration checks that operation completed quickly (no sleep).
func assertNoDuration(t testing.TB, start time.Time, name ...string) {
	t.Helper()

	elapsed := time.Since(start)
	if elapsed > tolerance {
		t.Errorf("%sexpected duration < %v, got %v", prefix(name...), tolerance, elapsed)
	}
}

// mustReadFile reads the entire file or fails the test.
func mustReadFile(t testing.TB, fsys fs.ReadFileFS, name string, label ...string) []byte {
	t.Helper()

	data, err := fsys.ReadFile(name)
	if err != nil {
		t.Fatalf("%sReadFile(%q) failed: %v", prefix(label...), name, err)
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
