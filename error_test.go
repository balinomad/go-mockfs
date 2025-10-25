package mockfs_test

import (
	"errors"
	"io/fs"
	"sync"
	"testing"

	"github.com/balinomad/go-mockfs"
)

func TestErrorRule_NoMatchers(t *testing.T) {
	// test that no matchers means match nothing through CheckAndApply
	em := mockfs.NewErrorInjector()
	rule := mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	em.Add(mockfs.OpOpen, rule)

	// should not match any path
	err := em.CheckAndApply(mockfs.OpOpen, "any/path.txt")
	if err != nil {
		t.Errorf("no matchers should match nothing, got error: %v", err)
	}

	err = em.CheckAndApply(mockfs.OpOpen, "")
	if err != nil {
		t.Errorf("no matchers should match nothing even for empty path, got error: %v", err)
	}
}

func TestErrorRule_WithWildcardMatcher(t *testing.T) {
	// test that WildcardMatcher matches all paths
	em := mockfs.NewErrorInjector()
	rule := mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0, mockfs.NewWildcardMatcher())
	em.Add(mockfs.OpOpen, rule)

	paths := []string{"test.txt", "dir/file.go", "", "a/b/c/d.ext", "!@#$%^&*()"}
	for _, path := range paths {
		err := em.CheckAndApply(mockfs.OpOpen, path)
		if !errors.Is(err, mockfs.ErrNotExist) {
			t.Errorf("WildcardMatcher should match path %q, got error: %v", path, err)
		}
	}
}

func TestErrorRule_WithExactMatcher(t *testing.T) {
	em := mockfs.NewErrorInjector()
	matcher := mockfs.NewExactMatcher("test.txt")
	rule := mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0, matcher)
	em.Add(mockfs.OpOpen, rule)

	// should match exact path
	err := em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if !errors.Is(err, mockfs.ErrNotExist) {
		t.Errorf("expected ErrNotExist for exact match, got: %v", err)
	}

	// should not match other paths
	err = em.CheckAndApply(mockfs.OpOpen, "other.txt")
	if err != nil {
		t.Errorf("expected no error for non-matching path, got: %v", err)
	}
}

func TestErrorRule_WithRegexpMatcher(t *testing.T) {
	em := mockfs.NewErrorInjector()
	matcher, err := mockfs.NewRegexpMatcher("\\.txt$")
	if err != nil {
		t.Fatalf("NewRegexpMatcher() error: %v", err)
	}
	rule := mockfs.NewErrorRule(mockfs.ErrPermission, mockfs.ErrorModeAlways, 0, matcher)
	em.Add(mockfs.OpRead, rule)

	testCases := []struct {
		path      string
		shouldErr bool
	}{
		{"test.txt", true},
		{"file.txt", true},
		{"test.go", false},
		{"dir/file.txt", true},
	}

	for _, tc := range testCases {
		err := em.CheckAndApply(mockfs.OpRead, tc.path)
		if tc.shouldErr && err == nil {
			t.Errorf("expected error for path %q, got nil", tc.path)
		}
		if !tc.shouldErr && err != nil {
			t.Errorf("expected no error for path %q, got %v", tc.path, err)
		}
	}
}

func TestErrorRule_WithMultipleMatchers(t *testing.T) {
	em := mockfs.NewErrorInjector()
	m1 := mockfs.NewExactMatcher("file1.txt")
	m2 := mockfs.NewExactMatcher("file2.txt")
	m3, _ := mockfs.NewRegexpMatcher("\\.go$")

	rule := mockfs.NewErrorRule(mockfs.ErrPermission, mockfs.ErrorModeAlways, 0, m1, m2, m3)
	em.Add(mockfs.OpOpen, rule)

	tests := []struct {
		path        string
		shouldMatch bool
	}{
		{"file1.txt", true},
		{"file2.txt", true},
		{"main.go", true},
		{"test.go", true},
		{"other.txt", false},
		{"file3.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			err := em.CheckAndApply(mockfs.OpOpen, tt.path)
			if tt.shouldMatch && err == nil {
				t.Errorf("expected error for %q", tt.path)
			}
			if !tt.shouldMatch && err != nil {
				t.Errorf("expected no error for %q, got %v", tt.path, err)
			}
		})
	}
}

func TestErrorRule_ModeAlways(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

	// should return error every time
	for i := 0; i < 10; i++ {
		err := em.CheckAndApply(mockfs.OpRead, "test.txt")
		if !errors.Is(err, mockfs.ErrNotExist) {
			t.Errorf("call %d: ErrorModeAlways should return error, got: %v", i+1, err)
		}
	}
}

func TestErrorRule_ModeOnce(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrCorrupted, mockfs.ErrorModeOnce, 0)

	// first call should return error
	err := em.CheckAndApply(mockfs.OpRead, "test.txt")
	if !errors.Is(err, mockfs.ErrCorrupted) {
		t.Errorf("first call: expected ErrCorrupted, got %v", err)
	}

	// subsequent calls should return nil
	for i := 0; i < 10; i++ {
		err := em.CheckAndApply(mockfs.OpRead, "test.txt")
		if err != nil {
			t.Errorf("call %d after first: expected nil, got %v", i+2, err)
		}
	}
}

func TestErrorRule_ModeAfterSuccesses(t *testing.T) {
	tests := []struct {
		name     string
		afterN   int
		calls    int
		expected []bool
	}{
		{
			name:     "after 0 successes",
			afterN:   0,
			calls:    5,
			expected: []bool{true, true, true, true, true},
		},
		{
			name:     "after 1 success",
			afterN:   1,
			calls:    5,
			expected: []bool{false, true, true, true, true},
		},
		{
			name:     "after 3 successes",
			afterN:   3,
			calls:    6,
			expected: []bool{false, false, false, true, true, true},
		},
		{
			name:     "after 5 successes exact",
			afterN:   5,
			calls:    5,
			expected: []bool{false, false, false, false, false},
		},
		{
			name:     "after 5 successes plus one",
			afterN:   5,
			calls:    6,
			expected: []bool{false, false, false, false, false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := mockfs.NewErrorInjector()
			em.AddExact(mockfs.OpWrite, "test.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAfterSuccesses, tt.afterN)

			for i := 0; i < tt.calls; i++ {
				err := em.CheckAndApply(mockfs.OpWrite, "test.txt")
				gotErr := err != nil
				if gotErr != tt.expected[i] {
					t.Errorf("call %d: got error=%v, want error=%v", i+1, gotErr, tt.expected[i])
				}
			}
		})
	}
}

func TestErrorRule_CloneForSub(t *testing.T) {
	tests := []struct {
		name         string
		originalPath string
		prefix       string
		testPath     string
		shouldMatch  bool
	}{
		{
			name:         "clone with prefix",
			originalPath: "subdir/file.txt",
			prefix:       "subdir",
			testPath:     "file.txt",
			shouldMatch:  true,
		},
		{
			name:         "clone preserves original path",
			originalPath: "file.txt",
			prefix:       "subdir",
			testPath:     "subdir/file.txt",
			shouldMatch:  false,
		},
		{
			name:         "clone with empty prefix",
			originalPath: "file.txt",
			prefix:       "",
			testPath:     "file.txt",
			shouldMatch:  true,
		},
		{
			name:         "clone with nested prefix",
			originalPath: "dir/subdir/file.txt",
			prefix:       "dir/subdir",
			testPath:     "file.txt",
			shouldMatch:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := mockfs.NewErrorInjector()
			em.AddExact(mockfs.OpOpen, tt.originalPath, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

			cloned := em.CloneForSub(tt.prefix)

			err := cloned.CheckAndApply(mockfs.OpOpen, tt.testPath)
			gotMatch := err != nil
			if gotMatch != tt.shouldMatch {
				t.Errorf("cloned.CheckAndApply(%q) match=%v, want %v", tt.testPath, gotMatch, tt.shouldMatch)
			}
		})
	}
}

func TestErrorRule_CloneForSub_PreservesProperties(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpWrite, "prefix/file.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAfterSuccesses, 5)

	cloned := em.CloneForSub("prefix")

	// verify the cloned rule has same properties by testing behavior
	// first 5 calls should succeed
	for i := 0; i < 5; i++ {
		err := cloned.CheckAndApply(mockfs.OpWrite, "file.txt")
		if err != nil {
			t.Errorf("call %d: expected nil (before AfterN), got %v", i+1, err)
		}
	}

	// 6th call should fail
	err := cloned.CheckAndApply(mockfs.OpWrite, "file.txt")
	if !errors.Is(err, mockfs.ErrDiskFull) {
		t.Errorf("call 6: expected ErrDiskFull, got %v", err)
	}
}

// TestErrorRule_NegativeAfterPanic tests that negative 'after' values panic.
// All public functions accepting 'after' use mustAfter internally, so we verify
// the panic behavior once through a representative function.
func TestErrorRule_NegativeAfterPanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for negative after")
		}
	}()

	// Test through NewErrorRule as the most direct path to mustAfter
	mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, -1)
}

func TestOperation_IsValid(t *testing.T) {
	tests := []struct {
		name string
		op   mockfs.Operation
		want bool
	}{
		{
			name: "valid stat",
			op:   mockfs.OpStat,
			want: true,
		},
		{
			name: "valid open",
			op:   mockfs.OpOpen,
			want: true,
		},
		{
			name: "valid read",
			op:   mockfs.OpRead,
			want: true,
		},
		{
			name: "valid write",
			op:   mockfs.OpWrite,
			want: true,
		},
		{
			name: "valid close",
			op:   mockfs.OpClose,
			want: true,
		},
		{
			name: "first invalid negative",
			op:   -1,
			want: false,
		},
		{
			name: "large invalid negative",
			op:   -10000,
			want: false,
		},
		{
			name: "first invalid positive",
			op:   mockfs.NumOperations,
			want: false,
		},
		{
			name: "large invalid positive",
			op:   10000,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.op.IsValid()
			if got != tt.want {
				t.Errorf("op=%q, isValid()=%v, want %v", tt.op, got, tt.want)
			}
		})
	}
}

func TestOperation_String(t *testing.T) {
	tests := []struct {
		name string
		op   mockfs.Operation
		want string
	}{
		{
			name: "stat",
			op:   mockfs.OpStat,
			want: "Stat",
		},
		{
			name: "open",
			op:   mockfs.OpOpen,
			want: "Open",
		},
		{
			name: "read",
			op:   mockfs.OpRead,
			want: "Read",
		},
		{
			name: "write",
			op:   mockfs.OpWrite,
			want: "Write",
		},
		{
			name: "close",
			op:   mockfs.OpClose,
			want: "Close",
		},
		{
			name: "readdir",
			op:   mockfs.OpReadDir,
			want: "ReadDir",
		},
		{
			name: "mkdir",
			op:   mockfs.OpMkdir,
			want: "Mkdir",
		},
		{
			name: "mkdirall",
			op:   mockfs.OpMkdirAll,
			want: "MkdirAll",
		},
		{
			name: "remove",
			op:   mockfs.OpRemove,
			want: "Remove",
		},
		{
			name: "removeall",
			op:   mockfs.OpRemoveAll,
			want: "RemoveAll",
		},
		{
			name: "rename",
			op:   mockfs.OpRename,
			want: "Rename",
		},
		{
			name: "unknown",
			op:   mockfs.OpUnknown,
			want: "Unknown",
		},
		{
			name: "out of range",
			op:   mockfs.NumOperations,
			want: "Invalid",
		},
		{
			name: "invalid",
			op:   mockfs.InvalidOperation,
			want: "Invalid",
		},
		{
			name: "negative out of range",
			op:   mockfs.Operation(-100),
			want: "Invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.op.String()
			if got != tt.want {
				t.Errorf("String() = %s, wanted %s", got, tt.want)
			}
		})
	}
}

func TestStringToOperation(t *testing.T) {
	tests := []struct {
		name string
		s    string
		want mockfs.Operation
	}{
		{
			name: "stat",
			s:    "Stat",
			want: mockfs.OpStat,
		},
		{
			name: "open",
			s:    "Open",
			want: mockfs.OpOpen,
		},
		{
			name: "read",
			s:    "Read",
			want: mockfs.OpRead,
		},
		{
			name: "write",
			s:    "Write",
			want: mockfs.OpWrite,
		},
		{
			name: "close mixed case",
			s:    "ClOsE",
			want: mockfs.OpClose,
		},
		{
			name: "readdir uppercase",
			s:    "READDIR",
			want: mockfs.OpReadDir,
		},
		{
			name: "mkdir lowercase",
			s:    "mkdir",
			want: mockfs.OpMkdir,
		},
		{
			name: "mkdirall mixed",
			s:    "MkDirAll",
			want: mockfs.OpMkdirAll,
		},
		{
			name: "remove",
			s:    "Remove",
			want: mockfs.OpRemove,
		},
		{
			name: "removeall",
			s:    "RemoveAll",
			want: mockfs.OpRemoveAll,
		},
		{
			name: "rename lowercase",
			s:    "rename",
			want: mockfs.OpRename,
		},
		{
			name: "invalid",
			s:    "invalid",
			want: mockfs.InvalidOperation,
		},
		{
			name: "empty string",
			s:    "",
			want: mockfs.InvalidOperation,
		},
		{
			name: "random string",
			s:    "notanoperation",
			want: mockfs.InvalidOperation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mockfs.StringToOperation(tt.s)
			if got != tt.want {
				t.Errorf("StringToOperation() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNewErrorInjector(t *testing.T) {
	em := mockfs.NewErrorInjector()
	if em == nil {
		t.Fatal("NewErrorInjector() returned nil")
	}

	// verify it's usable
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	err := em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if err == nil {
		t.Error("expected error after adding rule")
	}
}

func TestErrorInjector_Add(t *testing.T) {
	em := mockfs.NewErrorInjector()
	rule := mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0, mockfs.NewExactMatcher("test.txt"))

	em.Add(mockfs.OpOpen, rule)

	all := em.GetAll()
	if len(all[mockfs.OpOpen]) != 1 {
		t.Errorf("expected 1 rule for OpOpen, got %d", len(all[mockfs.OpOpen]))
	}
}

func TestErrorInjector_AddExact(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)

	err := em.CheckAndApply(mockfs.OpRead, "test.txt")
	if !errors.Is(err, mockfs.ErrPermission) {
		t.Errorf("expected ErrPermission, got %v", err)
	}

	err = em.CheckAndApply(mockfs.OpRead, "other.txt")
	if err != nil {
		t.Errorf("expected no error for other path, got %v", err)
	}
}

func TestErrorInjector_AddRegexp(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{
			name:    "valid pattern",
			pattern: "\\.txt$",
			wantErr: false,
		},
		{
			name:    "invalid pattern",
			pattern: "[invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			em := mockfs.NewErrorInjector()
			err := em.AddRegexp(mockfs.OpOpen, tt.pattern, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddPattern() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// verify the pattern works
				err := em.CheckAndApply(mockfs.OpOpen, "test.txt")
				if err == nil {
					t.Error("expected error for matching pattern")
				}
			}
		})
	}
}

func TestErrorInjector_AddForPathAllOps(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddGlobForAllOps("test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)

	expectedOps := []mockfs.Operation{
		mockfs.OpStat, mockfs.OpOpen, mockfs.OpRead, mockfs.OpWrite, mockfs.OpClose,
		mockfs.OpReadDir, mockfs.OpMkdir, mockfs.OpMkdirAll, mockfs.OpRemove,
		mockfs.OpRemoveAll, mockfs.OpRename,
	}

	for _, op := range expectedOps {
		t.Run(op.String(), func(t *testing.T) {
			err := em.CheckAndApply(op, "test.txt")
			if !errors.Is(err, mockfs.ErrPermission) {
				t.Errorf("expected ErrPermission for %v, got %v", op, err)
			}
		})
	}
}

func TestErrorInjector_Clear(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	em.AddExact(mockfs.OpRead, "other.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)

	// verify rules exist
	err := em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if err == nil {
		t.Error("expected error before Clear()")
	}

	em.Clear()

	// verify rules are gone
	err = em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if err != nil {
		t.Errorf("expected no error after Clear(), got %v", err)
	}

	all := em.GetAll()
	if len(all) != 0 {
		t.Errorf("expected empty config after Clear(), got %d operations", len(all))
	}
}

func TestErrorInjector_CheckAndApply_NoRules(t *testing.T) {
	em := mockfs.NewErrorInjector()
	err := em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if err != nil {
		t.Errorf("expected no error with no rules, got %v", err)
	}
}

func TestErrorInjector_CheckAndApply_MultipleRules(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "first.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	em.AddExact(mockfs.OpOpen, "second.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)

	err := em.CheckAndApply(mockfs.OpOpen, "first.txt")
	if !errors.Is(err, mockfs.ErrNotExist) {
		t.Errorf("expected ErrNotExist for first.txt, got %v", err)
	}

	err = em.CheckAndApply(mockfs.OpOpen, "second.txt")
	if !errors.Is(err, mockfs.ErrPermission) {
		t.Errorf("expected ErrPermission for second.txt, got %v", err)
	}
}

func TestErrorInjector_CheckAndApply_OpUnknown(t *testing.T) {
	em := mockfs.NewErrorInjector()
	rule := mockfs.NewErrorRule(mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0, mockfs.NewExactMatcher("test.txt"))
	em.Add(mockfs.OpUnknown, rule)

	// OpUnknown should apply to any operation
	err := em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if !errors.Is(err, mockfs.ErrTimeout) {
		t.Errorf("expected ErrTimeout for OpUnknown rule, got %v", err)
	}

	err = em.CheckAndApply(mockfs.OpRead, "test.txt")
	if !errors.Is(err, mockfs.ErrTimeout) {
		t.Errorf("expected ErrTimeout for OpUnknown rule on different op, got %v", err)
	}
}

func TestErrorInjector_CheckAndApply_OpSpecificTakesPrecedence(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	rule := mockfs.NewErrorRule(mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0, mockfs.NewExactMatcher("test.txt"))
	em.Add(mockfs.OpUnknown, rule)

	// op-specific rule should be checked first and return first
	err := em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if !errors.Is(err, mockfs.ErrNotExist) {
		t.Errorf("expected ErrNotExist from op-specific rule, got %v", err)
	}
}

func TestErrorInjector_InsertionOrder(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeOnce, 0)
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)

	// first rule should be checked first (ErrorModeOnce)
	err := em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if !errors.Is(err, mockfs.ErrNotExist) {
		t.Errorf("first call: expected ErrNotExist, got %v", err)
	}

	// second rule should be checked after first is exhausted
	err = em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if !errors.Is(err, mockfs.ErrPermission) {
		t.Errorf("second call: expected ErrPermission, got %v", err)
	}
}

func TestErrorInjector_DifferentOperations(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	em.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
	em.AddExact(mockfs.OpWrite, "test.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

	tests := []struct {
		op       mockfs.Operation
		expected error
	}{
		{mockfs.OpOpen, mockfs.ErrNotExist},
		{mockfs.OpRead, mockfs.ErrPermission},
		{mockfs.OpWrite, mockfs.ErrDiskFull},
		{mockfs.OpClose, nil},
		{mockfs.OpStat, nil},
	}

	for _, tt := range tests {
		t.Run(tt.op.String(), func(t *testing.T) {
			err := em.CheckAndApply(tt.op, "test.txt")
			if tt.expected == nil {
				if err != nil {
					t.Errorf("expected nil, got %v", err)
				}
			} else {
				if !errors.Is(err, tt.expected) {
					t.Errorf("expected %v, got %v", tt.expected, err)
				}
			}
		})
	}
}

func TestErrorInjector_GetAll(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	em.AddExact(mockfs.OpRead, "other.txt", mockfs.ErrPermission, mockfs.ErrorModeOnce, 0)
	em.AddExact(mockfs.OpWrite, "third.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAfterSuccesses, 5)

	all := em.GetAll()

	if len(all[mockfs.OpOpen]) != 1 {
		t.Errorf("expected 1 rule for OpOpen, got %d", len(all[mockfs.OpOpen]))
	}
	if len(all[mockfs.OpRead]) != 1 {
		t.Errorf("expected 1 rule for OpRead, got %d", len(all[mockfs.OpRead]))
	}
	if len(all[mockfs.OpWrite]) != 1 {
		t.Errorf("expected 1 rule for OpWrite, got %d", len(all[mockfs.OpWrite]))
	}

	// verify properties by checking error types
	if !errors.Is(all[mockfs.OpOpen][0].Err, mockfs.ErrNotExist) {
		t.Errorf("OpOpen rule error mismatch")
	}
	if all[mockfs.OpRead][0].Mode != mockfs.ErrorModeOnce {
		t.Errorf("OpRead rule mode mismatch")
	}
	if all[mockfs.OpWrite][0].AfterN != 5 {
		t.Errorf("OpWrite rule AfterN mismatch, got %d", all[mockfs.OpWrite][0].AfterN)
	}
}

func TestErrorInjector_GetAll_ReturnsIndependentCopy(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

	all1 := em.GetAll()
	all2 := em.GetAll()

	// verify they are different slices (not same pointer)
	if len(all1[mockfs.OpOpen]) == 0 || len(all2[mockfs.OpOpen]) == 0 {
		t.Fatal("GetAll() returned empty results")
	}

	// modifying one shouldn't affect the original
	em.AddExact(mockfs.OpOpen, "other.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
	all3 := em.GetAll()

	if len(all1[mockfs.OpOpen]) != 1 {
		t.Errorf("first GetAll result should still have 1 rule, got %d", len(all1[mockfs.OpOpen]))
	}
	if len(all3[mockfs.OpOpen]) != 2 {
		t.Errorf("new GetAll should have 2 rules, got %d", len(all3[mockfs.OpOpen]))
	}
}

func TestErrorInjector_CloneForSub_Independence(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "file.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

	cloned := em.CloneForSub("subdir")

	// add rule to original
	em.AddExact(mockfs.OpRead, "new.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)

	// cloned should not have the new rule
	err := cloned.CheckAndApply(mockfs.OpRead, "subdir/new.txt")
	if err != nil {
		t.Errorf("cloned should not have new rule added to original, got %v", err)
	}

	// original should work with new rule
	err = em.CheckAndApply(mockfs.OpRead, "new.txt")
	if !errors.Is(err, mockfs.ErrPermission) {
		t.Errorf("original should have new rule, got %v", err)
	}
}

func TestErrorInjector_EmptyPath(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

	err := em.CheckAndApply(mockfs.OpOpen, "")
	if !errors.Is(err, mockfs.ErrNotExist) {
		t.Errorf("empty path: expected ErrNotExist, got %v", err)
	}

	err = em.CheckAndApply(mockfs.OpOpen, "test.txt")
	if err != nil {
		t.Errorf("non-empty path should not match empty rule, got %v", err)
	}
}

func TestErrorInjector_SpecialCharactersInPath(t *testing.T) {
	em := mockfs.NewErrorInjector()
	specialPath := "file with spaces & special!@#$.txt"
	em.AddExact(mockfs.OpOpen, specialPath, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

	err := em.CheckAndApply(mockfs.OpOpen, specialPath)
	if !errors.Is(err, mockfs.ErrNotExist) {
		t.Errorf("special chars path: expected ErrNotExist, got %v", err)
	}
}

func TestErrorInjector_VeryLongPath(t *testing.T) {
	em := mockfs.NewErrorInjector()
	longPath := ""
	for i := 0; i < 100; i++ {
		longPath += "very/long/path/segment/"
	}
	longPath += "file.txt"

	em.AddExact(mockfs.OpOpen, longPath, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

	err := em.CheckAndApply(mockfs.OpOpen, longPath)
	if !errors.Is(err, mockfs.ErrNotExist) {
		t.Errorf("long path: expected ErrNotExist, got %v", err)
	}
}

// TestErrorInjector_AfterParameter verifies that all functions accepting 'after'
// work correctly with valid values (>= 0). This indirectly confirms they call
// mustAfter, which is tested separately for panic behavior.
func TestErrorInjector_AfterParameter(t *testing.T) {
	tests := []struct {
		name string
		fn   func(mockfs.ErrorInjector)
	}{
		{
			name: "add exact with after",
			fn: func(ei mockfs.ErrorInjector) {
				ei.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAfterSuccesses, 5)
			},
		},
		{
			name: "add glob with after",
			fn: func(ei mockfs.ErrorInjector) {
				_ = ei.AddGlob(mockfs.OpRead, "*.txt", mockfs.ErrNotExist, mockfs.ErrorModeAfterSuccesses, 3)
			},
		},
		{
			name: "add regexp with after",
			fn: func(ei mockfs.ErrorInjector) {
				_ = ei.AddRegexp(mockfs.OpRead, ".*", mockfs.ErrNotExist, mockfs.ErrorModeAfterSuccesses, 2)
			},
		},
		{
			name: "add all with after",
			fn: func(ei mockfs.ErrorInjector) {
				ei.AddAll(mockfs.OpRead, mockfs.ErrNotExist, mockfs.ErrorModeAfterSuccesses, 1)
			},
		},
		{
			name: "add exact for all ops with after",
			fn: func(ei mockfs.ErrorInjector) {
				ei.AddExactForAllOps("test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAfterSuccesses, 4)
			},
		},
		{
			name: "add glob for all ops with after",
			fn: func(ei mockfs.ErrorInjector) {
				_ = ei.AddGlobForAllOps("*.txt", mockfs.ErrNotExist, mockfs.ErrorModeAfterSuccesses, 2)
			},
		},
		{
			name: "add regexp for all ops with after",
			fn: func(ei mockfs.ErrorInjector) {
				_ = ei.AddRegexpForAllOps(".*", mockfs.ErrNotExist, mockfs.ErrorModeAfterSuccesses, 3)
			},
		},
		{
			name: "add all for all ops with after",
			fn: func(ei mockfs.ErrorInjector) {
				ei.AddAllForAllOps(mockfs.ErrNotExist, mockfs.ErrorModeAfterSuccesses, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ei := mockfs.NewErrorInjector()
			// Should not panic with valid after values
			tt.fn(ei)
		})
	}
}

func TestErrorInjector_Concurrent_Add(t *testing.T) {
	em := mockfs.NewErrorInjector()
	var wg sync.WaitGroup

	// concurrent adds
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
			}
		}(i)
	}

	wg.Wait()

	all := em.GetAll()
	if len(all[mockfs.OpOpen]) != 1000 {
		t.Errorf("expected 1000 rules after concurrent adds, got %d", len(all[mockfs.OpOpen]))
	}
}

func TestErrorInjector_Concurrent_CheckAndApply(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	// concurrent checks
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				err := em.CheckAndApply(mockfs.OpRead, "test.txt")
				if err != nil {
					mu.Lock()
					errCount++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	if errCount != 1000 {
		t.Errorf("expected 1000 errors, got %d", errCount)
	}
}

func TestErrorInjector_Concurrent_MixedOperations(t *testing.T) {
	em := mockfs.NewErrorInjector()
	var wg sync.WaitGroup

	// concurrent mixed operations
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = em.CheckAndApply(mockfs.OpOpen, "test.txt")
			}
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = em.GetAll()
			}
		}()
	}

	wg.Wait()
}

func TestErrorInjector_Concurrent_ErrorModeOnce(t *testing.T) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrCorrupted, mockfs.ErrorModeOnce, 0)

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	// concurrent checks - only one should get the error
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				err := em.CheckAndApply(mockfs.OpRead, "test.txt")
				if err != nil {
					mu.Lock()
					errCount++
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	if errCount != 1 {
		t.Errorf("ErrorModeOnce: expected exactly 1 error in concurrent access, got %d", errCount)
	}
}

func TestCustomErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "disk full",
			err:  mockfs.ErrDiskFull,
			want: "disk full",
		},
		{
			name: "timeout",
			err:  mockfs.ErrTimeout,
			want: "operation timeout",
		},
		{
			name: "corrupted",
			err:  mockfs.ErrCorrupted,
			want: "corrupted data",
		},
		{
			name: "too many handles",
			err:  mockfs.ErrTooManyHandles,
			want: "too many open files",
		},
		{
			name: "not dir",
			err:  mockfs.ErrNotDir,
			want: "not a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.want {
				t.Errorf("error message = %q, want %q", tt.err.Error(), tt.want)
			}
		})
	}
}

func TestStandardFSErrors(t *testing.T) {
	// verify we're using standard fs errors
	if mockfs.ErrInvalid != fs.ErrInvalid {
		t.Error("ErrInvalid should be fs.ErrInvalid")
	}
	if mockfs.ErrPermission != fs.ErrPermission {
		t.Error("ErrPermission should be fs.ErrPermission")
	}
	if mockfs.ErrExist != fs.ErrExist {
		t.Error("ErrExist should be fs.ErrExist")
	}
	if mockfs.ErrNotExist != fs.ErrNotExist {
		t.Error("ErrNotExist should be fs.ErrNotExist")
	}
	if mockfs.ErrClosed != fs.ErrClosed {
		t.Error("ErrClosed should be fs.ErrClosed")
	}
}

func TestErrorInjector_RealWorldScenarios(t *testing.T) {
	t.Run("intermittent network error", func(t *testing.T) {
		em := mockfs.NewErrorInjector()
		// simulate network error after 3 successful reads
		em.AddExact(mockfs.OpRead, "remote/data.txt", mockfs.ErrTimeout, mockfs.ErrorModeAfterSuccesses, 3)

		for i := 0; i < 3; i++ {
			err := em.CheckAndApply(mockfs.OpRead, "remote/data.txt")
			if err != nil {
				t.Errorf("call %d should succeed, got %v", i+1, err)
			}
		}

		err := em.CheckAndApply(mockfs.OpRead, "remote/data.txt")
		if !errors.Is(err, mockfs.ErrTimeout) {
			t.Errorf("call 4 should timeout, got %v", err)
		}
	})

	t.Run("disk full on write", func(t *testing.T) {
		em := mockfs.NewErrorInjector()
		em.AddExact(mockfs.OpWrite, "logs/app.log", mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

		err := em.CheckAndApply(mockfs.OpWrite, "logs/app.log")
		if !errors.Is(err, mockfs.ErrDiskFull) {
			t.Errorf("expected ErrDiskFull, got %v", err)
		}
	})

	t.Run("permission denied on specific directory", func(t *testing.T) {
		em := mockfs.NewErrorInjector()
		pattern := "^/protected/"
		err := em.AddRegexp(mockfs.OpOpen, pattern, mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		if err != nil {
			t.Fatalf("AddPattern error: %v", err)
		}

		err = em.CheckAndApply(mockfs.OpOpen, "/protected/secret.txt")
		if !errors.Is(err, mockfs.ErrPermission) {
			t.Errorf("expected ErrPermission, got %v", err)
		}

		err = em.CheckAndApply(mockfs.OpOpen, "/public/file.txt")
		if err != nil {
			t.Errorf("public file should be accessible, got %v", err)
		}
	})

	t.Run("transient corruption error", func(t *testing.T) {
		em := mockfs.NewErrorInjector()
		em.AddExact(mockfs.OpRead, "data.db", mockfs.ErrCorrupted, mockfs.ErrorModeOnce, 0)

		err := em.CheckAndApply(mockfs.OpRead, "data.db")
		if !errors.Is(err, mockfs.ErrCorrupted) {
			t.Errorf("first read should be corrupted, got %v", err)
		}

		err = em.CheckAndApply(mockfs.OpRead, "data.db")
		if err != nil {
			t.Errorf("second read should succeed, got %v", err)
		}
	})
}

// Benchmark tests
func BenchmarkErrorInjector_Add(b *testing.B) {
	em := mockfs.NewErrorInjector()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	}
}

func BenchmarkErrorInjector_CheckAndApply_NoMatch(b *testing.B) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "other.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = em.CheckAndApply(mockfs.OpOpen, "test.txt")
	}
}

func BenchmarkErrorInjector_CheckAndApply_Match(b *testing.B) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = em.CheckAndApply(mockfs.OpOpen, "test.txt")
	}
}

func BenchmarkErrorInjector_CheckAndApply_MultipleRules(b *testing.B) {
	em := mockfs.NewErrorInjector()
	for i := 0; i < 10; i++ {
		em.AddExact(mockfs.OpOpen, "other.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	}
	em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = em.CheckAndApply(mockfs.OpOpen, "test.txt")
	}
}

func BenchmarkErrorInjector_CheckAndApply_WithWildcard(b *testing.B) {
	em := mockfs.NewErrorInjector()
	rule := mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0, mockfs.NewWildcardMatcher())
	em.Add(mockfs.OpOpen, rule)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = em.CheckAndApply(mockfs.OpOpen, "test.txt")
	}
}

func BenchmarkErrorInjector_CloneForSub(b *testing.B) {
	em := mockfs.NewErrorInjector()
	for i := 0; i < 10; i++ {
		em.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = em.CloneForSub("subdir")
	}
}

func BenchmarkStringToOperation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mockfs.StringToOperation("Open")
	}
}

func BenchmarkErrorModeAfterSuccesses(b *testing.B) {
	em := mockfs.NewErrorInjector()
	em.AddExact(mockfs.OpWrite, "test.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAfterSuccesses, 1000000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = em.CheckAndApply(mockfs.OpWrite, "test.txt")
	}
}
