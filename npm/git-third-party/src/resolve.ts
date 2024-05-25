import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { existsSync } from "node:fs";
import { fileURLToPath } from "node:url";

export interface PlatformPaths {
  binPath: string;
  libPath: string;
}

const PLATFORM_TABLE: Record<string, Record<string, { pkg: string; lib: string }>> = {
  linux: {
    x64: { pkg: "git-third-party-linux-x64", lib: "libgitthirdparty.so" },
    arm64: { pkg: "git-third-party-linux-arm64", lib: "libgitthirdparty.so" },
  },
  darwin: {
    x64: { pkg: "git-third-party-darwin-x64", lib: "libgitthirdparty.dylib" },
    arm64: { pkg: "git-third-party-darwin-arm64", lib: "libgitthirdparty.dylib" },
  },
  win32: {
    x64: { pkg: "git-third-party-win32-x64", lib: "gitthirdparty.dll" },
  },
};

function platformKey(): { pkg: string; lib: string; binSuffix: string } {
  const os = process.platform;
  const arch = process.arch;
  const row = PLATFORM_TABLE[os]?.[arch];
  if (!row) {
    throw new Error(
      `git-third-party: no prebuilt binaries for ${os}/${arch}. ` +
        `Supported: linux-x64, linux-arm64, darwin-x64, darwin-arm64, win32-x64.`,
    );
  }
  return { pkg: row.pkg, lib: row.lib, binSuffix: os === "win32" ? ".exe" : "" };
}

function resolvePackageDir(pkgName: string): string {
  const here = dirname(fileURLToPath(import.meta.url));
  const req = createRequire(join(here, "package.json"));
  return dirname(req.resolve(`${pkgName}/package.json`));
}

export function resolvePlatformPaths(): PlatformPaths {
  const envBin = process.env.GIT_THIRD_PARTY_BIN;
  const envLib = process.env.GIT_THIRD_PARTY_LIB;
  if (envBin && envLib) {
    return { binPath: envBin, libPath: envLib };
  }

  const { pkg, lib, binSuffix } = platformKey();
  let pkgDir: string;
  try {
    pkgDir = resolvePackageDir(pkg);
  } catch {
    throw new Error(
      `git-third-party: platform package '${pkg}' is not installed. ` +
        `This usually means npm skipped it as an optional dependency on the wrong ` +
        `OS/arch. Reinstall on a supported platform, or set GIT_THIRD_PARTY_BIN ` +
        `and GIT_THIRD_PARTY_LIB to point at a development build.`,
    );
  }

  const binPath = envBin ?? join(pkgDir, "bin", `git-third-party${binSuffix}`);
  const libPath = envLib ?? join(pkgDir, "lib", lib);

  if (!envBin && !existsSync(binPath)) {
    throw new Error(`git-third-party: CLI binary missing at ${binPath}`);
  }
  if (!envLib && !existsSync(libPath)) {
    throw new Error(`git-third-party: shared library missing at ${libPath}`);
  }
  return { binPath, libPath };
}

export function resolveBinPath(): string {
  return resolvePlatformPaths().binPath;
}
