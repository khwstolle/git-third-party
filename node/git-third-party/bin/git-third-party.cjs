#!/usr/bin/env node
"use strict";

// Resolve the platform-specific package and exec its CLI binary.
// Mirrors the resolve logic in src/resolve.ts but stays in a CommonJS
// file for zero startup cost (no koffi/types.ts loaded).

const { spawnSync } = require("node:child_process");
const { existsSync } = require("node:fs");
const { join } = require("node:path");

const PLATFORM_TABLE = {
  linux: {
    x64: "git-third-party-linux-x64",
    arm64: "git-third-party-linux-arm64",
  },
  darwin: {
    x64: "git-third-party-darwin-x64",
    arm64: "git-third-party-darwin-arm64",
  },
  win32: {
    x64: "git-third-party-win32-x64",
  },
};

function fail(msg) {
  process.stderr.write(`git-third-party: ${msg}\n`);
  process.exit(1);
}

function resolveBin() {
  if (process.env.GIT_THIRD_PARTY_BIN) {
    return process.env.GIT_THIRD_PARTY_BIN;
  }
  const os = process.platform;
  const arch = process.arch;
  const pkg = (PLATFORM_TABLE[os] || {})[arch];
  if (!pkg) {
    fail(
      `no prebuilt binary for ${os}/${arch}. Supported: linux-x64, ` +
        `linux-arm64, darwin-x64, darwin-arm64, win32-x64.`,
    );
  }
  let pkgDir;
  try {
    pkgDir = require.resolve(`${pkg}/package.json`);
    pkgDir = pkgDir.slice(0, pkgDir.length - "/package.json".length);
  } catch (_e) {
    fail(
      `platform package '${pkg}' is not installed. Reinstall on a ` +
        `supported platform, or set GIT_THIRD_PARTY_BIN to a development build.`,
    );
  }
  const suffix = os === "win32" ? ".exe" : "";
  const binPath = join(pkgDir, "bin", `git-third-party${suffix}`);
  if (!existsSync(binPath)) {
    fail(`CLI binary missing at ${binPath}`);
  }
  return binPath;
}

const result = spawnSync(resolveBin(), process.argv.slice(2), {
  stdio: "inherit",
});

if (result.error) {
  fail(result.error.message);
}
process.exit(result.status === null ? 1 : result.status);
