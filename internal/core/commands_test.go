package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// addOne is a small helper around App.add used by the tests below to seed a
// vendored-dir entry.
func addOne(t *testing.T, host, upstream, dir string) {
	t.Helper()
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", dir, "", nil, nil, nil, false); err != nil {
			t.Fatalf("add: %v", err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "vendor")
}

func TestDoSetChangesURL(t *testing.T) {
	upstream1, _ := makeUpstream(t, map[string]string{"a.txt": "one\n"})
	upstream2, _ := makeUpstream(t, map[string]string{"a.txt": "two\n"})
	host := makeRepo(t)
	addOne(t, host, upstream1, "vendor/x")

	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.set(ctx, GlobalOptions{}, upstream2, "main", "", "vendor/x", nil, nil, nil, nil); err != nil {
			t.Fatalf("set: %v", err)
		}
	})
	got, err := os.ReadFile(filepath.Join(host, "vendor/x/a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two\n" {
		t.Errorf("a.txt = %q; want two\n (re-fetched from new url)", got)
	}
	cfg, _ := os.ReadFile(filepath.Join(host, configFileName))
	if !strings.Contains(string(cfg), upstream2) {
		t.Errorf("config didn't pick up new url:\n%s", cfg)
	}
}

func TestDoUnsetClearsInclude(t *testing.T) {
	// Seed an entry with --include patterns, then clear via `unset`.
	upstream, _ := makeUpstream(t, map[string]string{
		"a.c":   "c\n",
		"b.txt": "t\n",
	})
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/x", "",
			[]string{"*.c"}, nil, nil, false); err != nil {
			t.Fatalf("add: %v", err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "vendor")

	// b.txt should not be present yet (filtered out).
	if _, err := os.Stat(filepath.Join(host, "vendor/x/b.txt")); err == nil {
		t.Errorf("b.txt should have been excluded by --include=*.c")
	}

	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.unset(ctx, GlobalOptions{}, "vendor/x", []string{"include"}); err != nil {
			t.Fatalf("unset: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(host, "vendor/x/b.txt")); err != nil {
		t.Errorf("b.txt should be present after `unset include`: %v", err)
	}

	cfg, _ := os.ReadFile(filepath.Join(host, configFileName))
	if strings.Contains(string(cfg), "include = ") {
		t.Errorf("config still contains include; expected cleared:\n%s", cfg)
	}
}

func TestDoSetSwitchBranchToTag(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"a.txt": "v1\n"})
	// Tag the only commit as v1.0.0.
	runIn(t, upstream, "git", "tag", "v1.0.0")
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.set(ctx, GlobalOptions{}, "", "", "v1.0.0", "vendor/x", nil, nil, nil, nil); err != nil {
			t.Fatalf("set: %v", err)
		}
	})
	cfg, _ := os.ReadFile(filepath.Join(host, configFileName))
	if !strings.Contains(string(cfg), `pin = "v1.0.0"`) {
		t.Errorf("config missing pin:\n%s", cfg)
	}
	if strings.Contains(string(cfg), "follow") {
		t.Errorf("follow should have been cleared:\n%s", cfg)
	}
}

func TestDoSavePatchAndDiffPatch(t *testing.T) {
	if os.Getenv("GIT_AUTHOR_NAME") == "" {
		t.Skip("integration env not set up")
	}
	upstream, _ := makeUpstream(t, map[string]string{
		"hello.txt": "hello\nworld\n",
	})
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	// Modify the vendored file in the host repo and stage it.
	target := filepath.Join(host, "vendor/x/hello.txt")
	if err := os.WriteFile(target, []byte("hello\nworld\nlocal edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runIn(t, host, "git", "add", "vendor/x/hello.txt")

	// savePatch records the patch in the lockfile.
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.savePatch(ctx, GlobalOptions{}, "vendor/x"); err != nil {
			t.Fatalf("savePatch: %v", err)
		}
	})
	lock, err := os.ReadFile(filepath.Join(host, lockFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lock), "tree-patch = ") {
		t.Errorf("lockfile should contain tree-patch:\n%s", lock)
	}
	runIn(t, host, "git", "commit", "-q", "-m", "save edits")

	// Re-running savePatch with no further changes should be a no-op.
	stdout, _ := withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.savePatch(ctx, GlobalOptions{}, "vendor/x"); err != nil {
			t.Fatalf("savePatch (no-op): %v", err)
		}
	})
	if !strings.Contains(stdout, "Already up to date") {
		t.Errorf("expected Already up to date; got %q", stdout)
	}

	// diffPatch in dry-run mode prints the underlying git diff command.
	stdout, _ = withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.diffPatch(ctx, GlobalOptions{DryRun: true}, "vendor/x"); err != nil {
			t.Fatalf("diffPatch: %v", err)
		}
	})
	if !strings.Contains(stdout, "git diff ") {
		t.Errorf("expected 'git diff ...' in dry-run output; got %q", stdout)
	}
}

func TestDoSavePatchRefusesUnstaged(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"hello.txt": "hi\n"})
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	// Modify but DON'T stage.
	if err := os.WriteFile(filepath.Join(host, "vendor/x/hello.txt"),
		[]byte("hi\nlocal\n"), 0644); err != nil {
		t.Fatal(err)
	}

	withRepo(t, host, func(ctx context.Context, a *App) {
		err := a.savePatch(ctx, GlobalOptions{}, "vendor/x")
		if err == nil {
			t.Fatal("expected error about unstaged changes")
		}
		if !strings.Contains(err.Error(), "unstaged changes") {
			t.Errorf("expected unstaged-changes error; got %v", err)
		}
	})
}

// TestUpdateProducesConflictSuffix exercises the full savePatch → conflicting-
// upstream → update path and asserts that the resulting lockfile carries a
// `tree-patch = "...:-conflicts"` value and the user-visible output mentions
// the conflict.
func TestUpdateProducesConflictSuffix(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{
		"hello.txt": "line1\nline2\nline3\n",
	})
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	// Local edit on line 2, staged.
	target := filepath.Join(host, "vendor/x/hello.txt")
	if err := os.WriteFile(target, []byte("line1\nline2 LOCAL\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runIn(t, host, "git", "add", "vendor/x/hello.txt")
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.savePatch(ctx, GlobalOptions{}, "vendor/x"); err != nil {
			t.Fatalf("savePatch: %v", err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "save edits")

	// Upstream edits the *same* line — this should fail to merge cleanly.
	if err := os.WriteFile(filepath.Join(upstream, "hello.txt"),
		[]byte("line1\nline2 UPSTREAM\nline3\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runIn(t, upstream, "git", "add", "-A")
	runIn(t, upstream, "git", "commit", "-q", "-m", "upstream edit")

	stdout, _ := withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.update(ctx, GlobalOptions{}, "vendor/x", false); err != nil {
			t.Fatalf("update: %v", err)
		}
	})
	if !strings.Contains(stdout, "CONFLICT (git-third-party)") {
		t.Errorf("expected CONFLICT line in stdout; got %q", stdout)
	}

	lock, err := os.ReadFile(filepath.Join(host, lockFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lock), "-conflicts\"") {
		t.Errorf("lockfile tree-patch should end in -conflicts; got:\n%s", lock)
	}
}

func TestDoDiffPatchNoOpWhenNoPatch(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"a.txt": "x\n"})
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.diffPatch(ctx, GlobalOptions{}, "vendor/x"); err != nil {
			t.Errorf("diffPatch with no patch: unexpected error %v", err)
		}
	})
}
