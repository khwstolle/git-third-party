package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// stageTimer accumulates wall-clock spent in the three logical stages of
// downloadTheThing. Reported at debug level — `-v` is enough to see it.
type stageTimer struct {
	fetch  time.Duration
	filter time.Duration
	apply  time.Duration
}

func (s *stageTimer) report(ctx context.Context, a *App, dir string) {
	a.log().DebugContext(ctx, "downloadTheThing stages",
		"dir", dir,
		"fetch", s.fetch.Truncate(time.Millisecond),
		"filter", s.filter.Truncate(time.Millisecond),
		"apply", s.apply.Truncate(time.Millisecond),
	)
}

// fetchAndSetCommit resolves the configured ref to a commit hash and ensures it
// exists in the local object store. refreshRef controls whether we re-check
// the remote for follow-branch: when false, an already-resolved commit in the
// lockfile is honored and we skip ls-remote (used by save-edits, where
// re-resolving could advance the commit out from under the saved patch base).
func (a *App) fetchAndSetCommit(ctx context.Context, gopt GlobalOptions, item *ConfigItem, sectionIndex int, refreshRef bool) error {
	var resolveRef string
	switch {
	case item.Follow != "":
		switch {
		case !refreshRef && item.Commit != "":
			resolveRef = ""
		case strings.HasPrefix(item.Follow, "refs/"):
			resolveRef = item.Follow
		default:
			resolveRef = "refs/heads/" + item.Follow
		}
	case item.Pin != "":
		if isPinCommit(item.Pin) {
			resolveRef = ""
			item.Commit = item.Pin
		} else if item.Commit != "" {
			resolveRef = ""
		} else if strings.HasPrefix(item.Pin, "refs/") {
			resolveRef = item.Pin
		} else {
			resolveRef = "refs/tags/" + item.Pin
		}
	default:
		return fmt.Errorf("no follow/pin set")
	}
	if resolveRef != "" {
		r, err := a.git(ctx, gopt, []string{"ls-remote", item.URL, resolveRef}, modeNewlineTerminatedLines, gitOpts{})
		if err != nil {
			return err
		}
		if len(r.lines) != 1 {
			if err := a.gitFetchToCache(ctx, gopt, item.URL, resolveRef, sectionIndex); err != nil {
				return err
			}
			return fmt.Errorf("expected ls-remote and fetch to agree on the non-existence of ref: %s", resolveRef)
		}
		tab := strings.IndexByte(r.lines[0], '\t')
		if tab < 0 {
			return fmt.Errorf("malformed ls-remote line: %s", r.lines[0])
		}
		item.Commit = r.lines[0][:tab]
	}
	if a.isGitObjectResolvableAsTree(ctx, gopt, item.Commit) {
		return nil
	}
	return a.gitFetchToCache(ctx, gopt, item.URL, item.Commit, sectionIndex)
}

// downloadTheThing is the main "fetch + filter + maybe-merge + apply"
// sequence. items is the full config slate; idx is the entry being
// processed. The slate is rewritten to disk after the merge step but
// before any work-tree changes.
func (a *App) downloadTheThing(ctx context.Context, gopt GlobalOptions, items []*ConfigItem, idx int) (entryResult, error) {
	item := items[idx]
	sectionIndex := idx
	var stages stageTimer
	defer stages.report(ctx, a, item.Dir)

	res := entryResult{
		Dir:        item.Dir,
		URL:        item.URL,
		FromCommit: item.Commit,
		DryRun:     gopt.DryRun,
	}

	t0 := time.Now()
	if err := a.fetchAndSetCommit(ctx, gopt, item, sectionIndex, true); err != nil {
		return res, err
	}
	res.ToCommit = item.Commit
	stages.fetch = time.Since(t0)

	t0 = time.Now()
	commitTree, err := a.getGitCommitTreeObjectName(ctx, gopt, item.Commit)
	if err != nil {
		return res, err
	}
	vendoredTree, err := a.filterTree(ctx, gopt, commitTree, item.Subdir, item.Include, item.Exclude, sectionIndex)
	if err != nil {
		return res, err
	}

	if item.TreePatch != "" {
		segs := strings.Split(item.TreePatch, ":")
		if len(segs) != 3 {
			return res, userErrorf("invalid tree-patch: %s", item.TreePatch)
		}
		oldCommit := segs[0]
		oldTreeName := segs[1]
		newTreeName := segs[2]
		if strings.HasSuffix(newTreeName, "-conflicts") {
			return res, userErrorf("unresolved merge conflicts in: %s\n%s",
				shlexQuote(item.Dir), mergeConflictHint)
		}
		if vendoredTree == oldTreeName {
			vendoredTree = newTreeName
		} else {
			if !a.isGitObjectResolvableAsTree(ctx, gopt, oldTreeName) {
				if !a.isGitObjectResolvableAsTree(ctx, gopt, oldCommit) {
					if err := a.gitFetchToCache(ctx, gopt, item.URL, oldCommit, sectionIndex); err != nil {
						return res, err
					}
				}
				oldCommitTree, err := a.getGitCommitTreeObjectName(ctx, gopt, oldCommit)
				if err != nil {
					return res, err
				}
				recreatedOld, err := a.filterTree(ctx, gopt, oldCommitTree, item.Subdir, item.Include, item.Exclude, sectionIndex)
				if err != nil {
					return res, err
				}
				if recreatedOld != oldTreeName {
					return res, userErrorf("Something changed in the config files, and `git gc` has deleted an object critical for patching.\nPlease revert your changes to `third-party.toml` and `third-party.lock`, and run `git-third-party status` to recover the deleted object.\nThen redo your edits, and try again.")
				}
			}
			prePatch := vendoredTree
			merged, conflicts, err := a.applyTreePatch(ctx, gopt, vendoredTree, oldTreeName, newTreeName, item.Dir)
			if err != nil {
				return res, err
			}
			vendoredTree = merged
			if prePatch == vendoredTree {
				if conflicts {
					return res, fmt.Errorf("found text conflicts but merge produced base tree")
				}
				item.TreePatch = ""
			} else {
				suffix := ""
				if conflicts {
					suffix = "-conflicts"
				}
				item.TreePatch = fmt.Sprintf("%s:%s:%s%s", item.Commit, prePatch, vendoredTree, suffix)
				res.TreePatch = item.TreePatch
				res.Conflicts = conflicts
			}
		}
	}

	stages.filter = time.Since(t0)

	t0 = time.Now()
	if err := a.writeConfigAndLock(ctx, gopt, items); err != nil {
		return res, err
	}
	currentTree, err := a.git(ctx, gopt, []string{"write-tree"}, modeSingleLine, gitOpts{})
	if err != nil {
		return res, err
	}
	newComplete, err := a.insertTreeAtPath(ctx, gopt, currentTree.line, vendoredTree, item.Dir)
	if err != nil {
		return res, err
	}
	indexCorrect := currentTree.line == newComplete
	statusLines, err := a.gitStatusFor(ctx, gopt, item.Dir)
	if err != nil {
		return res, err
	}
	clean := len(statusLines) == 0
	if indexCorrect && clean {
		stages.apply = time.Since(t0)
		res.Action = "up-to-date"
		if !gopt.DryRun && !gopt.JSONOut {
			_, _ = fmt.Fprintf(a.Stdout, "%s: Already up to date.\n", item.Dir)
		}
		return res, nil
	}
	if gopt.DryRun {
		switch {
		case !indexCorrect:
			res.Action = "would-update"
		case !clean:
			res.Action = "would-discard"
		}
		if res.Conflicts {
			res.Action = "conflicts"
		}
		if !gopt.JSONOut {
			_, _ = fmt.Fprintln(a.Stdout, "")
			switch res.Action {
			case "would-update":
				_, _ = fmt.Fprintln(a.Stdout, "changes to be applied to: "+item.Dir)
			case "would-discard":
				_, _ = fmt.Fprintln(a.Stdout, "would discard local modifications to: "+item.Dir)
			case "conflicts":
				_, _ = fmt.Fprintln(a.Stdout, "would-be patch conflicts in: "+item.Dir)
			}
			_, err := a.git(ctx, gopt, []string{"diff", "--shortstat", currentTree.line, newComplete}, modeInheritStdout, gitOpts{})
			stages.apply = time.Since(t0)
			return res, err
		}
		stages.apply = time.Since(t0)
		return res, nil
	}
	if _, err := a.git(ctx, gopt, []string{"read-tree", newComplete}, modeMutating, gitOpts{}); err != nil {
		return res, err
	}
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return res, err
	}
	actualDir := filepath.Join(root, item.Dir)
	_ = os.MkdirAll(actualDir, 0755)
	restoreArgs := []string{"--work-tree", actualDir, "restore", "--source", vendoredTree, "."}
	if !item.LFS {
		// Vendor LFS pointer files rather than downloading the large objects.
		restoreArgs = append([]string{"-c", "filter.lfs.required=false", "-c", "filter.lfs.smudge=cat", "-c", "filter.lfs.process="}, restoreArgs...)
	}
	if _, err := a.git(ctx, gopt, restoreArgs, modeMutating, gitOpts{}); err != nil {
		return res, err
	}
	for {
		// Pathspec is repo-relative; absolute paths can be misinterpreted
		// (especially on Windows where `C:\...` collides with the colon-
		// separated pathspec syntax). Run from the repo root with
		// `item.Dir` as the relative pathspec, after a `--` to suppress
		// any chance of flag-vs-path ambiguity.
		r, err := a.git(ctx, gopt, []string{"clean", "-ffd", "--", item.Dir}, modeNewlineTerminatedLines, gitOpts{cwd: root})
		if err != nil {
			return res, err
		}
		if len(r.lines) == 0 {
			break
		}
	}
	if !item.LFS {
		if err := a.stripVendoredLFSAttributes(ctx, gopt, item.Dir); err != nil {
			return res, err
		}
		if err := a.ensureLFSExclusion(ctx, gopt, item.Dir); err != nil {
			return res, err
		}
	} else {
		if err := a.removeLFSExclusion(ctx, gopt, item.Dir); err != nil {
			return res, err
		}
	}
	statusAfter, err := a.gitStatusFor(ctx, gopt, item.Dir)
	if err != nil {
		return res, err
	}
	if len(statusAfter) > 0 {
		res.Action = "updated"
		if res.Conflicts {
			res.Action = "conflicts"
		}
		if !gopt.JSONOut {
			_, _ = fmt.Fprintln(a.Stdout, "Changes staged to be committed:")
			_, _ = a.git(ctx, gopt, []string{"diff", "--cached", "--shortstat"}, modeInheritStdout, gitOpts{})
			_, _ = fmt.Fprintln(a.Stdout, "")
			_, _ = fmt.Fprintln(a.Stdout, `Use "git commit" to proceed with the commit.`)
		}
	} else {
		res.Action = "updated"
		if !gopt.JSONOut {
			_, _ = fmt.Fprintf(a.Stdout, "%s: Local modifications discarded\n", item.Dir)
		}
	}
	stages.apply = time.Since(t0)
	return res, nil
}

const (
	gitattributesFile    = ".gitattributes"
	lfsExcludeBegin      = "# git-third-party: begin lfs-exclude"
	lfsExcludeEnd        = "# git-third-party: end lfs-exclude"
	lfsExcludeLineFormat = "%s/** -filter -diff -merge -text"
)

// ensureLFSExclusion adds dir to the git-third-party-managed lfs-exclude
// section of .gitattributes and stages the file. This ensures the exclusion
// is in the same commit as the vendored content, so server-side LFS hooks
// don't mistake pointer files for tracked LFS objects.
func (a *App) ensureLFSExclusion(ctx context.Context, gopt GlobalOptions, dir string) error {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return err
	}
	gaPath := filepath.Join(root, gitattributesFile)
	entry := fmt.Sprintf(lfsExcludeLineFormat, dir)

	data, err := os.ReadFile(gaPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	newContent, err := setLFSExcludeEntry(content, entry, true)
	if err != nil {
		return err
	}
	if newContent == content {
		return nil
	}
	return a.writeAndStage(ctx, gopt, gaPath, []byte(newContent))
}

// removeLFSExclusion removes dir from the git-third-party lfs-exclude section.
func (a *App) removeLFSExclusion(ctx context.Context, gopt GlobalOptions, dir string) error {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return err
	}
	gaPath := filepath.Join(root, gitattributesFile)
	entry := fmt.Sprintf(lfsExcludeLineFormat, dir)

	data, err := os.ReadFile(gaPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content := string(data)
	newContent, err := setLFSExcludeEntry(content, entry, false)
	if err != nil {
		return err
	}
	if newContent == content {
		return nil
	}
	if newContent == "" {
		if _, err := a.git(ctx, gopt, []string{"update-index", "--remove", relPath(gaPath)}, modeMutating, gitOpts{}); err != nil {
			return err
		}
		if err := os.Remove(gaPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return a.writeAndStage(ctx, gopt, gaPath, []byte(newContent))
}

// setLFSExcludeEntry adds or removes entry from the managed section,
// returning the updated file content. Returns the original string if unchanged.
// Returns an error if the begin marker is present without a matching end marker.
func setLFSExcludeEntry(content, entry string, add bool) (string, error) {
	// Locate the managed section.
	beginIdx := strings.Index(content, lfsExcludeBegin)
	endIdx := strings.Index(content, lfsExcludeEnd)

	if beginIdx >= 0 && (endIdx < 0 || endIdx < beginIdx) {
		return content, fmt.Errorf(
			".gitattributes: %q found without matching %q — the managed section may be corrupt; fix or delete it and re-run",
			lfsExcludeBegin, lfsExcludeEnd)
	}

	var before, section, after string
	if beginIdx >= 0 && endIdx > beginIdx {
		before = content[:beginIdx]
		section = content[beginIdx+len(lfsExcludeBegin) : endIdx]
		after = content[endIdx+len(lfsExcludeEnd):]
	} else {
		before = content
	}

	lines := strings.Split(strings.TrimSpace(section), "\n")
	var kept []string
	found := false
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if l == entry {
			found = true
			if add {
				kept = append(kept, l)
			}
		} else {
			kept = append(kept, l)
		}
	}
	if add && !found {
		kept = append(kept, entry)
	}

	if len(kept) == 0 && beginIdx < 0 {
		return content, nil // nothing to remove, no section existed
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(before, "\n"))
	if len(kept) > 0 {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(lfsExcludeBegin)
		b.WriteByte('\n')
		for _, l := range kept {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		b.WriteString(lfsExcludeEnd)
		b.WriteByte('\n')
	}
	if after != "" {
		b.WriteString(strings.TrimLeft(after, "\n"))
	}
	return b.String(), nil
}

// stripVendoredLFSAttributes removes LFS filter lines from any .gitattributes
// files within the vendored directory. Vendored repos (e.g. from HuggingFace)
// often include .gitattributes that re-enable LFS, which overrides the
// root-level exclusion added by ensureLFSExclusion.
func (a *App) stripVendoredLFSAttributes(ctx context.Context, gopt GlobalOptions, dir string) error {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return err
	}
	absDir := filepath.Join(root, dir)
	return filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || d.Name() != gitattributesFile {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		stripped := stripLFSLines(string(data))
		if stripped == string(data) {
			return nil
		}
		rel := relPath(path)
		if strings.TrimSpace(stripped) == "" {
			if _, err := a.git(ctx, gopt, []string{"update-index", "--remove", rel}, modeMutating, gitOpts{}); err != nil {
				return err
			}
			return os.Remove(path)
		}
		return a.writeAndStage(ctx, gopt, path, []byte(stripped))
	})
}

// stripLFSLines removes gitattributes lines that set filter=lfs, leaving
// comment lines and all other rules intact.
func stripLFSLines(content string) string {
	lines := strings.Split(content, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "#") || !strings.Contains(l, "filter=lfs") {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}
