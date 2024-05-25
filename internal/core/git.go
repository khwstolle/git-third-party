package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

type gitMode int

const (
	modeSingleLine gitMode = iota
	modeNullTerminatedLines
	modeNewlineTerminatedLines
	modeRawBytes
	modeMutating
	modeInheritStdout
)

// gitOpts holds optional parameters for the git() helper.
type gitOpts struct {
	cwd            string
	input          []byte
	outputPath     string
	suppressStderr bool
	skipInDryRun   bool
}

// gitResult carries the parsed result; only one of the fields will be populated
// per mode.
type gitResult struct {
	line  string
	lines []string
	bytes []byte
}

// git invokes the git subprocess and returns parsed output.
func (a *App) git(ctx context.Context, gopt GlobalOptions, args []string, mode gitMode, o gitOpts) (gitResult, error) {
	if o.cwd == "" {
		if a.WorkDir != "" {
			o.cwd = a.WorkDir
		} else {
			o.cwd = "."
		}
	}

	cmd := append([]string{"git"}, args...)

	// Trace: every invocation. Debug: mutating only. Format is identical so log scrapers don't branch.
	if a.traceEnabled() || (a.log().Enabled(ctx, LevelDebug) && mode == modeMutating) {
		a.emitShellScript(ctx, gopt, cmd, mode, o)
	}

	if mode == modeMutating && gopt.DryRun && o.skipInDryRun {
		return gitResult{}, nil
	}

	c := exec.Command(cmd[0], cmd[1:]...)
	c.Dir = o.cwd
	if len(o.input) > 0 {
		c.Stdin = bytes.NewReader(o.input)
	}
	if o.suppressStderr {
		c.Stderr = io.Discard
	} else {
		c.Stderr = a.Stderr
	}

	switch mode {
	case modeMutating:
		if o.outputPath != "" {
			f, err := os.Create(o.outputPath)
			if err != nil {
				return gitResult{}, err
			}
			defer func() { _ = f.Close() }()
			c.Stdout = f
		}
		if err := c.Run(); err != nil {
			return gitResult{}, &gitExecError{cmd: cmd, err: err}
		}
		return gitResult{}, nil
	case modeInheritStdout:
		c.Stdout = a.Stdout
		if err := c.Run(); err != nil {
			return gitResult{}, &gitExecError{cmd: cmd, err: err}
		}
		return gitResult{}, nil
	default:
		var buf bytes.Buffer
		c.Stdout = &buf
		if err := c.Run(); err != nil {
			return gitResult{}, &gitExecError{cmd: cmd, err: err}
		}
		out := buf.Bytes()
		switch mode {
		case modeRawBytes:
			return gitResult{bytes: out}, nil
		case modeSingleLine:
			s := string(out)
			parts := strings.Split(s, "\n")
			if len(parts) < 2 || parts[len(parts)-1] != "" {
				return gitResult{}, fmt.Errorf("expected single line; got %q", s)
			}
			lines := parts[:len(parts)-1]
			if len(lines) != 1 {
				return gitResult{}, fmt.Errorf("expected exactly one line; got %d", len(lines))
			}
			return gitResult{line: lines[0]}, nil
		case modeNullTerminatedLines:
			s := string(out)
			parts := strings.Split(s, "\x00")
			if len(parts) == 0 || parts[len(parts)-1] != "" {
				return gitResult{}, fmt.Errorf("expected null-terminated; got %q", s)
			}
			return gitResult{lines: parts[:len(parts)-1]}, nil
		case modeNewlineTerminatedLines:
			s := string(out)
			if s == "" {
				return gitResult{lines: nil}, nil
			}
			parts := strings.Split(s, "\n")
			if parts[len(parts)-1] != "" {
				return gitResult{}, fmt.Errorf("expected newline-terminated; got %q", s)
			}
			return gitResult{lines: parts[:len(parts)-1]}, nil
		}
	}
	return gitResult{}, fmt.Errorf("unknown mode")
}

// gitExecError wraps the underlying exec error with the command we ran.
type gitExecError struct {
	cmd []string
	err error
}

func (e *gitExecError) Error() string {
	return fmt.Sprintf("git %s: %v", strings.Join(e.cmd[1:], " "), e.err)
}

func (e *gitExecError) Unwrap() error { return e.err }

// gitExitCode returns the underlying exit code if the wrapped error was an
// exec.ExitError, or -1 otherwise. Used for "soft failures" like merge-file.
// errors.As walks the wrap chain (gitExecError → exec.ExitError) for us.
func gitExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// emitShellScript builds a copy/pasteable shell representation of the git command and logs it.
func (a *App) emitShellScript(ctx context.Context, gopt GlobalOptions, cmd []string, mode gitMode, o gitOpts) {
	script := shellJoin(cmd)
	if o.suppressStderr {
		script += " 2>/dev/null"
	}
	if o.cwd != "" && o.cwd != "." {
		script = fmt.Sprintf("(cd %s && %s)", shlexQuote(o.cwd), script)
	}
	if o.outputPath != "" {
		script += " > " + shlexQuote(o.outputPath)
	}
	if len(o.input) == 0 {
		if gopt.DryRun && o.skipInDryRun {
			script = "# " + script
		}
	} else {
		// Reproduce stdin as echo -ne lines, splitting on \x00 and \n boundaries.
		var lines [][]byte
		i := 0
		for i < len(o.input) {
			j := i
			for j < len(o.input) && o.input[j] != 0 && o.input[j] != '\n' {
				j++
			}
			for j < len(o.input) && (o.input[j] == 0 || o.input[j] == '\n') {
				j++
			}
			lines = append(lines, o.input[i:j])
			i = j
		}
		var script2 strings.Builder
		script2.WriteString("{")
		for _, line := range lines {
			script2.WriteString("\n  echo -ne ")
			script2.WriteString(quoteForEcho(line))
		}
		script2.WriteString("\n}")
		if gopt.DryRun && o.skipInDryRun {
			script = ": #" + script
		}
		script = script2.String() + " | " + script
	}
	level := LevelTrace
	if mode == modeMutating {
		level = LevelDebug
	}
	a.log().Log(ctx, level, script)
}

func quoteForEcho(line []byte) string {
	if len(line) == 0 {
		return "''"
	}
	startsWithHyphen := false
	if line[0] == '-' {
		startsWithHyphen = true
		line = line[1:]
	}
	var out strings.Builder
	for _, b := range line {
		switch {
		case b == '\n':
			out.WriteString(`\n`)
		case b == '\t':
			out.WriteString(`\t`)
		case b < 0x20 || b == '\\' || b == '\'' || b >= 0x7f:
			fmt.Fprintf(&out, `\x%02x`, b)
		default:
			out.WriteByte(b)
		}
	}
	s := out.String()
	if startsWithHyphen {
		s = `\x2d` + s
	}
	return "'" + s + "'"
}

func (a *App) keepCommit(name string) {
	if a.gitCommitsToKeep == nil {
		a.gitCommitsToKeep = map[string]struct{}{}
	}
	a.gitCommitsToKeep[name] = struct{}{}
}

func (a *App) haveCommitInKeep(name string) bool {
	if a.gitCommitsToKeep == nil {
		return false
	}
	_, ok := a.gitCommitsToKeep[name]
	return ok
}

// getRepoRoot returns the top-level path of the current git repo. Cached.
func (a *App) getRepoRoot(ctx context.Context, gopt GlobalOptions) (string, error) {
	if a.repoRootDone {
		return a.repoRootVal, nil
	}
	r, err := a.git(ctx, gopt, []string{"rev-parse", "--show-toplevel"}, modeSingleLine, gitOpts{})
	if err != nil {
		return "", err
	}
	a.repoRootVal = r.line
	a.repoRootDone = true
	return a.repoRootVal, nil
}

func (a *App) getGitCommitTreeObjectName(ctx context.Context, gopt GlobalOptions, commit string) (string, error) {
	a.keepCommit(commit)
	r, err := a.git(ctx, gopt, []string{"rev-parse", "--verify", commit + "^{tree}"}, modeSingleLine, gitOpts{})
	if err != nil {
		return "", err
	}
	return r.line, nil
}

func (a *App) isGitObjectResolvableAsTree(ctx context.Context, gopt GlobalOptions, name string) bool {
	a.keepCommit(name)
	_, err := a.git(ctx, gopt, []string{"rev-parse", "--verify", name + "^{tree}"}, modeSingleLine, gitOpts{suppressStderr: true})
	return err == nil
}

// fnv64Hash returns a deterministic hash for constructing cache ref names; stability across runs is not required.
func fnv64Hash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func (a *App) gitFetchToCache(ctx context.Context, gopt GlobalOptions, url, ref string, sectionIndex int) error {
	localRef := fmt.Sprintf("refs/third_party/%d/ref-%x", sectionIndex, fnv64Hash(url+"\x00"+ref))
	_, err := a.git(ctx, gopt, []string{
		"fetch", "--no-tags", "--force", "--depth", "1",
		url, fmt.Sprintf("%s:%s", ref, localRef),
	}, modeInheritStdout, gitOpts{})
	if err != nil {
		return err
	}
	r, err := a.git(ctx, gopt, []string{"rev-parse", "--verify", localRef}, modeSingleLine, gitOpts{})
	if err != nil {
		return err
	}
	a.keepCommit(r.line)
	return nil
}

var refThirdPartyRe = regexp.MustCompile(`^refs/third_party/(-?\d+)/.*$`)

func (a *App) cleanupRefs(ctx context.Context, gopt GlobalOptions, sectionIndexes []int, visitedAll bool) error {
	visited := make(map[int]struct{}, len(sectionIndexes))
	for _, s := range sectionIndexes {
		visited[s] = struct{}{}
	}
	r, err := a.git(ctx, gopt, []string{"show-ref"}, modeNewlineTerminatedLines, gitOpts{})
	if err != nil {
		// git show-ref exits 1 with no output when no refs exist; anything
		// else (corruption, permission errors) propagates.
		if gitExitCode(err) == 1 {
			return nil
		}
		return err
	}
	var instructions strings.Builder
	for _, line := range r.lines {
		sp := strings.SplitN(line, " ", 2)
		if len(sp) != 2 {
			continue
		}
		objectName, refName := sp[0], sp[1]
		m := refThirdPartyRe.FindStringSubmatch(refName)
		if m == nil {
			continue
		}
		var sec int
		if _, err := fmt.Sscanf(m[1], "%d", &sec); err != nil {
			continue
		}
		if _, ok := visited[sec]; !ok {
			if visitedAll {
				fmt.Fprintf(&instructions, "delete %s\x00%s\x00", refName, objectName)
			}
			continue
		}
		if a.haveCommitInKeep(objectName) {
			continue
		}
		fmt.Fprintf(&instructions, "delete %s\x00%s\x00", refName, objectName)
	}
	if instructions.Len() == 0 {
		return nil
	}
	_, err = a.git(ctx, gopt, []string{"update-ref", "--stdin", "-z"}, modeMutating, gitOpts{
		input:        []byte(instructions.String()),
		skipInDryRun: true,
	})
	return err
}

func (a *App) gitStatusFor(ctx context.Context, gopt GlobalOptions, dir string) ([]string, error) {
	root, err := a.getRepoRoot(ctx, gopt)
	if err != nil {
		return nil, err
	}
	r, err := a.git(ctx, gopt, []string{"status", "-z", "--", dir}, modeNullTerminatedLines, gitOpts{cwd: root})
	if err != nil {
		return nil, err
	}
	return r.lines, nil
}
