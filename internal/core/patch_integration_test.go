package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPatchIntegrationMergeConflict(t *testing.T) {
	if os.Getenv("GIT_AUTHOR_NAME") == "" {
		t.Skip("integration env not set up")
	}
	upstream, _ := makeUpstream(t, map[string]string{
		"a.txt": "line1\nline2\nline3\n",
	})
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	// 1. Local edit on line 2.
	target := filepath.Join(host, "vendor/x/a.txt")
	_ = os.WriteFile(target, []byte("line1\nline2 LOCAL\nline3\n"), 0644)
	runIn(t, host, "git", "add", "vendor/x/a.txt")
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.savePatch(ctx, GlobalOptions{}, "vendor/x"); err != nil {
			t.Fatal(err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "save patch")

	// 2. Upstream edit on line 2 (conflicting).
	_ = os.WriteFile(filepath.Join(upstream, "a.txt"), []byte("line1\nline2 UPSTREAM\nline3\n"), 0644)
	runIn(t, upstream, "git", "add", "-A")
	runIn(t, upstream, "git", "commit", "-q", "-m", "upstream edit")

	// 3. Update. Should report conflict.
	stdout, _ := withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.update(ctx, GlobalOptions{}, "vendor/x", false); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stdout, "CONFLICT") {
		t.Errorf("expected CONFLICT in output; got %q", stdout)
	}

	// 4. Resolve conflict and save again.
	_ = os.WriteFile(target, []byte("line1\nline2 RESOLVED\nline3\n"), 0644)
	runIn(t, host, "git", "add", "vendor/x/a.txt")
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.savePatch(ctx, GlobalOptions{}, "vendor/x"); err != nil {
			t.Fatal(err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "resolve and save")

	lock, _ := os.ReadFile(filepath.Join(host, lockFileName))
	if strings.Contains(string(lock), "-conflicts") {
		t.Errorf("lockfile should no longer have -conflicts suffix")
	}
}
