package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func (a *App) add(ctx context.Context, gopt GlobalOptions, url, follow, pin, dir, subdir string, include, exclude []string, allowDirExists bool) error {
	item := &ConfigItem{}
	d, err := a.actualDirToPathInRepo(ctx, gopt, dir, "--dir")
	if err != nil {
		return err
	}
	item.Dir = d
	if err := validateDir(item.Dir, "--dir"); err != nil {
		return err
	}
	existing, err := a.readConfig(ctx, gopt)
	if err != nil {
		return err
	}
	if err := validateNoPrimaryKeyConflicts(item.Dir, "--dir", existing); err != nil {
		return err
	}
	if !allowDirExists {
		if err := a.validateDirNotExists(ctx, gopt, item.Dir, "--dir"); err != nil {
			return err
		}
	}
	if err := a.validateURL(ctx, gopt, url, true); err != nil {
		return err
	}
	item.URL = url
	if subdir != "" {
		s, err := canonicalizeRelativePath(subdir, "--subdir")
		if err != nil {
			return err
		}
		item.Subdir = s
		if err := validateSubdir(item.Subdir, "--subdir"); err != nil {
			return err
		}
	}
	switch {
	case follow != "":
		if err := validateRef("--follow", follow); err != nil {
			return err
		}
		item.Follow = follow
	case pin != "":
		if isPinCommit(pin) {
			if err := validateObjectName("--pin", pin); err != nil {
				return err
			}
		} else {
			if err := validateRef("--pin", pin); err != nil {
				return err
			}
		}
		item.Pin = pin
	default:
		branch, err := a.resolveHeadBranch(ctx, gopt, url)
		if err != nil {
			return err
		}
		item.Follow = branch
	}
	if len(include) > 0 {
		item.Include = include
	}
	if len(exclude) > 0 {
		item.Exclude = exclude
	}
	items := append([]*ConfigItem(nil), existing...)
	items = append(items, item)
	res, err := a.downloadTheThing(ctx, gopt, items, len(items)-1)
	if err != nil {
		return err
	}
	// `add` always reports "added" — the downstream "updated" / "up-to-date"
	// distinctions don't apply when we just registered a new entry.
	res.Action = "added"
	if err := a.maybeAutoCommit(ctx, gopt, res); err != nil {
		return err
	}
	return a.emitSingleResult(ctx, gopt, res)
}

func (a *App) emitSingleResult(ctx context.Context, gopt GlobalOptions, r entryResult) error {
	if gopt.JSONOut {
		return emitJSONResults(a.Stdout, []entryResult{r})
	}
	return nil
}

// set: subdirOpt/includeOpt/excludeOpt are nil when not specified, non-nil to replace.
func (a *App) set(ctx context.Context, gopt GlobalOptions, url, follow, pin, dir string, subdirOpt *string, includeOpt, excludeOpt *[]string) error {
	items, item, idx, err := a.findConfigItemByDir(ctx, gopt, dir)
	if err != nil {
		return err
	}
	needsFetch := false
	if url != "" {
		if err := a.validateURL(ctx, gopt, url, true); err != nil {
			return err
		}
		if item.URL != url {
			needsFetch = true
		}
		item.URL = url
	}
	if follow != "" {
		if err := validateRef("--follow", follow); err != nil {
			return err
		}
		if item.Follow != follow {
			needsFetch = true
		}
		item.Follow = follow
		item.Pin = ""
	}
	if pin != "" {
		if isPinCommit(pin) {
			if err := validateObjectName("--pin", pin); err != nil {
				return err
			}
		} else {
			if err := validateRef("--pin", pin); err != nil {
				return err
			}
		}
		if item.Pin != pin {
			needsFetch = true
		}
		item.Follow = ""
		item.Pin = pin
	}
	if subdirOpt != nil {
		s, err := canonicalizeRelativePath(*subdirOpt, "--subdir")
		if err != nil {
			return err
		}
		item.Subdir = s
		if err := validateSubdir(item.Subdir, "--subdir"); err != nil {
			return err
		}
	}
	if includeOpt != nil {
		item.Include = *includeOpt
	}
	if excludeOpt != nil {
		item.Exclude = *excludeOpt
	}
	if needsFetch {
		item.Commit = ""
	}
	res, err := a.downloadTheThing(ctx, gopt, items, idx)
	if err != nil {
		return err
	}
	if err := a.cleanupRefs(ctx, gopt, []int{idx}, false); err != nil {
		return err
	}
	if err := a.maybeAutoCommit(ctx, gopt, res); err != nil {
		return err
	}
	return a.emitSingleResult(ctx, gopt, res)
}

func (a *App) unset(ctx context.Context, gopt GlobalOptions, dir string, fields []string) error {
	items, item, idx, err := a.findConfigItemByDir(ctx, gopt, dir)
	if err != nil {
		return err
	}
	for _, f := range fields {
		switch strings.ToLower(strings.TrimSpace(f)) {
		case "subdir":
			item.Subdir = ""
		case "include":
			item.Include = nil
		case "exclude":
			item.Exclude = nil
		case "url", "follow", "pin", "dir":
			return userErrorf("%q cannot be unset (it is required or replaced via `set`)", f)
		default:
			return userErrorf("unknown field %q (clearable: subdir, include, exclude)", f)
		}
	}
	res, err := a.downloadTheThing(ctx, gopt, items, idx)
	if err != nil {
		return err
	}
	if err := a.cleanupRefs(ctx, gopt, []int{idx}, false); err != nil {
		return err
	}
	if err := a.maybeAutoCommit(ctx, gopt, res); err != nil {
		return err
	}
	return a.emitSingleResult(ctx, gopt, res)
}

// update re-fetches one entry (when maybeDir != "") or all. --check always
// implies --dry-run so CI gates can never mutate the repo as a side effect.
func (a *App) update(ctx context.Context, gopt GlobalOptions, maybeDir string, check bool) error {
	if check {
		gopt.DryRun = true
	}
	var items []*ConfigItem
	var idxs []int
	visitedAll := false
	if maybeDir != "" {
		all, _, idx, err := a.findConfigItemByDir(ctx, gopt, maybeDir)
		if err != nil {
			return err
		}
		items = all
		idxs = []int{idx}
	} else {
		all, err := a.readConfig(ctx, gopt)
		if err != nil {
			return err
		}
		items = all
		for i := range all {
			idxs = append(idxs, i)
		}
		visitedAll = true
	}
	results := make([]entryResult, 0, len(idxs))
	anyChanged := false
	for _, i := range idxs {
		res, err := a.downloadTheThing(ctx, gopt, items, i)
		if err != nil {
			return err
		}
		if res.changed() {
			anyChanged = true
		}
		results = append(results, res)
	}
	if err := a.cleanupRefs(ctx, gopt, idxs, visitedAll); err != nil {
		return err
	}
	// Single auto-commit covering all updates in this run; we don't want
	// one commit per entry.
	if anyChanged && gopt.CommitMsg != "" && !gopt.DryRun {
		if _, err := a.git(ctx, gopt, []string{"commit", "-q", "-m", gopt.CommitMsg}, modeMutating, gitOpts{}); err != nil {
			return err
		}
	}
	if gopt.JSONOut {
		if err := emitJSONResults(a.Stdout, results); err != nil {
			return err
		}
	}
	if check && anyChanged {
		return errCheckMismatch
	}
	return nil
}

// errCheckMismatch is the sentinel for --check detecting a pending change; main() maps it to ExitCheckDirty (exit code 5) silently.
var errCheckMismatch = &checkErr{}

type checkErr struct{}

func (*checkErr) Error() string { return "check: working tree differs from lockfile state" }

func (a *App) list(ctx context.Context, gopt GlobalOptions, maybeDir string, jsonOut bool) error {
	var items []*ConfigItem
	if maybeDir != "" {
		_, it, _, err := a.findConfigItemByDir(ctx, gopt, maybeDir)
		if err != nil {
			return err
		}
		items = []*ConfigItem{it}
	} else {
		all, err := a.readConfig(ctx, gopt)
		if err != nil {
			return err
		}
		items = all
	}
	if jsonOut {
		return a.emitListJSON(ctx, gopt, items)
	}
	for _, it := range items {
		var ann []string
		switch {
		case it.Follow != "":
			ann = append(ann, "follow="+it.Follow)
		case it.Pin != "":
			if isPinCommit(it.Pin) {
				r, err := a.git(ctx, gopt, []string{"rev-parse", "--short", it.Pin}, modeSingleLine, gitOpts{})
				if err != nil {
					return err
				}
				ann = append(ann, "pin="+r.line)
			} else {
				ann = append(ann, "pin="+it.Pin)
			}
		default:
			return fmt.Errorf("invalid config item: %s", it.Dir)
		}
		if it.TreePatch != "" {
			if strings.HasSuffix(it.TreePatch, "-conflicts") {
				ann = append(ann, "patched and has merge conflicts")
			} else {
				ann = append(ann, "patched")
			}
		}
		_, _ = fmt.Fprintf(a.Stdout, "%s (%s)\n", it.Dir, strings.Join(ann, ", "))
	}
	return nil
}

// listEntry is the JSON shape produced by `list --json`.
type listEntry struct {
	Dir       string   `json:"dir"`
	URL       string   `json:"url"`
	Follow    string   `json:"follow,omitempty"`
	Pin       string   `json:"pin,omitempty"`
	Subdir    string   `json:"subdir,omitempty"`
	Include   []string `json:"include,omitempty"`
	Exclude   []string `json:"exclude,omitempty"`
	Commit    string   `json:"commit,omitempty"`
	Patched   bool     `json:"patched,omitempty"`
	Conflicts bool     `json:"conflicts,omitempty"`
}

func (a *App) emitListJSON(ctx context.Context, gopt GlobalOptions, items []*ConfigItem) error {
	out := make([]listEntry, 0, len(items))
	for _, it := range items {
		e := listEntry{
			Dir:     it.Dir,
			URL:     it.URL,
			Follow:  it.Follow,
			Pin:     it.Pin,
			Subdir:  it.Subdir,
			Include: it.Include,
			Exclude: it.Exclude,
			Commit:  it.Commit,
		}
		if it.TreePatch != "" {
			e.Patched = true
			e.Conflicts = strings.HasSuffix(it.TreePatch, "-conflicts")
		}
		out = append(out, e)
	}
	enc := json.NewEncoder(a.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func (a *App) remove(ctx context.Context, gopt GlobalOptions, actualDir string) error {
	items, removed, idx, err := a.findConfigItemByDir(ctx, gopt, actualDir)
	if err != nil {
		return err
	}
	res := entryResult{Dir: removed.Dir, URL: removed.URL, FromCommit: removed.Commit, DryRun: gopt.DryRun}
	items = append(items[:idx], items[idx+1:]...)
	if err := a.writeConfigAndLock(ctx, gopt, items); err != nil {
		return err
	}
	if gopt.DryRun {
		res.Action = "would-update" // would-remove, but we keep the action vocabulary minimal
		if !gopt.JSONOut {
			_, _ = fmt.Fprintln(a.Stdout, "")
			_, _ = fmt.Fprintln(a.Stdout, "would delete tree: "+filepath.Clean(actualDir))
			_, _ = fmt.Fprintln(a.Stdout, "")
		}
		return a.emitSingleResult(ctx, gopt, res)
	}
	if _, err := a.git(ctx, gopt, []string{"rm", "-r", "-q", "--force", "--", actualDir}, modeMutating, gitOpts{}); err != nil {
		return err
	}
	res.Action = "removed"
	if !gopt.JSONOut {
		_, _ = fmt.Fprintln(a.Stdout, "Changes staged to be committed:")
		_, _ = a.git(ctx, gopt, []string{"diff", "--cached", "--shortstat"}, modeInheritStdout, gitOpts{})
		_, _ = fmt.Fprintln(a.Stdout, "")
		_, _ = fmt.Fprintln(a.Stdout, `Use "git commit" to proceed with the commit.`)
	}
	if err := a.maybeAutoCommit(ctx, gopt, res); err != nil {
		return err
	}
	return a.emitSingleResult(ctx, gopt, res)
}

func (a *App) rename(ctx context.Context, gopt GlobalOptions, actualDir, actualNewDir string, allowDirExists bool) error {
	items, item, _, err := a.findConfigItemByDir(ctx, gopt, actualDir)
	if err != nil {
		return err
	}
	canon, err := a.actualDirToPathInRepo(ctx, gopt, actualNewDir, "--new-dir")
	if err != nil {
		return err
	}
	if item.Dir == canon {
		return userErrorf("cannot rename something to itself: %s", shlexQuote(canon))
	}
	// Build a "neighbors" view that excludes the item being renamed, then
	// check that no other entry would conflict with the new dir.
	others := make([]*ConfigItem, 0, len(items)-1)
	for _, it := range items {
		if it != item {
			others = append(others, it)
		}
	}
	if err := validateNoPrimaryKeyConflicts(canon, "--new-dir", others); err != nil {
		return err
	}
	item.Dir = canon
	if err := validateDir(item.Dir, "--new-dir"); err != nil {
		return err
	}
	if !allowDirExists {
		if err := a.validateDirNotExists(ctx, gopt, item.Dir, "--new-dir"); err != nil {
			return err
		}
	}
	if err := a.writeConfigAndLock(ctx, gopt, items); err != nil {
		return err
	}
	cleanNew := filepath.Clean(actualNewDir)
	res := entryResult{
		Dir:    item.Dir,
		URL:    item.URL,
		NewDir: item.Dir,
		DryRun: gopt.DryRun,
	}
	if gopt.DryRun {
		res.Action = "would-update"
		if !gopt.JSONOut {
			_, _ = fmt.Fprintln(a.Stdout, "")
			_, _ = fmt.Fprintf(a.Stdout, "would move tree: %s -> %s\n", shlexQuote(filepath.Clean(actualDir)), shlexQuote(cleanNew))
			_, _ = fmt.Fprintln(a.Stdout, "")
		}
		return a.emitSingleResult(ctx, gopt, res)
	}
	_ = os.MkdirAll(filepath.Dir(cleanNew), 0755)
	if _, err := a.git(ctx, gopt, []string{"mv", filepath.Clean(actualDir), cleanNew}, modeMutating, gitOpts{}); err != nil {
		return err
	}
	res.Action = "renamed"
	if !gopt.JSONOut {
		_, _ = fmt.Fprintln(a.Stdout, "Changes staged to be committed:")
		_, _ = a.git(ctx, gopt, []string{"diff", "--cached", "--shortstat"}, modeInheritStdout, gitOpts{})
		_, _ = fmt.Fprintln(a.Stdout, "")
		_, _ = fmt.Fprintln(a.Stdout, `Use "git commit" to proceed with the commit.`)
	}
	if err := a.maybeAutoCommit(ctx, gopt, res); err != nil {
		return err
	}
	return a.emitSingleResult(ctx, gopt, res)
}

func (a *App) savePatch(ctx context.Context, gopt GlobalOptions, actualDir string) error {
	items, item, idx, err := a.findConfigItemByDir(ctx, gopt, actualDir)
	if err != nil {
		return err
	}
	dirty, err := a.gitStatusFor(ctx, gopt, item.Dir)
	if err != nil {
		return err
	}
	for _, line := range dirty {
		if len(line) > 1 && line[1] != ' ' {
			return userErrorf("There are unstaged changes in: %s\nUse \"git add\" to stage the changes, then re-run this command.", shlexQuote(actualDir))
		}
	}
	r, err := a.git(ctx, gopt, []string{"write-tree", "--prefix", item.Dir}, modeSingleLine, gitOpts{})
	if err != nil {
		return err
	}
	newTree := r.line
	if err := a.fetchAndSetCommit(ctx, gopt, item, idx, false); err != nil {
		return err
	}
	commitTree, err := a.getGitCommitTreeObjectName(ctx, gopt, item.Commit)
	if err != nil {
		return err
	}
	oldTree, err := a.filterTree(ctx, gopt, commitTree, item.Subdir, item.Include, item.Exclude, idx)
	if err != nil {
		return err
	}
	newPatch := fmt.Sprintf("%s:%s:%s", item.Commit, oldTree, newTree)
	res := entryResult{
		Dir:        item.Dir,
		URL:        item.URL,
		FromCommit: item.Commit,
		DryRun:     gopt.DryRun,
	}
	if item.TreePatch == newPatch {
		res.Action = "up-to-date"
		res.TreePatch = newPatch
		if !gopt.JSONOut {
			_, _ = fmt.Fprintln(a.Stdout, "Already up to date.")
		}
		return a.emitSingleResult(ctx, gopt, res)
	}
	item.TreePatch = newPatch
	res.TreePatch = newPatch
	res.Action = "saved"
	if err := a.writeConfigAndLock(ctx, gopt, items); err != nil {
		return err
	}
	if !gopt.JSONOut {
		if gopt.DryRun {
			_, _ = fmt.Fprintln(a.Stdout, "")
		}
		_, _ = fmt.Fprintln(a.Stdout, "Changes staged to be committed:")
		_, _ = a.git(ctx, gopt, []string{"diff", "--cached", "--shortstat"}, modeInheritStdout, gitOpts{})
		_, _ = fmt.Fprintln(a.Stdout, "")
		if !gopt.DryRun {
			_, _ = fmt.Fprintln(a.Stdout, `Use "git commit" to proceed with the commit.`)
		}
	}
	if err := a.maybeAutoCommit(ctx, gopt, res); err != nil {
		return err
	}
	return a.emitSingleResult(ctx, gopt, res)
}

func (a *App) diffPatch(ctx context.Context, gopt GlobalOptions, actualDir string) error {
	_, item, idx, err := a.findConfigItemByDir(ctx, gopt, actualDir)
	if err != nil {
		return err
	}
	res := entryResult{Dir: item.Dir, URL: item.URL, FromCommit: item.Commit, DryRun: gopt.DryRun}
	if item.TreePatch == "" {
		res.Action = "up-to-date"
		return a.emitSingleResult(ctx, gopt, res)
	}
	parts := strings.SplitN(item.TreePatch, ":", 3)
	if len(parts) != 3 {
		return userErrorf("invalid tree-patch: %s", item.TreePatch)
	}
	oldCommit, oldTree, newTree := parts[0], parts[1], parts[2]
	if strings.HasSuffix(newTree, "-conflicts") {
		newTree = newTree[:len(newTree)-len("-conflicts")]
		res.Conflicts = true
	}
	res.TreePatch = item.TreePatch
	if !a.isGitObjectResolvableAsTree(ctx, gopt, oldTree) {
		if err := a.gitFetchToCache(ctx, gopt, item.URL, oldCommit, idx); err != nil {
			return err
		}
	}
	cmd := []string{"git", "diff", oldTree, newTree}
	switch {
	case gopt.JSONOut:
		// Capture the diff as a JSON field rather than streaming through
		// the user's pager.
		out, err := a.git(ctx, gopt, cmd[1:], modeRawBytes, gitOpts{})
		if err != nil {
			return err
		}
		res.Action = "patched"
		res.Diff = string(out.bytes)
		return a.emitSingleResult(ctx, gopt, res)
	case gopt.DryRun:
		_, _ = fmt.Fprintln(a.Stdout, shellJoin(cmd))
		return nil
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return err
	}
	return syscall.Exec(gitPath, cmd, os.Environ())
}

// info: --json emits the same listEntry shape as `list --json`, so consumers share one schema.
func (a *App) info(ctx context.Context, gopt GlobalOptions, actualDir string) error {
	_, item, _, err := a.findConfigItemByDir(ctx, gopt, actualDir)
	if err != nil {
		return err
	}
	if gopt.JSONOut {
		return a.emitListJSON(ctx, gopt, []*ConfigItem{item})
	}
	w := a.Stdout
	_, _ = fmt.Fprintf(w, "dir            %s\n", item.Dir)
	_, _ = fmt.Fprintf(w, "url            %s\n", item.URL)
	switch {
	case item.Follow != "":
		_, _ = fmt.Fprintf(w, "follow         %s\n", item.Follow)
	case item.Pin != "":
		_, _ = fmt.Fprintf(w, "pin            %s\n", item.Pin)
	}
	if item.Subdir != "" {
		_, _ = fmt.Fprintf(w, "subdir         %s\n", item.Subdir)
	}
	for _, p := range item.Include {
		_, _ = fmt.Fprintf(w, "include        %s\n", p)
	}
	for _, p := range item.Exclude {
		_, _ = fmt.Fprintf(w, "exclude        %s\n", p)
	}
	if item.Commit != "" {
		_, _ = fmt.Fprintf(w, "commit         %s\n", item.Commit)
	}
	if item.TreePatch != "" {
		_, _ = fmt.Fprintf(w, "tree-patch     %s\n", item.TreePatch)
	}
	return nil
}

// init creates an empty third-party.toml at the repo root. Useful for
// "scaffold the repo first, vendor later" flows; `add` creates it implicitly.
func (a *App) init(ctx context.Context, gopt GlobalOptions) error {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return err
	}
	path := filepath.Join(root, configFileName)
	if _, err := os.Stat(path); err == nil {
		if !gopt.JSONOut {
			_, _ = fmt.Fprintln(a.Stdout, configFileName+": already exists")
		}
		return a.emitSingleResult(ctx, gopt, entryResult{Action: "up-to-date"})
	} else if !os.IsNotExist(err) {
		return err
	}
	body := "# git-third-party — vendored content config.\n# Hand-edited intent. Resolved commits live in " + lockFileName + ".\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		return err
	}
	if !gopt.JSONOut {
		_, _ = fmt.Fprintln(a.Stdout, "created "+configFileName)
	}
	return a.emitSingleResult(ctx, gopt, entryResult{Action: "added"})
}

// maybeAutoCommit runs `git commit -m <msg>` if --commit was provided,
// the invocation isn't a dry-run, and the result represents an actual
// mutation (so we don't try to commit a no-op and surface "nothing to
// commit" as an error).
func (a *App) maybeAutoCommit(ctx context.Context, gopt GlobalOptions, r entryResult) error {
	if gopt.CommitMsg == "" || gopt.DryRun || !r.changed() {
		return nil
	}
	_, err := a.git(ctx, gopt, []string{"commit", "-q", "-m", gopt.CommitMsg}, modeMutating, gitOpts{})
	return err
}

// resolveHeadBranch asks the remote for its HEAD symref. Used by `add` when
// neither --follow nor --pin is given.
func (a *App) resolveHeadBranch(ctx context.Context, gopt GlobalOptions, url string) (string, error) {
	r, err := a.git(ctx, gopt, []string{"ls-remote", "--symref", url, "HEAD"}, modeNewlineTerminatedLines, gitOpts{})
	if err != nil {
		return "", err
	}
	for _, line := range r.lines {
		tab := strings.IndexByte(line, '\t')
		if tab < 0 {
			continue
		}
		value, name := line[:tab], line[tab+1:]
		if name != "HEAD" || !strings.HasPrefix(value, "ref: ") {
			continue
		}
		ref := strings.TrimPrefix(value, "ref: ")
		ref = strings.TrimPrefix(ref, "refs/heads/")
		return ref, nil
	}
	return "", userErrorf("no --follow or --pin given, and remote does not have a HEAD branch: %s", shlexQuote(url))
}
