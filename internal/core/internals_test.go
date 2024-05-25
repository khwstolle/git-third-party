package core

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestInternalsRepoRootCaching(t *testing.T) {
	host := makeRepo(t)

	app := NewApp(nil, nil)
	app.WorkDir = host
	ctx := context.Background()
	gopt := GlobalOptions{}

	root1, err := app.getRepoRoot(ctx, gopt)
	if err != nil {
		t.Fatal(err)
	}

	// Rename the repo dir. The cached value should still be the old one.
	newPath := host + "-moved"
	if err := os.Rename(host, newPath); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Rename(newPath, host) }()

	root2, err := app.getRepoRoot(ctx, gopt)
	if err != nil {
		t.Fatal(err)
	}
	if root1 != root2 {
		t.Errorf("repo root was not cached: %q != %q", root1, root2)
	}

	app = NewApp(nil, nil)
	// Now it should fail or return the new path if we chdir.
	prev, _ := os.Getwd()
	_ = os.Chdir(newPath)
	defer func() { _ = os.Chdir(prev) }()

	root3, err := app.getRepoRoot(ctx, gopt)
	if err != nil {
		t.Fatal(err)
	}
	if root3 == root1 {
		t.Errorf("reset did not clear cache")
	}
}

func TestInternalsGitErrorFormatting(t *testing.T) {
	app := NewApp(nil, nil)
	ctx := context.Background()
	gopt := GlobalOptions{}
	// Use a command that definitely fails and doesn't just print help.
	_, err := app.git(ctx, gopt, []string{"rev-parse", "deadbeefnonexistent"}, modeSingleLine, gitOpts{})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "git rev-parse deadbeefnonexistent") {
		t.Errorf("error message wrong: %q", msg)
	}
}

func TestInternalsGitExitCode(t *testing.T) {
	app := NewApp(nil, nil)
	ctx := context.Background()
	gopt := GlobalOptions{}
	_, err := app.git(ctx, gopt, []string{"rev-parse", "deadbeefnonexistent"}, modeSingleLine, gitOpts{})
	code := gitExitCode(err)
	if code <= 0 {
		t.Errorf("expected positive exit code for failed command; got %d", code)
	}
}

func TestInternalsGitResultTypes(t *testing.T) {
	host := makeRepo(t)
	prev, _ := os.Getwd()
	_ = os.Chdir(host)
	defer func() { _ = os.Chdir(prev) }()

	app := NewApp(nil, nil)
	ctx := context.Background()
	gopt := GlobalOptions{}

	// single line
	r, err := app.git(ctx, gopt, []string{"rev-parse", "--show-toplevel"}, modeSingleLine, gitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if r.line == "" || len(r.lines) > 0 {
		t.Errorf("expected line only; got %+v", r)
	}

	// null terminated
	r, err = app.git(ctx, gopt, []string{"status", "-z"}, modeNullTerminatedLines, gitOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if r.line != "" || r.lines == nil {
		t.Errorf("expected lines; got %+v", r)
	}
}

func TestInternalsCanonicalizeRelativePath(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"vendor/x", "vendor/x", false},
		{"./vendor/x", "vendor/x", false},
		{"vendor//x", "vendor/x", false},
		{"/abs", "", true},
		{"../outside", "", true},
		{".", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := canonicalizeRelativePath(c.in, "test")
		if (err != nil) != c.err {
			t.Errorf("in=%q err=%v; want err=%v", c.in, err, c.err)
		}
		if got != c.want {
			t.Errorf("in=%q got=%q; want %q", c.in, got, c.want)
		}
	}
}
