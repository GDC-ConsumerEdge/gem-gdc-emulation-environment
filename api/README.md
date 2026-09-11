# GEM REST API Service

The GEM REST API provides programmatic endpoints to orchestrate the lifecycle of GEM infrastructure (Foundations, Admin Workstations, Edge Routers, and Clusters) and manage day-2 GDC emulation workloads (VirtualMachines, Secondary Networks, ConfigSync, and Pods).

## Features
- **Async Long-Running Operations**: Infrastructure build and teardown requests execute asynchronously, returning `202 Accepted` with unique operation tracking IDs.
- **Real-Time Logs & Observability**: Stream live execution output using Server-Sent Events (`text/event-stream`) or poll step-by-step human-readable status.
- **Resource Locking & Cancellation**: Mutex guards prevent concurrent race conditions on clusters; running operations can be cancelled safely.
- **Cloud Run & Serverless Ready**: Configurable via environment variables and standard container lifecycles.

## Development with `uv`

```bash
cd api

# Create virtual environment and sync dependencies
uv sync --all-extras

# Run unit tests
uv run pytest

# Run linter and formatter
uv run ruff check .
uv run ruff format --check .

# Start development server
uv run uvicorn gem_api.main:app --reload --port 8080
```
