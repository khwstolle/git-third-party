package core

import (
	"context"
	"testing"
)

func TestGitmodulesParse(t *testing.T) {
	content := []byte(`
[submodule "foo"]
	path = vendor/foo
	url = https://github.com/foo/foo
[submodule "bar"]
	path = vendor/bar
	url = ../bar.git
`)
	app := NewApp(nil, nil)
	ctx := context.Background()
	gopt := GlobalOptions{}
	got, err := app.parseSubmodulePathToURLFromGitmodulesContent(ctx, gopt, content)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d submodules; want 2", len(got))
	}
	if got["vendor/foo"] != "https://github.com/foo/foo" {
		t.Errorf("foo url wrong: %q", got["vendor/foo"])
	}
	if got["vendor/bar"] != "../bar.git" {
		t.Errorf("bar url wrong: %q", got["vendor/bar"])
	}
}
