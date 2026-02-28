---
name: devport
description: Manage dev services with stable port assignment and process supervision. Use when the user wants to run, list, stop, restart, or remove dev services on a shared machine.
---

# devport

Stable port assignment and process supervision for dev services on shared dev machines.

Each service gets a unique port in 19000-19999, persisted across restarts. No central daemon — each `devport run` is its own supervisor. Opt-in Tailscale integration exposes services to your tailnet.

## Why

Running dev services on a shared machine (e.g. a Mac Mini) requires manually picking ports, remembering which port maps to what, and setting up Tailscale by hand. Ports clash when multiple projects use the same default. There's no way to know if a service is already running.

devport solves this:
- Automatic, stable port assignment — same port every time you restart
- Process supervision with crash recovery and graceful restart
- Opt-in Tailscale exposure with automatic service approval
- Idempotent operations — safe to re-run without duplication
- No daemon — filesystem is the database, kernel flock is liveness

## Building

```bash
go build -o devport ./cli/devport
```

## Commands

### `devport run` — Start a supervised service

```bash
# Named service — identity derived from key
devport run --key myapp -- npm run dev

# Unnamed service — identity derived from cwd + cmd
devport run -- go run ./cmd/server

# Use $PORT in command args (quote to prevent shell expansion)
devport run -- python3 -m http.server '$PORT'

# Custom port env var name (default is PORT)
devport run --port-env VITE_PORT --key frontend -- npm run dev

# With Tailscale exposure
devport run --key api --tailnet -- go run ./cmd/server
```

Outputs service metadata as JSON to stdout:

```json
{
  "hash": "b7d2f1a8c3",
  "hashid": "b7d",
  "key": "myapp",
  "port": 19000,
  "tailnet": false,
  "cwd": "/Users/me/projects/myapp",
  "cmd": ["npm", "run", "dev"]
}
```

**Idempotent**: if the service is already running, prints existing info and exits — no duplicate supervisor.

**Signal handling while running:**
- `SIGINT` / `SIGTERM` — kill child, exit supervisor
- `SIGHUP` / `SIGTSTP` (ctrl-z) — graceful restart: SIGTERM child, wait 5s, SIGKILL if needed, respawn

**Crash recovery**: automatic restart with exponential backoff (1s → 30s max), resets after child runs >5s.

### `devport ls` — List services

```bash
# All registered services (JSON array)
devport ls

# Only running services
devport ls --active
```

Output fields: `hash`, `hashid`, `key`, `status` (running/stopped/unknown), `port`, `tailnet`, `url`, `cwd`, `cmd`, `last_up`.

### `devport stop` — Stop a service

```bash
# Use hash prefix (like git) to refer to services
devport stop b7d
```

Sends SIGTERM to the supervisor. Port stays reserved — use `rm` to free it.

### `devport restart` — Restart a service's child process

```bash
devport restart b7d
```

Sends SIGHUP to the supervisor. The supervisor gracefully restarts the child (SIGTERM → wait 5s → SIGKILL → respawn). Service stays supervised throughout.

### `devport rm` — Remove a service entirely

```bash
devport rm b7d
```

Stops the service, tears down Tailscale (if enabled), deletes all state files. Frees the port for reuse.

### `devport tailup` / `devport taildown` — Toggle Tailscale exposure

```bash
# Enable Tailscale for an existing service
devport tailup b7d

# Disable Tailscale for an existing service
devport taildown b7d
```

## Service Identity

Every service is identified by a 10-character SHA-256 hash:
- `--key myapp` → `hash("myapp")`
- No key → `hash(cwd + " " + cmd args)`

Services are referenced by **hash prefix** (minimum 3 chars), like git commits:

```bash
devport stop b7d      # matches hash starting with "b7d"
devport restart a3f   # use more chars if ambiguous
```

The **hashid** (shortest unique prefix) is frozen at registration time — it never changes even if new services with similar hashes are added later.

## Port Assignment

- Range: 19000-19999 (1000 ports)
- First run: picks lowest unused port, persists in service JSON
- Subsequent runs: reuses the stored port (stable across restarts)
- `devport stop`: port stays reserved (not freed)
- `devport rm`: port freed for reuse
- Stale ports (last_up >30 days) are reclaimable when the pool is exhausted

## State Storage

All state lives under `~/.local/share/devport/`:

```
~/.local/share/devport/
  services/<hash>.json    — service metadata (persistent reservation)
  locks/<hash>.lock       — identity lock (supervisor liveness via flock)
  locks/register.lock     — serializes concurrent registrations
```

The JSON file **is** the reservation. As long as it exists, the port and hashid are taken — even if the supervisor isn't running. Only `devport rm` deletes it.

Liveness is always a live kernel query (flock probe), never stale PID tracking.

## Tailscale Integration

Opt-in via `--tailnet` flag or `devport tailup`/`devport taildown` commands.

When enabled, devport:
- Registers a Tailscale service: `tailscale serve --service svc:<hashid> http://localhost:<port>`
- Creates the service definition via Tailscale API
- Auto-approves for the current device via API (avoids manual admin console step)

Service becomes reachable at: `https://<hashid>.<tailnet>.ts.net`

**Requirements:**
- `TAILSCALE_API_KEY` env var (for auto-approval)
- Host must be tagged (e.g. `tag:services`) in Tailscale ACLs

**Graceful degradation**: if API calls fail, the service still starts locally. Warnings are logged and you can retry with `devport tailup` later.

## Quirks and Gotchas

- **No shell**: commands are executed directly (`exec.Command`), not via `/bin/sh -c`. Pipes, redirects, and shell expansions won't work.
  - Fix: `devport run -- bash -c "npm run dev > /tmp/log.txt"`

- **$PORT expansion**: devport expands `$VAR` and `${VAR}` in command args using its own env map. Missing vars become empty strings (not preserved as literal `$VAR`). Quote `'$PORT'` to prevent shell expansion — devport handles it.

- **Port reserved on stop**: `devport stop` kills the supervisor but the port stays reserved. This prevents port churn on restart. Use `devport rm` to free a port.

- **HashID is frozen**: a service's shortest unique prefix is computed once at registration and never updated. This keeps Tailscale service names stable.

- **Concurrent safety**: multiple `devport run` calls are serialized by a blocking flock during registration. No port or hashid collisions.

- **Crash backoff**: exponential backoff (1s → 2s → 4s → ... → 30s) resets if the child runs for >5s. No configurable max retries — supervisor restarts indefinitely.

- **Corrupt JSON skipped**: `devport ls` silently skips unparseable service JSON files.

- **Process group kill**: supervisor kills the entire process group (negative PID), so child subprocesses are also terminated.

- **last_up heartbeat**: updated every 30s while the supervisor runs, regardless of child activity. Prevents accidental reclamation of active services.
