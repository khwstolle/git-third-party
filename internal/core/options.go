package core

import (
	"context"
	"io"
	"log/slog"
)

// App holds long-lived infrastructure.
type App struct {
	// Output sinks. Stdout is for tool output; Stderr is for diagnostic
	// logging.
	Stdout io.Writer
	Stderr io.Writer

	Logger   *slog.Logger
	LogLevel slog.Level

	WorkDir      string
	repoRootVal  string
	repoRootDone bool

	gitCommitsToKeep map[string]struct{}
}

func NewApp(stdout, stderr io.Writer) *App {
	return &App{
		Stdout:           stdout,
		Stderr:           stderr,
		gitCommitsToKeep: map[string]struct{}{},
	}
}

// GlobalOptions holds per-call flags.
type GlobalOptions struct {
	DryRun    bool
	JSONOut   bool
	CommitMsg string // when non-empty, mutating commands run `git commit -m MSG` after staging
}

func (a *App) log() *slog.Logger {
	if a.Logger != nil {
		return a.Logger
	}
	return slog.Default()
}

func (a *App) traceEnabled() bool {
	return a.log().Enabled(context.Background(), LevelTrace)
}
