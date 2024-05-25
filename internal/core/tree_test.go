package core

import "testing"

func TestPathRelativeToSubmoduleRoot(t *testing.T) {
	cases := []struct {
		full  string
		depth int
		want  string
	}{
		// Top-level tree IS the submodule: nothing to strip.
		{"file.c", 0, "file.c"},
		{"a/b/c", 0, "a/b/c"},

		// Submodule rooted one level deep.
		{"a/b/c/d", 1, "b/c/d"},
		{"vendor/file.c", 1, "file.c"},

		// Two-level submodule path (the common case for vendor/foo/...).
		{"vendor/foo/inner/file.c", 2, "inner/file.c"},
		{"vendor/foo/file.c", 2, "file.c"},

		// Three-level.
		{"a/b/c/d/e", 3, "d/e"},

		// Defensive: depth equal to segment count returns the last segment
		// alone (this matches the historical SplitN behavior).
		{"a/b/c", 2, "c"},

		// Negative / zero depth is a no-op (paranoia — current callers
		// always pass non-negative depths).
		{"a/b/c", -1, "a/b/c"},
	}
	for _, c := range cases {
		got := pathRelativeToSubmoduleRoot(c.full, c.depth)
		if got != c.want {
			t.Errorf("pathRelativeToSubmoduleRoot(%q, %d) = %q; want %q",
				c.full, c.depth, got, c.want)
		}
	}
}
