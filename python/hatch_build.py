"""Hatchling build hook that compiles the Go CLI and packages it as a wheel.

Each wheel build produces two artifacts:

  1. The static CLI binary (CGO_ENABLED=0), shipped as a wheel script so
     `pip install` puts it on PATH (~/.local/bin via pipx, etc.).
  2. The cgo c-shared library (CGO_ENABLED=1), staged inside the Python
     package so `import git_third_party` finds it next to _ffi.py and
     loads it through ctypes.

Set GIT_THIRD_PARTY_SKIP_SHAREDLIB=1 to skip artifact (2) — useful for
host-only dev builds without a C cross-toolchain.

The target platform comes from HATCH_GOOS / HATCH_GOARCH env vars (set
by the release CI matrix) or, when absent, from the host machine. The
resulting wheel carries a platform-specific tag.
"""

from __future__ import annotations

import os
import platform
import shutil
import subprocess
from pathlib import Path

from hatchling.builders.hooks.plugin.interface import BuildHookInterface


# (GOOS, GOARCH) -> wheel platform tag. manylinux2014 is widely compatible
# (glibc 2.17, ~CentOS 7 / 2014). The static CLI honors that promise; the
# cgo shared library links dynamically against libc/libpthread, which are
# baseline on manylinux2014, so the tag remains valid.
_WHEEL_PLATFORMS = {
    ("linux", "amd64"): "manylinux2014_x86_64",
    ("linux", "arm64"): "manylinux2014_aarch64",
    ("darwin", "arm64"): "macosx_11_0_arm64",
    ("windows", "amd64"): "win_amd64",
}


def _shared_lib_name(goos: str) -> str:
    if goos == "linux":
        return "libgitthirdparty.so"
    if goos == "darwin":
        return "libgitthirdparty.dylib"
    if goos == "windows":
        return "gitthirdparty.dll"
    raise RuntimeError(f"unsupported GOOS for shared library: {goos}")


class GoBuildHook(BuildHookInterface):
    PLUGIN_NAME = "custom"

    def initialize(self, version, build_data):
        if self.target_name != "wheel":
            return

        goos = os.environ.get("HATCH_GOOS") or _default_goos()
        goarch = os.environ.get("HATCH_GOARCH") or _default_goarch()
        key = (goos, goarch)
        if key not in _WHEEL_PLATFORMS:
            raise RuntimeError(
                f"Unsupported target: GOOS={goos} GOARCH={goarch}. "
                f"Set HATCH_GOOS/HATCH_GOARCH to one of: "
                f"{sorted(_WHEEL_PLATFORMS)}"
            )

        bin_name = "git-third-party"
        if goos == "windows":
            bin_name += ".exe"

        root = Path(self.root)
        # Go source lives at the repo root; this pyproject is in python/.
        go_root = root.parent
        out_dir = root / "build" / f"{goos}-{goarch}"
        out_dir.mkdir(parents=True, exist_ok=True)
        bin_out_path = out_dir / bin_name

        base_env = dict(os.environ)
        base_env["GOOS"] = goos
        base_env["GOARCH"] = goarch

        # Static CLI binary. CGO_ENABLED=0 keeps the manylinux tag honest.
        cli_env = dict(base_env)
        cli_env["CGO_ENABLED"] = "0"
        subprocess.run(
            [
                "go",
                "build",
                "-trimpath",
                "-ldflags",
                "-s -w",
                "-o",
                str(bin_out_path),
                "./cmd/git-third-party",
            ],
            check=True,
            env=cli_env,
            cwd=go_root,
        )

        force_include = {}

        # cgo shared library. GIT_THIRD_PARTY_SKIP_SHAREDLIB lets dev
        # builds opt out without a C cross-toolchain.
        if not _truthy_env("GIT_THIRD_PARTY_SKIP_SHAREDLIB"):
            lib_name = _shared_lib_name(goos)
            lib_out_path = out_dir / lib_name
            lib_env = dict(base_env)
            lib_env["CGO_ENABLED"] = "1"
            subprocess.run(
                [
                    "go",
                    "build",
                    "-buildmode=c-shared",
                    "-trimpath",
                    "-ldflags",
                    "-s -w",
                    "-o",
                    str(lib_out_path),
                    "./cmd/git-third-party-lib",
                ],
                check=True,
                env=lib_env,
                cwd=go_root,
            )
            # Stage into the package directory so hatchling picks it up
            # as package data alongside the .py modules. The on-disk dir
            # is hyphenated; the sources map in pyproject.toml rewrites
            # the wheel path to git_third_party/ so Python can import it.
            pkg_dir = root / "git-third-party"
            pkg_lib_path = pkg_dir / lib_name
            shutil.copyfile(lib_out_path, pkg_lib_path)
            self._staged_lib_path = pkg_lib_path
            force_include[str(pkg_lib_path)] = f"git_third_party/{lib_name}"

        if force_include:
            existing = build_data.get("force_include") or {}
            existing.update(force_include)
            build_data["force_include"] = existing

        # Tell hatchling: this is a platform wheel, here's its tag, and
        # ship the CLI binary under the wheel's data/scripts/.
        build_data["pure_python"] = False
        build_data["infer_tag"] = False
        build_data["tag"] = f"py3-none-{_WHEEL_PLATFORMS[key]}"
        build_data["shared_scripts"] = {str(bin_out_path): bin_name}

    def finalize(self, version, build_data, artifact_path):
        # Avoid leaving a stale shared lib in the source tree across
        # repeated builds; the next initialize() re-copies it.
        path = getattr(self, "_staged_lib_path", None)
        if path is not None:
            try:
                Path(path).unlink()
            except FileNotFoundError:
                pass


def _truthy_env(name: str) -> bool:
    val = os.environ.get(name, "").strip().lower()
    return val in ("1", "true", "yes", "on")


def _default_goos() -> str:
    s = platform.system().lower()
    return {"linux": "linux", "darwin": "darwin", "windows": "windows"}.get(s, s)


def _default_goarch() -> str:
    m = platform.machine().lower()
    return {
        "x86_64": "amd64",
        "amd64": "amd64",
        "aarch64": "arm64",
        "arm64": "arm64",
    }.get(m, m)
