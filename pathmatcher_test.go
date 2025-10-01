package mockfs

import (
	"testing"
)

func TestPathMatcher_Interface(t *testing.T) {
	// verify all types implement PathMatcher interface
	var _ PathMatcher = (*ExactMatcher)(nil)
	var _ PathMatcher = (*RegexpMatcher)(nil)
	var _ PathMatcher = (*WildcardMatcher)(nil)

	// test that interface methods work polymorphically
	matchers := []PathMatcher{
		NewExactMatcher("test.txt"),
		&WildcardMatcher{},
	}

	m, err := NewRegexpMatcher("\\.txt$")
	if err != nil {
		t.Fatalf("failed to create regexp matcher: %v", err)
	}
	matchers = append(matchers, m)

	for i, matcher := range matchers {
		// should not panic
		_ = matcher.Matches("test.txt")
		_ = matcher.CloneForSub("prefix")
		t.Logf("matcher %d works as PathMatcher interface", i)
	}
}

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
			name:     "path with leading slash",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewExactMatcher(tt.stored)
			if got := m.Matches(tt.input); got != tt.expected {
				t.Errorf("ExactMatcher.Matches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExactMatcher_CloneForSub(t *testing.T) {
	tests := []struct {
		name     string
		stored   string
		prefix   string
		testPath string
		expected bool
	}{
		{
			name:     "clone with prefix",
			stored:   "file.txt",
			prefix:   "subdir",
			testPath: "subdir/file.txt",
			expected: true,
		},
		{
			name:     "clone with prefix ending slash",
			stored:   "file.txt",
			prefix:   "subdir/",
			testPath: "subdir/file.txt",
			expected: true,
		},
		{
			name:     "clone with path leading slash",
			stored:   "/file.txt",
			prefix:   "subdir",
			testPath: "subdir/file.txt",
			expected: true,
		},
		{
			name:     "clone with empty prefix",
			stored:   "file.txt",
			prefix:   "",
			testPath: "file.txt",
			expected: true,
		},
		{
			name:     "clone with nested prefix",
			stored:   "file.txt",
			prefix:   "dir/subdir",
			testPath: "dir/subdir/file.txt",
			expected: true,
		},
		{
			name:     "clone with nested path",
			stored:   "sub/file.txt",
			prefix:   "dir",
			testPath: "dir/sub/file.txt",
			expected: true,
		},
		{
			name:     "clone does not match original",
			stored:   "file.txt",
			prefix:   "subdir",
			testPath: "file.txt",
			expected: false,
		},
		{
			name:     "clone with both slashes",
			stored:   "/file.txt",
			prefix:   "subdir/",
			testPath: "subdir/file.txt",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewExactMatcher(tt.stored)
			cloned := m.CloneForSub(tt.prefix)
			if got := cloned.Matches(tt.testPath); got != tt.expected {
				t.Errorf("CloneForSub().Matches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestExactMatcher_Concurrent tests concurrent access to ExactMatcher.
func TestExactMatcher_Concurrent(t *testing.T) {
	m := NewExactMatcher("test.txt")

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

	m := NewExactMatcher(longPath)
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
			got, err := NewRegexpMatcher(tt.pattern)
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
			m, err := NewRegexpMatcher(tt.pattern)
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
		name     string
		pattern  string
		prefix   string
		testPath string
		expected bool
	}{
		{
			name:     "clone preserves pattern",
			pattern:  "\\.txt$",
			prefix:   "subdir",
			testPath: "test.txt",
			expected: true,
		},
		{
			name:     "clone ignores prefix",
			pattern:  "^test",
			prefix:   "subdir",
			testPath: "test.txt",
			expected: true,
		},
		{
			name:     "clone with empty prefix",
			pattern:  "file",
			prefix:   "",
			testPath: "myfile.txt",
			expected: true,
		},
		{
			name:     "clone does not modify original",
			pattern:  "^exact$",
			prefix:   "dir",
			testPath: "exact",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewRegexpMatcher(tt.pattern)
			if err != nil {
				t.Fatalf("NewRegexpMatcher() unexpected error: %v", err)
			}
			cloned := m.CloneForSub(tt.prefix)
			if got := cloned.Matches(tt.testPath); got != tt.expected {
				t.Errorf("CloneForSub().Matches() = %v, want %v", got, tt.expected)
			}
			// ensure original still works
			if got := m.Matches(tt.testPath); got != tt.expected {
				t.Errorf("original matcher affected by clone: got %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestRegexpMatcher_Concurrent tests concurrent access to RegexpMatcher.
func TestRegexpMatcher_Concurrent(t *testing.T) {
	m, err := NewRegexpMatcher("\\.txt$")
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
			m, err := NewRegexpMatcher(tt.pattern)
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

// TeTestRegexpMatcher_SafeCompilation test regex compilation errors don't panic.
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
			m, err := NewRegexpMatcher(pattern)
			if err == nil {
				t.Errorf("expected error for invalid pattern %q", pattern)
			}
			if m != nil {
				t.Errorf("expected nil matcher for invalid pattern %q", pattern)
			}
		})
	}
}

// Test that [regexp.Regexp] pointer is not nil after creation.
func TestRegexpMatcher_NonNilRegexp(t *testing.T) {
	m, err := NewRegexpMatcher("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.re == nil {
		t.Error("regexp should not be nil")
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

	m := NewWildcardMatcher()
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

	m := NewWildcardMatcher()
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

func Test_cleanJoin(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		path     string
		expected string
	}{
		{
			name:     "both non-empty",
			prefix:   "dir",
			path:     "file.txt",
			expected: "dir/file.txt",
		},
		{
			name:     "prefix with trailing slash",
			prefix:   "dir/",
			path:     "file.txt",
			expected: "dir/file.txt",
		},
		{
			name:     "path with leading slash",
			prefix:   "dir",
			path:     "/file.txt",
			expected: "dir/file.txt",
		},
		{
			name:     "both with slashes",
			prefix:   "dir/",
			path:     "/file.txt",
			expected: "dir/file.txt",
		},
		{
			name:     "empty prefix",
			prefix:   "",
			path:     "file.txt",
			expected: "file.txt",
		},
		{
			name:     "empty path",
			prefix:   "dir",
			path:     "",
			expected: "dir",
		},
		{
			name:     "both empty",
			prefix:   "",
			path:     "",
			expected: "",
		},
		{
			name:     "nested prefix",
			prefix:   "dir/subdir",
			path:     "file.txt",
			expected: "dir/subdir/file.txt",
		},
		{
			name:     "nested path",
			prefix:   "dir",
			path:     "subdir/file.txt",
			expected: "dir/subdir/file.txt",
		},
		{
			name:     "nested both",
			prefix:   "dir/sub",
			path:     "another/file.txt",
			expected: "dir/sub/another/file.txt",
		},
		{
			name:     "multiple leading slashes in path",
			prefix:   "dir",
			path:     "//file.txt",
			expected: "dir//file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanJoin(tt.prefix, tt.path)
			if got != tt.expected {
				t.Errorf("cleanJoin() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestCleanJoin_MultipleSlashes tests that cleanJoin handles edge cases with multiple slashes.
func Test_cleanJoin_MultipleSlashes(t *testing.T) {
	tests := []struct {
		prefix   string
		path     string
		expected string
	}{
		{"dir/", "/file.txt", "dir/file.txt"},
		{"dir//", "file.txt", "dir//file.txt"},
		{"dir", "//file.txt", "dir//file.txt"},
	}

	for _, tt := range tests {
		got := cleanJoin(tt.prefix, tt.path)
		if got != tt.expected {
			t.Errorf("cleanJoin(%q, %q) = %q, want %q", tt.prefix, tt.path, got, tt.expected)
		}
	}
}

// TestCloneIndependence tests that matchers work correctly after cloning.
func TestCloneIndependence(t *testing.T) {
	// test ExactMatcher
	t.Run("exact matcher", func(t *testing.T) {
		original := NewExactMatcher("file.txt")
		cloned := original.CloneForSub("dir")

		if !original.Matches("file.txt") {
			t.Error("original should match 'file.txt'")
		}
		if original.Matches("dir/file.txt") {
			t.Error("original should not match 'dir/file.txt'")
		}
		if cloned.Matches("file.txt") {
			t.Error("cloned should not match 'file.txt'")
		}
		if !cloned.Matches("dir/file.txt") {
			t.Error("cloned should match 'dir/file.txt'")
		}
	})

	// test RegexpMatcher
	t.Run("regexp matcher", func(t *testing.T) {
		original, _ := NewRegexpMatcher("\\.txt$")
		cloned := original.CloneForSub("dir")

		if !original.Matches("test.txt") {
			t.Error("original should match 'test.txt'")
		}
		if !cloned.Matches("test.txt") {
			t.Error("cloned should match 'test.txt'")
		}
	})

	// test WildcardMatcher
	t.Run("wildcard matcher", func(t *testing.T) {
		original := NewWildcardMatcher()
		cloned := original.CloneForSub("dir")

		if !original.Matches("anything") {
			t.Error("original should match anything")
		}
		if !cloned.Matches("anything") {
			t.Error("cloned should match anything")
		}
	})
}

// BenchmarkMatchers benchmarks the performance of different matchers on a given path.
func BenchmarkMatchers(b *testing.B) {
	path := "dir/subdir/file.txt"
	mExact := NewExactMatcher("dir/subdir/file.txt")
	mRegexp, _ := NewRegexpMatcher("^dir/.*/.*\\.txt$")
	mWildcard := NewWildcardMatcher()

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
}

// BenchmarkCloneForSub benchmarks the performance of CloneForSub method on different matchers.
// It measures how long it takes to call CloneForSub() on each matcher type with a given prefix.
func BenchmarkCloneForSub(b *testing.B) {
	mExact := NewExactMatcher("file.txt")
	mRegexp, _ := NewRegexpMatcher("^dir/.*/.*\\.txt$")
	mWildcard := NewWildcardMatcher()

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
		_, _ = NewRegexpMatcher(pattern)
	}
}

// Benchmark_cleanJoin benchmarks the performance of cleanJoin with various
// input combinations, with and without leading and trailing slashes.
func Benchmark_cleanJoin(b *testing.B) {
	prefix := "dir/subdir"
	prefixWithSlash := prefix + "/"
	path := "another/file.txt"
	pathWithSlash := "/" + path

	b.Run("no slashes", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = cleanJoin(prefix, path)
		}
	})
	b.Run("leading slash in path", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = cleanJoin(prefix, pathWithSlash)
		}
	})
	b.Run("trailing slash in prefix", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = cleanJoin(prefixWithSlash, path)
		}
	})
	b.Run("both with slashes", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = cleanJoin(prefixWithSlash, pathWithSlash)
		}
	})
}
