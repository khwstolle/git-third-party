package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDryRunInvariance asserts that `update --dry-run` against a repo whose
// upstream has moved on does NOT mutate the lockfile, the working tree, or
// the index. This locks down the contract for `status` and CI gates.
func TestDryRunInvariance(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"a.txt": "v1\n"})
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/x", "", nil, nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "vendor")

	// Snapshot lock + working tree + HEAD before the dry-run update.
	lockBefore := mustRead(t, filepath.Join(host, lockFileName))
	cfgBefore := mustRead(t, filepath.Join(host, configFileName))
	fileBefore := mustRead(t, filepath.Join(host, "vendor/x/a.txt"))
	headBefore := strings.TrimSpace(runIn(t, host, "git", "rev-parse", "HEAD"))
	indexBefore := strings.TrimSpace(runIn(t, host, "git", "ls-files", "-s", "vendor/x"))

	// Move upstream forward.
	if err := os.WriteFile(filepath.Join(upstream, "a.txt"), []byte("v2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runIn(t, upstream, "git", "add", "-A")
	runIn(t, upstream, "git", "commit", "-q", "-m", "v2")

	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.update(ctx, GlobalOptions{DryRun: true}, "", false); err != nil {
			t.Fatalf("update dry-run: %v", err)
		}
	})

	if got := mustRead(t, filepath.Join(host, lockFileName)); got != lockBefore {
		t.Errorf("dry-run mutated lockfile.\nbefore:\n%s\nafter:\n%s", lockBefore, got)
	}
	if got := mustRead(t, filepath.Join(host, configFileName)); got != cfgBefore {
		t.Errorf("dry-run mutated config.\nbefore:\n%s\nafter:\n%s", cfgBefore, got)
	}
	if got := mustRead(t, filepath.Join(host, "vendor/x/a.txt")); got != fileBefore {
		t.Errorf("dry-run mutated vendored file.\nbefore: %q\nafter: %q", fileBefore, got)
	}
	if got := strings.TrimSpace(runIn(t, host, "git", "rev-parse", "HEAD")); got != headBefore {
		t.Errorf("dry-run advanced HEAD: %s -> %s", headBefore, got)
	}
	if got := strings.TrimSpace(runIn(t, host, "git", "ls-files", "-s", "vendor/x")); got != indexBefore {
		t.Errorf("dry-run mutated index for vendor/x")
	}
}

// TestCleanupRefsScoping asserts that an "update <one-dir>" run only cleans
// up refs in that section's namespace, leaving other sections' refs intact.
func TestCleanupRefsScoping(t *testing.T) {
	upstreamA, _ := makeUpstream(t, map[string]string{"a.txt": "A\n"})
	upstreamB, _ := makeUpstream(t, map[string]string{"b.txt": "B\n"})
	host := makeRepo(t)

	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstreamA, "main", "", "vendor/a", "", nil, nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "vendor a")
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstreamB, "main", "", "vendor/b", "", nil, nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "vendor b")

	// Both sections should now have refs/third_party/{0,1}/...
	allBefore := runIn(t, host, "git", "for-each-ref", "--format=%(refname)", "refs/third_party/")
	if !strings.Contains(allBefore, "refs/third_party/0/") {
		t.Fatalf("expected refs/third_party/0/* before scoped update; got:\n%s", allBefore)
	}
	if !strings.Contains(allBefore, "refs/third_party/1/") {
		t.Fatalf("expected refs/third_party/1/* before scoped update; got:\n%s", allBefore)
	}

	// Update only section A. cleanupRefs should leave section B's refs alone.
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.update(ctx, GlobalOptions{}, "vendor/a", false); err != nil {
			t.Fatalf("update vendor/a: %v", err)
		}
	})

	allAfter := runIn(t, host, "git", "for-each-ref", "--format=%(refname)", "refs/third_party/")
	if !strings.Contains(allAfter, "refs/third_party/1/") {
		t.Errorf("cleanupRefs erroneously removed section 1's refs in a scoped update.\nbefore:\n%s\nafter:\n%s",
			allBefore, allAfter)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
