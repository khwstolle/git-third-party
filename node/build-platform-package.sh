#!/usr/bin/env bash
# Build one platform-specific npm package: a static CLI binary plus the
# c-shared FFI library, both produced by `go build`. Mirrors the
# per-platform output of python/hatch_build.py for the Python wheel target.
#
# Required env:
#   GOOS     one of linux | darwin | windows
#   GOARCH   one of amd64 | arm64
#   VERSION  semver string written into the per-platform package.json
#
# Optional env:
#   GTP_NPM_SKIP_SHAREDLIB=1  skip the c-shared lib (CLI-only package).
#                             Mirrors GIT_THIRD_PARTY_SKIP_SHAREDLIB in
#                             python/hatch_build.py for cross-compile dev runs
#                             without a C cross-toolchain.
#   OUT_DIR                   override output directory (default: node/dist).

set -euo pipefail

: "${GOOS:?GOOS must be set (linux|darwin|windows)}"
: "${GOARCH:?GOARCH must be set (amd64|arm64)}"
: "${VERSION:?VERSION must be set (e.g. 0.0.1)}"

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="${OUT_DIR:-${repo_root}/node/dist}"

# (GOOS, GOARCH) -> (npm os, npm cpu, suffix). Mirrors _WHEEL_PLATFORMS.
case "${GOOS}/${GOARCH}" in
  linux/amd64)   npm_os=linux;  npm_cpu=x64;   suffix=linux-x64 ;;
  linux/arm64)   npm_os=linux;  npm_cpu=arm64; suffix=linux-arm64 ;;
  darwin/arm64)  npm_os=darwin; npm_cpu=arm64; suffix=darwin-arm64 ;;
  windows/amd64) npm_os=win32;  npm_cpu=x64;   suffix=win32-x64 ;;
  *)
    echo "unsupported target: GOOS=${GOOS} GOARCH=${GOARCH}" >&2
    exit 2
    ;;
esac

case "${GOOS}" in
  linux)   lib_name=libgitthirdparty.so ;;
  darwin)  lib_name=libgitthirdparty.dylib ;;
  windows) lib_name=gitthirdparty.dll ;;
esac

bin_name=git-third-party
[[ "${GOOS}" == "windows" ]] && bin_name=git-third-party.exe

pkg_name="git-third-party-${suffix}"
pkg_dir="${out_dir}/${pkg_name}"

rm -rf "${pkg_dir}"
mkdir -p "${pkg_dir}/bin" "${pkg_dir}/lib"

# Static CLI. CGO_ENABLED=0 keeps the artifact dependency-free across distros.
( cd "${repo_root}" && \
  GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED=0 \
    go build -trimpath -ldflags "-s -w" \
      -o "${pkg_dir}/bin/${bin_name}" \
      ./cmd/git-third-party )

# c-shared FFI lib. Skip when no cross C toolchain is available.
if [[ "${GTP_NPM_SKIP_SHAREDLIB:-}" != "1" ]]; then
  ( cd "${repo_root}" && \
    GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED=1 \
      go build -buildmode=c-shared -trimpath -ldflags "-s -w" \
        -o "${pkg_dir}/lib/${lib_name}" \
        ./cmd/git-third-party-lib )
  # The companion .h header is not needed at runtime.
  rm -f "${pkg_dir}/lib/${lib_name%.*}.h"
else
  rmdir "${pkg_dir}/lib"
fi

cat > "${pkg_dir}/package.json" <<EOF
{
  "name": "${pkg_name}",
  "version": "${VERSION}",
  "description": "Platform-specific binaries for git-third-party (${suffix}).",
  "license": "MIT",
  "homepage": "https://github.com/khwstolle/git-third-party",
  "repository": {
    "type": "git",
    "url": "https://github.com/khwstolle/git-third-party"
  },
  "os": ["${npm_os}"],
  "cpu": ["${npm_cpu}"],
  "files": ["bin", "lib"]
}
EOF

echo "built ${pkg_dir}"
