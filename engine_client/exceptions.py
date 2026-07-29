"""Stable client-side errors for the HypoMux engine protocol."""

from __future__ import annotations

from typing import Any


class EngineClientError(RuntimeError):
    """Base class for local engine-client failures."""


class EngineStateError(EngineClientError):
    """The requested operation is invalid for the current client state."""


class EngineTimeoutError(EngineClientError):
    """The engine did not answer within the request deadline."""


class EngineProtocolError(EngineClientError):
    """The engine produced a malformed or incompatible protocol message."""


class EngineProcessError(EngineClientError):
    """The engine process exited or could not be started."""

    def __init__(self, message: str, *, returncode: int | None = None):
        super().__init__(message)
        self.returncode = returncode


class EngineRemoteError(EngineClientError):
    """A structured error returned by the engine."""

    def __init__(
        self,
        code: str,
        message: str,
        *,
        details: dict[str, Any] | None = None,
        request_id: str = "",
    ):
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message
        self.details = details or {}
        self.request_id = request_id
