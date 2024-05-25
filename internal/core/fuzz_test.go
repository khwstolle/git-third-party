package core

import (
	"context"
	"testing"
)

func FuzzShlexSplit(f *testing.F) {
	for _, seed := range []string{
		"",
		"a b c",
		"'quoted value'",
		`"double quoted"`,
		"backslash\\ escape",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = shlexSplit(s)
	})
}

func FuzzCanonicalizeRelativePath(f *testing.F) {
	for _, seed := range []string{
		"",
		"a/b/c",
		"./a/b",
		"../outside",
		"/absolute",
		"trailing/",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, _ = canonicalizeRelativePath(s, "fuzz")
	})
}

func FuzzActualDirToPathInRepo(f *testing.F) {
	for _, seed := range []string{
		"",
		"vendor/x",
		"./vendor/x",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// This one needs a real repo root to succeed, but fuzzing should
		// exercise the path manipulation logic even when it returns err.
		app := NewApp(nil, nil)
		ctx := context.Background()
		_, _ = app.actualDirToPathInRepo(ctx, GlobalOptions{}, s, "fuzz")
	})
}

func FuzzFnmatch(f *testing.F) {
	f.Add("*.c", "foo.c")
	f.Add("v[12]/", "v1/x")
	f.Add("**/*.h", "a/b/c.h")
	f.Fuzz(func(t *testing.T, pat, s string) {
		_ = fnmatch(pat, s)
	})
}

func FuzzReadNewConfig(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte(""),
		[]byte("[[third_party]]\ndir = \"x\"\nurl = \"y\"\nfollow = \"main\"\n"),
		[]byte("garbage = ?\n"),
		[]byte("[[third_party]]\ndir = \"\"\nurl = \"\"\n"),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		app := NewApp(nil, nil)
		ctx := context.Background()
		_, _ = app.readNewConfig(ctx, GlobalOptions{}, data, "fuzz.toml")
	})
}
