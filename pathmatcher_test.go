package mockfs_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/balinomad/go-mockfs/v2"
)

func TestExactMatcher_Matches(t *testing.T) {
	tests := []struct {
		name     string
		stored   string
		input    string
		expected bool
	}{
		{
			name:     "exact match",
			stored:   "test.txt",
			input:    "test.txt",
			expected: true,
		},
		{
			name:     "no match different name",
			stored:   "test.txt",
			input:    "other.txt",
			expected: false,
		},
		{
			name:     "no match case sensitive",
			stored:   "test.txt",
			input:    "Test.txt",
			expected: false,
		},
		{
			name:     "no match prefix",
			stored:   "test.txt",
			input:    "test.txt.backup",
			expected: false,
		},
		{
			name:     "no match suffix",
			stored:   "test.txt",
			input:    "mytest.txt",
			expected: false,
		},
		{
			name:     "empty path matches empty",
			stored:   "",
			input:    "",
			expected: true,
		},
		{
			name:     "empty path no match",
			stored:   "",
			input:    "test.txt",
			expected: false,
		},
		{
			name:     "path with slashes",
			stored:   "dir/subdir/file.txt",
			input:    "dir/subdir/file.txt",
			expected: true,
		},
		{
			name:     "leading slash literal",
			stored:   "/absolute/path.txt",
			input:    "/absolute/path.txt",
			expected: true,
		},
		{
			name:     "path with spaces",
			stored:   "file with spaces.txt",
			input:    "file with spaces.txt",
			expected: true,
		},
		{
			name:     "path with special chars",
			stored:   "file-name_test.v2.txt",
			input:    "file-name_test.v2.txt",
			expected: true,
		},
		{
			name:     "unicode path",
			stored:   "файл.txt",
			input:    "файл.txt",
			expected: true,
		},
		{
			name:     "unicode no match",
			stored:   "файл.txt",
			input:    "file.txt",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mockfs.NewExactMatcher(tt.stored)
			if got := m.Matches(tt.input); got != tt.expected {
				t.Errorf("ExactMatcher.Matches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExactMatcher_CloneForSub(t *testing.T) {
	tests := []struct {
		name       string
		stored     string // full parent path stored in the original matcher
		prefix     string // sub prefix
		inputInSub string // path used inside sub filesystem (relative path)
		expected   bool
	}{
		{
			name:       "simple child",
			stored:     "subdir/file.txt",
			prefix:     "subdir",
			inputInSub: "file.txt",
			expected:   true,
		},
		{
			name:       "prefix with trailing slash",
			stored:     "subdir/file.txt",
			prefix:     "subdir/",
			inputInSub: "file.txt",
			expected:   true,
		},
		{
			name:       "nested prefix",
			stored:     "dir/subdir/file.txt",
			prefix:     "dir/subdir",
			inputInSub: "file.txt",
			expected:   true,
		},
		{
			name:       "nested path preserved",
			stored:     "dir/sub/file.txt",
			prefix:     "dir",
			inputInSub: "sub/file.txt",
			expected:   true,
		},
		{
			name:       "directory maps to subdir name",
			stored:     "dir/subdir",
			prefix:     "dir",
			inputInSub: "subdir",
			expected:   true,
		},
		{
			name:       "prefix path maps to dot",
			stored:     "dir",
			prefix:     "dir",
			inputInSub: ".",
			expected:   true,
		},
		{
			name:       "prefix with trailing slash normalized",
			stored:     "dir/file.txt",
			prefix:     "dir/",
			inputInSub: "file.txt",
			expected:   true,
		},
		{
			name:       "outside subtree yields no match",
			stored:     "file.txt",
			prefix:     "subdir",
			inputInSub: "file.txt",
			expected:   false,
		},
		{
			name:       "both slashes in stored and prefix",
			stored:     "/file.txt",
			prefix:     "subdir/",
			inputInSub: "/file.txt", // literal matching preserved
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := mockfs.NewExactMatcher(tt.stored)
			cloned := orig.CloneForSub(tt.prefix)
			if got := cloned.Matches(tt.inputInSub); got != tt.expected {
				t.Errorf("CloneForSub().Matches() = %v, want %v (stored=%q prefix=%q input=%q)", got, tt.expected, tt.stored, tt.prefix, tt.inputInSub)
			}
		})
	}
}

// TestExactMatcher_Concurrent tests concurrent access to ExactMatcher.
func TestExactMatcher_Concurrent(t *testing.T) {
	m := mockfs.NewExactMatcher("test.txt")

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = m.Matches("test.txt")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestExactMatcher_LongPaths tests ExactMatcher with very long paths.
func TestExactMatcher_LongPaths(t *testing.T) {
	longPath := ""
	for i := 0; i < 100; i++ {
		longPath += "very/long/path/segment/"
	}
	longPath += "file.txt"

	m := mockfs.NewExactMatcher(longPath)
	if !m.Matches(longPath) {
		t.Error("should match long path")
	}
	if m.Matches(longPath + "x") {
		t.Error("should not match modified long path")
	}
}

func TestNewRegexpMatcher(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{
			name:    "valid simple pattern",
			pattern: "^test.*\\.txt$",
			wantErr: false,
		},
		{
			name:    "valid complex pattern",
			pattern: "^(foo|bar)/.*\\.(go|txt)$",
			wantErr: false,
		},
		{
			name:    "valid empty pattern",
			pattern: "",
			wantErr: false,
		},
		{
			name:    "invalid pattern unclosed bracket",
			pattern: "test[.txt",
			wantErr: true,
		},
		{
			name:    "invalid pattern unclosed paren",
			pattern: "test(foo",
			wantErr: true,
		},
		{
			name:    "invalid pattern bad escape",
			pattern: "test\\",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mockfs.NewRegexpMatcher(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRegexpMatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got == nil {
				t.Error("NewRegexpMatcher() returned nil matcher without error")
			}
			if tt.wantErr && got != nil {
				t.Error("NewRegexpMatcher() returned matcher with error")
			}
		})
	}
}

func TestRegexpMatcher_Matches(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		{
			name:     "match prefix pattern",
			pattern:  "^test",
			input:    "test.txt",
			expected: true,
		},
		{
			name:     "no match prefix pattern",
			pattern:  "^test",
			input:    "mytest.txt",
			expected: false,
		},
		{
			name:     "match suffix pattern",
			pattern:  "\\.txt$",
			input:    "test.txt",
			expected: true,
		},
		{
			name:     "no match suffix pattern",
			pattern:  "\\.txt$",
			input:    "test.go",
			expected: false,
		},
		{
			name:     "match anywhere",
			pattern:  "test",
			input:    "mytest.txt",
			expected: true,
		},
		{
			name:     "match with alternation",
			pattern:  "\\.(go|txt)$",
			input:    "file.go",
			expected: true,
		},
		{
			name:     "match complex path",
			pattern:  "^src/.*/.*\\.go$",
			input:    "src/pkg/main.go",
			expected: true,
		},
		{
			name:     "no match complex path",
			pattern:  "^src/.*/.*\\.go$",
			input:    "test/pkg/main.go",
			expected: false,
		},
		{
			name:     "match with character class",
			pattern:  "^[a-z]+\\.txt$",
			input:    "test.txt",
			expected: true,
		},
		{
			name:     "no match character class",
			pattern:  "^[a-z]+\\.txt$",
			input:    "Test.txt",
			expected: false,
		},
		{
			name:     "match empty string with empty pattern",
			pattern:  "",
			input:    "",
			expected: true,
		},
		{
			name:     "match any with empty pattern",
			pattern:  "",
			input:    "anything",
			expected: true,
		},
		{
			name:     "match dot metachar",
			pattern:  "test.txt",
			input:    "testXtxt",
			expected: true,
		},
		{
			name:     "match escaped dot",
			pattern:  "test\\.txt",
			input:    "test.txt",
			expected: true,
		},
		{
			name:     "no match escaped dot",
			pattern:  "test\\.txt",
			input:    "testXtxt",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := mockfs.NewRegexpMatcher(tt.pattern)
			if err != nil {
				t.Fatalf("NewRegexpMatcher() unexpected error: %v", err)
			}
			if got := m.Matches(tt.input); got != tt.expected {
				t.Errorf("RegexpMatcher.Matches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestRegexpMatcher_CloneForSub(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		prefix      string
		testPath    string // path inside the sub-FS (relative)
		expected    bool   // expectation for cloned matcher
		origCheck   string // optional: a path the original must still match
		origCheckOK bool   // expected result for origCheck
		nestedClone string // optional second CloneForSub
	}{
		{
			// original pattern matches suffix; when cloned into "subdir" it will check "subdir/<testPath>"
			name:     "clone preserves suffix semantics",
			pattern:  "\\.txt$",
			prefix:   "subdir",
			testPath: "test.txt",
			expected: true,
		},
		{
			// anchored prefix ^test will not match when composed with "subdir/<testPath>" because the assembled path
			// starts with "subdir/"
			name:     "anchored prefix not matched after cloning",
			pattern:  "^test",
			prefix:   "subdir",
			testPath: "test.txt",
			expected: false,
		},
		{
			// empty prefix returns same behaviour as original (CloneForSub returns original)
			name:     "empty prefix no-op",
			pattern:  "file",
			prefix:   "",
			testPath: "myfile.txt",
			expected: true,
		},
		{
			// cloning with "." should be treated like no-op and return the same matcher
			name:     "dot prefix no-op",
			pattern:  "abc",
			prefix:   ".",
			testPath: "abc",
			expected: true,
		},
		{
			// trailing slash in prefix should be trimmed
			name:     "trailing slash in prefix",
			pattern:  "\\.txt$",
			prefix:   "dir/",
			testPath: "file.txt",
			expected: true,
		},
		{
			// cloning should not modify the original matcher behaviour.
			// The original pattern matches "exact" in the parent FS,
			// but inside Sub("dir"), the full path becomes "dir/exact",
			// which does not match ^exact$.
			name:        "original unaffected by clone",
			pattern:     "^exact$",
			prefix:      "dir",
			testPath:    "exact",
			expected:    false,
			origCheck:   "exact",
			origCheckOK: true,
		},
		{
			name:        "nested clone combines prefixes",
			pattern:     "\\.txt$",
			prefix:      "dir",
			testPath:    "file.txt",
			expected:    true,
			origCheck:   "file.txt", // original still works
			origCheckOK: true,
			nestedClone: "subdir", // we’ll handle nestedClone inside the loop (see below)
		},
		{
			name:     "exact outside prefix becomes noneMatcher",
			pattern:  "^file\\.txt$",
			prefix:   "otherdir",
			testPath: "file.txt",
			expected: false,
		},
		{
			// dot path matches the prefix itself
			name:     "dot matches prefix",
			pattern:  "^subdir$",
			prefix:   "subdir",
			testPath: ".",
			expected: true,
		},
		// empty string path also matches prefix
		{
			name:     "empty string matches prefix",
			pattern:  "^subdir$",
			prefix:   "subdir",
			testPath: "",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := mockfs.NewRegexpMatcher(tt.pattern)
			if err != nil {
				t.Fatalf("NewRegexpMatcher() unexpected error: %v", err)
			}
			cloned := m.CloneForSub(tt.prefix)
			if tt.nestedClone != "" {
				cloned = cloned.CloneForSub(tt.nestedClone)
			}
			// check cloned behaviour
			if got := cloned.Matches(tt.testPath); got != tt.expected {
				t.Errorf("CloneForSub().Matches() = %v, want %v (pattern=%q prefix=%q subject=%q)",
					got, tt.expected, tt.pattern, tt.prefix, tt.testPath)
			}

			// optional original-behaviour check
			if tt.origCheck != "" {
				if got := m.Matches(tt.origCheck); got != tt.origCheckOK {
					t.Errorf("original matcher: Matches(%q) = %v, want %v",
						tt.origCheck, got, tt.origCheckOK)
				}
			}
		})
	}
}

// TestRegexpParentMatcher_CloneForSub_DotPrefix tests that CloneForSub("."), called on a
// regexpParentMatcher, returns the same matcher. This is because the dot prefix is treated
// as a no-op and the returned matcher should preserve the original behaviour.
func TestRegexpParentMatcher_CloneForSub_DotPrefix(t *testing.T) {
	m, err := mockfs.NewRegexpMatcher(".*")
	if err != nil {
		t.Fatalf("failed to compile regexp: %v", err)
	}
	pm := m.CloneForSub("dir") // now we have a regexpParentMatcher

	clone := pm.CloneForSub(".") // this should trigger the dot-prefix branch

	if clone != pm {
		t.Errorf("expected CloneForSub(\".\") on regexpParentMatcher to return the same matcher")
	}
}

// TestRegexpMatcher_Concurrent tests concurrent access to RegexpMatcher.
func TestRegexpMatcher_Concurrent(t *testing.T) {
	m, err := mockfs.NewRegexpMatcher("\\.txt$")
	if err != nil {
		t.Fatalf("NewRegexpMatcher() error: %v", err)
	}

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = m.Matches("test.txt")
			}
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestRegexpMatcher_RealWorldPatterns tests regexp matcher with real-world patterns.
func TestRegexpMatcher_RealWorldPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		matches  []string
		nonMatch []string
	}{
		{
			name:     "go files",
			pattern:  "\\.go$",
			matches:  []string{"main.go", "test.go", "dir/file.go"},
			nonMatch: []string{"main.go.bak", "readme.md", "go.mod"},
		},
		{
			name:     "test files",
			pattern:  "_test\\.go$",
			matches:  []string{"main_test.go", "util_test.go"},
			nonMatch: []string{"main.go", "test.txt"},
		},
		{
			name:     "vendor directory",
			pattern:  "^vendor/",
			matches:  []string{"vendor/pkg/file.go", "vendor/mod.go"},
			nonMatch: []string{"src/vendor/file.go", "myvendor/file.go"},
		},
		{
			name:     "hidden files",
			pattern:  "^\\.",
			matches:  []string{".git", ".gitignore", ".env"},
			nonMatch: []string{"file.txt", "dir/.git"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := mockfs.NewRegexpMatcher(tt.pattern)
			if err != nil {
				t.Fatalf("NewRegexpMatcher() error: %v", err)
			}

			for _, path := range tt.matches {
				if !m.Matches(path) {
					t.Errorf("expected %q to match pattern %q", path, tt.pattern)
				}
			}

			for _, path := range tt.nonMatch {
				if m.Matches(path) {
					t.Errorf("expected %q not to match pattern %q", path, tt.pattern)
				}
			}
		})
	}
}

// TestRegexpMatcher_SafeCompilation test regex compilation errors don't panic.
func TestRegexpMatcher_SafeCompilation(t *testing.T) {
	invalidPatterns := []string{
		"[",
		"(",
		"(?P<",
		"(?P<name",
		"*",
		"(?",
	}

	for _, pattern := range invalidPatterns {
		t.Run(pattern, func(t *testing.T) {
			m, err := mockfs.NewRegexpMatcher(pattern)
			if err == nil {
				t.Errorf("expected error for invalid pattern %q", pattern)
			}
			if m != nil {
				t.Errorf("expected nil matcher for invalid pattern %q", pattern)
			}
		})
	}
}

// TestGlobMatcher_Matches tests glob pattern matching.
func TestGlobMatcher_Matches(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
		wantErr  bool
	}{
		{"simple wildcard", "*.txt", "file.txt", true, false},
		{"no match extension", "*.txt", "file.go", false, false},
		{"question mark single", "test?.txt", "test1.txt", true, false},
		{"question mark no match", "test?.txt", "test12.txt", false, false},
		{"character class", "[abc].txt", "a.txt", true, false},
		{"character class no match", "[abc].txt", "d.txt", false, false},
		{"range", "[0-9].txt", "5.txt", true, false},
		{"negated class caret", "[^0-9].txt", "a.txt", true, false},
		{"negated class no match", "[^0-9].txt", "5.txt", false, false},
		{"path component only", "*.txt", "file.txt", true, false},
		{"slash in input no match", "*.txt", "dir/file.txt", false, false},
		{"exact match", "test.txt", "test.txt", true, false},
		{"empty pattern empty path", "", "", true, false},
		{"empty pattern non-empty", "", "anything", false, false},
		{"trailing slash", "dir/", "dir/", true, false},
		{"escape star", "test\\*.txt", "test*.txt", true, false},
		{"invalid pattern unclosed", "[", "", false, true},
		{"invalid pattern escape", "test\\", "", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := mockfs.NewGlobMatcher(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGlobMatcher() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			got := m.Matches(tt.input)
			if got != tt.expected {
				t.Errorf("Matches(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// TestGlobMatcher_CloneForSub tests glob matcher cloning for sub-filesystems.
func TestGlobMatcher_CloneForSub(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		prefix      string
		testPath    string
		expected    bool
		origCheck   string
		origCheckOK bool
	}{
		{
			name:        "component pattern with prefix",
			pattern:     "file.txt",
			prefix:      "subdir",
			testPath:    "file.txt",
			expected:    false, // "subdir/file.txt" doesn't match "file.txt"
			origCheck:   "file.txt",
			origCheckOK: true,
		},
		{
			name:        "exact full path pattern",
			pattern:     "subdir/file.txt",
			prefix:      "subdir",
			testPath:    "file.txt",
			expected:    true,
			origCheck:   "subdir/file.txt",
			origCheckOK: true,
		},
		{
			name:        "empty prefix no-op",
			pattern:     "*.go",
			prefix:      "",
			testPath:    "main.go",
			expected:    true,
			origCheck:   "main.go",
			origCheckOK: true,
		},
		{
			name:        "dot prefix no-op",
			pattern:     "test*",
			prefix:      ".",
			testPath:    "test.txt",
			expected:    true,
			origCheck:   "test.txt",
			origCheckOK: true,
		},
		{
			name:     "dot path matches exact prefix",
			pattern:  "subdir",
			prefix:   "subdir",
			testPath: ".",
			expected: true,
		},
		{
			name:     "empty string matches exact prefix",
			pattern:  "subdir",
			prefix:   "subdir",
			testPath: "",
			expected: true,
		},
		{
			name:        "wildcard pattern no match after prefix",
			pattern:     "*.txt",
			prefix:      "dir",
			testPath:    "file.txt",
			expected:    false, // "dir/file.txt" doesn't match "*.txt"
			origCheck:   "test.txt",
			origCheckOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := mockfs.NewGlobMatcher(tt.pattern)
			if err != nil {
				t.Fatalf("NewGlobMatcher() error: %v", err)
			}

			cloned := m.CloneForSub(tt.prefix)
			got := cloned.Matches(tt.testPath)
			if got != tt.expected {
				t.Errorf("CloneForSub().Matches(%q) = %v, want %v", tt.testPath, got, tt.expected)
			}

			if tt.origCheck != "" {
				if got := m.Matches(tt.origCheck); got != tt.origCheckOK {
					t.Errorf("original Matches(%q) = %v, want %v", tt.origCheck, got, tt.origCheckOK)
				}
			}
		})
	}
}

// TestGlobMatcher_NestedClone tests nested CloneForSub calls.
func TestGlobMatcher_NestedClone(t *testing.T) {
	// path.Match doesn't support multi-component patterns with /,
	// so we test with exact path patterns
	m, err := mockfs.NewGlobMatcher("dir/subdir/file.txt")
	if err != nil {
		t.Fatalf("NewGlobMatcher() error: %v", err)
	}

	cloned1 := m.CloneForSub("dir")
	cloned2 := cloned1.CloneForSub("subdir")

	// Should match when full path is reconstructed
	if !cloned2.Matches("file.txt") {
		t.Error("nested clone should match file.txt (full path: dir/subdir/file.txt)")
	}

	if cloned2.Matches("other.txt") {
		t.Error("nested clone should not match other.txt")
	}
}

// TestGlobMatcher_Concurrent tests concurrent access to glob matcher.
func TestGlobMatcher_Concurrent(t *testing.T) {
	m, err := mockfs.NewGlobMatcher("*.txt")
	if err != nil {
		t.Fatalf("NewGlobMatcher() error: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = m.Matches("test.txt")
				_ = m.CloneForSub("dir")
			}
		}()
	}
	wg.Wait()
}

// TestPathMatcher_PathValidation tests edge cases in path validation.
func TestPathMatcher_PathValidation(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		match bool
	}{
		{"trailing slash", "dir/", false},
		{"double slash", "dir//file", false},
		{"leading dot slash", "./file", false},
		{"parent reference", "../file", false},
		{"multiple dots", "dir/.../file", false},
		{"only dots", "...", false},
		{"absolute unix path", "/absolute/path", true},
		{"backslash", "dir\\file", true},
	}

	exact := mockfs.NewExactMatcher("test")
	wildcard := mockfs.NewWildcardMatcher()
	regexp, _ := mockfs.NewRegexpMatcher(".*")
	glob, _ := mockfs.NewGlobMatcher("*")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchers := []struct {
				name    string
				matcher mockfs.PathMatcher
			}{
				{"exact", exact},
				{"wildcard", wildcard},
				{"regexp", regexp},
				{"glob", glob},
			}

			for _, m := range matchers {
				// Just verify matchers handle these paths without panic
				_ = m.matcher.Matches(tt.path)
			}
		})
	}
}

// TestExactMatcher_EmptyPath tests empty path edge cases.
func TestExactMatcher_EmptyPath(t *testing.T) {
	tests := []struct {
		name     string
		stored   string
		input    string
		expected bool
	}{
		{"both empty", "", "", true},
		{"stored empty input not", "", "test", false},
		{"input empty stored not", "test", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mockfs.NewExactMatcher(tt.stored)
			got := m.Matches(tt.input)
			if got != tt.expected {
				t.Errorf("Matches(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

// TestRegexpMatcher_EmptyPattern tests empty pattern behavior.
func TestRegexpMatcher_EmptyPattern(t *testing.T) {
	m, err := mockfs.NewRegexpMatcher("")
	if err != nil {
		t.Fatalf("NewRegexpMatcher(\"\") error: %v", err)
	}

	tests := []string{"", "anything", "test.txt", "dir/file.go"}
	for _, path := range tests {
		if !m.Matches(path) {
			t.Errorf("empty pattern should match %q", path)
		}
	}
}

// TestGlobParentMatcher_EdgeCases tests globParentMatcher edge cases.
func TestGlobParentMatcher_EdgeCases(t *testing.T) {
	m, err := mockfs.NewGlobMatcher("dir/*.txt")
	if err != nil {
		t.Fatalf("NewGlobMatcher() error: %v", err)
	}

	pm := m.CloneForSub("dir")

	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"simple match", "file.txt", true},
		{"no match", "file.go", false},
		{"dot path", ".", false},
		{"empty path", "", false},
		{"nested no match", "sub/file.txt", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pm.Matches(tt.path)
			if got != tt.expected {
				t.Errorf("Matches(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestWildcardMatcher_Matches(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty string", input: ""},
		{name: "simple file", input: "test.txt"},
		{name: "path with slashes", input: "dir/subdir/file.txt"},
		{name: "absolute path", input: "/absolute/path.txt"},
		{name: "path with spaces", input: "file with spaces.txt"},
		{name: "path with special chars", input: "!@#$%^&*()_+-={}[]|\\:;\"'<>?,./"},
		{name: "long path", input: "very/long/path/with/many/segments/file.txt"},
		{name: "unicode path", input: "файл.txt"},
	}

	m := mockfs.NewWildcardMatcher()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.Matches(tt.input); !got {
				t.Errorf("WildcardMatcher.Matches() = false, want true")
			}
		})
	}
}

func TestWildcardMatcher_CloneForSub(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		testPath string
	}{
		{name: "clone with prefix", prefix: "subdir", testPath: "file.txt"},
		{name: "clone with empty prefix", prefix: "", testPath: "file.txt"},
		{name: "clone with nested prefix", prefix: "dir/subdir", testPath: "test.txt"},
	}

	m := mockfs.NewWildcardMatcher()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloned := m.CloneForSub(tt.prefix)
			if got := cloned.Matches(tt.testPath); !got {
				t.Errorf("CloneForSub().Matches() = false, want true")
			}
			// ensure original still works
			if got := m.Matches(tt.testPath); !got {
				t.Errorf("original matcher affected by clone")
			}
		})
	}
}

// TestWildcardMatcher_EdgeCases tests wildcard matcher with unusual inputs.
func TestWildcardMatcher_EdgeCases(t *testing.T) {
	m := mockfs.NewWildcardMatcher()

	tests := []string{
		"",
		".",
		"..",
		"...",
		"/",
		"//",
		"null\x00byte",
		string([]byte{0xff, 0xfe, 0xfd}),
	}

	for _, path := range tests {
		t.Run(fmt.Sprintf("path=%q", path), func(t *testing.T) {
			if !m.Matches(path) {
				t.Errorf("WildcardMatcher should match %q", path)
			}
		})
	}
}

// TestPathMatcher_CloneIndependence tests that matchers work correctly after cloning.
func TestPathMatcher_CloneIndependence(t *testing.T) {
	// test ExactMatcher: original stores parent path, cloned should match relative path
	t.Run("exact matcher", func(t *testing.T) {
		original := mockfs.NewExactMatcher("dir/file.txt")
		cloned := original.CloneForSub("dir")

		if !original.Matches("dir/file.txt") {
			t.Error("original should match 'dir/file.txt'")
		}
		if original.Matches("file.txt") {
			t.Error("original should not match 'file.txt'")
		}
		if cloned.Matches("dir/file.txt") {
			t.Error("cloned should not match 'dir/file.txt'")
		}
		if !cloned.Matches("file.txt") {
			t.Error("cloned should match 'file.txt'")
		}
	})

	// test RegexpMatcher: ensure original and clone behave consistently
	t.Run("regexp matcher", func(t *testing.T) {
		original, _ := mockfs.NewRegexpMatcher("\\.txt$")
		cloned := original.CloneForSub("dir")

		if !original.Matches("test.txt") {
			t.Error("original should match 'test.txt'")
		}
		// cloned matches when composed parent path "dir/test.txt" matches pattern
		if !cloned.Matches("test.txt") {
			t.Error("cloned should match 'test.txt' when parent path matches")
		}
	})

	// test WildcardMatcher
	t.Run("wildcard matcher", func(t *testing.T) {
		original := mockfs.NewWildcardMatcher()
		cloned := original.CloneForSub("dir")

		if !original.Matches("anything") {
			t.Error("original should match anything")
		}
		if !cloned.Matches("anything") {
			t.Error("cloned should match anything")
		}
	})
}
func TestNoneMatcher_CloneForSub(t *testing.T) {
	// Get a noneMatcher by cloning an ExactMatcher outside the prefix
	em := mockfs.NewExactMatcher("foo")
	nm := em.CloneForSub("bar") // foo is outside bar, returns noneMatcher

	// sanity check
	if nm.Matches("foo") {
		t.Errorf("expected noneMatcher to never match")
	}

	// now call CloneForSub on the noneMatcher itself
	nm2 := nm.CloneForSub("baz")

	// must still be a noneMatcher and never match
	if nm2.Matches("anything.txt") {
		t.Errorf("expected noneMatcher.CloneForSub result to never match")
	}
}

func TestGlobParentMatcher_CloneForSub_EmptyPrefix(t *testing.T) {
	m, err := mockfs.NewGlobMatcher("dir/*.txt")
	if err != nil {
		t.Fatalf("NewGlobMatcher() error: %v", err)
	}
	pm := m.CloneForSub("dir")

	// Clone with empty prefix should return same matcher
	cloned := pm.CloneForSub("")
	if cloned != pm {
		t.Error("CloneForSub(\"\") should return same matcher")
	}

	// Clone with dot prefix should return same matcher
	cloned = pm.CloneForSub(".")
	if cloned != pm {
		t.Error("CloneForSub(\".\") should return same matcher")
	}
}

// BenchmarkMatchers benchmarks the performance of different matchers on a given path.
func BenchmarkMatchers(b *testing.B) {
	path := "dir/subdir/file.txt"
	mExact := mockfs.NewExactMatcher("dir/subdir/file.txt")
	mRegexp, _ := mockfs.NewRegexpMatcher("^dir/.*/.*\\.txt$")
	mGlob, _ := mockfs.NewGlobMatcher("dir/subdir/*.txt")
	mWildcard := mockfs.NewWildcardMatcher()

	b.Run("ExactMatcher", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = mExact.Matches(path)
		}
	})

	b.Run("RegexpMatcher", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = mRegexp.Matches(path)
		}
	})

	b.Run("WildcardMatcher", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = mWildcard.Matches(path)
		}
	})

	b.Run("GlobMatcher", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = mGlob.Matches(path)
		}
	})
}

// BenchmarkCloneForSub benchmarks the performance of CloneForSub method on different matchers.
// It measures how long it takes to call CloneForSub() on each matcher type with a given prefix.
func BenchmarkCloneForSub(b *testing.B) {
	mExact := mockfs.NewExactMatcher("file.txt")
	mRegexp, _ := mockfs.NewRegexpMatcher("^dir/.*/.*\\.txt$")
	mWildcard := mockfs.NewWildcardMatcher()

	prefix := "dir/subdir"

	b.Run("ExactMatcher", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = mExact.CloneForSub(prefix)
		}
	})

	b.Run("RegexpMatcher", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = mRegexp.CloneForSub(prefix)
		}
	})

	b.Run("WildcardMatcher", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = mWildcard.CloneForSub(prefix)
		}
	})
}

// BenchmarkRegexpMatcher_Compile benchmarks the performance of compiling
// a regular expression matcher with a given pattern.
func BenchmarkRegexpMatcher_Compile(b *testing.B) {
	pattern := "^dir/.*/.*\\.txt$"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mockfs.NewRegexpMatcher(pattern)
	}
}
