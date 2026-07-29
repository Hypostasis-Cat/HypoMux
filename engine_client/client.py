"""Thread-safe stdio JSONL client for the HypoMux Go engine."""

from __future__ import annotations

from dataclasses import dataclass, field
import subprocess
import threading
from typing import Any, Callable, Mapping

from .exceptions import (
    EngineClientError,
    EngineProcessError,
    EngineProtocolError,
    EngineStateError,
    EngineTimeoutError,
)
from .process import EngineCommand, start_engine_process, stop_process
from .protocol import (
    MAX_MESSAGE_BYTES,
    PROTOCOL_VERSION,
    decode_message,
    encode_request,
    response_value,
    validate_event,
)


EventCallback = Callable[[dict[str, Any]], None]
TextCallback = Callable[[str], None]
DisconnectCallback = Callable[[EngineClientError | None], None]


@dataclass
class _PendingRequest:
    completed: threading.Event = field(default_factory=threading.Event)
    value: Any = None
    error: EngineClientError | None = None


class EngineClient:
    """Own one engine process and multiplex requests with unsolicited events."""

    def __init__(
        self,
        command: EngineCommand,
        *,
        request_timeout: float = 2.0,
        shutdown_timeout: float = 1.0,
        environment: Mapping[str, str] | None = None,
        on_event: EventCallback | None = None,
        on_stderr: TextCallback | None = None,
        on_disconnect: DisconnectCallback | None = None,
    ):
        self._command = command
        self._request_timeout = max(float(request_timeout), 0.05)
        self._shutdown_timeout = max(float(shutdown_timeout), 0.05)
        self._environment = environment
        self._on_event = on_event
        self._on_stderr = on_stderr
        self._on_disconnect = on_disconnect

        self._state_lock = threading.RLock()
        self._write_lock = threading.Lock()
        self._process: subprocess.Popen[bytes] | None = None
        self._stdout_thread: threading.Thread | None = None
        self._stderr_thread: threading.Thread | None = None
        self._pending: dict[str, _PendingRequest] = {}
        self._next_request_id = 0
        self._last_event_sequence = 0
        self._disconnect_notified = False
        self._stopping = False
        self._hello: dict[str, Any] | None = None

    @property
    def hello(self) -> dict[str, Any] | None:
        with self._state_lock:
            return dict(self._hello) if self._hello is not None else None

    @property
    def process_id(self) -> int | None:
        with self._state_lock:
            return self._process.pid if self._process is not None else None

    def is_running(self) -> bool:
        with self._state_lock:
            return self._process is not None and self._process.poll() is None

    def start(self) -> dict[str, Any]:
        with self._state_lock:
            if self._process is not None and self._process.poll() is None:
                if self._hello is None:
                    raise EngineStateError("engine handshake is already in progress")
                return dict(self._hello)
            self._process = start_engine_process(
                self._command,
                environment=self._environment,
            )
            self._pending.clear()
            self._next_request_id = 0
            self._last_event_sequence = 0
            self._disconnect_notified = False
            self._stopping = False
            self._hello = None
            self._stdout_thread = threading.Thread(
                target=self._read_stdout,
                name="HypoMuxEngineStdout",
                daemon=True,
            )
            self._stderr_thread = threading.Thread(
                target=self._read_stderr,
                name="HypoMuxEngineStderr",
                daemon=True,
            )
            self._stdout_thread.start()
            self._stderr_thread.start()

        try:
            hello = self.request("engine.hello")
            if not isinstance(hello, dict):
                raise EngineProtocolError("engine.hello result must be an object")
            if hello.get("protocol_version") != PROTOCOL_VERSION:
                raise EngineProtocolError("engine.hello returned an incompatible protocol")
            capabilities = hello.get("capabilities")
            if not isinstance(capabilities, list) or "host.shutdown" not in capabilities:
                raise EngineProtocolError("engine does not advertise required host capabilities")
            with self._state_lock:
                self._hello = dict(hello)
            return dict(hello)
        except Exception:
            self.stop(graceful=False)
            raise

    def request(self, method: str, params: Any = None, *, timeout: float | None = None) -> Any:
        request_timeout = self._request_timeout if timeout is None else max(float(timeout), 0.01)
        with self._state_lock:
            process = self._process
            if process is None or process.poll() is not None:
                raise EngineStateError("engine process is not running")
            if self._disconnect_notified:
                raise EngineStateError("engine connection is no longer available")
            if self._stopping and method != "host.shutdown":
                raise EngineStateError("engine process is stopping")
            if process.stdin is None:
                raise EngineStateError("engine stdin is unavailable")
            self._next_request_id += 1
            request_id = f"py-{self._next_request_id}"

        payload = encode_request(request_id, method, params)
        pending = _PendingRequest()
        with self._state_lock:
            if self._process is not process or process.poll() is not None:
                raise EngineProcessError(
                    "engine exited before request could be queued",
                    returncode=process.returncode,
                )
            if self._disconnect_notified:
                raise EngineStateError("engine connection is no longer available")
            self._pending[request_id] = pending
        try:
            with self._write_lock:
                if process.poll() is not None:
                    raise EngineProcessError(
                        "engine exited before request could be sent",
                        returncode=process.returncode,
                    )
                process.stdin.write(payload)
                process.stdin.flush()
        except (BrokenPipeError, OSError, EngineProcessError) as exc:
            with self._state_lock:
                self._pending.pop(request_id, None)
            if isinstance(exc, EngineProcessError):
                raise
            raise EngineProcessError(f"could not write engine request: {exc}") from exc

        if not pending.completed.wait(request_timeout):
            with self._state_lock:
                self._pending.pop(request_id, None)
            raise EngineTimeoutError(
                f"engine request {method!r} timed out after {request_timeout:.2f}s"
            )
        if pending.error is not None:
            raise pending.error
        return pending.value

    def stop(self, *, graceful: bool = True) -> str:
        with self._state_lock:
            process = self._process
            if process is None:
                return "not-started"
            self._stopping = True

        action = "exited"
        if process.poll() is None and graceful:
            try:
                self.request(
                    "host.shutdown",
                    timeout=self._shutdown_timeout,
                )
            except EngineClientError:
                pass

        if process.poll() is None:
            try:
                process.wait(timeout=self._shutdown_timeout)
            except subprocess.TimeoutExpired:
                action = stop_process(process, self._shutdown_timeout)

        returncode = process.poll()
        self._close_streams(process)
        self._join_reader_threads()
        disconnect_error = None
        if returncode not in (None, 0) and not self._stopping:
            disconnect_error = EngineProcessError(
                f"engine process exited with code {returncode}",
                returncode=returncode,
            )
        self._mark_disconnected(disconnect_error)
        with self._state_lock:
            self._process = None
            self._hello = None
        return action

    close = stop

    def _read_stdout(self):
        with self._state_lock:
            process = self._process
        if process is None or process.stdout is None:
            return
        try:
            while True:
                line = process.stdout.readline(MAX_MESSAGE_BYTES + 2)
                if not line:
                    break
                if not line.endswith(b"\n"):
                    if len(line) > MAX_MESSAGE_BYTES:
                        raise EngineProtocolError(
                            "engine output line exceeds the protocol size limit"
                        )
                    raise EngineProtocolError("engine output ended before a JSONL delimiter")
                payload = line.rstrip(b"\r\n")
                if len(payload) > MAX_MESSAGE_BYTES:
                    raise EngineProtocolError("engine output line exceeds the protocol size limit")
                message = decode_message(payload)
                self._dispatch_message(message)
        except EngineClientError as exc:
            self._abort_protocol(exc)
            return
        except OSError as exc:
            self._mark_disconnected(EngineProcessError(f"could not read engine output: {exc}"))
            return

        returncode = process.wait()
        with self._state_lock:
            stopping = self._stopping
        error = None
        if not stopping and returncode != 0:
            error = EngineProcessError(
                f"engine process exited with code {returncode}",
                returncode=returncode,
            )
        elif not stopping:
            error = EngineProcessError("engine process exited unexpectedly", returncode=returncode)
        self._mark_disconnected(error)

    def _read_stderr(self):
        with self._state_lock:
            process = self._process
        if process is None or process.stderr is None:
            return
        try:
            while True:
                chunk = process.stderr.readline(64 * 1024)
                if not chunk:
                    return
                text = chunk.decode("utf-8", errors="replace").rstrip("\r\n")
                if text:
                    self._safe_callback(self._on_stderr, text)
        except OSError:
            return

    def _dispatch_message(self, message: dict[str, Any]):
        request_id = message.get("id")
        if isinstance(request_id, str):
            with self._state_lock:
                pending = self._pending.pop(request_id, None)
            if pending is None:
                return
            try:
                pending.value = response_value(message)
            except EngineClientError as exc:
                pending.error = exc
            finally:
                pending.completed.set()
            return

        if "event" in message:
            with self._state_lock:
                sequence = validate_event(message, self._last_event_sequence)
                self._last_event_sequence = sequence
            self._safe_callback(self._on_event, dict(message))
            return

        raise EngineProtocolError("engine message is neither a response nor an event")

    def _abort_protocol(self, error: EngineClientError):
        self._mark_disconnected(error)
        with self._state_lock:
            process = self._process
        if process is not None and process.poll() is None:
            try:
                process.terminate()
            except OSError:
                pass

    def _mark_disconnected(self, error: EngineClientError | None):
        with self._state_lock:
            if self._disconnect_notified:
                return
            self._disconnect_notified = True
            pending = list(self._pending.values())
            self._pending.clear()
        pending_error = error or EngineProcessError("engine connection closed")
        for request in pending:
            request.error = pending_error
            request.completed.set()
        self._safe_callback(self._on_disconnect, error)

    @staticmethod
    def _safe_callback(callback, *args):
        if callback is None:
            return
        try:
            callback(*args)
        except Exception:
            pass

    @staticmethod
    def _close_streams(process: subprocess.Popen[bytes]):
        for stream in (process.stdin, process.stdout, process.stderr):
            try:
                if stream is not None:
                    stream.close()
            except OSError:
                pass

    def _join_reader_threads(self):
        current = threading.current_thread()
        for thread in (self._stdout_thread, self._stderr_thread):
            if thread is not None and thread is not current:
                thread.join(timeout=self._shutdown_timeout)
