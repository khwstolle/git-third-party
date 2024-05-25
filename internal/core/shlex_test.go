package core

import (
	"reflect"
	"testing"
)

func TestShlexQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "''"},
		{"abc", "abc"},
		{"abc.def", "abc.def"},
		{"a/b_c-d", "a/b_c-d"},
		{"hello world", "'hello world'"},
		{"can't", `'can'"'"'t'`},
		{"$x", "'$x'"},
		{"#comment", "'#comment'"},
	}
	for _, c := range cases {
		got := shlexQuote(c.in)
		if got != c.want {
			t.Errorf("shlexQuote(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}

func TestShlexSplit(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		err  bool
	}{
		{"", []string{}, false},
		{"  ", []string{}, false},
		{"abc", []string{"abc"}, false},
		{"a b c", []string{"a", "b", "c"}, false},
		{"'a b' c", []string{"a b", "c"}, false},
		{`"a b" c`, []string{"a b", "c"}, false},
		{`'can'"'"'t'`, []string{"can't"}, false},
		{`a\ b`, []string{"a b"}, false},
		{`'unterminated`, nil, true},
	}
	for _, c := range cases {
		got, err := shlexSplit(c.in)
		if c.err {
			if err == nil {
				t.Errorf("shlexSplit(%q) = no error; want error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("shlexSplit(%q) error: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("shlexSplit(%q) = %#v; want %#v", c.in, got, c.want)
		}
	}
}
