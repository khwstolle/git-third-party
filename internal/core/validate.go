package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func validateDir(dir, paramName string) error {
	return validateCanonicalizedRelativePath(dir, paramName)
}

func validateSubdir(subdir, paramName string) error {
	return validateCanonicalizedRelativePath(subdir, paramName)
}

func validateCanonicalizedRelativePath(dir, paramName string) error {
	canon, err := canonicalizeRelativePath(dir, paramName)
	if err != nil {
		return err
	}
	if canon != dir {
		return userErrorf("%s must be normalized: %s", paramName, shlexQuote(dir))
	}
	return nil
}

func (a *App) validateDirNotExists(ctx context.Context, gopt GlobalOptions, dir, paramName string) error {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return err
	}
	actual := filepath.Join(root, dir)
	info, err := os.Stat(actual)
	switch {
	case err == nil:
		_ = info
		return userErrorf("%s already exists: %s\ntry giving --allow-dir-exists.",
			paramName, shlexQuote(relPath(actual)))
	case os.IsNotExist(err):
		return nil
	default:
		// Stat failure for any other reason (typically EACCES on a parent
		// component) — surface it now rather than letting `add`/`rename`
		// fail later in a less obvious place.
		return fmt.Errorf("stat %s: %w", actual, err)
	}
}

func validateNoPrimaryKeyConflicts(dir, paramName string, existingItems []*ConfigItem) error {
	for _, item := range existingItems {
		if dir == item.Dir {
			return userErrorf("%s is already a vendored dir: %s", paramName, shlexQuote(dir))
		}
		if strings.HasPrefix(dir, item.Dir+"/") {
			return userErrorf("%s is within an already vendored dir: %s", paramName, shlexQuote(dir))
		}
		if strings.HasPrefix(item.Dir, dir+"/") {
			return userErrorf("%s would completely contain another vendored dir: %s", paramName, shlexQuote(dir))
		}
	}
	return nil
}

var (
	urlSchemeRe  = regexp.MustCompile(`^[a-z+.-]+://.*`)
	urlScpLikeRe = regexp.MustCompile(`^[A-Za-z0-9._-]+@[^/]+:.*`)
	objectNameRe = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

func (a *App) validateURL(ctx context.Context, gopt GlobalOptions, url string, warn bool) error {
	if urlSchemeRe.MatchString(url) {
		return nil
	}
	if urlScpLikeRe.MatchString(url) {
		return nil
	}
	if filepath.IsAbs(url) {
		if _, err := os.Stat(url); err == nil {
			if warn {
				a.warning(ctx,
					"Absolute path urls are not portable! (But you probably already knew that.)",
					"--url="+shlexQuote(url),
				)
			}
			return nil
		}
	}
	return userErrorf("Invalid --url=%s", shlexQuote(url))
}

func validateRef(paramName, ref string) error {
	if strings.Contains(ref, "*") {
		return userErrorf("Invalid %s=%s", paramName, shlexQuote(ref))
	}
	return nil
}

func validateObjectName(paramName, name string) error {
	if objectNameRe.MatchString(name) {
		return nil
	}
	return userErrorf("Invalid %s=%s", paramName, shlexQuote(name))
}

// isPinCommit reports whether pin looks like a raw commit SHA (40 or 64 hex
// chars) rather than a tag name.
func isPinCommit(pin string) bool {
	return objectNameRe.MatchString(pin)
}

func validateTreePatch(treePatch string) error {
	tp := strings.TrimSuffix(treePatch, "-conflicts")
	parts := strings.Split(tp, ":")
	if len(parts) != 3 {
		return userErrorf("Invalid tree-patch format: %s", treePatch)
	}
	for i, seg := range parts {
		if err := validateObjectName(fmt.Sprintf("(tree-patch[%d])", i), seg); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) warning(ctx context.Context, lines ...string) {
	for _, line := range lines {
		a.log().WarnContext(ctx, line)
	}
}
