package core

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// pathRelativeToSubmoduleRoot returns the portion of fullPath that lies inside
// the submodule whose root is at depth `submoduleRootDepth` from the upstream
// tree root. Used to look up the submodule's URL in the .gitmodules map keyed
// by submodule path.
//
// fullPath is "/"-separated and never has a leading slash. submoduleRootDepth
// counts segments: 0 means "the top-level tree being filtered is itself the
// submodule" (no segments stripped); 2 means "the submodule was rooted two
// segments deep" (strip the first two).
//
// Examples:
//
//	pathRelativeToSubmoduleRoot("vendor/foo/inner/file.c", 2) == "inner/file.c"
//	pathRelativeToSubmoduleRoot("vendor/foo/file.c", 2)       == "file.c"
//	pathRelativeToSubmoduleRoot("file.c", 0)                   == "file.c"
//	pathRelativeToSubmoduleRoot("a/b/c/d", 1)                  == "b/c/d"
func pathRelativeToSubmoduleRoot(fullPath string, submoduleRootDepth int) string {
	if submoduleRootDepth <= 0 {
		return fullPath
	}
	parts := strings.SplitN(fullPath, "/", submoduleRootDepth+1)
	return parts[len(parts)-1]
}

// insertTreeAtPath splices newTreeObjectName into baseTreeObjectName at path
// newTreeSubdirPath, returning the new root tree's hash.
func (a *App) insertTreeAtPath(ctx context.Context, gopt GlobalOptions, baseTreeObjectName, newTreeObjectName, newTreeSubdirPath string) (string, error) {
	var recurse func(base, subdirPath string) (string, error)
	recurse = func(base, subdirPath string) (string, error) {
		var name, rest string
		if i := strings.IndexByte(subdirPath, '/'); i >= 0 {
			name, rest = subdirPath[:i], subdirPath[i+1:]
		} else {
			name = subdirPath
		}
		var newLines []string
		insertedYet := false
		if base != "" {
			r, err := a.git(ctx, gopt, []string{"ls-tree", "--full-tree", "-z", base}, modeNullTerminatedLines, gitOpts{})
			if err != nil {
				return "", err
			}
			for _, line := range r.lines {
				tab := strings.IndexByte(line, '\t')
				if tab < 0 {
					continue
				}
				stuff := line[:tab]
				thisName := line[tab+1:]
				if thisName == name {
					sp := strings.LastIndexByte(stuff, ' ')
					modeAndType := stuff[:sp]
					objectName := stuff[sp+1:]
					if modeAndType != "040000 tree" {
						return "", userErrorf("A non-directory file is in the way: %s", newTreeSubdirPath)
					}
					if rest != "" {
						newSub, err := recurse(objectName, rest)
						if err != nil {
							return "", err
						}
						line = fmt.Sprintf("040000 tree %s\t%s", newSub, name)
					} else {
						line = fmt.Sprintf("040000 tree %s\t%s", newTreeObjectName, name)
					}
					insertedYet = true
				}
				newLines = append(newLines, line)
			}
		}
		if !insertedYet {
			var subTree string
			if rest != "" {
				s, err := recurse("", rest)
				if err != nil {
					return "", err
				}
				subTree = s
			} else {
				subTree = newTreeObjectName
			}
			newLines = append(newLines, fmt.Sprintf("040000 tree %s\t%s", subTree, name))
		}
		var input strings.Builder
		for _, line := range newLines {
			input.WriteString(line)
			input.WriteByte(0)
		}
		r, err := a.git(ctx, gopt, []string{"mktree", "-z"}, modeSingleLine, gitOpts{input: []byte(input.String())})
		if err != nil {
			return "", err
		}
		return r.line, nil
	}
	return recurse(baseTreeObjectName, newTreeSubdirPath)
}

// filterTree walks the upstream tree, applies include/exclude filters, recurses
// into submodules, and rebuilds the resulting tree bottom-up.
func (a *App) filterTree(ctx context.Context, gopt GlobalOptions, treeObjectName, startAtSubdir string, include, exclude []string, sectionIndex int) (string, error) {
	includeFns := make([]FilterFn, 0, len(include))
	excludeFns := make([]FilterFn, 0, len(exclude))
	includeSoFar := FilterMaybe
	if len(include) == 0 {
		includeSoFar = FilterTrue
	} else {
		for _, p := range include {
			fn, err := compileFilter(p, "--include="+shlexQuote(p), true)
			if err != nil {
				return "", err
			}
			includeFns = append(includeFns, fn)
		}
	}
	for _, p := range exclude {
		fn, err := compileFilter(p, "--exclude="+shlexQuote(p), false)
		if err != nil {
			return "", err
		}
		excludeFns = append(excludeFns, fn)
	}

	if startAtSubdir != "" {
		r, err := a.git(ctx, gopt, []string{"ls-tree", "-z", "-d", "--full-tree", treeObjectName, "--", startAtSubdir}, modeNullTerminatedLines, gitOpts{})
		if err != nil {
			return "", err
		}
		if len(r.lines) == 0 {
			return "", userErrorf("not found in external repo: --subdir=%s", shlexQuote(startAtSubdir))
		}
		if len(r.lines) != 1 {
			return "", fmt.Errorf("expected exactly one ls-tree line for subdir, got %d", len(r.lines))
		}
		stuff := r.lines[0][:strings.IndexByte(r.lines[0], '\t')]
		treeObjectName = stuff[strings.LastIndexByte(stuff, ' ')+1:]
	}

	depthToParentToLines := map[int]map[string][]string{}

	addLine := func(depth int, parent string, line string) {
		if _, ok := depthToParentToLines[depth]; !ok {
			depthToParentToLines[depth] = map[string][]string{}
		}
		depthToParentToLines[depth][parent] = append(depthToParentToLines[depth][parent], line)
	}

	type submoduleResolver struct {
		treeObjectName string
		cached         map[string]string
		err            error
	}
	makeResolver := func(tn string) *submoduleResolver {
		return &submoduleResolver{treeObjectName: tn}
	}
	getSubmoduleMap := func(s *submoduleResolver) (map[string]string, error) {
		if s.cached != nil || s.err != nil {
			return s.cached, s.err
		}
		blob, err := a.git(ctx, gopt, []string{"cat-file", "blob", s.treeObjectName + ":.gitmodules"}, modeRawBytes, gitOpts{})
		if err != nil {
			s.err = err
			return nil, err
		}
		m, err := a.parseSubmodulePathToURLFromGitmodulesContent(ctx, gopt, blob.bytes)
		if err != nil {
			s.err = err
			return nil, err
		}
		s.cached = m
		return m, nil
	}

	var recurse func(treeObjectName, pathSoFar string, submoduleRootDepth int, includeSoFar FilterResult, sub *submoduleResolver) error
	recurse = func(treeObjectName, pathSoFar string, submoduleRootDepth int, includeSoFar FilterResult, sub *submoduleResolver) error {
		r, err := a.git(ctx, gopt, []string{"ls-tree", "--full-tree", "-z", treeObjectName}, modeNullTerminatedLines, gitOpts{})
		if err != nil {
			return err
		}
		for _, line := range r.lines {
			tab := strings.IndexByte(line, '\t')
			if tab < 0 {
				continue
			}
			stuff := line[:tab]
			name := line[tab+1:]
			lastSp := strings.LastIndexByte(stuff, ' ')
			modeAndType := stuff[:lastSp]
			objectName := stuff[lastSp+1:]
			fullPath := name
			if pathSoFar != "" {
				fullPath = pathSoFar + "/" + name
			}
			isTree := modeAndType == "040000 tree" || modeAndType == "160000 commit"
			segments := strings.Split(fullPath, "/")
			includeThis := FilterFalse
			if includeSoFar == FilterMaybe {
				for _, fn := range includeFns {
					res := fn(isTree, segments)
					if res == FilterTrue {
						includeThis = FilterTrue
						break
					}
					if res == FilterFalse {
						continue
					}
					includeThis = FilterMaybe
				}
				if includeThis == FilterFalse {
					continue
				}
			} else {
				includeThis = FilterTrue
			}
			excluded := false
			for _, fn := range excludeFns {
				if fn(isTree, segments) == FilterTrue {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
			if modeAndType == "040000 tree" {
				if err := recurse(objectName, fullPath, submoduleRootDepth, includeThis, sub); err != nil {
					return err
				}
				continue
			}
			if modeAndType == "160000 commit" {
				resolved, err := a.getGitCommitTreeObjectName(ctx, gopt, objectName)
				if err != nil {
					// Need to fetch from submodule URL.
					m, mErr := getSubmoduleMap(sub)
					if mErr != nil {
						return mErr
					}
					pathInSubmodule := pathRelativeToSubmoduleRoot(fullPath, submoduleRootDepth)
					url, ok := m[pathInSubmodule]
					if !ok {
						return userErrorf("submodule has no URL: %s", pathInSubmodule)
					}
					if err := a.gitFetchToCache(ctx, gopt, url, objectName, sectionIndex); err != nil {
						return err
					}
					resolved, err = a.getGitCommitTreeObjectName(ctx, gopt, objectName)
					if err != nil {
						return err
					}
				}
				newDepth := len(strings.Split(fullPath, "/"))
				if err := recurse(resolved, fullPath, newDepth, includeThis, makeResolver(resolved)); err != nil {
					return err
				}
				continue
			}
			if modeAndType != "100644 blob" && modeAndType != "100755 blob" && modeAndType != "120000 blob" {
				return fmt.Errorf("unknown tree mode/type: %s", modeAndType)
			}
			if i := strings.LastIndexByte(fullPath, '/'); i >= 0 {
				parent, child := fullPath[:i], fullPath[i+1:]
				addLine(strings.Count(parent, "/")+1, parent, fmt.Sprintf("%s\t%s", stuff, child))
			} else {
				addLine(0, "", line)
			}
		}
		return nil
	}

	if err := recurse(treeObjectName, "", 0, includeSoFar, makeResolver(treeObjectName)); err != nil {
		return "", err
	}

	if len(depthToParentToLines) == 0 {
		return "", userErrorf("no content after filters!")
	}

	maxDepth := 0
	for d := range depthToParentToLines {
		if d > maxDepth {
			maxDepth = d
		}
	}

	var rootTree string
	for depth := maxDepth; depth >= 0; depth-- {
		layer := depthToParentToLines[depth]
		if layer == nil {
			continue
		}
		// Stable iteration (sorted) for deterministic mktree --batch input.
		var parents []string
		for p := range layer {
			parents = append(parents, p)
		}
		slices.Sort(parents)

		var input strings.Builder
		for _, p := range parents {
			for _, l := range layer[p] {
				input.WriteString(l)
				input.WriteByte(0)
			}
			input.WriteByte(0)
		}
		r, err := a.git(ctx, gopt, []string{"mktree", "-z", "--batch"}, modeNewlineTerminatedLines, gitOpts{
			input: []byte(input.String()),
		})
		if err != nil {
			return "", err
		}
		if len(r.lines) != len(parents) {
			return "", fmt.Errorf("mktree --batch returned %d lines, expected %d", len(r.lines), len(parents))
		}
		for i, parent := range parents {
			tn := r.lines[i]
			if rootTree != "" {
				return "", fmt.Errorf("rootTree set unexpectedly")
			}
			if i := strings.LastIndexByte(parent, '/'); i >= 0 {
				gp, child := parent[:i], parent[i+1:]
				addLine(strings.Count(gp, "/")+1, gp, fmt.Sprintf("040000 tree %s\t%s", tn, child))
			} else if parent != "" {
				addLine(0, "", fmt.Sprintf("040000 tree %s\t%s", tn, parent))
			} else {
				rootTree = tn
			}
		}
	}
	if rootTree == "" {
		return "", fmt.Errorf("filterTree: no root tree produced")
	}
	return rootTree, nil
}
