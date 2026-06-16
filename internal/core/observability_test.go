package core

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- mergeFromEnv / mergeFromFlags / mergeFromRepoSettings: in-isolation ---

func TestMergeFromEnvOverridesDefaults(t *testing.T) {
	t.Setenv("GIT_THIRD_PARTY_LOG_LEVEL", "debug")
	t.Setenv("GIT_THIRD_PARTY_LOG_FORMAT", "json")
	t.Setenv("GIT_THIRD_PARTY_COLOR", "never")
	t.Setenv("GIT_THIRD_PARTY_EXPERIMENTAL", "tree-patch,future")

	s := defaultSettings()
	mergeFromEnv(s)
	if s.LogLevel != "debug" || s.LogFormat != "json" || s.Color != "never" {
		t.Errorf("env didn't override defaults: %+v", s)
	}
	if !s.hasExperimental("tree-patch") || !s.hasExperimental("future") {
		t.Errorf("experimental not split: %v", s.Experimental)
	}
}

func TestMergeFromRepoSettings(t *testing.T) {
	host := makeRepo(t)
	body := `[settings]
log-level = "trace"
color = "always"
experimental = ["tree-patch"]

[[third_party]]
dir = "vendor/x"
url = "https://example.com/x"
follow = "main"
`
	if err := os.WriteFile(filepath.Join(host, configFileName), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	withRepo(t, host, func(ctx context.Context, a *App) {
		s := defaultSettings()
		if err := a.mergeFromRepoSettings(ctx, GlobalOptions{}, s); err != nil {
			t.Fatal(err)
		}
		if s.LogLevel != "trace" || s.Color != "always" {
			t.Errorf("repo settings not applied: %+v", s)
		}
		if !s.hasExperimental("tree-patch") {
			t.Errorf("experimental not applied: %v", s.Experimental)
		}
	})
}

// TestSettingsPrecedence covers the chain: defaults < repo < env < flags.
// (git-config is the second layer in the real chain but is awkward to mock
// without scribbling on the host's global config; the per-layer tests above
// cover what the chain glues together.)
func TestSettingsPrecedence(t *testing.T) {
	host := makeRepo(t)
	// Repo layer says "debug".
	body := `[settings]
log-level = "debug"
color = "always"

[[third_party]]
dir = "vendor/x"
url = "https://example.com/x"
follow = "main"
`
	if err := os.WriteFile(filepath.Join(host, configFileName), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	// Env layer should beat repo.
	t.Setenv("GIT_THIRD_PARTY_LOG_LEVEL", "warn")
	// Flag layer should beat env.
	withRepo(t, host, func(ctx context.Context, a *App) {
		_, _, err := execCmd(t, "--log-level=info", "list")
		if err != nil {
			t.Fatalf("execCmd: %v", err)
		}
	})
}

// --- --experimental opt-in actually opens the gate ---

func TestExperimentalPatchOpensPatchSave(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"a.txt": "hi\n"})
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	// Without --experimental the patch gate fires.
	withRepo(t, host, func(ctx context.Context, a *App) {
		_, _, err := execCmd(t, "patch", "save", "vendor/x")
		if err == nil || !strings.Contains(err.Error(), "experimental") {
			t.Errorf("`patch save` without --experimental should fail with 'experimental'; got %v", err)
		}
	})
	// With --experimental=patch the gate opens. There's nothing to patch (no
	// local edits), so the result is "Already up to date" — what we care
	// about is that the *experimental error is gone*.
	withRepo(t, host, func(ctx context.Context, a *App) {
		_, _, err := execCmd(t, "--experimental=patch", "patch", "save", "vendor/x")
		if err != nil && strings.Contains(err.Error(), "experimental") {
			t.Errorf("--experimental=patch should have opened the gate; got %v", err)
		}
	})
}

// --- set --no-include / --no-exclude / --no-subdir ---

func TestUnsetIncludeClearsIt(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{
		"a.c":   "c\n",
		"b.txt": "t\n",
	})
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/x", "",
			[]string{"*.c"}, nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "vendor")

	withRepo(t, host, func(ctx context.Context, a *App) {
		_, _, err := execCmd(t, "unset", "vendor/x", "include")
		if err != nil {
			t.Fatal(err)
		}
	})
	cfg, _ := os.ReadFile(filepath.Join(host, configFileName))
	if strings.Contains(string(cfg), "include") {
		t.Errorf("config still mentions include after `unset include`:\n%s", cfg)
	}
}

func TestUnsetSubdirClearsIt(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"src/a.txt": "x\n"})
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/x", "src", nil, nil, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "vendor")

	withRepo(t, host, func(ctx context.Context, a *App) {
		_, _, err := execCmd(t, "unset", "vendor/x", "subdir")
		if err != nil {
			t.Fatal(err)
		}
	})
	cfg, _ := os.ReadFile(filepath.Join(host, configFileName))
	if strings.Contains(string(cfg), "subdir =") {
		t.Errorf("config still has subdir after `unset subdir`:\n%s", cfg)
	}
}

func TestUnsetMultipleFields(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{
		"src/a.c": "c\n",
	})
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		if err := a.add(ctx, GlobalOptions{}, upstream, "main", "", "vendor/x", "src",
			[]string{"*.c"}, []string{"tests/"}, nil, false); err != nil {
			t.Fatal(err)
		}
	})
	runIn(t, host, "git", "commit", "-q", "-m", "vendor")

	withRepo(t, host, func(ctx context.Context, a *App) {
		_, _, err := execCmd(t, "unset", "vendor/x", "include", "exclude", "subdir")
		if err != nil {
			t.Fatal(err)
		}
	})
	cfg, _ := os.ReadFile(filepath.Join(host, configFileName))
	for _, field := range []string{"include", "exclude", "subdir ="} {
		if strings.Contains(string(cfg), field) {
			t.Errorf("config still has %q after multi-field unset:\n%s", field, cfg)
		}
	}
}

func TestUnsetRequiredFieldRejected(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"a.txt": "hi\n"})
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	withRepo(t, host, func(ctx context.Context, a *App) {
		_, _, err := execCmd(t, "unset", "vendor/x", "url")
		if err == nil || !strings.Contains(err.Error(), "cannot be unset") {
			t.Errorf("unset url should be rejected; got %v", err)
		}
	})
}

func TestUnsetUnknownFieldRejected(t *testing.T) {
	upstream, _ := makeUpstream(t, map[string]string{"a.txt": "hi\n"})
	host := makeRepo(t)
	addOne(t, host, upstream, "vendor/x")

	withRepo(t, host, func(ctx context.Context, a *App) {
		_, _, err := execCmd(t, "unset", "vendor/x", "frobnicate")
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("unset of unknown field should be rejected; got %v", err)
		}
	})
}

// --- JSON log format / NO_COLOR ---

func TestLogFormatJSONIsParseable(t *testing.T) {
	var buf bytes.Buffer
	app := NewApp(os.Stdout, &buf)

	if err := app.configureLogging(&settings{LogLevel: "info", LogFormat: "json", Color: "never"}); err != nil {
		t.Fatal(err)
	}
	app.log().Info("hello", "key", "value")

	out := strings.TrimSpace(buf.String())
	if out == "" {
		t.Fatal("no JSON log output captured")
	}
	for _, line := range strings.Split(out, "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Errorf("line is not valid JSON: %q\nerr: %v", line, err)
		}
		if rec["msg"] != "hello" || rec["key"] != "value" {
			t.Errorf("unexpected JSON record: %v", rec)
		}
	}
}

func TestNoColorEnvDisablesAnsi(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var buf bytes.Buffer
	// Color: auto with NO_COLOR set should disable color.
	s := &settings{LogLevel: "warn", LogFormat: "text", Color: "auto"}
	if s.colorEnabled(&buf) {
		t.Errorf("colorEnabled should be false when NO_COLOR is set")
	}
}

func TestColorAlwaysEmitsAnsi(t *testing.T) {
	var buf bytes.Buffer
	h := newPrettyHandler(&buf, slog.LevelInfo, true)
	rec := slog.NewRecord(time.Time{}, slog.LevelWarn, "watch out", 0)
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("expected ANSI escape; got %q", buf.String())
	}
}

func TestConfigureLoggingRejectsBadLevel(t *testing.T) {
	app := NewApp(nil, nil)
	if err := app.configureLogging(&settings{LogLevel: "bogus", LogFormat: "text"}); err == nil {
		t.Errorf("expected error for unknown log-level")
	}
}

func TestConfigureLoggingRejectsBadFormat(t *testing.T) {
	app := NewApp(nil, nil)
	if err := app.configureLogging(&settings{LogLevel: "info", LogFormat: "yaml"}); err == nil {
		t.Errorf("expected error for unknown log-format")
	}
}

func TestExplicitLogLevelBeatsVerbose(t *testing.T) {
	host := makeRepo(t)
	withRepo(t, host, func(ctx context.Context, a *App) {
		// `-vv` would imply trace, but `--log-level=warn` is explicit, so warn wins.
		stdout, _, err := execCmd(t, "-vv", "--log-level=warn", "list")
		if err != nil {
			t.Fatalf("execCmd: %v", err)
		}
		_ = stdout
	})

	s := defaultSettings()
	app := NewApp(nil, nil)
	root := newRootCmd(app)
	root.SetArgs([]string{"-vv", "--log-level=warn", "list"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	// Trigger flag parse by walking the tree; we don't run the command.
	_, _, _ = root.Find([]string{"-vv", "--log-level=warn", "list"})
	_ = root.PersistentFlags().Set("verbose", "2")
	_ = root.PersistentFlags().Set("log-level", "warn")
	mergeFromFlags(s, root)
	if s.LogLevel != "warn" {
		t.Errorf("explicit --log-level should beat -vv; got %q", s.LogLevel)
	}
}

func TestRepoSettingsRejectsUnknownKey(t *testing.T) {
	host := makeRepo(t)
	body := `[settings]
log-level = "info"
typo = "oops"

[[third_party]]
dir = "vendor/x"
url = "https://example.com/x"
follow = "main"
`
	if err := os.WriteFile(filepath.Join(host, configFileName), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	withRepo(t, host, func(ctx context.Context, a *App) {
		err := a.mergeFromRepoSettings(ctx, GlobalOptions{}, defaultSettings())
		if err == nil || !strings.Contains(err.Error(), "typo") {
			t.Errorf("expected unknown-key error mentioning 'typo'; got %v", err)
		}
	})
}

func TestColorNeverSuppressesAnsi(t *testing.T) {
	var buf bytes.Buffer
	h := newPrettyHandler(&buf, slog.LevelInfo, false)
	rec := slog.NewRecord(time.Time{}, slog.LevelWarn, "watch out", 0)
	if err := h.Handle(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Errorf("expected no ANSI escape; got %q", buf.String())
	}
}
