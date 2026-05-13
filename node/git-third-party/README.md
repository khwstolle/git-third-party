# git-third-party (Node)

Vendor third-party Git repositories into your own, with path filtering and local patches.

This package ships **two** entry points:

1. The `git-third-party` CLI (added to your `PATH` via `npm install -g git-third-party` or available via `npx`).
2. In-process Node bindings (`import { add, list, ... } from 'git-third-party'`) backed by the same Go core via a c-shared library.

For the full feature set, configuration format, and CLI reference, see the [project README](https://github.com/khwstolle/git-third-party).

## Install

```sh
npm install -g git-third-party
# or:
pnpm add -g git-third-party
# or scoped to a project:
npm install git-third-party
```

`npm` fetches the right platform binaries through `optionalDependencies`. Supported platforms: `linux-x64`, `linux-arm64`, `darwin-x64`, `darwin-arm64`, `win32-x64`.

## CLI

```sh
git-third-party --help
git-third-party add vendor/foo https://github.com/x/y --follow main
git-third-party update
```

## Library

```ts
import { init, add, list, version } from "git-third-party";

console.log(version());
init();
add({ dir: "vendor/foo", url: "https://github.com/x/y", follow: "main" });
for (const e of list()) {
  console.log(e.dir, e.commit);
}
```

Mutating calls accept `dryRun?: boolean` and `commitMsg?: string`. All calls accept `repoPath?: string` (default `"."`).

## Errors

```ts
import { ConfigError, ConflictError, GitThirdPartyError } from "git-third-party";

try {
  add({ /* ... */ });
} catch (e) {
  if (e instanceof ConflictError) { /* unresolved 3-way merge */ }
  else if (e instanceof ConfigError) { /* invalid TOML */ }
  else if (e instanceof GitThirdPartyError) { /* anything else */ }
}
```

## Concurrency

The cgo bridge serializes calls process-wide. For parallel work, use `worker_threads`.
