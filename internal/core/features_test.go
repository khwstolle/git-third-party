package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFeaturesConfigOrdering(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"a": "a"})
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/a", "", nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	stdout, _ := withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.list(ctx, GlobalOptions{}, "", false); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.HasPrefix(stdout, "vendor/a") {
		t.Errorf("list output wrong: %q", stdout)
	}

	// Add second entry.
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/b", "", nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	stdout, _ = withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.list(ctx, GlobalOptions{}, "", false); err != nil {
			t.Fatal(err)
		}
	})
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "vendor/a") || !strings.Contains(lines[1], "vendor/b") {
		t.Errorf("list output ordering/count wrong: %q", stdout)
	}
}

func TestFeaturesSubdirVendoring(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{
		"src/foo.c": "foo\n",
		"tests/a.c": "test\n",
	})
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/foo", "src", nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(filepath.Join(host, "vendor/foo/foo.c")); err != nil {
		t.Errorf("subdir content not at root of vendored dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(host, "vendor/foo/src")); err == nil {
		t.Errorf("subdir itself should not be present in host")
	}
}

func TestFeaturesDryRun(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"a": "a"})
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{DryRun: true}, upstream, "main", "", "vendor/x", "", nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(filepath.Join(host, "vendor/x")); err == nil {
		t.Errorf("dry-run add created directory")
	}
	if _, err := os.Stat(filepath.Join(host, configFileName)); err == nil {
		t.Errorf("dry-run add created config file")
	}
}
