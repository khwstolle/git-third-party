package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateURL(t *testing.T) {
	app := NewApp(nil, nil)
	ctx := context.Background()
	gopt := GlobalOptions{}

	cases := []struct {
		url   string
		valid bool
	}{
		{"https://github.com/foo/bar", true},
		{"git@github.com:foo/bar.git", true},
		{"ssh://git@github.com/foo/bar", true},
		{"file:///tmp/repo", true},
		{"invalid url", false},
	}
	for _, c := range cases {
		err := app.validateURL(ctx, gopt, c.url, false)
		if (err == nil) != c.valid {
			t.Errorf("url=%q valid=%v; got err=%v", c.url, c.valid, err)
		}
	}
}

func TestValidateDirNotExists(t *testing.T) {
	host := makeRepo(t)

	withRepo(t, host, func(ctx context.Context, a *App) {
		gopt := GlobalOptions{}
		if err := a.validateDirNotExists(ctx, gopt, "vendor/new", "test"); err != nil {
			t.Errorf("expected success for non-existent dir; got %v", err)
		}
		// Create a file at that path.
		_ = os.MkdirAll(filepath.Join(host, "vendor/new"), 0755)
		if err := a.validateDirNotExists(ctx, gopt, "vendor/new", "test"); err == nil {
			t.Errorf("expected error for existing dir")
		}
	})
}
