# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import asyncio
import contextlib
import logging
import os
import signal
from collections import deque
from collections.abc import AsyncGenerator
from datetime import UTC, datetime
from pathlib import Path

from fastapi import HTTPException, status

from gem_api.config import get_settings
from gem_api.models.operations import (
    OperationCancelResponse,
    OperationResponse,
    OperationStatus,
    OperationType,
)

logger = logging.getLogger("gem_api.operations")


class OperationRecord:
    """Internal tracking record for a single operation."""

    def __init__(
        self,
        operation_id: str,
        operation_type: OperationType,
        target_resource: str,
        initial_step: str = "Initializing",
        initial_message: str = "Operation queued...",
        max_buffer_lines: int = 1000,
    ) -> None:
        self.operation_id = operation_id
        self.operation_type = operation_type
        self.target_resource = target_resource
        self.status = OperationStatus.QUEUED
        self.current_step: str | None = initial_step
        self.message: str | None = initial_message
        self.created_at = datetime.now(UTC)
        self.updated_at = datetime.now(UTC)
        self.completed_at: datetime | None = None
        self.error: str | None = None

        self.log_buffer: deque[str] = deque(maxlen=max_buffer_lines)
        self.subscribers: list[asyncio.Queue[str | None]] = []
        self.process: asyncio.subprocess.Process | None = None
        self.task: asyncio.Task | None = None

    def to_response(self) -> OperationResponse:
        """Convert internal record to Pydantic API response model."""
        return OperationResponse(
            operation_id=self.operation_id,
            operation_type=self.operation_type,
            status=self.status,
            target_resource=self.target_resource,
            current_step=self.current_step,
            message=self.message,
            created_at=self.created_at,
            updated_at=self.updated_at,
            completed_at=self.completed_at,
            error=self.error,
        )


class OperationManager:
    """Singleton manager for operation lifecycle tracking, logging, and concurrency control."""

    def __init__(self) -> None:
        self._operations: dict[str, OperationRecord] = {}
        self._lock = asyncio.Lock()

    def _get_log_file_path(self, operation_id: str) -> Path:
        settings = get_settings()
        log_dir = settings.log_dir
        log_dir.mkdir(parents=True, exist_ok=True)
        return log_dir / f"{operation_id}.log"

    async def register_operation(
        self,
        operation_id: str,
        operation_type: OperationType,
        target_resource: str,
        initial_step: str = "Initializing",
        initial_message: str = "Operation queued...",
    ) -> OperationRecord:
        """Register a new operation.

        Raises HTTPException 409 if an active operation is already running on the target resource.
        """
        async with self._lock:
            # Check for existing active operations targeting the same resource
            for op in self._operations.values():
                if op.target_resource == target_resource and op.status in (
                    OperationStatus.QUEUED,
                    OperationStatus.RUNNING,
                ):
                    raise HTTPException(
                        status_code=status.HTTP_409_CONFLICT,
                        detail=(
                            f"An active operation '{op.operation_id}' of type '{op.operation_type}' "
                            f"is already in progress for target resource '{target_resource}'."
                        ),
                    )

            settings = get_settings()
            record = OperationRecord(
                operation_id=operation_id,
                operation_type=operation_type,
                target_resource=target_resource,
                initial_step=initial_step,
                initial_message=initial_message,
                max_buffer_lines=settings.max_log_buffer_lines,
            )
            self._operations[operation_id] = record

            # Initialize log file
            log_path = self._get_log_file_path(operation_id)
            init_line = (
                f"[{record.created_at.strftime('%Y-%m-%dT%H:%M:%SZ')}] "
                f"Registered operation {operation_id} ({operation_type}) on {target_resource}."
            )

            def _init_log_file() -> None:
                try:
                    with open(log_path, "w", encoding="utf-8") as f:
                        f.write(init_line + "\n")
                except OSError as e:
                    logger.warning(
                        "Could not write initial log file %s: %s", log_path, e
                    )

            await asyncio.to_thread(_init_log_file)

            record.log_buffer.append(init_line)
            return record

    async def update_operation(
        self,
        operation_id: str,
        status: OperationStatus | None = None,
        current_step: str | None = None,
        message: str | None = None,
        error: str | None = None,
        completed: bool = False,
    ) -> OperationRecord:
        """Update an existing operation's status and details."""
        async with self._lock:
            record = self._operations.get(operation_id)
            if not record:
                raise HTTPException(
                    status_code=status.HTTP_404_NOT_FOUND,
                    detail=f"Operation '{operation_id}' not found.",
                )

            record.updated_at = datetime.now(UTC)
            if status is not None:
                record.status = status
            if current_step is not None:
                record.current_step = current_step
            if message is not None:
                record.message = message
            if error is not None:
                record.error = error
            if completed:
                record.completed_at = datetime.now(UTC)
                # Notify SSE subscribers that the stream has closed
                for sub_queue in record.subscribers:
                    await sub_queue.put(None)

            return record

    def append_log(self, operation_id: str, log_line: str) -> None:
        """Append a log line to the operation's buffer, write to disk, and push to SSE subscribers."""
        record = self._operations.get(operation_id)
        if not record:
            return

        formatted_line = log_line.rstrip()
        if not formatted_line:
            return

        # Ensure timestamp prefix if not already present
        if not formatted_line.startswith("[20"):
            now_str = datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
            formatted_line = f"[{now_str}] {formatted_line}"

        record.log_buffer.append(formatted_line)

        # Write to log file
        log_path = self._get_log_file_path(operation_id)
        try:
            with open(log_path, "a", encoding="utf-8") as f:
                f.write(formatted_line + "\n")
        except OSError as e:
            logger.warning("Failed to write to log file %s: %s", log_path, e)

        # Distribute to SSE subscribers
        for sub_queue in list(record.subscribers):
            with contextlib.suppress(asyncio.QueueFull):
                sub_queue.put_nowait(formatted_line)

    def get_operation(self, operation_id: str) -> OperationResponse | None:
        """Retrieve operation details by ID."""
        record = self._operations.get(operation_id)
        if record:
            return record.to_response()
        return None

    def list_operations(self) -> list[OperationResponse]:
        """List all tracked operations."""
        return [record.to_response() for record in self._operations.values()]

    def get_logs(self, operation_id: str, tail: int | None = None) -> list[str]:
        """Retrieve buffered or stored log lines for an operation."""
        record = self._operations.get(operation_id)
        if not record:
            # Check if file exists on disk
            log_path = self._get_log_file_path(operation_id)
            if log_path.exists():
                try:
                    with open(log_path, encoding="utf-8") as f:
                        lines = [line.rstrip() for line in f]
                        if tail and tail > 0:
                            return lines[-tail:]
                        return lines
                except OSError:
                    pass
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Logs for operation '{operation_id}' not found.",
            )

        lines = list(record.log_buffer)
        if tail and tail > 0:
            return lines[-tail:]
        return lines

    async def stream_logs(self, operation_id: str) -> AsyncGenerator[str]:
        """Stream logs for an operation via SSE async generator."""
        record = self._operations.get(operation_id)
        if not record:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND,
                detail=f"Operation '{operation_id}' not found.",
            )

        queue: asyncio.Queue[str | None] = asyncio.Queue(maxsize=100)
        record.subscribers.append(queue)

        try:
            # Yield historical lines first
            for line in list(record.log_buffer):
                yield line

            # If already finished, return immediately
            if record.status in (
                OperationStatus.SUCCEEDED,
                OperationStatus.FAILED,
                OperationStatus.CANCELLED,
            ):
                return

            # Yield new lines as they arrive
            while True:
                line = await queue.get()
                if line is None:
                    break
                yield line
        finally:
            if queue in record.subscribers:
                record.subscribers.remove(queue)

    async def cancel_operation(self, operation_id: str) -> OperationCancelResponse:
        """Cancel an in-progress operation and terminate underlying subprocesses."""
        async with self._lock:
            record = self._operations.get(operation_id)
            if not record:
                raise HTTPException(
                    status_code=status.HTTP_404_NOT_FOUND,
                    detail=f"Operation '{operation_id}' not found.",
                )

            if record.status in (
                OperationStatus.SUCCEEDED,
                OperationStatus.FAILED,
                OperationStatus.CANCELLED,
            ):
                return OperationCancelResponse(
                    success=False,
                    operation_id=operation_id,
                    status=record.status,
                    message=f"Operation '{operation_id}' has already finished with status '{record.status}'.",
                )

            # Signal cancellation
            record.status = OperationStatus.CANCELLED
            record.completed_at = datetime.now(UTC)
            record.message = f"Operation '{operation_id}' was cancelled and background processes terminated."
            record.updated_at = datetime.now(UTC)

            # Terminate running process if any
            if record.process and record.process.returncode is None:
                pid = record.process.pid
                try:
                    logger.info(
                        "Sending SIGTERM to process group %d for operation %s",
                        pid,
                        operation_id,
                    )
                    os.killpg(os.getpgid(pid), signal.SIGTERM)
                except (ProcessLookupError, OSError) as e:
                    logger.warning("Failed to SIGTERM process %d: %s", pid, e)

                # Force kill if needed
                async def _force_kill(
                    p: asyncio.subprocess.Process, proc_pid: int
                ) -> None:
                    await asyncio.sleep(2.0)
                    if p.returncode is None:
                        try:
                            logger.info("Sending SIGKILL to process group %d", proc_pid)
                            os.killpg(os.getpgid(proc_pid), signal.SIGKILL)
                        except (ProcessLookupError, OSError):
                            pass

                asyncio.create_task(_force_kill(record.process, pid))

            # Cancel asyncio task if any
            if record.task and not record.task.done():
                record.task.cancel()

            # Notify subscribers
            for sub_queue in record.subscribers:
                await sub_queue.put(None)

            cancel_log = f"[{record.completed_at.strftime('%Y-%m-%dT%H:%M:%SZ')}] Operation cancelled by user request."
            self.append_log(operation_id, cancel_log)

            return OperationCancelResponse(
                success=True,
                operation_id=operation_id,
                status=OperationStatus.CANCELLED,
                message=record.message,
            )

    def reset(self) -> None:
        """Reset internal state (primarily for testing)."""
        self._operations.clear()


# Global Singleton Instance
_operation_manager = OperationManager()


def get_operation_manager() -> OperationManager:
    """Return the global OperationManager singleton instance."""
    return _operation_manager
