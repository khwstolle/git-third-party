package core

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	configFileName = "third-party.toml"
	lockFileName   = "third-party.lock"

	// Bumped only on incompatible lockfile schema changes.
	lockSchemaVersion = 1
)

// configFileTOML mirrors the TOML structure of `third-party.toml`. The
// `Settings` field is only present so the decoder doesn't reject the
// `[settings]` block as an unknown key; its contents are loaded separately
// by mergeFromRepoSettings during the precedence chain.
type configFileTOML struct {
	ThirdParty []configEntryTOML `toml:"third_party"`
	Settings   map[string]any    `toml:"settings,omitempty"`
}

type configEntryTOML struct {
	Dir     string   `toml:"dir"`
	URL     string   `toml:"url"`
	Follow  string   `toml:"follow,omitempty"`
	Pin     string   `toml:"pin,omitempty"`
	Subdir  string   `toml:"subdir,omitempty"`
	Include []string `toml:"include,omitempty"`
	Exclude []string `toml:"exclude,omitempty"`
}

type lockFileTOML struct {
	Version    int             `toml:"version"`
	ThirdParty []lockEntryTOML `toml:"third_party"`
}

type lockEntryTOML struct {
	Dir       string `toml:"dir"`
	Commit    string `toml:"commit,omitempty"`
	TreePatch string `toml:"tree-patch,omitempty"`
}

func (a *App) configFilePath(ctx context.Context, gopt GlobalOptions) (string, error) {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, configFileName), nil
}

func (a *App) lockFilePath(ctx context.Context, gopt GlobalOptions) (string, error) {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, lockFileName), nil
}

// readConfig loads the merged config + lock state into a single in-memory
// representation. Returns an empty slice if `third-party.toml` is absent.
//
// Any UserError returned from this path gets ExitConfig (2) stamped — the
// error category is "your config files are wrong", which is distinct
// enough from other failures to deserve a stable exit code.
func (a *App) readConfig(ctx context.Context, gopt GlobalOptions) ([]*ConfigItem, error) {
	cfgPath, err := a.configFilePath(ctx, gopt)
	if err != nil {
		return nil, err
	}
	cfgData, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	items, err := a.readNewConfig(ctx, gopt, cfgData, cfgPath)
	return items, withExitCode(err, ExitConfig)
}

// readNewConfig parses third-party.toml and merges lock state, joined on dir.
func (a *App) readNewConfig(ctx context.Context, gopt GlobalOptions, cfgData []byte, cfgPath string) ([]*ConfigItem, error) {
	var cfg configFileTOML
	meta, err := toml.Decode(string(cfgData), &cfg)
	if err != nil {
		return nil, userErrorf("%s: %s", relPath(cfgPath), err.Error())
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		var keys []string
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return nil, userErrorf("%s: unknown keys: %s", relPath(cfgPath), strings.Join(keys, ", "))
	}

	primary := map[string]struct{}{}
	items := make([]*ConfigItem, 0, len(cfg.ThirdParty))
	for i, e := range cfg.ThirdParty {
		hint := fmt.Sprintf("[[third_party]] #%d", i+1)
		if e.Dir == "" {
			return nil, userErrorf("%s: %s missing required dir", relPath(cfgPath), hint)
		}
		if _, dup := primary[e.Dir]; dup {
			return nil, userErrorf("%s: multiple sections with the same dir: %s", relPath(cfgPath), shlexQuote(e.Dir))
		}
		primary[e.Dir] = struct{}{}
		if e.URL == "" {
			return nil, userErrorf("%s: dir=%s missing required url", relPath(cfgPath), shlexQuote(e.Dir))
		}
		if boolToInt(e.Follow != "")+boolToInt(e.Pin != "") != 1 {
			return nil, userErrorf("%s: dir=%s must specify exactly one of: follow, pin", relPath(cfgPath), shlexQuote(e.Dir))
		}
		item := &ConfigItem{
			Dir:     e.Dir,
			URL:     e.URL,
			Follow:  e.Follow,
			Pin:     e.Pin,
			Subdir:  e.Subdir,
			Include: append([]string(nil), e.Include...),
			Exclude: append([]string(nil), e.Exclude...),
		}
		if err := validateDir(item.Dir, "dir"); err != nil {
			return nil, decoratedConfigErr(cfgPath, hint, err)
		}
		if err := a.validateURL(ctx, gopt, item.URL, false); err != nil {
			return nil, decoratedConfigErr(cfgPath, hint, err)
		}
		if item.Subdir != "" {
			if err := validateSubdir(item.Subdir, "subdir"); err != nil {
				return nil, decoratedConfigErr(cfgPath, hint, err)
			}
		}
		if item.Follow != "" {
			if err := validateRef("follow", item.Follow); err != nil {
				return nil, decoratedConfigErr(cfgPath, hint, err)
			}
		}
		if item.Pin != "" {
			if isPinCommit(item.Pin) {
				if err := validateObjectName("pin", item.Pin); err != nil {
					return nil, decoratedConfigErr(cfgPath, hint, err)
				}
			} else {
				if err := validateRef("pin", item.Pin); err != nil {
					return nil, decoratedConfigErr(cfgPath, hint, err)
				}
			}
		}
		for _, p := range item.Include {
			if _, err := compileFilter(p, "include="+shlexQuote(p), false); err != nil {
				return nil, decoratedConfigErr(cfgPath, hint, err)
			}
		}
		for _, p := range item.Exclude {
			if _, err := compileFilter(p, "exclude="+shlexQuote(p), false); err != nil {
				return nil, decoratedConfigErr(cfgPath, hint, err)
			}
		}
		items = append(items, item)
	}

	lockPath, _ := a.lockFilePath(ctx, gopt)
	if lockData, err := os.ReadFile(lockPath); err == nil {
		var lock lockFileTOML
		if _, err := toml.Decode(string(lockData), &lock); err != nil {
			return nil, userErrorf("%s: %s", relPath(lockPath), err.Error())
		}
		if lock.Version != lockSchemaVersion {
			return nil, userErrorf(
				"%s: unsupported lockfile version %d (this build expects %d). "+
					"Update git-third-party, or delete the lockfile and re-run "+
					"`git-third-party update` to regenerate it.",
				relPath(lockPath), lock.Version, lockSchemaVersion)
		}
		byDir := map[string]*lockEntryTOML{}
		for i := range lock.ThirdParty {
			le := &lock.ThirdParty[i]
			byDir[le.Dir] = le
		}
		for _, item := range items {
			if le, ok := byDir[item.Dir]; ok {
				if le.Commit != "" {
					if err := validateObjectName("commit", le.Commit); err != nil {
						return nil, userErrorf("%s: dir=%s: %s", relPath(lockPath), shlexQuote(item.Dir), err.Error())
					}
					item.Commit = le.Commit
				}
				if le.TreePatch != "" {
					if err := validateTreePatch(le.TreePatch); err != nil {
						return nil, userErrorf("%s: dir=%s: %s", relPath(lockPath), shlexQuote(item.Dir), err.Error())
					}
					item.TreePatch = le.TreePatch
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return items, nil
}

// findConfigItemByDir loads the config and locates the entry whose dir matches
// the given path. Returns the full items slice along with the matched item and
// its index, so callers that need to mutate-and-resave have everything ready.
func (a *App) findConfigItemByDir(ctx context.Context, gopt GlobalOptions, actualDir string) ([]*ConfigItem, *ConfigItem, int, error) {
	pathInRepo, err := a.actualDirToPathInRepo(ctx, gopt, actualDir, "--dir")
	if err != nil {
		return nil, nil, 0, err
	}
	items, err := a.readConfig(ctx, gopt)
	if err != nil {
		return nil, nil, 0, err
	}
	for i, it := range items {
		if it.Dir == pathInRepo {
			return items, it, i, nil
		}
	}
	return nil, nil, 0, userErrorf("dir is not vendored content: %s\ntip: try \"git-third-party list\"", shlexQuote(pathInRepo))
}

func decoratedConfigErr(path, hint string, err error) error {
	var ue *UserError
	if errors.As(err, &ue) {
		return userErrorf("%s: %s: %s", relPath(path), hint, ue.Msg)
	}
	return userErrorf("%s: %s: %s", relPath(path), hint, err.Error())
}

func relPath(path string) string {
	if r, err := filepath.Rel(".", path); err == nil && r != "" {
		return r
	}
	return path
}

// writeConfigAndLock persists the given full list of items to both files. If
// items is empty, both files are removed (and unstaged from the index).
func (a *App) writeConfigAndLock(ctx context.Context, gopt GlobalOptions, items []*ConfigItem) error {
	cfgPath, err := a.configFilePath(ctx, gopt)
	if err != nil {
		return err
	}
	lockPath, err := a.lockFilePath(ctx, gopt)
	if err != nil {
		return err
	}

	if len(items) == 0 {
		return a.removeAllConfigFiles(ctx, gopt, cfgPath, lockPath)
	}

	cfgBytes := emitConfigTOML(items)
	lockBytes := emitLockTOML(items)

	if gopt.DryRun {
		return a.printConfigDryRun(ctx, gopt, cfgPath, lockPath, cfgBytes, lockBytes)
	}

	if err := a.writeAndStage(ctx, gopt, cfgPath, cfgBytes); err != nil {
		return err
	}
	return a.writeAndStage(ctx, gopt, lockPath, lockBytes)
}

// emitConfigTOML produces the canonical layout of `third-party.toml`.
//
// We hand-write the bytes (rather than calling toml.NewEncoder) so that the
// field order is fixed, lists render on one line, and the file looks the same
// across writes. Comments in the user's existing file are NOT preserved on
// write — that's the documented tradeoff for moving to TOML.
func emitConfigTOML(items []*ConfigItem) []byte {
	var b strings.Builder
	b.WriteString("# git-third-party — vendored content config.\n")
	b.WriteString("# Hand-edited intent. Resolved commits live in ")
	b.WriteString(lockFileName)
	b.WriteString(".\n")
	for _, it := range items {
		b.WriteString("\n[[third_party]]\n")
		writeTOMLString(&b, "dir", it.Dir)
		writeTOMLString(&b, "url", it.URL)
		switch {
		case it.Follow != "":
			writeTOMLString(&b, "follow", it.Follow)
		case it.Pin != "":
			writeTOMLString(&b, "pin", it.Pin)
		}
		if it.Subdir != "" {
			writeTOMLString(&b, "subdir", it.Subdir)
		}
		if len(it.Include) > 0 {
			writeTOMLStringList(&b, "include", it.Include)
		}
		if len(it.Exclude) > 0 {
			writeTOMLStringList(&b, "exclude", it.Exclude)
		}
	}
	return []byte(b.String())
}

func emitLockTOML(items []*ConfigItem) []byte {
	// Sort entries by dir for stable output.
	sorted := slices.Clone(items)
	slices.SortFunc(sorted, func(a, b *ConfigItem) int { return cmp.Compare(a.Dir, b.Dir) })

	var b strings.Builder
	b.WriteString("# git-third-party lockfile — generated; do not edit by hand.\n")
	_, _ = fmt.Fprintf(&b, "version = %d\n", lockSchemaVersion)
	for _, it := range sorted {
		if it.Commit == "" && it.TreePatch == "" {
			continue
		}
		b.WriteString("\n[[third_party]]\n")
		writeTOMLString(&b, "dir", it.Dir)
		if it.Commit != "" {
			writeTOMLString(&b, "commit", it.Commit)
		}
		if it.TreePatch != "" {
			writeTOMLString(&b, "tree-patch", it.TreePatch)
		}
	}
	return []byte(b.String())
}

// writeTOMLString writes one `key = "value"` pair using TOML basic-string
// quoting. It is intentionally narrow — it covers exactly the kinds of values
// this tool stores (no embedded NULs, no control chars beyond \n/\t).
func writeTOMLString(b *strings.Builder, key, val string) {
	b.WriteString(key)
	b.WriteString(" = ")
	writeTOMLBasicString(b, val)
	b.WriteByte('\n')
}

func writeTOMLStringList(b *strings.Builder, key string, vals []string) {
	b.WriteString(key)
	b.WriteString(" = [")
	for i, v := range vals {
		if i > 0 {
			b.WriteString(", ")
		}
		writeTOMLBasicString(b, v)
	}
	b.WriteString("]\n")
}

func writeTOMLBasicString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if c < 0x20 || c == 0x7f {
				_, _ = fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
}

func (a *App) writeAndStage(ctx context.Context, gopt GlobalOptions, path string, data []byte) error {
	if err := os.WriteFile(path, data, 0644); err != nil {
		return err
	}
	rel := relPath(path)
	_, err := a.git(ctx, gopt, []string{"update-index", "--add", rel}, modeMutating, gitOpts{})
	return err
}

func (a *App) unstageAndRemove(ctx context.Context, gopt GlobalOptions, path string) error {
	rel := relPath(path)
	// Best-effort: `update-index --remove` errors if the file isn't tracked,
	// which is fine — we still want to delete the working-tree file.
	_, _ = a.git(ctx, gopt, []string{"update-index", "--remove", rel}, modeMutating, gitOpts{})
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (a *App) removeAllConfigFiles(ctx context.Context, gopt GlobalOptions, cfgPath, lockPath string) error {
	if gopt.DryRun {
		_, _ = fmt.Fprintln(a.Stdout, "")
		_, _ = fmt.Fprintln(a.Stdout, "would delete file: "+relPath(cfgPath))
		_, _ = fmt.Fprintln(a.Stdout, "would delete file: "+relPath(lockPath))
		_, _ = fmt.Fprintln(a.Stdout, "")
		return nil
	}
	if err := a.unstageAndRemove(ctx, gopt, cfgPath); err != nil {
		return err
	}
	return a.unstageAndRemove(ctx, gopt, lockPath)
}

func (a *App) printConfigDryRun(ctx context.Context, gopt GlobalOptions, cfgPath, lockPath string, cfgBytes, lockBytes []byte) error {
	for _, p := range []struct {
		path string
		data []byte
	}{{cfgPath, cfgBytes}, {lockPath, lockBytes}} {
		current, err := os.ReadFile(p.path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if bytes.Equal(current, p.data) {
			continue
		}
		if err == nil {
			r1, err := a.git(ctx, gopt, []string{"hash-object", "-w", relPath(p.path)}, modeSingleLine, gitOpts{})
			if err != nil {
				return err
			}
			r2, err := a.git(ctx, gopt, []string{"hash-object", "-w", "--stdin"}, modeSingleLine, gitOpts{input: p.data})
			if err != nil {
				return err
			}
			if _, err := a.git(ctx, gopt, []string{"diff", r1.line, r2.line}, modeInheritStdout, gitOpts{}); err != nil {
				return err
			}
		} else {
			_, _ = fmt.Fprintln(a.Stdout, "")
			_, _ = fmt.Fprintln(a.Stdout, "would create file: "+relPath(p.path))
			_, _ = fmt.Fprintln(a.Stdout, "with contents:")
			_, _ = fmt.Fprintln(a.Stdout, "")
			_, _ = fmt.Fprint(a.Stdout, string(p.data))
			_, _ = fmt.Fprintln(a.Stdout, "")
		}
	}
	return nil
}
