"""Python bindings for git-third-party.

Library-only: callers script vendoring operations by importing this
module. The CLI binary (`git-third-party`) is unchanged and remains
the canonical user-facing tool.

Example:
    from third_party import init, add, list_
    init(".")
    add(".", dir="vendor/foo", url="https://github.com/x/y", follow="main")
    for e in list_("."):
        print(e.dir, e.commit)
"""

from __future__ import annotations

from ._exceptions import (
    CheckDirtyError,
    ConfigError,
    ConflictError,
    GitThirdPartyError,
    NetworkError,
)
from ._ffi import version as _version
from .api import (
    add,
    diff_patch,
    info,
    init,
    list_,
    remove,
    rename,
    save_patch,
    set_,
    unset,
    update,
)
from .types import Entry, EntryResult

__all__ = [
    "Entry",
    "EntryResult",
    "GitThirdPartyError",
    "ConfigError",
    "NetworkError",
    "ConflictError",
    "CheckDirtyError",
    "init",
    "add",
    "set_",
    "unset",
    "update",
    "list_",
    "remove",
    "rename",
    "save_patch",
    "diff_patch",
    "info",
    "version",
]


def version() -> str:
    """Return the git-third-party version baked into the shared library."""
    return _version()
