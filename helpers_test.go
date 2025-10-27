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

// requireNoError fails test immediately if err is non-nil.
func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal("expected no error, got:", err)
	}
}

// assertPanic verifies that fn panics; fails test if no panic occurs.
func assertPanic(t *testing.T, fn func(), name string) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("%s did not panic as expected", name)
		}
	}()
	fn()
}

// assertError asserts that got matches want. Handles fs.PathError wrapping.
func assertError(t *testing.T, got error, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		var pathErr *fs.PathError
		// If 'got' is a PathError, check if its internal error is 'want'.
		if errors.As(got, &pathErr) && errors.Is(pathErr.Err, want) {
			return
		}
		t.Errorf("expected error %q, got %q", want, got)
	}
}

// assertDuration checks if elapsed time is within expected range.
func assertDuration(t *testing.T, start time.Time, expected time.Duration, name string) {
	t.Helper()
	elapsed := time.Since(start)
	if elapsed < expected-tolerance || elapsed > expected+tolerance {
		t.Errorf("%s: expected ~%v, got %v", name, expected, elapsed)
	}
}

// assertNoDuration checks that operation completed quickly (no sleep).
func assertNoDuration(t *testing.T, start time.Time, name string) {
	t.Helper()
	elapsed := time.Since(start)
	if elapsed > tolerance {
		t.Errorf("%s: expected no latency, but it took %v", name, elapsed)
	}
}

// mustReadFile reads the entire file or fails the test.
func mustReadFile(t *testing.T, fsys fs.ReadFileFS, name string) []byte {
	t.Helper()
	data, err := fsys.ReadFile(name)
	if err != nil {
		t.Fatalf("ReadFile(%q) failed: %v", name, err)
	}
	return data
}
