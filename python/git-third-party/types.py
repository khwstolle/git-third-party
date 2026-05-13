"""Dataclasses mirroring the Go-side JSON shapes.

`Entry` matches the listEntry struct produced by `list --json` /
`info --json` (commands.go). `EntryResult` matches the entryResult
struct produced by mutating commands (output.go).
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Dict, List


@dataclass
class Entry:
    dir: str
    url: str
    follow: str = ""
    pin: str = ""
    subdir: str = ""
    include: List[str] = field(default_factory=list)
    exclude: List[str] = field(default_factory=list)
    commit: str = ""
    patched: bool = False
    conflicts: bool = False

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "Entry":
        return cls(
            dir=d.get("dir", ""),
            url=d.get("url", ""),
            follow=d.get("follow", ""),
            pin=d.get("pin", ""),
            subdir=d.get("subdir", ""),
            include=list(d.get("include") or []),
            exclude=list(d.get("exclude") or []),
            commit=d.get("commit", ""),
            patched=bool(d.get("patched", False)),
            conflicts=bool(d.get("conflicts", False)),
        )


@dataclass
class EntryResult:
    dir: str = ""
    action: str = ""
    url: str = ""
    from_commit: str = ""
    to_commit: str = ""
    new_dir: str = ""
    tree_patch: str = ""
    conflicts: bool = False
    dry_run: bool = False
    diff: str = ""

    @classmethod
    def from_json(cls, d: Dict[str, Any]) -> "EntryResult":
        return cls(
            dir=d.get("dir", ""),
            action=d.get("action", ""),
            url=d.get("url", ""),
            from_commit=d.get("from_commit", ""),
            to_commit=d.get("to_commit", ""),
            new_dir=d.get("new_dir", ""),
            tree_patch=d.get("tree_patch", ""),
            conflicts=bool(d.get("conflicts", False)),
            dry_run=bool(d.get("dry_run", False)),
            diff=d.get("diff", ""),
        )
