<p align="center">
  <picture>
    <img alt="git-third-party" src="docs/assets/logo.svg" width="320">
  </picture>
</p>

# git-third-party

Vendor third-party git content into your repo as ordinary files — no submodules.

Three surfaces, one Go core: a `git-third-party` CLI, Python bindings (`import git_third_party`), and Node bindings (`import { add, list, ... } from 'git-third-party'`). All three speak the same `third-party.toml` / `third-party.lock` config and share the same cgo bridge for in-process calls.

`third-party.toml` records each entry's source URL, the ref it tracks, and any filters applied. `third-party.lock` pins the resolved commits. `git-third-party update` re-fetches upstream and stages the changes.

## Why

Submodules need a second clone, complicate CI, and hide third-party code from grep, build, and IDE tooling. Subtree merges lose provenance. `git-third-party` keeps content in-tree and visible to every tool, with enough metadata to update in one command.

## Requirements

- `git`
- Go 1.21+ (build only)

## Install

### CLI

Prebuilt binaries cover `linux-x64`, `linux-arm64`, `darwin-x64`, `darwin-arm64`, and `win32-x64`. Pick whichever package manager you already have:

```sh
# PyPI (wheel ships the binary as a pip script):
uv tool install git-third-party
pip install --user git-third-party

# npm (main package shells out to the platform-specific binary):
npm install -g git-third-party
pnpm add -g git-third-party
```

Build from source:

```sh
go build -o ~/.local/bin/git-third-party .
```

The binary's `git-` prefix lets you invoke it as either `git-third-party` or `git third-party`.

### Python bindings

```sh
uv tool install git-third-party    # or pip install
```

```py
from git_third_party import init, add, list_, version

print(version())
init()
add(dir="vendor/foo", url="https://github.com/x/y", follow="main")
for e in list_():
    print(e.dir, e.commit)
```

Mutating calls take `dry_run=` and `commit_msg=`. All calls take `repo_path=` (default `.`). Errors map to `GitThirdPartyError` and its subclasses (`ConfigError`, `NetworkError`, `ConflictError`, `CheckDirtyError`).

### Node bindings

```sh
npm install git-third-party     # or pnpm/yarn add
```

```ts
import { init, add, list, version } from "git-third-party";

console.log(version());
init();
add({ dir: "vendor/foo", url: "https://github.com/x/y", follow: "main" });
for (const e of list()) {
  console.log(e.dir, e.commit);
}
```

ECMAScript Modules (ESM) only, ships TypeScript types, requires Node ≥ 18. Same options shape as the Python API; same error hierarchy. The bridge serializes calls process-wide — for parallel work, use `worker_threads`.

## Quick start

Vendor a directory tracking a remote branch:

```sh
git-third-party add third_party/zlib https://github.com/madler/zlib --follow master
git commit -m "Vendor zlib"
```

Pull updates:

```sh
git-third-party update                    # all entries
git-third-party update third_party/zlib   # one entry
git-third-party status                    # dry-run
git commit -m "Update vendored zlib"
```

List, rename, remove:

```sh
git-third-party list
git-third-party rename third_party/zlib vendor/zlib
git-third-party remove third_party/zlib
```

## Tracking a ref

Each entry tracks exactly one of:

- `--follow <branch>` — track the branch tip, re-resolving on every `update`. Default when neither flag is set (resolved from the remote's `HEAD`).
- `--pin <tag-or-sha>` — pin to a tag (resolved once, then cached) or a commit SHA (used as-is). 40-hex-char strings count as SHAs.

Switch with `git-third-party set <dir> --pin v1.3.1` (or `--follow master`).

## Filtering content

Vendor part of an upstream repo:

```sh
git-third-party add vendor/foo https://example.com/foo.git \
    --subdir src \
    --include '*.c' --include '*.h' \
    --exclude 'tests/'
```

- `--subdir` — start from a subdirectory of the upstream repo.
- `--include` — keep only matching paths (repeatable; default keeps everything).
- `--exclude` — drop matching paths (repeatable; always wins over `--include`).

Patterns follow `gitignore` rules with documented deviations; see `git-third-party add --help` for the spec. Upstream submodules inline recursively unless excluded.

## Configuration files

Two files live at the repo root, both committed:

- `third-party.toml` — hand-editable intent. Each `[[third_party]]` table is one vendored directory. Each `add`/`set`/`rename`/`remove` rewrites the file, dropping any user comments.
- `third-party.lock` — generated. Records the resolved commit and any saved `tree-patch` per entry. Sorted by `dir` for stable diffs. Do not edit by hand.

Example `third-party.toml`:

```toml
# git-third-party — vendored content config.

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

Corresponding `third-party.lock`:

```toml
# git-third-party lockfile — generated; do not edit by hand.
version = 1

[[third_party]]
dir = "third_party/zlib"
commit = "abc123..."

[[third_party]]
dir = "vendor/foo"
commit = "def456..."
```

## Commands

| Command              | Aliases | Purpose                                                                       |
| -------------------- | ------- | ----------------------------------------------------------------------------- |
| `init`               |         | Create an empty `third-party.toml` (most users skip this — `add` creates it). |
| `add DIR URL`        |         | Register a new entry and download it.                                         |
| `set DIR …`          | `edit`  | Change URL, ref, or filters for an existing entry.                            |
| `unset DIR FIELD…`   |         | Clear `subdir`, `include`, or `exclude`.                                      |
| `update [DIR]`       | `up`    | Re-fetch and stage updates.                                                   |
| `status [DIR]`       | `st`    | `update --dry-run`.                                                           |
| `list [DIR]`         | `ls`    | Show entries and their tracking mode.                                         |
| `info DIR`           | `show`  | Print full details for one entry.                                             |
| `rename DIR NEW_DIR` | `mv`    | Move a vendored directory and update the config.                              |
| `remove DIR`         | `rm`    | Unregister a directory and `git rm -r` its content.                           |
| `patch save DIR`     |         | (experimental) Record local edits as a tree-patch.                            |
| `patch diff DIR`     |         | (experimental) Show the saved tree-patch via `git diff`.                      |
| `completion SHELL`   |         | Print a `bash`/`zsh`/`fish`/`powershell` completion script.                   |

### Common flags

- `-v`, `-vv`, `-q` — debug, trace, or warn-only logging.
- `--log-level=trace|debug|info|warn|error` — explicit level.
- `--log-format=text|json` — switch the stderr handler to JSON.
- `--color=auto|always|never` — honors `NO_COLOR`.
- `--dry-run` — plan without staging.
- `-f`, `--allow-dir-exists` — let `add`/`rename` write into a non-empty target.
- `--profile <path>` — write a CPU profile.
- `--json` — emit a structured `entryResult` (or array) on stdout instead of human text. Schema lives in `output.go`.
- `--commit MSG` — run `git commit -m MSG` after the command stages changes.
- `--check` (on `update`/`status`) — exit non-zero if any entry would change. Useful in CI and pre-commit hooks.

### Exit codes

| Code | Meaning                                                                  |
| ---- | ------------------------------------------------------------------------ |
| 0    | success                                                                  |
| 1    | generic failure                                                          |
| 2    | configuration invalid (TOML parse, validation, lockfile schema mismatch) |
| 3    | network, fetch, or ref-resolution failure                                |
| 4    | unresolvable merge conflict during `update`                              |
| 5    | `--check` detected a pending change                                      |

## Settings

Settings resolve through five layers, each overriding the previous:

1. Built-in defaults.
2. Per-user: `git config --global third-party.<key>`.
3. Per-repo: a `[settings]` table in `third-party.toml`.
4. Environment variables.
5. CLI flags.

Environment variables:

- `GIT_THIRD_PARTY_LOG_LEVEL` (`trace`/`debug`/`info`/`warn`/`error`) — same as `--log-level`.
- `GIT_THIRD_PARTY_LOG_FORMAT` (`text`/`json`) — same as `--log-format`.
- `GIT_THIRD_PARTY_COLOR` (`auto`/`always`/`never`) — same as `--color`.
- `GIT_THIRD_PARTY_EXPERIMENTAL` — comma-separated feature names; same as `--experimental`.
- `NO_COLOR` — standard cross-tool convention; disables ANSI color even when `--color=auto`.

Experimental commands (currently the `patch` subtree) need explicit opt-in: `--experimental=patch` (`-Z patch`), `experimental = ["patch"]` under `[settings]`, or `git config --global third-party.experimental patch`.

## Shell completions

```sh
# bash
source <(git-third-party completion bash)

# zsh
git-third-party completion zsh > "${fpath[1]}/_git-third-party"

# fish
git-third-party completion fish > ~/.config/fish/completions/git-third-party.fish
```

## Editing vendored content (experimental)

Enable with `--experimental=patch` (or set the equivalent in `[settings]` or git-config — see [Settings](#settings)):

- `git-third-party --experimental=patch patch save <dir>` — record local modifications as a tree-level patch in `third-party.lock`. The patch reapplies on every `update` via a 3-way merge.
- `git-third-party --experimental=patch patch diff <dir>` — show the saved patch.

If `update` produces conflicts, `git-third-party` stores the patch with a `-conflicts` suffix; resolve with `git add` followed by another `patch save`. Review the resulting commits carefully — the feature is experimental for a reason.

## License

MIT — see [LICENSE](LICENSE).
