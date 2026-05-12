---
name: vendor-third-party
description: Use when the user wants to vendor third-party Git content into a host repo as ordinary files via the `git-third-party` CLI. Triggers on bare "vendor", "vendoring", or "git vendor" in a Git/repo context; on `git-third-party`, `git third-party`, `third-party.toml`, or `third-party.lock`; on replacing `git submodule` or `git subtree`; and on pinning upstream commits or applying include/exclude filters to vendored code. Fire even when the user does not name the tool — phrases like "vendor zlib into our repo", "drop submodules in favor of plain files", or "CI gate on stale vendored code" all qualify.
---

# Vendoring with `git-third-party`

`git-third-party` is a CLI that materializes upstream Git content as ordinary files in the host repo. A hand-edited `third-party.toml` declares intent; a tool-managed `third-party.lock` records resolved state. The binary's `git-` prefix lets you invoke it as either `git-third-party <subcommand>` or `git third-party <subcommand>`.

## Install

```sh
uv tool install git-third-party    # PyPI; ships prebuilt platform binaries in the wheel
npm install -g git-third-party     # npm; uses optionalDependencies for platform binaries
go build -o ~/.local/bin/git-third-party .   # from source
```

Prebuilt binaries cover `linux-x64`, `linux-arm64`, `darwin-x64`, `darwin-arm64`, and `win32-x64`. Runtime dependency: `git` on `PATH`.

## Mental model

- **Entry vs. directory.** An *entry* is a config record in `third-party.toml` identified by `dir`. The *vendored directory* is the materialised output on disk at that path. This skill uses "entry" for the config record throughout.
- **Intent vs. resolved state.** `third-party.toml` captures what the user *wants* (URL, ref, filters). `third-party.lock` records what the tool *resolved* (commit SHA, optional saved patch). Commit both.
- **Tree rewrite, not history merge.** Each `update` overwrites the entry's directory with the resolved upstream tree (after filters and any saved patch). Provenance lives in the lockfile, not in merge commits.
- **One ref per entry.** Every entry tracks *exactly one* of `follow = "<branch>"` (re-resolved on every update) or `pin = "<tag-or-sha>"` (resolved once for tags, used verbatim for 40-hex SHAs).
- **`exclude` always wins over `include`.** Filters use `gitignore` semantics with documented deviations. Upstream submodules inline recursively unless excluded.

## Recognizing the situation

Reach for this skill when the user is:

- Adding, removing, or moving a vendored directory (`add`, `remove`, `rename`).
- Bumping versions or pulling upstream changes (`update`, `status`, `update --check` in CI).
- Filtering an upstream down to the parts they use.
- Switching tracking mode between branch-follow and pinned tag/SHA (`set --pin` / `set --follow`).
- Editing `third-party.toml` or reading `third-party.lock` directly.
- Recording or troubleshooting local edits to vendored code (the experimental `patch` subtree).

Two files at the repo root signal this skill applies: `third-party.toml` and `third-party.lock`.

## Workflows

### Add an entry

```sh
git-third-party add <DIR> <URL> [--follow <branch> | --pin <tag-or-sha>] \
    [--subdir <path>] [--include <pat>] [--exclude <pat>]
```

- `<DIR>` is repo-relative, forward-slash. The tool creates it. If it already exists and is non-empty, pass `-f` / `--allow-dir-exists`.
- With neither `--follow` nor `--pin`, the entry tracks the remote's `HEAD` branch.
- `--include` and `--exclude` are repeatable. Combine with `--subdir` to scope to a sub-tree of upstream.

```sh
# Track a branch, vendor only C sources and headers from src/
git-third-party add vendor/foo https://example.com/foo.git \
    --follow main --subdir src --include '*.c' --include '*.h'

# Pin a tag
git-third-party add third_party/zlib https://github.com/madler/zlib --pin v1.3.1
```

### Change an existing entry

```sh
git-third-party set <DIR> [--url URL] [--follow B | --pin T] \
    [--subdir P] [--include PAT ...] [--exclude PAT ...]
git-third-party unset <DIR> subdir|include|exclude
```

`set` has the alias `edit`. `unset` clears optional fields back to their defaults.

### Update / status / CI gate

```sh
git-third-party update                # all entries; stage the diff
git-third-party update <DIR>          # one entry
git-third-party status                # dry-run; alias for `update --dry-run`
git-third-party update --check        # CI: exit 5 if any entry would change
git-third-party update --commit "Bump vendored deps"
```

`--check` implies `--dry-run` and serves as the canonical pre-commit / CI gate against stale vendoring. `--commit MSG` runs `git commit -m MSG` after staging, and skips itself on no-op or dry-run runs.

### Inspect

```sh
git-third-party list                  # one line per entry; alias `ls`
git-third-party info <DIR>            # full details; alias `show`
```

Pair with `--json` (top-level flag) when scripting; the schema is `entryResult` in `output.go`. Single-entry commands emit a one-element array; multi-entry commands emit an array.

### Move / remove

```sh
git-third-party rename <DIR> <NEW_DIR>   # alias `mv`; updates config + lock
git-third-party remove <DIR>             # alias `rm`; `git rm -r`s the content
```

### Bootstrap

`init` creates an empty `third-party.toml`. Most users skip it; the first `add` creates the file.

## Configuration files

Both files live at the repo root; commit both.

`third-party.toml` (hand-editable intent):

```toml
[[third_party]]
dir = "third_party/zlib"
url = "https://github.com/madler/zlib"
follow = "master"

[[third_party]]
dir = "vendor/foo"
url = "https://example.com/foo.git"
pin = "v1.3.1"
subdir = "src"
include = ["*.c", "*.h"]
exclude = ["tests/"]
```

`third-party.lock` (generated, sorted by `dir`, do not hand-edit):

```toml
version = 1

[[third_party]]
dir = "third_party/zlib"
commit = "abc123..."

[[third_party]]
dir = "vendor/foo"
commit = "def456..."
```

Each `add`/`set`/`rename`/`remove` rewrites `third-party.toml`, **dropping any user comments**. Keep comments meant to survive outside the file — in a sibling note or in the PR description.

## Filters

- Patterns follow `gitignore` rules with documented deviations: no trailing `/**`, no empty / `.` / `..` segments, no empty pattern. See `git-third-party add --help` for the spec.
- `--exclude` always wins; `--include` is repeatable and defaults to keeping everything.
- Upstream submodules materialize recursively as plain files in the vendored directory unless excluded.

## Tracking refs

| Goal                            | Use this                              | Behavior                                                           |
| ------------------------------- | ------------------------------------- | ------------------------------------------------------------------ |
| Track upstream tip              | `--follow <branch>`                   | Re-resolves on every `update`.                                     |
| Pin to a tag                    | `--pin <tag>`                         | Resolves once, then freezes in the lock.                           |
| Pin to a specific commit        | `--pin <40-hex-sha>`                  | Used verbatim; `isPinCommit()` detects it by 40 hex chars.         |
| Switch modes on existing entry  | `git-third-party set <DIR> --pin ...` | Replaces the previously-recorded ref.                              |

## Settings precedence

Five layers, each overriding the previous:

1. Built-in defaults (`defaultSettings()` in `settings.go`).
2. Per-user: `git config --global third-party.<key>`.
3. Per-repo `[settings]` table in `third-party.toml`.
4. Environment variables.
5. CLI flags.

Environment variables and their flag equivalents:

| Variable                          | Equivalent flag                    |
| --------------------------------- | ---------------------------------- |
| `GIT_THIRD_PARTY_LOG_LEVEL`       | `--log-level=trace\|debug\|info\|warn\|error` |
| `GIT_THIRD_PARTY_LOG_FORMAT`      | `--log-format=text\|json`          |
| `GIT_THIRD_PARTY_COLOR`           | `--color=auto\|always\|never`      |
| `GIT_THIRD_PARTY_EXPERIMENTAL`    | `--experimental=<feature>` (`-Z`)  |
| `NO_COLOR`                        | Forces `--color=never` when `auto` |

Verbosity shortcuts: `-v` (debug), `-vv` (trace), `-q` (warn-only).

## Exit codes

| Code | Meaning                                                      |
| ---- | ------------------------------------------------------------ |
| 0    | success                                                      |
| 1    | generic failure                                              |
| 2    | configuration invalid (TOML parse, validation, lock schema)  |
| 3    | network, fetch, or ref-resolution failure                    |
| 4    | unresolvable merge conflict during `update` (patch 3-way)    |
| 5    | `--check` detected a pending change                          |

## Local patches (experimental)

The `patch` subtree is opt-in. Enable it per-invocation, per-repo, or per-user:

```sh
# CLI flag (per invocation)
git-third-party --experimental=patch patch save vendor/foo

# Per-repo, in third-party.toml
[settings]
experimental = ["patch"]

# Per-user
git config --global third-party.experimental patch
```

Workflow:

```sh
# Edit files under vendor/foo as you would any source file, then:
git-third-party --experimental=patch patch save vendor/foo
git-third-party --experimental=patch patch diff vendor/foo
```

`update` re-applies a saved patch via blob-by-blob 3-way merge between the new upstream tree (`base`), the upstream tree captured at save-time (`old`), and the user's modified tree (`new`). Conflicts surface in the lockfile's `tree-patch` field with a `-conflicts` suffix, so they remain visible until resolved — resolve with `git add` then `patch save` again. The feature remains experimental for a reason; review the produced commits.

## Common pitfalls

- **Target directory already populated.** `add` refuses a non-empty target by default. Pass `-f` / `--allow-dir-exists` to write into it; filters still apply afterwards.
- **Comments in `third-party.toml` disappear on `set`/`add`/`rename`.** The writer is hand-emitted for predictable layout; document intent elsewhere.
- **Tags freeze once resolved.** A pinned tag resolves once, then stays frozen. To pick up tag movement, run `set --pin <tag>` again or switch to `--follow`.
- **`--check` exits 5, not 1.** Scripts that treat any non-zero as fatal still work, but a CI job that distinguishes vendoring drift from other failures should special-case 5.
- **Submodule recursion is total.** Upstream `.gitmodules` entries inline recursively. A private or dead upstream submodule URL fails the whole `update` with exit 3 — exclude that path or vendor the submodule directly.
- **Logging vs. tool output split by stream.** Diagnostics go to stderr via slog; machine-readable output and human messages go to stdout. When piping `--json` into `jq`, leave stderr alone.

## When extending the tool itself

When the user is editing this repo's source rather than using the tool, prefer the project's `CLAUDE.md` over this skill — that file documents architecture, the file map, the `git()` helper, and the settings/observability flow. This skill is for *consumers* of the tool.
