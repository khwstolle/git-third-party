package core

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// execCmd runs the cobra root with the given argv against fresh stdout/stderr
// buffers and returns (stdout, stderr, error). Used by tests that exercise
// the CLI surface end-to-end without going through main().
func execCmd(t *testing.T, argv ...string) (string, string, error) {
	t.Helper()

	// Clear git environment variables to avoid interference when running
	// tests from inside a git hook (where GIT_DIR, etc. are set).
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "GIT_") {
			key := strings.SplitN(env, "=", 2)[0]
			old, ok := os.LookupEnv(key)
			_ = os.Unsetenv(key)
			if ok {
				t.Cleanup(func() { _ = os.Setenv(key, old) })
			}
		}
	}

	var stdout, stderr bytes.Buffer
	app := NewApp(&stdout, &stderr)

	root := newRootCmd(app)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(argv)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestCLIAddRequiresURLPositional(t *testing.T) {
	_, _, err := execCmd(t, "add", "vendor/x")
	if err == nil {
		t.Errorf("expected error: URL positional is required")
	}
}

func TestCLIAddRequiresDirPositional(t *testing.T) {
	_, _, err := execCmd(t, "add")
	if err == nil {
		t.Errorf("expected error: DIR positional required")
	}
}

func TestCLIFollowPinMutuallyExclusive(t *testing.T) {
	cases := [][]string{
		{"add", "vendor/x", "https://x", "--follow", "main", "--pin", "v1"},
	}
	for _, argv := range cases {
		_, _, err := execCmd(t, argv...)
		if err == nil {
			t.Errorf("expected mutex error for argv=%v", argv)
		}
	}
}

func TestCLIUnknownCommand(t *testing.T) {
	_, _, err := execCmd(t, "frobnicate")
	if err == nil {
		t.Errorf("expected unknown-command error")
	}
}

func TestCLIUnknownFlag(t *testing.T) {
	_, _, err := execCmd(t, "list", "--frobnicate")
	if err == nil {
		t.Errorf("expected unknown-flag error")
	}
}

func TestCLIRenameRequiresBothPositionals(t *testing.T) {
	_, _, err := execCmd(t, "rename", "a")
	if err == nil {
		t.Errorf("rename needs both DIR and NEW_DIR")
	}
}

func TestCLIVersion(t *testing.T) {
	stdout, _, err := execCmd(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, Version) {
		t.Errorf("--version output %q does not contain %q", stdout, Version)
	}
}

func TestCLIPatchGated(t *testing.T) {
	// Without --experimental, the patch subtree should refuse. We don't need
	// a vendored dir for this test — the gate fires before any leaf body.
	_, _, err := execCmd(t, "patch", "save", "vendor/x")
	if err == nil {
		t.Errorf("patch should be gated behind --experimental")
		return
	}
	if !strings.Contains(err.Error(), "experimental") {
		t.Errorf("expected 'experimental' in error; got %v", err)
	}
}

func TestCLICompletionShells(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		stdout, _, err := execCmd(t, "completion", sh)
		if err != nil {
			t.Errorf("completion %s: %v", sh, err)
			continue
		}
		if len(stdout) < 100 {
			t.Errorf("completion %s output suspiciously short: %d bytes", sh, len(stdout))
		}
	}
}

func TestCLICompletionUnknownShell(t *testing.T) {
	_, _, err := execCmd(t, "completion", "powershell-x")
	if err == nil {
		t.Errorf("expected error for unknown shell")
	}
}
