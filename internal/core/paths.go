package core

import (
	"context"
	"path/filepath"
	"strings"
)

// canonicalizeRelativePath normalizes `dir` to a forward-slash, repo-relative
// path, returning a UserError if the path escapes the repo or is empty.
func canonicalizeRelativePath(dir, paramName string) (string, error) {
	if filepath.IsAbs(dir) {
		return "", userErrorf("%s must be a relative path: %s", paramName, shlexQuote(dir))
	}
	clean := filepath.Clean(dir)
	clean = strings.ReplaceAll(clean, string(filepath.Separator), "/")
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", userErrorf("%s is invalid: %s", paramName, shlexQuote(dir))
	}
	return clean, nil
}

// actualDirToPathInRepo converts a filesystem path to a repo-relative canonical path.
func (a *App) actualDirToPathInRepo(ctx context.Context, gopt GlobalOptions, actualDir, paramName string) (string, error) {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(actualDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	return canonicalizeRelativePath(rel, paramName)
}
