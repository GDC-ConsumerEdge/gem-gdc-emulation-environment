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

logger = logging.getLogger("gem_api.process")

# How long to wait for a killed child to be reaped before giving up on it.
_REAP_TIMEOUT = 5.0


async def communicate_or_kill(
    proc: asyncio.subprocess.Process,
    timeout: float,
    input_bytes: bytes | None = None,
) -> tuple[bytes, bytes]:
    """Run ``proc.communicate()`` under a timeout, killing the child if it expires.

    ``asyncio.wait_for`` only cancels the local coroutine; the child keeps running,
    holding its pipes open and eventually lingering as a zombie. Every short-lived
    CLI shell-out (gcloud, kubectl) goes through here so a hung binary cannot
    accumulate orphaned processes inside the API container.
    """
    try:
        return await asyncio.wait_for(proc.communicate(input_bytes), timeout)
    except TimeoutError:
        with contextlib.suppress(ProcessLookupError):
            proc.kill()
        try:
            await asyncio.wait_for(proc.wait(), _REAP_TIMEOUT)
        except TimeoutError:
            logger.warning(
                "Timed-out process pid=%s did not exit after SIGKILL", proc.pid
            )
        raise
