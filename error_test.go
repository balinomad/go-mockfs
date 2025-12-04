package mockfs_test

import (
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/balinomad/go-mockfs/v2"
)

// --- ErrorRule ---

// TestErrorRule_Matchers verifies matcher behavior with different types.
func TestErrorRule_Matchers(t *testing.T) {
	t.Parallel()

	type pathError struct {
		path        string
		shouldError bool
	}

	regexpTxt, _ := mockfs.NewRegexpMatcher(`\.txt$`)
	regexpGo, _ := mockfs.NewRegexpMatcher(`\.go$`)

	tests := []struct {
		name       string
		op         mockfs.Operation
		errToApply error
		mode       mockfs.ErrorMode
		matchers   []mockfs.PathMatcher
		paths      []pathError
	}{
		{
			name:       "no matchers match nothing",
			op:         mockfs.OpOpen,
			errToApply: mockfs.ErrNotExist,
			mode:       mockfs.ErrorModeAlways,
			matchers:   nil,
			paths: []pathError{
				{"any/path.txt", false},
				{"", false},
				{"test.go", false},
			},
		},
		{
			name:       "wildcard matches all",
			op:         mockfs.OpOpen,
			errToApply: mockfs.ErrNotExist,
			mode:       mockfs.ErrorModeAlways,
			matchers:   []mockfs.PathMatcher{mockfs.NewWildcardMatcher()},
			paths: []pathError{
				{"test.txt", true},
				{"dir/file.go", true},
				{"", true},
				{"a/b/c/d.ext", true},
				{"!@#$%^&*()", true},
			},
		},
		{
			name:       "exact matcher",
			op:         mockfs.OpOpen,
			errToApply: mockfs.ErrNotExist,
			mode:       mockfs.ErrorModeAlways,
			matchers:   []mockfs.PathMatcher{mockfs.NewExactMatcher("test.txt")},
			paths: []pathError{
				{"test.txt", true},
				{"other.txt", false},
			},
		},
		{
			name:       "regexp matcher",
			op:         mockfs.OpRead,
			errToApply: mockfs.ErrPermission,
			mode:       mockfs.ErrorModeAlways,
			matchers:   []mockfs.PathMatcher{regexpTxt},
			paths: []pathError{
				{"test.txt", true},
				{"file.txt", true},
				{"test.go", false},
				{"dir/file.txt", true},
			},
		},
		{
			name:       "multiple matchers",
			op:         mockfs.OpOpen,
			errToApply: mockfs.ErrPermission,
			mode:       mockfs.ErrorModeAlways,
			matchers: []mockfs.PathMatcher{
				mockfs.NewExactMatcher("file1.txt"),
				mockfs.NewExactMatcher("file2.txt"),
				regexpGo,
			},
			paths: []pathError{
				{"file1.txt", true},
				{"file2.txt", true},
				{"main.go", true},
				{"test.go", true},
				{"other.txt", false},
				{"file3.txt", false},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inj := mockfs.NewErrorInjector()
			rule := mockfs.NewErrorRule(tt.errToApply, tt.mode, 0, tt.matchers...)
			inj.Add(tt.op, rule)

			for _, p := range tt.paths {
				t.Run(p.path, func(t *testing.T) {
					assertErrorWant(t, inj.CheckAndApply(tt.op, p.path), p.shouldError, tt.errToApply, fmt.Sprintf("path %q", p.path))
				})
			}
		})
	}
}

// TestErrorRule_Modes verifies error mode behavior.
func TestErrorRule_Modes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		op         mockfs.Operation
		path       string
		errToApply error
		mode       mockfs.ErrorMode
		n          int
		sequence   []bool // whether each call should error
	}{
		{
			name:       "always",
			op:         mockfs.OpRead,
			path:       "test.txt",
			errToApply: mockfs.ErrNotExist,
			mode:       mockfs.ErrorModeAlways,
			n:          0,
			sequence:   []bool{true, true, true, true, true, true, true, true, true, true},
		},
		{
			name:       "once",
			op:         mockfs.OpRead,
			path:       "test.txt",
			errToApply: mockfs.ErrCorrupted,
			mode:       mockfs.ErrorModeOnce,
			n:          0,
			sequence:   []bool{true, false, false, false, false, false, false, false, false, false, false},
		},
		{
			name:       "after 0",
			op:         mockfs.OpWrite,
			path:       "test.txt",
			errToApply: mockfs.ErrDiskFull,
			mode:       mockfs.ErrorModeAfterSuccesses,
			n:          0,
			sequence:   []bool{true, true, true, true, true},
		},
		{
			name:       "after 1",
			op:         mockfs.OpWrite,
			path:       "test.txt",
			errToApply: mockfs.ErrDiskFull,
			mode:       mockfs.ErrorModeAfterSuccesses,
			n:          1,
			sequence:   []bool{false, true, true, true, true},
		},
		{
			name:       "after 3",
			op:         mockfs.OpWrite,
			path:       "test.txt",
			errToApply: mockfs.ErrDiskFull,
			mode:       mockfs.ErrorModeAfterSuccesses,
			n:          3,
			sequence:   []bool{false, false, false, true, true, true},
		},
		{
			name:       "after 5 exact",
			op:         mockfs.OpWrite,
			path:       "test.txt",
			errToApply: mockfs.ErrDiskFull,
			mode:       mockfs.ErrorModeAfterSuccesses,
			n:          5,
			sequence:   []bool{false, false, false, false, false},
		},
		{
			name:       "after 5 plus one",
			op:         mockfs.OpWrite,
			path:       "test.txt",
			errToApply: mockfs.ErrDiskFull,
			mode:       mockfs.ErrorModeAfterSuccesses,
			n:          5,
			sequence:   []bool{false, false, false, false, false, true},
		},
		{
			name:       "next 0",
			op:         mockfs.OpRead,
			path:       "test.txt",
			errToApply: mockfs.ErrCorrupted,
			mode:       mockfs.ErrorModeNext,
			n:          0,
			sequence:   []bool{false, false, false, false, false},
		},
		{
			name:       "next 1",
			op:         mockfs.OpRead,
			path:       "test.txt",
			errToApply: mockfs.ErrCorrupted,
			mode:       mockfs.ErrorModeNext,
			n:          1,
			sequence:   []bool{true, false, false, false, false},
		},
		{
			name:       "next 3",
			op:         mockfs.OpRead,
			path:       "test.txt",
			errToApply: mockfs.ErrCorrupted,
			mode:       mockfs.ErrorModeNext,
			n:          3,
			sequence:   []bool{true, true, true, false, false, false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inj := mockfs.NewErrorInjector()
			inj.AddExact(tt.op, tt.path, tt.errToApply, tt.mode, tt.n)

			for i, wantErr := range tt.sequence {
				var expectedErr error
				if wantErr {
					expectedErr = tt.errToApply
				}
				assertError(t, inj.CheckAndApply(tt.op, tt.path), expectedErr, fmt.Sprintf("call %d", i+1))
			}
		})
	}
}

// TestErrorRule_Panics verifies panic conditions.
func TestErrorRule_Panics(t *testing.T) {
	t.Parallel()

	t.Run("invalid mode", func(t *testing.T) {
		rule := mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorMode(999), 0, mockfs.NewWildcardMatcher())
		inj := mockfs.NewErrorInjector()
		inj.Add(mockfs.OpRead, rule)
		requirePanic(t, func() { inj.CheckAndApply(mockfs.OpRead, "test.txt") }, "invalid ErrorMode")
	})

	t.Run("negative after", func(t *testing.T) {
		requirePanic(t, func() { mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, -1) }, "negative after")
	})
}

// TestErrorRule_CloneForSub tests the CloneForSub method.
func TestErrorRule_CloneForSub(t *testing.T) {
	t.Parallel()

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
			inj := mockfs.NewErrorInjector()
			inj.AddExact(mockfs.OpOpen, tt.originalPath, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

			cloned := inj.CloneForSub(tt.prefix)

			var wantErr error
			if tt.shouldMatch {
				wantErr = mockfs.ErrNotExist
			}
			assertError(t, cloned.CheckAndApply(mockfs.OpOpen, tt.testPath), wantErr, fmt.Sprintf("cloned.CheckAndApply(%q)", tt.testPath))
		})
	}

	t.Run("preserves properties", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpWrite, "prefix/file.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAfterSuccesses, 5)

		cloned := inj.CloneForSub("prefix")

		// verify the cloned rule has same properties by testing behavior
		// first 5 calls should succeed
		for i := 0; i < 5; i++ {
			assertError(t, cloned.CheckAndApply(mockfs.OpWrite, "file.txt"), nil, fmt.Sprintf("call %d (before AfterN)", i+1))
		}

		// 6th call should fail
		assertError(t, cloned.CheckAndApply(mockfs.OpWrite, "file.txt"), mockfs.ErrDiskFull, "call 6 (after AfterN)")
	})
}

// --- Operation ---

func TestOperation_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		op   mockfs.Operation
		want bool
	}{
		{"unknown", mockfs.OpUnknown, false},
		{"stat", mockfs.OpStat, true},
		{"open", mockfs.OpOpen, true},
		{"read", mockfs.OpRead, true},
		{"write", mockfs.OpWrite, true},
		{"close", mockfs.OpClose, true},
		{"negative", -1, false},
		{"large negative", -10000, false},
		{"num operations", mockfs.NumOperations, false},
		{"large positive", 10000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.op.IsValid(); got != tt.want {
				t.Errorf("isValid()=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestOperation_String(t *testing.T) {
	t.Parallel()

	t.Run("output", func(t *testing.T) {
		tests := []struct {
			name string
			op   mockfs.Operation
			want string
		}{
			{"unknown", mockfs.OpUnknown, "Unknown"},
			{"stat", mockfs.OpStat, "Stat"},
			{"open", mockfs.OpOpen, "Open"},
			{"read", mockfs.OpRead, "Read"},
			{"write", mockfs.OpWrite, "Write"},
			{"close", mockfs.OpClose, "Close"},
			{"negative", -1, "Invalid"},
			{"large negative", -10000, "Invalid"},
			{"num operations", mockfs.NumOperations, "Invalid"},
			{"large positive", 10000, "Invalid"},
		}

		for _, tt := range tests {
			if got := tt.op.String(); got != tt.want {
				t.Errorf("String() = %s, want %s", got, tt.want)
			}
		}
	})

	t.Run("missing string values", func(t *testing.T) {
		missing := []string{}
		for op := mockfs.OpUnknown; op < mockfs.NumOperations; op++ {
			if op.String() == "Invalid" {
				missing = append(missing, strconv.Itoa(int(op)))
			}
		}

		if len(missing) > 0 {
			t.Errorf("string value is missing for the following operations: %s", strings.Join(missing, ", "))
		}
	})

	t.Run("string to operation", func(t *testing.T) {
		tests := []struct {
			s    string
			want mockfs.Operation
		}{
			{"Stat", mockfs.OpStat},
			{"Open", mockfs.OpOpen},
			{"Read", mockfs.OpRead},
			{"ClOsE", mockfs.OpClose},
			{"READDIR", mockfs.OpReadDir},
			{"mkdir", mockfs.OpMkdir},
			{"MkDirAll", mockfs.OpMkdirAll},
			{"rename", mockfs.OpRename},
			{"invalid", mockfs.InvalidOperation},
			{"", mockfs.InvalidOperation},
			{"notanoperation", mockfs.InvalidOperation},
		}

		for _, tt := range tests {
			if got := mockfs.StringToOperation(tt.s); got != tt.want {
				t.Errorf("StringToOperation(%q) = %d, want %d", tt.s, got, tt.want)
			}
		}
	})
}

// --- ErrorInjector ---

func TestNewErrorInjector(t *testing.T) {
	inj := mockfs.NewErrorInjector()
	if inj == nil {
		t.Fatal("NewErrorInjector() returned nil")
	}

	// verify it's usable
	inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrNotExist, "injected ErrNotExist")
}

func TestErrorInjector_Add(t *testing.T) {
	t.Parallel()

	t.Run("add", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		rule := mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0, mockfs.NewExactMatcher("test.txt"))

		inj.Add(mockfs.OpOpen, rule)

		all := inj.GetAll()
		if len(all[mockfs.OpOpen]) != 1 {
			t.Errorf("expected 1 rule for OpOpen, got %d", len(all[mockfs.OpOpen]))
		}
	})

	t.Run("add exact", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpRead, "test.txt"), mockfs.ErrPermission, "test.txt")
		assertError(t, inj.CheckAndApply(mockfs.OpRead, "other.txt"), nil, "other.txt")
	})

	t.Run("add all", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddAll(mockfs.OpWrite, mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

		for _, path := range []string{"file1.txt", "file2.txt", "file3.txt"} {
			assertError(t, inj.CheckAndApply(mockfs.OpWrite, path), mockfs.ErrDiskFull, path)
		}
	})
}

func TestErrorInjector_AddRegexp(t *testing.T) {
	t.Parallel()

	t.Run("single operation", func(t *testing.T) {
		tests := []struct {
			name    string
			pattern string
			wantErr bool
		}{
			{"valid", "\\.txt$", false},
			{"invalid", "[invalid", true},
		}

		for _, tt := range tests {
			inj := mockfs.NewErrorInjector()

			t.Run("single operation with "+tt.name+" pattern", func(t *testing.T) {
				err := inj.AddRegexp(mockfs.OpOpen, tt.pattern, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
				assertErrorWant(t, err, tt.wantErr, nil, "AddRegexp()")
				if !tt.wantErr {
					assertAnyError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), "malformed regexp in AddRegexp()")
				} else {
					all := inj.GetAll()
					if len(all) != 0 {
						t.Errorf("expected no rules after failed AddRegexp(), got %d operations", len(all))
					}
				}
			})

			t.Run("all operations with "+tt.name+" pattern", func(t *testing.T) {
				err := inj.AddRegexpForAllOps(tt.pattern, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
				assertErrorWant(t, err, tt.wantErr, nil, "AddRegexpForAllOps()")
				if !tt.wantErr {
					assertAnyError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), "malformed regexp in AddRegexpForAllOps()")
				} else {
					all := inj.GetAll()
					if len(all) != 0 {
						t.Errorf("expected no rules after failed AddRegexpForAllOps(), got %d operations", len(all))
					}
				}
			})
		}
	})
}

func TestErrorInjector_AddGlob(t *testing.T) {
	t.Parallel()

	t.Run("single operation", func(t *testing.T) {
		tests := []struct {
			name    string
			pattern string
			wantErr bool
		}{
			{"valid", "*.txt", false},
			{"invalid", "[invalid", true},
		}

		for _, tt := range tests {
			inj := mockfs.NewErrorInjector()

			t.Run("single operation with "+tt.name+" pattern", func(t *testing.T) {
				err := inj.AddGlob(mockfs.OpOpen, tt.pattern, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
				assertErrorWant(t, err, tt.wantErr, nil, "AddGlob()")
				if !tt.wantErr {
					assertAnyError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), "malformed glob pattern in AddGlob()")
				} else {
					all := inj.GetAll()
					if len(all) != 0 {
						t.Errorf("expected no rules after failed AddGlob(), got %d operations", len(all))
					}
				}
			})

			t.Run("all operations with "+tt.name+" pattern", func(t *testing.T) {
				err := inj.AddGlobForAllOps(tt.pattern, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
				assertErrorWant(t, err, tt.wantErr, nil, "AddGlobForAllOps()")
				if !tt.wantErr {
					assertAnyError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), "malformed glob pattern in AddGlobForAllOps()")
				} else {
					all := inj.GetAll()
					if len(all) != 0 {
						t.Errorf("expected no rules after failed AddGlobForAllOps(), got %d operations", len(all))
					}
				}
			})
		}
	})
}

func TestErrorInjector_Clear(t *testing.T) {
	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	inj.AddExact(mockfs.OpRead, "other.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)

	// verify rules exist
	assertAnyError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), "before Clear()")

	inj.Clear()

	// verify rules are gone
	requireNoError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), "after Clear()")
	all := inj.GetAll()
	if len(all) != 0 {
		t.Errorf("expected empty config after Clear(), got %d operations", len(all))
	}
}

// TestErrorInjector_Priority verifies rule precedence.
func TestErrorInjector_Priority(t *testing.T) {
	t.Parallel()

	t.Run("add all then exact", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddAll(mockfs.OpOpen, mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0)
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrTimeout, "test.txt with AddAll()")
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "other.txt"), mockfs.ErrTimeout, "other.txt with AddAll()")
	})

	t.Run("exact then add all", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		inj.AddAll(mockfs.OpOpen, mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrNotExist, "test.txt with AddExact()")
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "other.txt"), mockfs.ErrTimeout, "other.txt with AddAll()")
	})

	t.Run("insertion order", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeOnce, 0)
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrNotExist, "first call -> AddExact() with ErrorModeOnce")
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrPermission, "second call -> AddExact() with ErrorModeAlways")
	})

	t.Run("op unknown fallback", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		rule := mockfs.NewErrorRule(mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0, mockfs.NewExactMatcher("test.txt"))
		inj.Add(mockfs.OpUnknown, rule)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrTimeout, "OpUnknown rule with OpOpen")
		assertError(t, inj.CheckAndApply(mockfs.OpRead, "test.txt"), mockfs.ErrTimeout, "OpUnknown rule with OpRead")
	})

	t.Run("op specific precedence", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		rule := mockfs.NewErrorRule(mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0, mockfs.NewExactMatcher("test.txt"))
		inj.Add(mockfs.OpUnknown, rule)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrNotExist, "op-specific rule preceeds OpUnknown rule")
	})
}

// TestErrorInjector_CheckAndApply verifies application logic.
func TestErrorInjector_CheckAndApply(t *testing.T) {
	t.Parallel()

	t.Run("no rules", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		assertNoError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), "no rules")
	})

	t.Run("multiple rules", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "first.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		inj.AddExact(mockfs.OpOpen, "second.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "first.txt"), mockfs.ErrNotExist, "first.txt")
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "second.txt"), mockfs.ErrPermission, "second.txt")
	})

	t.Run("different operations", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		inj.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		inj.AddExact(mockfs.OpWrite, "test.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)

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
			assertError(t, inj.CheckAndApply(tt.op, "test.txt"), tt.expected, tt.op.String())
		}
	})

	t.Run("op-specific takes precedence", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		rule := mockfs.NewErrorRule(mockfs.ErrTimeout, mockfs.ErrorModeAlways, 0, mockfs.NewExactMatcher("test.txt"))
		inj.Add(mockfs.OpUnknown, rule)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrNotExist, "test.txt op-specific rule")
	})

	t.Run("order of rules", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeOnce, 0)
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrNotExist, "first call")
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), mockfs.ErrPermission, "second call")
	})
}

// TestErrorInjector_GetAll verifies state introspection.
func TestErrorInjector_GetAll(t *testing.T) {
	t.Parallel()

	t.Run("get all rules", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		inj.AddExact(mockfs.OpRead, "other.txt", mockfs.ErrPermission, mockfs.ErrorModeOnce, 0)
		inj.AddExact(mockfs.OpWrite, "third.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAfterSuccesses, 5)

		all := inj.GetAll()

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
			t.Error("OpOpen rule error mismatch")
		}
		if all[mockfs.OpRead][0].Mode != mockfs.ErrorModeOnce {
			t.Error("OpRead rule mode mismatch")
		}
		if all[mockfs.OpWrite][0].AfterN != 5 {
			t.Errorf("OpWrite rule AfterN = %d, want 5", all[mockfs.OpWrite][0].AfterN)
		}
	})

	t.Run("independent copy", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

		all1 := inj.GetAll()
		all2 := inj.GetAll()

		// verify they are different slices (not same pointer)
		if len(all1[mockfs.OpOpen]) == 0 || len(all2[mockfs.OpOpen]) == 0 {
			t.Fatal("GetAll() returned empty results")
		}

		// modifying one shouldn't affect the original
		inj.AddExact(mockfs.OpOpen, "other.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
		all3 := inj.GetAll()

		if len(all1[mockfs.OpOpen]) != 1 {
			t.Errorf("first GetAll result should still have 1 rule, got %d", len(all1[mockfs.OpOpen]))
		}
		if len(all3[mockfs.OpOpen]) != 2 {
			t.Errorf("new GetAll should have 2 rules, got %d", len(all3[mockfs.OpOpen]))
		}
	})
}

// TestErrorInjector_CloneForSub verifies cloning independence.
func TestErrorInjector_CloneForSub(t *testing.T) {
	t.Parallel()

	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpOpen, "file.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	cloned := inj.CloneForSub("subdir")
	inj.AddExact(mockfs.OpRead, "new.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
	assertError(t, cloned.CheckAndApply(mockfs.OpRead, "subdir/new.txt"), nil, "cloned should not have new rule")
	assertError(t, inj.CheckAndApply(mockfs.OpRead, "new.txt"), mockfs.ErrPermission, "original should have new rule")
}

// TestErrorInjector_EdgeCases verifies edge case handling.
func TestErrorInjector_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty path", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, "", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, ""), mockfs.ErrNotExist, "empty path")
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "test.txt"), nil, "non-empty path should not match")
	})

	t.Run("special characters", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		specialPath := "file with spaces & special!@#$.txt"
		inj.AddExact(mockfs.OpOpen, specialPath, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, specialPath), mockfs.ErrNotExist, "special chars")
	})

	t.Run("very long path", func(t *testing.T) {
		longPath := ""
		for i := 0; i < 100; i++ {
			longPath += "very/long/path/segment/"
		}
		longPath += "file.txt"

		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpOpen, longPath, mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, longPath), mockfs.ErrNotExist, "long path")
	})
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
			inj := mockfs.NewErrorInjector()
			// Should not panic with valid after values
			tt.fn(inj)
		})
	}
}

func TestErrorInjector_Add_Concurrent(t *testing.T) {
	inj := mockfs.NewErrorInjector()
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
			}
		}(i)
	}

	wg.Wait()

	all := inj.GetAll()
	if len(all[mockfs.OpOpen]) != 1000 {
		t.Errorf("expected 1000 rules after concurrent adds, got %d", len(all[mockfs.OpOpen]))
	}
}

func TestErrorInjector_CheckAndApply_Concurrent(t *testing.T) {
	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	// concurrent checks
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				err := inj.CheckAndApply(mockfs.OpRead, "test.txt")
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

func TestErrorInjector_MixedOperations_Concurrent(t *testing.T) {
	inj := mockfs.NewErrorInjector()
	var wg sync.WaitGroup

	ops := []func(){
		func() {
			for j := 0; j < 50; j++ {
				inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
			}
		},
		func() {
			for j := 0; j < 50; j++ {
				_ = inj.CheckAndApply(mockfs.OpOpen, "test.txt")
			}
		},
		func() {
			for j := 0; j < 50; j++ {
				_ = inj.GetAll()
			}
		},
	}

	for i := 0; i < 5; i++ {
		for _, op := range ops {
			wg.Add(1)
			go func(fn func()) {
				defer wg.Done()
				fn()
			}(op)
		}
	}

	wg.Wait()
}

func TestErrorInjector_ErrorModeOnce_Concurrent(t *testing.T) {
	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpRead, "test.txt", mockfs.ErrCorrupted, mockfs.ErrorModeOnce, 0)

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	// concurrent checks - only one should get the error
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				err := inj.CheckAndApply(mockfs.OpRead, "test.txt")
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

// TestStandardFSErrors verifies standard library error usage.
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

// TestErrorInjector_RealWorldScenarios verifies real-world patterns.
func TestErrorInjector_RealWorldScenarios(t *testing.T) {
	t.Run("intermittent network", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		// simulate network error after 3 successful reads
		inj.AddExact(mockfs.OpRead, "remote/data.txt", mockfs.ErrTimeout, mockfs.ErrorModeAfterSuccesses, 3)

		for i := 0; i < 3; i++ {
			assertNoError(t, inj.CheckAndApply(mockfs.OpRead, "remote/data.txt"), fmt.Sprintf("call %d", i+1))
		}
		assertError(t, inj.CheckAndApply(mockfs.OpRead, "remote/data.txt"), mockfs.ErrTimeout, "call 4")
	})

	t.Run("disk full", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpWrite, "logs/app.log", mockfs.ErrDiskFull, mockfs.ErrorModeAlways, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpWrite, "logs/app.log"), mockfs.ErrDiskFull)
	})

	t.Run("permission denied on specific directory", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		assertNoError(t, inj.AddRegexp(mockfs.OpOpen, "^/protected/", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0), "protect directory")
		assertError(t, inj.CheckAndApply(mockfs.OpOpen, "/protected/secret.txt"), mockfs.ErrPermission, "open protected file")
		assertNoError(t, inj.CheckAndApply(mockfs.OpOpen, "/public/file.txt"), "open public file")
	})

	t.Run("transient corruption", func(t *testing.T) {
		inj := mockfs.NewErrorInjector()
		inj.AddExact(mockfs.OpRead, "data.db", mockfs.ErrCorrupted, mockfs.ErrorModeOnce, 0)
		assertError(t, inj.CheckAndApply(mockfs.OpRead, "data.db"), mockfs.ErrCorrupted, "first read should be corrupted")
		assertNoError(t, inj.CheckAndApply(mockfs.OpRead, "data.db"), "second read should succeed")
	})
}

// --- Benchmarks ---

func BenchmarkErrorInjector_Add(b *testing.B) {
	inj := mockfs.NewErrorInjector()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	}
}

func BenchmarkErrorInjector_CheckAndApply_NoMatch(b *testing.B) {
	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpOpen, "other.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = inj.CheckAndApply(mockfs.OpOpen, "test.txt")
	}
}

func BenchmarkErrorInjector_CheckAndApply_Match(b *testing.B) {
	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = inj.CheckAndApply(mockfs.OpOpen, "test.txt")
	}
}

func BenchmarkErrorInjector_CheckAndApply_MultipleRules(b *testing.B) {
	inj := mockfs.NewErrorInjector()
	for i := 0; i < 10; i++ {
		inj.AddExact(mockfs.OpOpen, "other.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	}
	inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrPermission, mockfs.ErrorModeAlways, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = inj.CheckAndApply(mockfs.OpOpen, "test.txt")
	}
}

func BenchmarkErrorInjector_CheckAndApply_WithWildcard(b *testing.B) {
	inj := mockfs.NewErrorInjector()
	rule := mockfs.NewErrorRule(mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0, mockfs.NewWildcardMatcher())
	inj.Add(mockfs.OpOpen, rule)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = inj.CheckAndApply(mockfs.OpOpen, "test.txt")
	}
}

func BenchmarkErrorInjector_CloneForSub(b *testing.B) {
	inj := mockfs.NewErrorInjector()
	for i := 0; i < 10; i++ {
		inj.AddExact(mockfs.OpOpen, "test.txt", mockfs.ErrNotExist, mockfs.ErrorModeAlways, 0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = inj.CloneForSub("subdir")
	}
}

func BenchmarkStringToOperation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mockfs.StringToOperation("Open")
	}
}

func BenchmarkErrorModeAfterSuccesses(b *testing.B) {
	inj := mockfs.NewErrorInjector()
	inj.AddExact(mockfs.OpWrite, "test.txt", mockfs.ErrDiskFull, mockfs.ErrorModeAfterSuccesses, 1000000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = inj.CheckAndApply(mockfs.OpWrite, "test.txt")
	}
}
