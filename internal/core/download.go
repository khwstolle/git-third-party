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
