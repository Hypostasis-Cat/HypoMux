"""Protocol-v1 encoding and validation helpers."""

from __future__ import annotations

import json
from typing import Any

from .exceptions import EngineProtocolError, EngineRemoteError


PROTOCOL_VERSION = 1
MAX_MESSAGE_BYTES = 1024 * 1024


def encode_request(request_id: str, method: str, params: Any = None) -> bytes:
    if not request_id.strip():
        raise EngineProtocolError("request id is required")
    if not method.strip():
        raise EngineProtocolError("request method is required")

    message: dict[str, Any] = {
        "protocol": PROTOCOL_VERSION,
        "id": request_id,
        "method": method,
    }
    if params is not None:
        message["params"] = params
    try:
        payload = json.dumps(
            message,
            ensure_ascii=False,
            separators=(",", ":"),
        ).encode("utf-8")
    except (TypeError, ValueError) as exc:
        raise EngineProtocolError(f"request parameters are not JSON serializable: {exc}") from exc
    if len(payload) > MAX_MESSAGE_BYTES:
        raise EngineProtocolError("request exceeds the protocol message size limit")
    return payload + b"\n"


def decode_message(payload: bytes) -> dict[str, Any]:
    if len(payload) > MAX_MESSAGE_BYTES:
        raise EngineProtocolError("engine message exceeds the protocol size limit")
    try:
        message = json.loads(payload.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise EngineProtocolError(f"engine produced invalid JSON: {exc}") from exc
    if not isinstance(message, dict):
        raise EngineProtocolError("engine message must be a JSON object")
    if message.get("protocol") != PROTOCOL_VERSION:
        raise EngineProtocolError(
            f"unsupported engine protocol {message.get('protocol')!r}; "
            f"expected {PROTOCOL_VERSION}"
        )
    return message


def response_value(message: dict[str, Any]) -> Any:
    has_result = "result" in message
    has_error = "error" in message
    if has_result == has_error:
        raise EngineProtocolError("response must contain exactly one of result or error")
    if has_result:
        return message["result"]

    error = message["error"]
    if not isinstance(error, dict):
        raise EngineProtocolError("response error must be an object")
    code = error.get("code")
    text = error.get("message")
    details = error.get("details")
    if not isinstance(code, str) or not code.strip():
        raise EngineProtocolError("response error code is required")
    if not isinstance(text, str) or not text.strip():
        raise EngineProtocolError("response error message is required")
    if details is not None and not isinstance(details, dict):
        raise EngineProtocolError("response error details must be an object")
    raise EngineRemoteError(
        code,
        text,
        details=details,
        request_id=str(message.get("id", "")),
    )


def validate_event(message: dict[str, Any], previous_sequence: int) -> int:
    name = message.get("event")
    sequence = message.get("sequence")
    if not isinstance(name, str) or not name.strip():
        raise EngineProtocolError("event name is required")
    if not isinstance(sequence, int) or isinstance(sequence, bool) or sequence <= 0:
        raise EngineProtocolError("event sequence must be a positive integer")
    if sequence <= previous_sequence:
        raise EngineProtocolError(
            f"event sequence {sequence} is not newer than {previous_sequence}"
        )
    return sequence
