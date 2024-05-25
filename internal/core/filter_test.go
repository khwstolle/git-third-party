package core

import (
	"strings"
	"testing"
)

func segs(p string) []string {
	if p == "" {
		return []string{""}
	}
	return strings.Split(p, "/")
}

func TestCompileFilterBasic(t *testing.T) {
	fn, err := compileFilter("*.c", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if fn(false, segs("foo.c")) != FilterTrue {
		t.Error("foo.c should match *.c")
	}
	if fn(false, segs("a/foo.c")) != FilterTrue {
		t.Error("a/foo.c should match *.c (relative pattern)")
	}
	if fn(false, segs("foo.h")) != FilterFalse {
		t.Error("foo.h should not match *.c")
	}
}

func TestCompileFilterAbsolute(t *testing.T) {
	fn, err := compileFilter("/src/foo.c", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if fn(false, segs("src/foo.c")) != FilterTrue {
		t.Error("src/foo.c should match /src/foo.c")
	}
	if fn(false, segs("foo.c")) != FilterFalse {
		t.Error("foo.c should not match /src/foo.c")
	}
	if fn(false, segs("a/src/foo.c")) != FilterFalse {
		t.Error("a/src/foo.c should not match /src/foo.c")
	}
}

func TestCompileFilterTreeOnly(t *testing.T) {
	fn, err := compileFilter("tests/", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if fn(true, segs("tests")) != FilterTrue {
		t.Error("tests dir should match tests/")
	}
	if fn(false, segs("tests")) != FilterFalse {
		t.Error("tests file should not match tests/")
	}
	if fn(true, segs("a/tests")) != FilterTrue {
		t.Error("a/tests dir should match tests/")
	}
}

func TestCompileFilterDoubleStar(t *testing.T) {
	fn, err := compileFilter("a/**/b", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"a/b", "a/x/b", "a/x/y/b"} {
		if fn(false, segs(p)) != FilterTrue {
			t.Errorf("%s should match a/**/b", p)
		}
	}
	for _, p := range []string{"b", "x/a/b"} {
		if fn(false, segs(p)) != FilterFalse {
			t.Errorf("%s should not match a/**/b", p)
		}
	}
}

func TestCompileFilterPrefixMaybe(t *testing.T) {
	fn, err := compileFilter("/src/*.c", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := fn(true, segs("src")); got != FilterMaybe {
		t.Errorf("src tree: got %v, want maybe", got)
	}
	if got := fn(true, segs("other")); got != FilterFalse {
		t.Errorf("other tree: got %v, want false", got)
	}
}

func TestCompileFilterRejectsBad(t *testing.T) {
	bad := []string{"", "//", "/", "**", "**/", "a/**", "a/./b", "a/../b"}
	for _, p := range bad {
		if _, err := compileFilter(p, "", false); err == nil {
			t.Errorf("expected error for %q", p)
		}
	}
}

func TestFnmatch(t *testing.T) {
	cases := []struct {
		name, pat string
		want      bool
	}{
		{"foo.c", "*.c", true},
		{"foo.h", "*.c", false},
		{"abc", "a?c", true},
		{"abbc", "a?c", false},
		{"abc", "[abc]bc", true},
		{"dbc", "[abc]bc", false},
		{"dbc", "[!abc]bc", true},
		{"abc", "abc", true},
		{"a", "*", true},
		{"", "*", true},
	}
	for _, c := range cases {
		got := fnmatch(c.name, c.pat)
		if got != c.want {
			t.Errorf("fnmatch(%q, %q) = %v; want %v", c.name, c.pat, got, c.want)
		}
	}
}
