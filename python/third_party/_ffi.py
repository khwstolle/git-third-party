"""ctypes-based loader for the libgitthirdparty shared library.

Locates the platform-appropriate library file next to this package, or
honors $GIT_THIRD_PARTY_LIB for development builds. Each gtp_* symbol
takes a single JSON request string and returns a single JSON response
string allocated by Go; we copy it into a Python str and free the Go
allocation through gtp_free.
"""

from __future__ import annotations

import ctypes
import json
import os
import sys
from pathlib import Path
from typing import Any, Dict


def _libname() -> str:
    if sys.platform.startswith("linux"):
        return "libgitthirdparty.so"
    if sys.platform == "darwin":
        return "libgitthirdparty.dylib"
    if sys.platform == "win32":
        return "gitthirdparty.dll"
    raise RuntimeError(f"third_party: unsupported platform: {sys.platform}")


def _find_lib() -> Path:
    env = os.environ.get("GIT_THIRD_PARTY_LIB")
    if env:
        p = Path(env)
        if not p.exists():
            raise FileNotFoundError(f"GIT_THIRD_PARTY_LIB={env!s} not found")
        return p
    here = Path(__file__).resolve().parent
    p = here / _libname()
    if p.exists():
        return p
    raise FileNotFoundError(
        f"third_party: shared library {_libname()!r} not found next to {here}; "
        "set GIT_THIRD_PARTY_LIB to point at a development build"
    )


_LIB: ctypes.CDLL = ctypes.CDLL(str(_find_lib()))

_SYMBOLS = (
    "gtp_init",
    "gtp_add",
    "gtp_set",
    "gtp_unset",
    "gtp_update",
    "gtp_list",
    "gtp_remove",
    "gtp_rename",
    "gtp_save_patch",
    "gtp_diff_patch",
    "gtp_info",
)

# Bind argtypes/restype. Use c_void_p for the return value (not c_char_p)
# so ctypes does not auto-copy and free; we own the pointer and pass it
# back to gtp_free after copying its contents.
for _sym in _SYMBOLS:
    _fn = getattr(_LIB, _sym)
    _fn.argtypes = [ctypes.c_char_p]
    _fn.restype = ctypes.c_void_p

_LIB.gtp_free.argtypes = [ctypes.c_void_p]
_LIB.gtp_free.restype = None
_LIB.gtp_version.argtypes = []
_LIB.gtp_version.restype = ctypes.c_void_p


def call(symbol: str, request: Dict[str, Any]) -> Dict[str, Any]:
    """Invoke gtp_<symbol> with the request dict and decode the JSON response."""
    fn = getattr(_LIB, symbol)
    payload = json.dumps(request).encode("utf-8")
    ptr = fn(payload)
    if not ptr:
        raise RuntimeError(f"{symbol}: bridge returned NULL")
    try:
        raw = ctypes.string_at(ptr).decode("utf-8")
    finally:
        _LIB.gtp_free(ptr)
    return json.loads(raw)


def version() -> str:
    """Return the version string baked into the shared library."""
    ptr = _LIB.gtp_version()
    if not ptr:
        return ""
    try:
        return ctypes.string_at(ptr).decode("utf-8")
    finally:
        _LIB.gtp_free(ptr)
