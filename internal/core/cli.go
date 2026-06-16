// Package core implements the git-third-party CLI internals: the cobra
// command tree, the do*() command bodies, the git subprocess wrapper, the
// TOML config + lockfile, the filter/tree/patch pipeline, and the JSON
// bridge that backs the Python and Node bindings. The cmd/git-third-party
// and cmd/git-third-party-lib binaries are thin shells around this package.
package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/pprof"
	"strings"

	"github.com/spf13/cobra"
)

// newRootCmd builds the cobra command tree; construction is deferred so tests can create a fresh tree per case.
func newRootCmd(a *App) *cobra.Command {
	// Required so the patch subtree's gate runs after the root's
	// settings-loading PreRunE — without it only the patch parent's
	// PreRunE fires and settings are unset.
	cobra.EnableTraverseRunHooks = true

	gopt := &GlobalOptions{}

	root := &cobra.Command{
		Use:     "git-third-party",
		Short:   "Vendor third-party git content into a host git repo",
		Version: Version,
		// We format errors ourselves in main(). Cobra's usage dump is
		// silenced inside PersistentPreRunE so flag-parsing errors still
		// get the usage hint, but runtime errors (UserError, business
		// logic) don't.
		SilenceErrors: true,
	}

	pf := root.PersistentFlags()
	pf.CountP("verbose", "v", "increase log verbosity (-v debug, -vv trace)")
	pf.BoolP("quiet", "q", false, "suppress non-error output")
	pf.Bool("dry-run", false, "show what would be done; do not mutate")
	pf.Bool("json", false, "emit machine-readable JSON to stdout")
	pf.String("commit", "", "after staging, run `git commit -m MSG`; ignored in dry-run/json mode")
	pf.String("log-level", "", "explicit log level (trace|debug|info|warn|error); overrides -v/-q")
	pf.String("log-format", "", "log handler (text|json)")
	pf.String("color", "", "colorize stderr (auto|always|never); honors NO_COLOR")
	pf.String("profile", "", "write a CPU profile to PATH")
	pf.StringSliceP("experimental", "Z", nil, "comma-separated experimental feature names to opt into")

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// We've gotten past flag parsing; from here on, errors are
		// runtime, not usage problems. Silence the usage dump.
		cmd.SilenceUsage = true

		s, err := a.loadSettings(cmd.Context(), cmd)
		if err != nil {
			return err
		}
		if err := a.configureLogging(s); err != nil {
			return err
		}

		gopt.DryRun = s.DryRun
		// Read from root so subcommands inherit even if cobra clones the flag set.
		gopt.JSONOut, _ = cmd.Root().PersistentFlags().GetBool("json")
		gopt.CommitMsg, _ = cmd.Root().PersistentFlags().GetString("commit")

		if s.Profile != "" {
			f, err := os.Create(s.Profile)
			if err != nil {
				return fmt.Errorf("create profile: %w", err)
			}
			cmd.Annotations = map[string]string{"profileFile": f.Name()}
			if err := pprof.StartCPUProfile(f); err != nil {
				_ = f.Close()
				return fmt.Errorf("start profile: %w", err)
			}
			cobra.OnFinalize(func() {
				pprof.StopCPUProfile()
				_ = f.Close()
			})
		}

		ctx := withSettings(cmd.Context(), s)
		cmd.SetContext(ctx)
		return nil
	}

	root.AddCommand(
		newInitCmd(a, gopt),
		newAddCmd(a, gopt),
		newSetCmd(a, gopt),
		newUnsetCmd(a, gopt),
		newUpdateCmd(a, gopt, false),
		newUpdateCmd(a, gopt, true), // status = update --dry-run
		newListCmd(a, gopt),
		newInfoCmd(a, gopt),
		newRemoveCmd(a, gopt),
		newRenameCmd(a, gopt),
		newPatchCmd(a, gopt),
		newCompletionCmd(a),
	)
	return root
}

func newInitCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create an empty third-party.toml at the repo root",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.init(cmd.Context(), *gopt)
		},
	}
}

func newInfoCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "info DIR",
		Aliases: []string{"show"},
		Short:   "Show full details for one vendored directory",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.info(cmd.Context(), *gopt, args[0])
		},
	}
}

func newAddCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	var subdir, follow, pin string
	var include, exclude []string
	var allowDirExists, lfs bool

	cmd := &cobra.Command{
		Use:   "add DIR URL",
		Short: "Register a new vendored directory and download it",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if follow != "" && pin != "" {
				return userErrorf("--follow and --pin are mutually exclusive")
			}
			var lfsOpt *bool
			if cmd.Flags().Changed("lfs") {
				lfsOpt = &lfs
			}
			return a.add(cmd.Context(), *gopt, args[1], follow, pin, args[0], subdir, include, exclude, lfsOpt, allowDirExists)
		},
	}
	f := cmd.Flags()
	f.StringVar(&subdir, "subdir", "", "subdirectory of the upstream to vendor")
	f.StringVar(&follow, "follow", "", "branch to follow")
	f.StringVar(&pin, "pin", "", "tag name or commit SHA to pin to")
	f.StringSliceVar(&include, "include", nil, "include pattern (gitignore-style; repeatable)")
	f.StringSliceVar(&exclude, "exclude", nil, "exclude pattern (gitignore-style; repeatable)")
	f.BoolVar(&lfs, "lfs", false, "download LFS objects during checkout (default: vendor pointer files only)")
	f.BoolVarP(&allowDirExists, "allow-dir-exists", "f", false, "allow target dir to already exist")
	return cmd
}

func newSetCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	var url, follow, pin string
	var subdirFlag string
	var include, exclude []string
	var lfs bool

	cmd := &cobra.Command{
		Use:     "set DIR",
		Aliases: []string{"edit"},
		Short:   "Edit settings of an existing vendored directory",
		Long: `Edit settings of an existing vendored directory.

To clear a field (subdir, include, exclude), use ` + "`unset`" + ` instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if follow != "" && pin != "" {
				return userErrorf("--follow and --pin are mutually exclusive")
			}
			var subdirOpt *string
			if cmd.Flags().Changed("subdir") {
				v := subdirFlag
				subdirOpt = &v
			}
			var incOpt, excOpt *[]string
			if cmd.Flags().Changed("include") {
				v := include
				incOpt = &v
			}
			if cmd.Flags().Changed("exclude") {
				v := exclude
				excOpt = &v
			}
			var lfsOpt *bool
			if cmd.Flags().Changed("lfs") {
				lfsOpt = &lfs
			}
			return a.set(cmd.Context(), *gopt, url, follow, pin, args[0], subdirOpt, incOpt, excOpt, lfsOpt)
		},
	}
	f := cmd.Flags()
	f.StringVar(&url, "url", "", "new upstream URL")
	f.StringVar(&subdirFlag, "subdir", "", "new upstream subdir")
	f.StringVar(&follow, "follow", "", "switch to follow this branch")
	f.StringVar(&pin, "pin", "", "switch to pin to this tag or commit")
	f.StringSliceVar(&include, "include", nil, "replace include patterns")
	f.StringSliceVar(&exclude, "exclude", nil, "replace exclude patterns")
	f.BoolVar(&lfs, "lfs", false, "download LFS objects during checkout (default: vendor pointer files only)")
	return cmd
}

// newUnsetCmd mirrors `git config --unset` for clearable entry fields.
func newUnsetCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "unset DIR FIELD [FIELD...]",
		Short: "Clear one or more fields (subdir, include, exclude) on a vendored directory",
		Long: `Clear one or more clearable fields on a vendored directory.

Clearable fields: subdir, include, exclude. Other fields (url, follow-branch,
pin-to-tag, pin-to-commit, dir) are required or replaced via ` + "`set`" + `.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.unset(cmd.Context(), *gopt, args[0], args[1:])
		},
	}
}

// newUpdateCmd builds either `update` or `status` (which is `update --dry-run`).
func newUpdateCmd(a *App, gopt *GlobalOptions, asStatus bool) *cobra.Command {
	var check bool

	cmd := &cobra.Command{
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}
			lopt := *gopt
			if asStatus {
				lopt.DryRun = true
			}
			return a.update(cmd.Context(), lopt, dir, check)
		},
	}
	if asStatus {
		cmd.Use = "status [DIR]"
		cmd.Aliases = []string{"st"}
		cmd.Short = "Equivalent to `update --dry-run`"
	} else {
		cmd.Use = "update [DIR]"
		cmd.Aliases = []string{"up"}
		cmd.Short = "Re-fetch upstream and stage updates"
	}
	cmd.Flags().BoolVar(&check, "check", false, "exit non-zero if any entry would change (implies --dry-run)")
	return cmd
}

func newListCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "list [DIR]",
		Aliases: []string{"ls"},
		Short:   "Show registered directories",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ""
			if len(args) > 0 {
				dir = args[0]
			}
			return a.list(cmd.Context(), *gopt, dir, gopt.JSONOut)
		},
	}
}

func newRemoveCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	return &cobra.Command{
		Use:     "remove DIR",
		Aliases: []string{"rm"},
		Short:   "Unregister a directory and remove its content",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.remove(cmd.Context(), *gopt, args[0])
		},
	}
}

func newRenameCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	var allowDirExists bool
	cmd := &cobra.Command{
		Use:     "rename DIR NEW_DIR",
		Aliases: []string{"mv"},
		Short:   "Move a vendored directory",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.rename(cmd.Context(), *gopt, args[0], args[1], allowDirExists)
		},
	}
	cmd.Flags().BoolVar(&allowDirExists, "allow-dir-exists", false, "allow target dir to already exist")
	return cmd
}

// newPatchCmd groups `save` and `diff` behind --experimental=patch. The lockfile
// field is `tree-patch` (the data format) regardless of the user-facing name.
func newPatchCmd(a *App, gopt *GlobalOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "patch",
		Short: "(experimental) Operate on saved tree-patches for vendored directories",
		Long: `Operate on saved tree-patches.

Tree-patches let you carry local modifications across upstream updates
via a 3-way merge. The full subtree is experimental: opt in with
` + "`--experimental=patch`" + ` (or persist via [settings] / git-config / env).`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			s, _ := settingsFromContext(cmd.Context())
			if s == nil || !s.hasExperimental("patch") {
				return userErrorf("`patch` is experimental; opt in with `--experimental=patch`")
			}
			return nil
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "save DIR",
			Short: "Record local edits as a tree-patch in the lockfile",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.savePatch(cmd.Context(), *gopt, args[0])
			},
		},
		&cobra.Command{
			Use:   "diff DIR",
			Short: "Show the saved tree-patch via `git diff`",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return a.diffPatch(cmd.Context(), *gopt, args[0])
			},
		},
	)
	return cmd
}

func newCompletionCmd(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:       "completion {bash|zsh|fish|powershell}",
		Short:     "Generate a shell completion script",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(a.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(a.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(a.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(a.Stdout)
			}
			return userErrorf("unknown shell: %s", args[0])
		},
	}
	return cmd
}

type settingsCtxKey struct{}

func withSettings(ctx context.Context, s *settings) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, settingsCtxKey{}, s)
}

func settingsFromContext(ctx context.Context) (*settings, bool) {
	if ctx == nil {
		return nil, false
	}
	s, ok := ctx.Value(settingsCtxKey{}).(*settings)
	return s, ok
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// printUserError prints one ERROR: prefix per line of a multi-line UserError.
func printUserError(w *os.File, err error) {
	var ue *UserError
	if errors.As(err, &ue) {
		for _, line := range strings.Split(ue.Msg, "\n") {
			_, _ = fmt.Fprintln(w, "ERROR: "+line)
		}
		return
	}
	_, _ = fmt.Fprintln(w, "ERROR: "+err.Error())
}

// Main is the CLI entry point. cmd/git-third-party calls this; on error it
// terminates the process with the appropriate exit code.
func Main() {
	app := NewApp(os.Stdout, os.Stderr)

	root := newRootCmd(app)
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)

	if err := root.Execute(); err != nil {
		if errors.Is(err, errCheckMismatch) {
			// --check failure: working tree differs from lockfile.
			// The diff was already printed during dry-run.
			os.Exit(ExitCheckDirty)
		}
		printUserError(os.Stderr, err)
		var ue *UserError
		if errors.As(err, &ue) && ue.ExitCode != 0 {
			os.Exit(ue.ExitCode)
		}
		os.Exit(ExitUserError)
	}
}
