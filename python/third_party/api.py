"""Pythonic wrappers around the git-third-party Go commands.

Each function maps to one do*() entry point in commands.go. `repo_path`
defaults to "." (the current working directory must sit inside a host
git repo). Mutating commands accept `dry_run=` and `commit_msg=` to
match the CLI's --dry-run and --commit flags.
"""

from __future__ import annotations

import json as _json
from typing import Any, Dict, List, Optional

from . import _ffi
from ._exceptions import raise_from_response
from .types import Entry, EntryResult


def _envelope(
    repo_path: str,
    *,
    dry_run: bool = False,
    json_out: bool = True,
    commit_msg: str = "",
    log_level: str = "",
    log_format: str = "",
    color: str = "",
    args: Optional[Dict[str, Any]] = None,
) -> Dict[str, Any]:
    return {
        "repo_path": repo_path,
        "dry_run": dry_run,
        "json_out": json_out,
        "commit_msg": commit_msg,
        "log_level": log_level,
        "log_format": log_format,
        "color": color,
        "args": args or {},
    }


def _parsed_results(resp: Dict[str, Any]) -> List[Dict[str, Any]]:
    raw = resp.get("results")
    if raw is None or raw == "":
        return []
    if isinstance(raw, (list, dict)):
        return raw if isinstance(raw, list) else [raw]
    if isinstance(raw, (bytes, bytearray)):
        raw = raw.decode("utf-8")
    return _json.loads(raw)


def _first_entry_result(resp: Dict[str, Any]) -> EntryResult:
    rows = _parsed_results(resp)
    if not rows:
        return EntryResult()
    return EntryResult.from_json(rows[0])


def init(repo_path: str = ".") -> EntryResult:
    """Create third-party.toml at the repo root if absent. Idempotent."""
    resp = _ffi.call("gtp_init", _envelope(repo_path))
    raise_from_response(resp)
    return _first_entry_result(resp)


def add(
    repo_path: str = ".",
    *,
    dir: str,
    url: str,
    follow: str = "",
    pin: str = "",
    subdir: str = "",
    include: Optional[List[str]] = None,
    exclude: Optional[List[str]] = None,
    allow_dir_exists: bool = False,
    dry_run: bool = False,
    commit_msg: str = "",
) -> EntryResult:
    resp = _ffi.call(
        "gtp_add",
        _envelope(
            repo_path,
            dry_run=dry_run,
            commit_msg=commit_msg,
            args={
                "url": url,
                "follow": follow,
                "pin": pin,
                "dir": dir,
                "subdir": subdir,
                "include": list(include or []),
                "exclude": list(exclude or []),
                "allow_dir_exists": allow_dir_exists,
            },
        ),
    )
    raise_from_response(resp)
    return _first_entry_result(resp)


def set_(
    repo_path: str = ".",
    *,
    dir: str,
    url: str = "",
    follow: str = "",
    pin: str = "",
    subdir: Optional[str] = None,
    include: Optional[List[str]] = None,
    exclude: Optional[List[str]] = None,
    dry_run: bool = False,
    commit_msg: str = "",
) -> EntryResult:
    args: Dict[str, Any] = {
        "dir": dir,
        "url": url,
        "follow": follow,
        "pin": pin,
    }
    if subdir is not None:
        args["subdir"] = subdir
    if include is not None:
        args["include"] = list(include)
    if exclude is not None:
        args["exclude"] = list(exclude)
    resp = _ffi.call(
        "gtp_set",
        _envelope(repo_path, dry_run=dry_run, commit_msg=commit_msg, args=args),
    )
    raise_from_response(resp)
    return _first_entry_result(resp)


def unset(
    repo_path: str = ".",
    *,
    dir: str,
    fields: List[str],
    dry_run: bool = False,
    commit_msg: str = "",
) -> EntryResult:
    resp = _ffi.call(
        "gtp_unset",
        _envelope(
            repo_path,
            dry_run=dry_run,
            commit_msg=commit_msg,
            args={"dir": dir, "fields": list(fields)},
        ),
    )
    raise_from_response(resp)
    return _first_entry_result(resp)


def update(
    repo_path: str = ".",
    *,
    dir: Optional[str] = None,
    check: bool = False,
    dry_run: bool = False,
    commit_msg: str = "",
) -> List[EntryResult]:
    """Re-resolve and re-pull entries. With check=True, raise CheckDirtyError if any entry would change."""
    resp = _ffi.call(
        "gtp_update",
        _envelope(
            repo_path,
            dry_run=dry_run,
            commit_msg=commit_msg,
            args={"dir": dir or "", "check": check},
        ),
    )
    raise_from_response(resp)
    return [EntryResult.from_json(d) for d in _parsed_results(resp)]


def list_(repo_path: str = ".", *, dir: Optional[str] = None) -> List[Entry]:
    resp = _ffi.call(
        "gtp_list",
        _envelope(repo_path, args={"dir": dir or ""}),
    )
    raise_from_response(resp)
    return [Entry.from_json(d) for d in _parsed_results(resp)]


def remove(
    repo_path: str = ".",
    *,
    dir: str,
    dry_run: bool = False,
    commit_msg: str = "",
) -> EntryResult:
    resp = _ffi.call(
        "gtp_remove",
        _envelope(
            repo_path,
            dry_run=dry_run,
            commit_msg=commit_msg,
            args={"dir": dir},
        ),
    )
    raise_from_response(resp)
    return _first_entry_result(resp)


def rename(
    repo_path: str = ".",
    *,
    dir: str,
    new_dir: str,
    allow_dir_exists: bool = False,
    dry_run: bool = False,
    commit_msg: str = "",
) -> EntryResult:
    resp = _ffi.call(
        "gtp_rename",
        _envelope(
            repo_path,
            dry_run=dry_run,
            commit_msg=commit_msg,
            args={
                "dir": dir,
                "new_dir": new_dir,
                "allow_dir_exists": allow_dir_exists,
            },
        ),
    )
    raise_from_response(resp)
    return _first_entry_result(resp)


def save_patch(
    repo_path: str = ".",
    *,
    dir: str,
    dry_run: bool = False,
    commit_msg: str = "",
) -> EntryResult:
    """Experimental: save the local modifications under `dir` as a tree-patch."""
    resp = _ffi.call(
        "gtp_save_patch",
        _envelope(
            repo_path,
            dry_run=dry_run,
            commit_msg=commit_msg,
            args={"dir": dir},
        ),
    )
    raise_from_response(resp)
    return _first_entry_result(resp)


def diff_patch(repo_path: str = ".", *, dir: str) -> EntryResult:
    """Experimental: emit the recorded tree-patch for `dir` as a diff."""
    resp = _ffi.call(
        "gtp_diff_patch",
        _envelope(repo_path, args={"dir": dir}),
    )
    raise_from_response(resp)
    return _first_entry_result(resp)


def info(repo_path: str = ".", *, dir: str) -> Entry:
    resp = _ffi.call(
        "gtp_info",
        _envelope(repo_path, args={"dir": dir}),
    )
    raise_from_response(resp)
    rows = _parsed_results(resp)
    if not rows:
        raise LookupError(f"info: no entry for {dir!r}")
    return Entry.from_json(rows[0])
