package core

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"
)

// settings holds the resolved tool-level configuration after the precedence
// chain has been applied:
//
//	defaults  →  per-user (`git config third-party.*`)
//	          →  per-repo `[settings]` table in third-party.toml
//	          →  environment vars
//	          →  CLI flags
//
// Each layer is applied in order. The persisted layers (git-config, repo
// `[settings]`, env) carry the user-tunable scalars: LogLevel, LogFormat,
// Color, Quiet. DryRun and Profile are flag-only — they're per-invocation
// concerns, so only mergeFromFlags populates them. The Experimental feature
// set is intentionally additive across layers — opting into a feature in
// git-config and another via --experimental on the CLI yields both opted in,
// not just the CLI's. Repo settings is the one exception: it replaces
// rather than appends, on the theory that a per-repo `experimental = [...]`
// list expresses the repo's full intent.
type settings struct {
	LogLevel     string   // trace|debug|info|warn|error
	LogFormat    string   // text|json
	Color        string   // auto|always|never
	Quiet        bool     // suppress non-error tool output
	DryRun       bool     // do not mutate the host repo (flag-only)
	Profile      string   // CPU profile output path (flag-only)
	Experimental []string // opt-in feature flags (e.g. "patch")
}

func defaultSettings() *settings {
	return &settings{
		LogLevel:  "info",
		LogFormat: "text",
		Color:     "auto",
	}
}

// colorEnabled answers whether ANSI color escapes should be emitted for the
// given writer, honoring `--color`, NO_COLOR, and TTY detection.
func (s *settings) colorEnabled(w io.Writer) bool {
	switch strings.ToLower(s.Color) {
	case "always":
		return true
	case "never":
		return false
	}
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// hasExperimental reports whether the named feature is opted in.
func (s *settings) hasExperimental(name string) bool {
	return slices.Contains(s.Experimental, name)
}

// loadSettings runs the precedence chain. cmd is the cobra command that was
// dispatched; its flag set carries the CLI layer.
func (a *App) loadSettings(ctx context.Context, cmd *cobra.Command) (*settings, error) {
	s := defaultSettings()
	gopt := GlobalOptions{} // empty for bootstrap
	if err := a.mergeFromGitConfig(ctx, gopt, s); err != nil {
		// Non-fatal: failure to read user-level config (e.g. no git
		// installed yet, or running outside any git repo for `--help`).
		// Surface at debug level only.
		a.log().DebugContext(ctx, "git-config read failed; falling back to lower precedence layers", "err", err)
	}
	if err := a.mergeFromRepoSettings(ctx, gopt, s); err != nil {
		return nil, err
	}
	mergeFromEnv(s)
	mergeFromFlags(s, cmd)
	return s, nil
}

// mergeFromGitConfig pulls per-user defaults from `git config --get-all
// third-party.<key>`. Honored keys mirror the [settings] TOML table.
//
// `git config --get-regexp` exits 1 with no output when the regex matches
// nothing — the common "no third-party.* keys configured" case. We treat
// that as an empty result, not a failure, so the debug-level "git-config
// read failed" line in loadSettings doesn't fire on a clean install.
func (a *App) mergeFromGitConfig(ctx context.Context, gopt GlobalOptions, s *settings) error {
	r, err := a.git(ctx, gopt, []string{"config", "--global", "--get-regexp", `^third-party\.`}, modeNewlineTerminatedLines, gitOpts{suppressStderr: true})
	if err != nil {
		if gitExitCode(err) == 1 {
			return nil
		}
		return err
	}
	for _, line := range r.lines {
		// Format: "third-party.<key> <value>"
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			continue
		}
		key := strings.TrimPrefix(line[:sp], "third-party.")
		val := line[sp+1:]
		applySettingKV(s, key, val)
	}
	return nil
}

// mergeFromRepoSettings reads the optional `[settings]` table out of
// `third-party.toml`. Unknown keys are an error there too — the
// existing config-file parser is strict and we want the same here.
func (a *App) mergeFromRepoSettings(ctx context.Context, gopt GlobalOptions, s *settings) error {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		// Outside a repo (e.g. `git-third-party --help`): nothing to merge.
		return nil
	}
	path := filepath.Join(root, configFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var doc struct {
		Settings *struct {
			LogLevel     string   `toml:"log-level"`
			LogFormat    string   `toml:"log-format"`
			Color        string   `toml:"color"`
			Quiet        *bool    `toml:"quiet"`
			Experimental []string `toml:"experimental"`
		} `toml:"settings"`
		// Other top-level tables (notably `[[third_party]]`) are read by
		// readNewConfig; we accept their presence here without complaint
		// by giving them a permissive home in the decode struct.
		ThirdParty []map[string]any `toml:"third_party"`
	}
	meta, err := toml.Decode(string(data), &doc)
	if err != nil {
		return userErrorf("%s: %s", relPath(path), err.Error())
	}
	// Surface unknown keys *inside* [settings] only — leaving the third_party
	// array's per-entry validation to readNewConfig.
	for _, key := range meta.Undecoded() {
		if len(key) >= 2 && key[0] == "settings" {
			return userErrorf("%s: unknown key in [settings]: %s",
				relPath(path), strings.Join(key, "."))
		}
	}
	if doc.Settings == nil {
		return nil
	}
	if doc.Settings.LogLevel != "" {
		s.LogLevel = doc.Settings.LogLevel
	}
	if doc.Settings.LogFormat != "" {
		s.LogFormat = doc.Settings.LogFormat
	}
	if doc.Settings.Color != "" {
		s.Color = doc.Settings.Color
	}
	if doc.Settings.Quiet != nil {
		s.Quiet = *doc.Settings.Quiet
	}
	if doc.Settings.Experimental != nil {
		s.Experimental = append([]string(nil), doc.Settings.Experimental...)
	}
	return nil
}

func mergeFromEnv(s *settings) {
	if v := os.Getenv("GIT_THIRD_PARTY_LOG_LEVEL"); v != "" {
		s.LogLevel = v
	}
	if v := os.Getenv("GIT_THIRD_PARTY_LOG_FORMAT"); v != "" {
		s.LogFormat = v
	}
	if v := os.Getenv("GIT_THIRD_PARTY_COLOR"); v != "" {
		s.Color = v
	}
	if v := os.Getenv("GIT_THIRD_PARTY_EXPERIMENTAL"); v != "" {
		s.Experimental = append(s.Experimental, splitCSV(v)...)
	}
}

func mergeFromFlags(s *settings, cmd *cobra.Command) {
	f := cmd.Root().PersistentFlags()
	if f.Changed("log-level") {
		v, _ := f.GetString("log-level")
		s.LogLevel = v
	}
	if f.Changed("log-format") {
		v, _ := f.GetString("log-format")
		s.LogFormat = v
	}
	if f.Changed("color") {
		v, _ := f.GetString("color")
		s.Color = v
	}
	if f.Changed("quiet") {
		v, _ := f.GetBool("quiet")
		s.Quiet = v
	}
	if f.Changed("dry-run") {
		v, _ := f.GetBool("dry-run")
		s.DryRun = v
	}
	if f.Changed("profile") {
		v, _ := f.GetString("profile")
		s.Profile = v
	}
	if f.Changed("experimental") {
		v, _ := f.GetStringSlice("experimental")
		s.Experimental = append(s.Experimental, v...)
	}
	// -v / -vv / -q derive a log level only when --log-level was NOT
	// explicitly given. Explicit --log-level always wins (matches the help
	// text: "explicit log level ...; overrides -v/-q").
	if !f.Changed("log-level") && (f.Changed("verbose") || f.Changed("quiet")) {
		count, _ := f.GetCount("verbose")
		quiet, _ := f.GetBool("quiet")
		s.LogLevel = slogLevelToString(levelFromVerbosity(count, quiet))
	}
}

// applySettingKV writes a single key→value pair into s. Used by the
// git-config and `[settings]` TOML loaders. Keys are dash-separated.
func applySettingKV(s *settings, key, val string) {
	switch strings.ToLower(key) {
	case "log-level":
		s.LogLevel = val
	case "log-format":
		s.LogFormat = val
	case "color":
		s.Color = val
	case "quiet":
		s.Quiet = parseBool(val)
	case "experimental":
		s.Experimental = append(s.Experimental, splitCSV(val)...)
	}
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func slogLevelToString(l slog.Level) string {
	switch {
	case l <= LevelTrace:
		return "trace"
	case l <= LevelDebug:
		return "debug"
	case l <= LevelInfo:
		return "info"
	case l <= LevelWarn:
		return "warn"
	default:
		return "error"
	}
}
