package core

import (
	"errors"
	"fmt"
)

// Exit codes — documented as a stable contract.
//
//	0   success
//	1   generic / unspecified failure (default for unwrapped errors)
//	2   configuration invalid (TOML parse, validation, lockfile schema mismatch)
//	3   network / fetch / ref-resolution failure
//	4   unresolvable merge conflict in `update`
//	5   `--check` detected a pending change
//
// Codes are chosen for stability rather than sysexits.h compliance — they
// won't change without a major version bump and a CHANGELOG entry.
const (
	ExitOK         = 0
	ExitUserError  = 1
	ExitConfig     = 2
	ExitNetwork    = 3
	ExitConflict   = 4
	ExitCheckDirty = 5
)

// UserError is an error reported back to the user as the program exit
// message. The optional ExitCode lets specific call sites associate a
// stable exit code; zero means "use the default" (1).
type UserError struct {
	Msg      string
	ExitCode int
}

func (e *UserError) Error() string { return e.Msg }

func userErrorf(format string, args ...any) *UserError {
	return &UserError{Msg: fmt.Sprintf(format, args...)}
}

func userErrorWithCode(code int, format string, args ...any) *UserError {
	return &UserError{Msg: fmt.Sprintf(format, args...), ExitCode: code}
}

// withExitCode tags a UserError with `code` if it doesn't already have one.
// Pass-through for nil and non-UserError values. Used at the persist/conflict/
// fetch boundaries to associate a stable exit code with the error category.
func withExitCode(err error, code int) error {
	if err == nil {
		return nil
	}
	var ue *UserError
	if errors.As(err, &ue) && ue.ExitCode == 0 {
		ue.ExitCode = code
	}
	return err
}
