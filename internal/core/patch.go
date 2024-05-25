package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// `patch save` / `patch diff` are gated behind --experimental=patch (or the
// same opt-in via [settings] / GIT_THIRD_PARTY_EXPERIMENTAL / git-config), so
// the recovery hint includes the flag explicitly. Otherwise users
// following the hint hit an "experimental" error first.
const mergeConflictHint = `hint: After resolving the conflicts, mark them with 'git add <path>',
hint: then run 'git-third-party --experimental=patch patch save', then 'git commit'.
hint: To inspect the currently saved patch:
hint:   git-third-party --experimental=patch patch diff
hint: To rollback, 'git reset' and 'git checkout' changes to 'third-party.toml',
hint: 'third-party.lock', and the vendored directory.`

// applyTreePatch implements a 3-way merge between the just-filtered upstream
// tree (base), the upstream-as-of-the-patch tree (old), and the user's saved
// modified tree (new).
//
// Returns the new tree's hash and whether any text conflicts occurred.
func (a *App) applyTreePatch(ctx context.Context, gopt GlobalOptions, baseTreeObjectName, oldTreeObjectName, newTreeObjectName, configItemDir string) (string, bool, error) {
	readTree := func(name string) (map[string]string, error) {
		out := map[string]string{}
		r, err := a.git(ctx, gopt, []string{"ls-tree", "--full-tree", "-r", "-z", name}, modeNullTerminatedLines, gitOpts{})
		if err != nil {
			return nil, err
		}
		for _, line := range r.lines {
			tab := strings.IndexByte(line, '\t')
			if tab < 0 {
				continue
			}
			out[line[tab+1:]] = line[:tab]
		}
		return out, nil
	}
	baseTree, err := readTree(baseTreeObjectName)
	if err != nil {
		return "", false, err
	}
	oldTree, err := readTree(oldTreeObjectName)
	if err != nil {
		return "", false, err
	}
	newTree, err := readTree(newTreeObjectName)
	if err != nil {
		return "", false, err
	}

	var dire []string
	var resolvable []string
	var updated []string

	splitStuff := func(stuff string) (string, string) {
		i := strings.LastIndexByte(stuff, ' ')
		return stuff[:i], stuff[i+1:]
	}

	for name, newStuff := range newTree {
		oldStuff, oldHas := oldTree[name]
		if oldHas && oldStuff == newStuff {
			continue
		}
		if !oldHas {
			if _, ok := baseTree[name]; ok {
				dire = append(dire, "both created: "+shlexQuote(name))
			} else {
				baseTree[name] = newStuff
				updated = append(updated, name)
			}
			continue
		}
		baseStuff, hasBase := baseTree[name]
		if !hasBase {
			dire = append(dire, "patched file no longer exists: "+shlexQuote(name))
			continue
		}
		baseModeType, baseObj := splitStuff(baseStuff)
		oldModeType, oldObj := splitStuff(oldStuff)
		newModeType, newObj := splitStuff(newStuff)
		var useModeType string
		if oldModeType == baseModeType {
			useModeType = newModeType
		} else if newModeType == baseModeType {
			useModeType = newModeType
		} else {
			dire = append(dire, "mode changed: "+shlexQuote(name))
			continue
		}

		baseFile, err := os.CreateTemp("", "gtp-base-*")
		if err != nil {
			return "", false, err
		}
		oldFile, err := os.CreateTemp("", "gtp-old-*")
		if err != nil {
			_ = baseFile.Close()
			_ = os.Remove(baseFile.Name())
			return "", false, err
		}
		newFile, err := os.CreateTemp("", "gtp-new-*")
		if err != nil {
			_ = baseFile.Close()
			_ = oldFile.Close()
			_ = os.Remove(baseFile.Name())
			_ = os.Remove(oldFile.Name())
			return "", false, err
		}
		_ = baseFile.Close()
		_ = oldFile.Close()
		_ = newFile.Close()
		defer func() { _ = os.Remove(baseFile.Name()) }()
		defer func() { _ = os.Remove(oldFile.Name()) }()
		defer func() { _ = os.Remove(newFile.Name()) }()

		if _, err := a.git(ctx, gopt, []string{"cat-file", "blob", baseObj}, modeMutating, gitOpts{outputPath: baseFile.Name()}); err != nil {
			return "", false, err
		}
		if _, err := a.git(ctx, gopt, []string{"cat-file", "blob", oldObj}, modeMutating, gitOpts{outputPath: oldFile.Name()}); err != nil {
			return "", false, err
		}
		if _, err := a.git(ctx, gopt, []string{"cat-file", "blob", newObj}, modeMutating, gitOpts{outputPath: newFile.Name()}); err != nil {
			return "", false, err
		}
		_, mergeErr := a.git(ctx, gopt, []string{
			"merge-file",
			"-L", "<new-upstream>/" + name,
			"-L", "<old-upstream>/" + name,
			"-L", "<patched>/" + name,
			baseFile.Name(), oldFile.Name(), newFile.Name(),
		}, modeInheritStdout, gitOpts{})
		if mergeErr != nil {
			code := gitExitCode(mergeErr)
			if code < 0 {
				return "", false, mergeErr
			}
			resolvable = append(resolvable, filepath.Join(configItemDir, name))
		}
		hashed, err := a.git(ctx, gopt, []string{"hash-object", "-w", baseFile.Name()}, modeSingleLine, gitOpts{})
		if err != nil {
			return "", false, err
		}
		baseTree[name] = useModeType + " " + hashed.line
		updated = append(updated, name)
	}

	for name, oldStuff := range oldTree {
		if _, has := newTree[name]; has {
			continue
		}
		baseStuff, hasBase := baseTree[name]
		if !hasBase || baseStuff != oldStuff {
			dire = append(dire, "file deleted by patch was modified by the upstream (tip: try excluding the file instead of patching it out): "+shlexQuote(name))
			continue
		}
		delete(baseTree, name)
		updated = append(updated, name)
	}

	if len(resolvable) > 0 {
		for _, name := range resolvable {
			_, _ = fmt.Fprintln(a.Stdout, "CONFLICT (git-third-party): Merge conflict in "+shlexQuote(name))
		}
		_, _ = fmt.Fprintln(a.Stdout, mergeConflictHint)
	}
	if len(dire) > 0 {
		slices.Sort(dire)
		var sb strings.Builder
		sb.WriteString("applying tree patch would produce unresolvable conflicts:")
		for _, d := range dire {
			sb.WriteString("\n  ")
			sb.WriteString(d)
		}
		return "", false, userErrorWithCode(ExitConflict, "%s", sb.String())
	}

	type updates struct {
		children map[string]struct{}
	}
	depthToParent := map[int]map[string]*updates{}
	addUpdate := func(depth int, parent, child string) {
		if depthToParent[depth] == nil {
			depthToParent[depth] = map[string]*updates{}
		}
		u, ok := depthToParent[depth][parent]
		if !ok {
			u = &updates{children: map[string]struct{}{}}
			depthToParent[depth][parent] = u
		}
		u.children[child] = struct{}{}
	}
	for _, name := range updated {
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			parent, child := name[:i], name[i+1:]
			addUpdate(strings.Count(parent, "/")+1, parent, child)
		} else {
			addUpdate(0, "", name)
		}
	}

	maxDepth := 0
	for d := range depthToParent {
		if d > maxDepth {
			maxDepth = d
		}
	}
	var rootTree string
	for depth := maxDepth; depth >= 0; depth-- {
		layer := depthToParent[depth]
		if layer == nil {
			continue
		}
		var parents []string
		for p := range layer {
			parents = append(parents, p)
		}
		slices.Sort(parents)
		for _, parent := range parents {
			updatedChildren := layer[parent].children
			var listSrc string
			if depth == 0 {
				listSrc = baseTreeObjectName
			} else {
				listSrc = baseTreeObjectName + ":" + parent
			}
			r, err := a.git(ctx, gopt, []string{"ls-tree", "--full-tree", "-z", listSrc}, modeNullTerminatedLines, gitOpts{})
			if err != nil {
				return "", false, err
			}
			var newLines []string
			for _, line := range r.lines {
				tab := strings.IndexByte(line, '\t')
				if tab < 0 {
					continue
				}
				stuff := line[:tab]
				name := line[tab+1:]
				if _, ok := updatedChildren[name]; !ok {
					newLines = append(newLines, line)
					continue
				}
				delete(updatedChildren, name)
				var fullName string
				if depth > 0 {
					fullName = parent + "/" + name
				} else {
					fullName = name
				}
				updatedStuff, has := baseTree[fullName]
				if !has {
					continue
				}
				newLines = append(newLines, fmt.Sprintf("%s\t%s", updatedStuff, name))
				_ = stuff
			}
			for child := range updatedChildren {
				var fullName string
				if depth > 0 {
					fullName = parent + "/" + child
				} else {
					fullName = child
				}
				updatedStuff, has := baseTree[fullName]
				if !has {
					continue
				}
				newLines = append(newLines, fmt.Sprintf("%s\t%s", updatedStuff, child))
			}
			var input strings.Builder
			for _, l := range newLines {
				input.WriteString(l)
				input.WriteByte(0)
			}
			tn, err := a.git(ctx, gopt, []string{"mktree", "-z"}, modeSingleLine, gitOpts{input: []byte(input.String())})
			if err != nil {
				return "", false, err
			}
			if depth == 0 {
				rootTree = tn.line
			} else {
				baseTree[parent] = "040000 tree " + tn.line
				if i := strings.LastIndexByte(parent, '/'); i >= 0 {
					gp, parentName := parent[:i], parent[i+1:]
					addUpdate(strings.Count(gp, "/")+1, gp, parentName)
				} else {
					addUpdate(0, "", parent)
				}
			}
		}
	}
	if rootTree == "" {
		return "", false, fmt.Errorf("applyTreePatch: no root tree produced")
	}
	return rootTree, len(resolvable) > 0, nil
}
