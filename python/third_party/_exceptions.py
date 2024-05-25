"""Exception hierarchy for the third_party bindings.

Maps the Go side's stable exit codes (errors.go) to Python exception
classes so callers can `except ConfigError:` instead of inspecting an
integer code.
"""

from __future__ import annotations

from typing import Any, Dict


class GitThirdPartyError(Exception):
    """Base class for all errors raised by the bindings.

    Carries the Go side's exit_code, the captured stderr, and any text
    that landed on stdout (commands that didn't go through --json).
    """

    def __init__(self, exit_code: int, message: str, stdout: str = "", stderr: str = "") -> None:
        super().__init__(message)
        self.exit_code = exit_code
        self.stdout = stdout
        self.stderr = stderr


class ConfigError(GitThirdPartyError):
    """Exit 2: TOML parse, validation, lockfile schema mismatch."""


class NetworkError(GitThirdPartyError):
    """Exit 3: fetch / ref-resolution failure."""


class ConflictError(GitThirdPartyError):
    """Exit 4: unresolvable merge conflict in update."""


class CheckDirtyError(GitThirdPartyError):
    """Exit 5: --check detected a pending change."""


_BY_CODE: Dict[int, type] = {
    2: ConfigError,
    3: NetworkError,
    4: ConflictError,
    5: CheckDirtyError,
}


def raise_from_response(resp: Dict[str, Any]) -> None:
    code = int(resp.get("exit_code", 1))
    if code == 0:
        return
    cls = _BY_CODE.get(code, GitThirdPartyError)
    raise cls(
        code,
        resp.get("error") or "git-third-party command failed",
        resp.get("stdout", "") or "",
        resp.get("stderr", "") or "",
    )
