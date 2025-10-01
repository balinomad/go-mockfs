package mockfs

import (
	"regexp"
	"strings"
)

// PathMatcher matches a path against a set of rules.
type PathMatcher interface {
	// Matches returns true if the path matches the matcher.
	Matches(path string) bool

	// CloneForSub returns a matcher adjusted for a sub-namespace (used by SubFS).
	CloneForSub(prefix string) PathMatcher
}

// ExactMatcher matches a single path exactly.
type ExactMatcher struct {
	path string
}

// NewExactMatcher creates a matcher for a single path.
func NewExactMatcher(path string) *ExactMatcher {
	return &ExactMatcher{path: path}
}

// Matches returns true if the path exactly matches the stored path.
func (m *ExactMatcher) Matches(path string) bool {
	return path == m.path
}

// CloneForSub returns a matcher adjusted for a sub-namespace (used by SubFS).
func (m *ExactMatcher) CloneForSub(prefix string) PathMatcher {
	return &ExactMatcher{path: cleanJoin(prefix, m.path)}
}

// RegexpMatcher matches a path against a regular expression.
type RegexpMatcher struct {
	re *regexp.Regexp
}

// NewRegexpMatcher creates a matcher for a regular expression.
func NewRegexpMatcher(pattern string) (*RegexpMatcher, error) {
	r, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &RegexpMatcher{re: r}, nil
}

// Matches returns true if the path matches the regular expression.
func (m *RegexpMatcher) Matches(path string) bool {
	return m.re.MatchString(path)
}

// CloneForSub returns a matcher adjusted for a sub-namespace (used by SubFS).
func (m *RegexpMatcher) CloneForSub(prefix string) PathMatcher {
	return &RegexpMatcher{re: m.re}
}

// WildcardMatcher matches all paths.
type WildcardMatcher struct{}

// NewWildcardMatcher creates a matcher that matches all paths.
func NewWildcardMatcher() *WildcardMatcher {
	return &WildcardMatcher{}
}

// Matches returns true for all paths.
func (m *WildcardMatcher) Matches(path string) bool {
	return true
}

// CloneForSub returns a matcher adjusted for a sub-namespace (used by SubFS).
func (m *WildcardMatcher) CloneForSub(prefix string) PathMatcher {
	return m
}

// cleanJoin is a helper for sub path joins.
func cleanJoin(prefix string, p string) string {
	if prefix == "" {
		return p
	}

	if p == "" {
		return prefix
	}

	// ensure prefix ends with /
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	// strip leading / from p
	p = strings.TrimPrefix(p, "/")

	return prefix + p
}
