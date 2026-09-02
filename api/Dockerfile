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

# Copy dependency files first for layer caching
COPY pyproject.toml /app/

# Install dependencies using uv
RUN uv pip install --system --no-cache -r pyproject.toml

# Copy application source code and group_vars configuration
COPY gem_api/ /app/gem_api/
COPY ansible/group_vars/all.yaml /app/ansible/group_vars/all.yaml

ENV PYTHONPATH=/app

# Cloud Run defaults: listens on port 8080 or $PORT
ENV PORT=8080
ENV HOST=0.0.0.0

EXPOSE 8080

CMD ["sh", "-c", "uvicorn gem_api.main:app --host $HOST --port $PORT"]
