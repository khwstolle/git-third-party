package core

import (
	"context"
	"strings"
)

// parseSubmodulePathToURLFromGitmodulesContent parses a .gitmodules blob into a path→URL map.
// Uses `git config -z --list -f -` so git handles quoting, multi-line values, and escape sequences.
func (a *App) parseSubmodulePathToURLFromGitmodulesContent(ctx context.Context, gopt GlobalOptions, content []byte) (map[string]string, error) {
	r, err := a.git(ctx, gopt, []string{"config", "-z", "--list", "-f", "-"}, modeNullTerminatedLines, gitOpts{
		input: content,
	})
	if err != nil {
		return nil, err
	}
	pathConfigs := map[string]string{}
	urlConfigs := map[string]string{}
	for _, line := range r.lines {
		nl := strings.IndexByte(line, '\n')
		if nl < 0 {
			continue
		}
		name := line[:nl]
		value := line[nl+1:]
		dot := strings.IndexByte(name, '.')
		if dot < 0 {
			continue
		}
		section := name[:dot]
		rest := name[dot+1:]
		if section != "submodule" {
			continue
		}
		lastDot := strings.LastIndexByte(rest, '.')
		if lastDot < 0 {
			continue
		}
		subName := rest[:lastDot]
		field := rest[lastDot+1:]
		switch field {
		case "path":
			pathConfigs[subName] = value
		case "url":
			urlConfigs[subName] = value
		}
	}
	out := map[string]string{}
	for name, p := range pathConfigs {
		if u, ok := urlConfigs[name]; ok {
			out[p] = u
		}
	}
	return out, nil
}
