---
name: devport
description: Manage dev services with stable port assignment and process supervision. Use when the user wants to run, list, stop, restart, or remove dev services on a shared machine.
---

# devport

Stable port assignment and process supervision for dev services. Each service gets a unique port in 19000-19999, persisted across restarts. No central daemon — each `devport run` is its own supervisor.

## Usage

```bash
# Start a service with a named key
devport run --key myapp -- npm run dev

# Start a service (identity derived from cwd + cmd)
# NOTE: quote or escape $PORT so the shell doesn't expand it — devport expands env vars in args
devport run -- python3 -m http.server '$PORT'

# Custom port env var name
devport run --port-env VITE_PORT --key frontend -- npm run dev

# List all services (JSON)
devport ls

# List only running services
devport ls --active

# Stop a service (by hash prefix, like git)
devport stop d7e

# Restart a service's child process
devport restart d7e

# Remove a service entirely (frees port, deletes state)
devport rm d7e
```

## How it works

- **Identity**: SHA-256 hash of `--key` or `cwd + cmd`, truncated to 10 hex chars
- **HashID**: shortest unique prefix (min 3 chars), frozen at registration
- **Port allocation**: lowest unused in 19000-19999, stale ports (>30d) reclaimable
- **Liveness**: flock on `~/.local/share/devport/locks/<hash>.lock` — kernel auto-releases on exit
- **Registration lock**: serializes port + hashid assignment across concurrent `devport run` calls
- **Supervisor**: spawns child in own process group, SIGINT kills, SIGHUP/SIGTSTP restarts, crash backoff
- **Idempotent**: re-running prints service info as JSON and exits if already running

## Building

```bash
go build -o devport ./cli/devport
```
