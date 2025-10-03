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
	// The returned matcher should match the relative path inside the sub filesystem.
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
// It converts a parent-path matcher into a matcher that matches the relative path
// inside the sub-file system. If the original path is not under prefix, it returns
// a matcher that never matches.
func (m *ExactMatcher) CloneForSub(prefix string) PathMatcher {
	if prefix == "" || prefix == "." {
		return &ExactMatcher{path: m.path}
	}

	prefix = strings.TrimSuffix(prefix, "/")

	// Exact directory match
	if m.path == prefix {
		return &ExactMatcher{path: "."}
	}

	// Match inside the subtree
	if rel := strings.TrimPrefix(m.path, prefix+"/"); rel != m.path {
		return &ExactMatcher{path: rel}
	}

	// Outside subtree
	return &noneMatcher{}
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
// The returned matcher will test the original regexp against the full parent path
// assembled from the prefix and the candidate path inside the sub filesystem.
func (m *RegexpMatcher) CloneForSub(prefix string) PathMatcher {
	if prefix == "" || prefix == "." {
		return m
	}
	return &regexpParentMatcher{re: m.re, prefix: prefix}
}

// regexpParentMatcher matches by assembling prefix + "/" + candidatePath,
// then applying the original regexp against that assembled string.
type regexpParentMatcher struct {
	re     *regexp.Regexp
	prefix string // normalized, without trailing slash
}

func (r *regexpParentMatcher) Matches(path string) bool {
	// If the candidate path represents the directory root inside the sub-FS,
	// treat it as the prefix itself (equivalent to ".")
	if path == "." || path == "" {
		return r.re.MatchString(r.prefix)
	}
	// Compose parent path and test original regexp.
	parentPath := r.prefix + "/" + path
	return r.re.MatchString(parentPath)
}

func (r *regexpParentMatcher) CloneForSub(prefix string) PathMatcher {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" || prefix == "." {
		return r
	}
	// Compose prefixes: existingPrefix + "/" + newPrefix
	combined := r.prefix + "/" + prefix
	return &regexpParentMatcher{re: r.re, prefix: combined}
}

// WildcardMatcher matches all paths.
// It is equivalent to a glob pattern "*".
// Use this when you want an error rule to apply universally.
type WildcardMatcher struct{}

// NewWildcardMatcher creates a matcher that matches all paths.
func NewWildcardMatcher() *WildcardMatcher {
	return &WildcardMatcher{}
}

// Matches returns true for all paths.
func (m *WildcardMatcher) Matches(path string) bool {
	return true
}

// CloneForSub essentially returns the same matcher (used by SubFS).
func (m *WildcardMatcher) CloneForSub(prefix string) PathMatcher {
	return m
}

// noneMatcher never matches anything (used when a parent matcher does not apply inside a sub-tree).
type noneMatcher struct{}

// Matches returns false for all paths.
func (n *noneMatcher) Matches(_ string) bool {
	return false
}

// CloneForSub essentially returns the same matcher (used by SubFS).
func (n *noneMatcher) CloneForSub(_ string) PathMatcher {
	return n
}
