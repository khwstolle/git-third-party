package core

import (
	"reflect"
	"testing"
)

// These tests originally shelled out to Python's shlex/fnmatch via the
// historical git-third-party.py reference implementation to validate that
// the Go port matched it bit-for-bit. With the Python source removed, the
// expected outputs below are the captured Python results from the moment of
// the cutover — i.e. the Go implementation is now compared against frozen
// Python behavior.
//
// If a future change intentionally diverges from Python semantics, update
// the Want fields here and call out the divergence in the commit message.

type compileFilterCase struct {
	Pattern     string
	ReturnMaybe bool
	IsTree      bool
	Segments    []string
	Want        FilterResult // captured from Python's compile_filter
}

func TestParityCompileFilterFrozen(t *testing.T) {
	cases := []compileFilterCase{
		{Pattern: "*.c", IsTree: false, Segments: []string{"foo.c"}, Want: FilterTrue},
		{Pattern: "*.c", IsTree: false, Segments: []string{"a", "foo.c"}, Want: FilterTrue},
		{Pattern: "*.c", IsTree: false, Segments: []string{"foo.h"}, Want: FilterFalse},
		{Pattern: "/src/foo.c", IsTree: false, Segments: []string{"src", "foo.c"}, Want: FilterTrue},
		{Pattern: "/src/foo.c", IsTree: false, Segments: []string{"a", "src", "foo.c"}, Want: FilterFalse},
		{Pattern: "/src/*.c", ReturnMaybe: true, IsTree: true, Segments: []string{"src"}, Want: FilterMaybe},
		{Pattern: "/src/*.c", ReturnMaybe: true, IsTree: true, Segments: []string{"other"}, Want: FilterFalse},
		{Pattern: "tests/", IsTree: true, Segments: []string{"tests"}, Want: FilterTrue},
		{Pattern: "tests/", IsTree: false, Segments: []string{"tests"}, Want: FilterFalse},
		{Pattern: "a/**/b", IsTree: false, Segments: []string{"a", "b"}, Want: FilterTrue},
		{Pattern: "a/**/b", IsTree: false, Segments: []string{"a", "x", "b"}, Want: FilterTrue},
		{Pattern: "a/**/b", IsTree: false, Segments: []string{"a", "x", "y", "b"}, Want: FilterTrue},
		{Pattern: "a/**/b", IsTree: false, Segments: []string{"x", "a", "b"}, Want: FilterFalse},
		{Pattern: "[abc]bc", IsTree: false, Segments: []string{"abc"}, Want: FilterTrue},
		{Pattern: "[!abc]bc", IsTree: false, Segments: []string{"abc"}, Want: FilterFalse},
		{Pattern: "**/foo", IsTree: false, Segments: []string{"a", "b", "foo"}, Want: FilterTrue},
	}
	for i, c := range cases {
		fn, err := compileFilter(c.Pattern, "test", c.ReturnMaybe)
		if err != nil {
			t.Errorf("[%d] compileFilter(%q) error: %v", i, c.Pattern, err)
			continue
		}
		got := fn(c.IsTree, c.Segments)
		if got != c.Want {
			t.Errorf("[%d] pattern=%q segments=%v isTree=%v maybe=%v: got %v, want %v",
				i, c.Pattern, c.Segments, c.IsTree, c.ReturnMaybe, got, c.Want)
		}
	}
}

func TestParityShlexQuoteFrozen(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "''"},
		{"abc", "abc"},
		{"a b", "'a b'"},
		{"can't", `'can'"'"'t'`},
		{"*.c", "'*.c'"},
		{"$x", "'$x'"},
		{"#y", "'#y'"},
		{"a@b.com", "a@b.com"},
		{"a/b/c", "a/b/c"},
		{"a-b_c.d", "a-b_c.d"},
		{"with\nnewline", "'with\nnewline'"},
		{"\\backslash", "'\\backslash'"},
		{"a'b\"c", `'a'"'"'b"c'`},
	}
	for _, c := range cases {
		got := shlexQuote(c.in)
		if got != c.want {
			t.Errorf("shlexQuote(%q) = %q; want %q (Python-frozen)", c.in, got, c.want)
		}
	}
}

func TestParityShlexSplitFrozen(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"a b c", []string{"a", "b", "c"}},
		{"'a b' c", []string{"a b", "c"}},
		{`"a b" c`, []string{"a b", "c"}},
		{`'can'"'"'t'`, []string{"can't"}},
		{`a\ b`, []string{"a b"}},
		{"  ", []string{}},
		{"", []string{}},
		{"abc", []string{"abc"}},
	}
	for _, c := range cases {
		got, err := shlexSplit(c.in)
		if err != nil {
			t.Errorf("shlexSplit(%q) error: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("shlexSplit(%q) = %v; want %v (Python-frozen)", c.in, got, c.want)
		}
	}
}
