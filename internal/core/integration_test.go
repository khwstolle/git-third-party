package core

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runIn is a small exec.Command wrapper that runs name+args in dir and
// returns combined output. Fails the test on non-zero exit.
func runIn(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	c := exec.Command(name, args...)
	c.Dir = dir
	// Clear git environment variables to avoid interference when running
	// tests from inside a git hook (where GIT_DIR, etc. are set).
	env := os.Environ()
	for i := 0; i < len(env); i++ {
		if strings.HasPrefix(env[i], "GIT_") {
			env = append(env[:i], env[i+1:]...)
			i--
		}
	}
	c.Env = append(env,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@example.com",
		// Override any user/global commit-signing config that might be
		// active in the dev environment.
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=commit.gpgsign", "GIT_CONFIG_VALUE_0=false",
		"GIT_CONFIG_KEY_1=tag.gpgsign", "GIT_CONFIG_VALUE_1=false",
	)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return string(out)
}

// makeRepo creates a fresh git repo in a temp dir.
func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runIn(t, dir, "git", "init", "-q", "-b", "main")
	runIn(t, dir, "git", "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

// makeUpstream creates a remote-style git repo (with content) and returns its
// path along with the commit hash and content tree.
func makeUpstream(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	runIn(t, dir, "git", "init", "-q", "-b", "main")
	for name, content := range files {
		full := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(full), 0755)
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	runIn(t, dir, "git", "add", "-A")
	runIn(t, dir, "git", "commit", "-q", "-m", "content")
	out := runIn(t, dir, "git", "rev-parse", "HEAD")
	return dir, strings.TrimSpace(out)
}

// withRepo runs f inside dir, capturing stdout/stderr through a fresh App.
func withRepo(t *testing.T, dir string, f func(ctx context.Context, a *App)) (string, string) {
	t.Helper()
	prev, _ := os.Getwd()
	defer func() { _ = os.Chdir(prev) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Clear git environment variables to avoid interference when running
	// tests from inside a git hook (where GIT_DIR, etc. are set).
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "GIT_") {
			key := strings.SplitN(env, "=", 2)[0]
			// We must use os.Unsetenv because t.Setenv(key, "") sets it to
			// empty, which still confuses git. t.Cleanup ensures it's
			// restored for other tests (though we've disabled parallel tests).
			old, ok := os.LookupEnv(key)
			_ = os.Unsetenv(key)
			if ok {
				t.Cleanup(func() { _ = os.Setenv(key, old) })
			}
		}
	}

	var sb, se bytes.Buffer
	app := NewApp(&sb, &se)
	ctx := context.Background()
	f(ctx, app)
	return sb.String(), se.String()
}

func TestIntegrationAddListUpdateRemove(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	upstream, _ := makeUpstream(t, map[string]string{
		"src/foo.c": "void foo(void) {}\n",
		"src/foo.h": "extern void foo(void);\n",
		"tests/x.c": "// test\n",
		"README.md": "hi\n",
	})
	host := makeRepo(t)

	stdout, stderr := withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/foo", "", nil, nil, false); err != nil {
			t.Errorf("add: %v", err)
		}
	})
	t.Logf("add stdout=%q stderr=%q", stdout, stderr)

	cfgBytes, err := os.ReadFile(filepath.Join(host, "third-party.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(cfgBytes)
	if !strings.Contains(cfg, `dir = "vendor/foo"`) {
		t.Errorf("config missing dir line:\n%s", cfg)
	}
	if !strings.Contains(cfg, `follow = "main"`) {
		t.Errorf("config missing follow line:\n%s", cfg)
	}

	lockBytes, err := os.ReadFile(filepath.Join(host, "third-party.lock"))
	if err != nil {
		t.Fatal(err)
	}
	lock := string(lockBytes)
	if !strings.Contains(lock, `dir = "vendor/foo"`) {
		t.Errorf("lockfile missing dir line:\n%s", lock)
	}

	// Verify content.
	got, _ := os.ReadFile(filepath.Join(host, "vendor/foo/src/foo.c"))
	if string(got) != "void foo(void) {}\n" {
		t.Errorf("foo.c content wrong: %q", got)
	}

	// list
	stdout, _ = withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.list(ctx, GlobalOptions{}, "", false); err != nil {
			t.Errorf("list: %v", err)
		}
	})
	if !strings.Contains(stdout, "vendor/foo (follow=main)") {
		t.Errorf("list output wrong: %q", stdout)
	}

	// update
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.update(ctx, GlobalOptions{}, "", false); err != nil {
			t.Errorf("update: %v", err)
		}
	})

	// remove
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.remove(ctx, GlobalOptions{}, "vendor/foo"); err != nil {
			t.Errorf("remove: %v", err)
		}
	})
	if _, err := os.Stat(filepath.Join(host, "vendor/foo")); err == nil {
		t.Errorf("vendor/foo should be deleted")
	}
	if _, err := os.Stat(filepath.Join(host, "third-party.toml")); err == nil {
		t.Errorf("third-party.toml should be deleted (empty)")
	}
}
