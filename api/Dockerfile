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

FROM python:3.14-slim

# Install system dependencies (curl, git, bash, ca-certificates)
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    git \
    bash \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Install uv binary from official image
COPY --from=ghcr.io/astral-sh/uv:0.8.3 /uv /uvx /bin/

WORKDIR /app

# The build context is the repository root, because the image needs both the API
# sources under api/ and ansible/group_vars/all.yaml (the single source of truth
# that gem_api.manifest refuses to start without):
#   docker build -f api/Dockerfile -t gem-api .

# Copy dependency files first for layer caching
COPY api/pyproject.toml api/uv.lock /app/

# Install the exact dependency set recorded in uv.lock rather than re-resolving
# pyproject.toml, so image builds match the tested environment.
RUN uv export --frozen --no-dev --no-emit-project -o /tmp/requirements.txt \
    && uv pip install --system --no-cache -r /tmp/requirements.txt \
    && rm /tmp/requirements.txt

# Copy application source code and group_vars configuration
COPY api/gem_api/ /app/gem_api/
COPY ansible/group_vars/all.yaml /app/ansible/group_vars/all.yaml

ENV PYTHONPATH=/app

# manifest.py and runner.py resolve repo-relative paths from this root
ENV REPO_ROOT=/app

# Operation logs, written by the unprivileged runtime user
ENV GEM_LOG_DIR=/var/log/gem-api

# Cloud Run defaults: listens on port 8080 or $PORT
ENV PORT=8080
ENV HOST=0.0.0.0

RUN useradd --system --create-home --uid 10001 gemapi \
    && mkdir -p "$GEM_LOG_DIR" \
    && chown -R gemapi:gemapi "$GEM_LOG_DIR" /app
USER gemapi

EXPOSE 8080

CMD ["sh", "-c", "uvicorn gem_api.main:app --host $HOST --port $PORT"]
