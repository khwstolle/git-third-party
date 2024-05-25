// End-to-end tests for the git-third-party Node bindings.
// Each test starts from a fresh host repo + upstream repo and drives the
// public API. Mirrors tests/test_python_bindings.py.

import test from "node:test";
import assert from "node:assert/strict";
import { mkdtempSync, mkdirSync, writeFileSync, readdirSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

import {
  add,
  GitThirdPartyError,
  info,
  init,
  list,
  remove,
  rename,
  set,
  update,
  version,
} from "../dist/index.js";

const GIT_ENV = {
  ...process.env,
  GIT_AUTHOR_NAME: "Test",
  GIT_AUTHOR_EMAIL: "test@example.com",
  GIT_COMMITTER_NAME: "Test",
  GIT_COMMITTER_EMAIL: "test@example.com",
  GIT_CONFIG_COUNT: "2",
  GIT_CONFIG_KEY_0: "commit.gpgsign",
  GIT_CONFIG_VALUE_0: "false",
  GIT_CONFIG_KEY_1: "tag.gpgsign",
  GIT_CONFIG_VALUE_1: "false",
};

function git(cwd, ...args) {
  const r = spawnSync("git", args, { cwd, env: GIT_ENV, encoding: "utf8" });
  if (r.status !== 0) {
    throw new Error(`git ${args.join(" ")} failed: ${r.stderr}`);
  }
  return r.stdout;
}

function freshTmp() {
  return mkdtempSync(join(tmpdir(), "gtp-test-"));
}

function hostRepo() {
  const root = freshTmp();
  const repo = join(root, "host");
  mkdirSync(repo);
  git(repo, "init", "-q", "-b", "main");
  git(repo, "commit", "-q", "--allow-empty", "-m", "init");
  return repo;
}

function upstreamRepo() {
  const root = freshTmp();
  const repo = join(root, "upstream");
  mkdirSync(repo);
  git(repo, "init", "-q", "-b", "main");
  writeFileSync(join(repo, "a.txt"), "hi\n");
  mkdirSync(join(repo, "src"));
  writeFileSync(join(repo, "src", "foo.c"), "void foo(void) {}\n");
  git(repo, "add", ".");
  git(repo, "commit", "-q", "-m", "seed");
  return repo;
}

test("version is nonempty", () => {
  const v = version();
  assert.equal(typeof v, "string");
  assert.ok(v.length > 0);
});

test("init then add then list", () => {
  const host = hostRepo();
  const up = upstreamRepo();
  init(host);
  const res = add({ repoPath: host, dir: "vendor/x", url: up, follow: "main" });
  assert.equal(res.action, "added");
  assert.equal(res.dir, "vendor/x");
  assert.ok(res.toCommit);
  const entries = list({ repoPath: host });
  assert.equal(entries.length, 1);
  assert.equal(entries[0].dir, "vendor/x");
  assert.equal(entries[0].follow, "main");
  assert.equal(entries[0].commit, res.toCommit);
});

test("update is no-op when unchanged", () => {
  const host = hostRepo();
  const up = upstreamRepo();
  init(host);
  const addRes = add({
    repoPath: host,
    dir: "vendor/x",
    url: up,
    follow: "main",
  });
  const results = update({ repoPath: host });
  assert.equal(results.length, 1);
  assert.equal(results[0].toCommit, addRes.toCommit);
});

test("info returns full entry", () => {
  const host = hostRepo();
  const up = upstreamRepo();
  init(host);
  add({ repoPath: host, dir: "vendor/x", url: up, follow: "main" });
  const e = info({ repoPath: host, dir: "vendor/x" });
  assert.equal(e.dir, "vendor/x");
  assert.equal(e.url, up);
  assert.equal(e.follow, "main");
});

test("remove", () => {
  const host = hostRepo();
  const up = upstreamRepo();
  init(host);
  add({ repoPath: host, dir: "vendor/x", url: up, follow: "main" });
  remove({ repoPath: host, dir: "vendor/x" });
  assert.deepEqual(list({ repoPath: host }), []);
});

test("rename", () => {
  const host = hostRepo();
  const up = upstreamRepo();
  init(host);
  add({ repoPath: host, dir: "vendor/x", url: up, follow: "main" });
  const res = rename({ repoPath: host, dir: "vendor/x", newDir: "vendor/y" });
  assert.equal(res.action, "renamed");
  assert.deepEqual(
    list({ repoPath: host }).map((e) => e.dir),
    ["vendor/y"],
  );
});

test("subdir filter", () => {
  const host = hostRepo();
  const up = upstreamRepo();
  init(host);
  const res = add({
    repoPath: host,
    dir: "vendor/x",
    url: up,
    follow: "main",
    subdir: "src",
  });
  assert.equal(res.action, "added");
  const files = readdirSync(join(host, "vendor", "x")).sort();
  assert.deepEqual(files, ["foo.c"]);
});

test("unknown dir raises GitThirdPartyError", () => {
  const host = hostRepo();
  assert.throws(
    () => list({ repoPath: host, dir: "does/not/exist" }),
    (err) =>
      err instanceof GitThirdPartyError &&
      /not vendored content/.test(err.message),
  );
});

test("init outside git repo raises", () => {
  const root = freshTmp();
  const notRepo = join(root, "plain");
  mkdirSync(notRepo);
  assert.throws(
    () => init(notRepo),
    (err) => err instanceof GitThirdPartyError,
  );
});

test("dry run does not mutate", () => {
  const host = hostRepo();
  const up = upstreamRepo();
  init(host);
  add({ repoPath: host, dir: "vendor/x", url: up, follow: "main" });
  set({ repoPath: host, dir: "vendor/x", follow: "main", dryRun: true });
  const entries = list({ repoPath: host });
  assert.equal(entries[0].url, up);
});
