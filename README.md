---
name: devport
description: Manage dev services with stable port assignment and process supervision. Use when the user wants to run, list, stop, restart, or remove dev services on a shared machine.
---

# devport

Stable port assignment and process supervision for dev services on shared dev machines.

Each service gets a unique port in 19000-19999, persisted across restarts. No central daemon — each supervisor runs in a window of a shared `devport` tmux session. Opt-in Tailscale integration exposes services to your tailnet.

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

### `devport start` — Start a service in the background (recommended)

The primary way to run services. Launches the supervisor in a dedicated window of the shared `devport` tmux session and returns immediately.

```bash
# Named service — identity derived from key
devport start --key myapp -- npm run dev

# Unnamed service — identity derived from cwd + cmd
devport start -- go run ./cmd/server

# Use $PORT in command args (quote to prevent shell expansion)
devport start -- python3 -m http.server '$PORT'

# Custom port env var name (default is PORT)
devport start --port-env VITE_PORT --key frontend -- npm run dev

# Service with no port (background worker, compiler, etc.)
devport start --no-port --key watcher -- watchexec -e go go build ./...

# With Tailscale exposure
devport start --key api --tailnet -- go run ./cmd/server
```

Prints service metadata as JSON, then returns. The service runs in a tmux window named after the key (or hash):

```
devport session
  ├── myapp          ← devport start --key myapp
  ├── frontend       ← devport start --key frontend
  └── b7d2f1a8c3     ← devport start (no key)
```

**Batch start from a config file:**

```bash
devport start -f devport.yaml
```

Where `devport.yaml` is a list of service specs:

```yaml
# String shorthand — unnamed service, default options
- go run ./cli/service

# Full form — named service with options
- key: api-server
  exec: go run ./cli/apiServer
  no-port: true
  env: ~/.env.secret

# Multiple env files (later overrides earlier)
- key: worker
  exec: python3 worker.py
  env:
    - ~/.env.secret
    - .env.local
```

Each entry is either a **string** (shorthand for just a command) or an **object** with fields:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `key` | string | (none) | Named key for the service |
| `exec` | string | required | Command to run |
| `no-port` | bool | false | Don't allocate a port |
| `tailnet` | bool | false | Expose via Tailscale |
| `port-env` | string | `PORT` | Env var name for the port |
| `env` | string or list | (none) | Dotenv file paths to load (tilde-expanded) |

Services start sequentially in YAML order. If one fails, the rest still start. A summary table is printed at the end.

**Idempotent**: if the service is already running, prints existing info and exits — no duplicate supervisor.

**Env snapshot**: captures `os.Environ()` at registration so the service can be reliably restarted from any shell.

### `devport attach` — Attach to a running service

```bash
# Interactive picker — fzf over all running service windows
devport attach

# Jump directly to a specific service by hash prefix
devport attach b7d
```

If already inside tmux, switches to the service window (`switch-client`). Otherwise attaches to the devport session at that window.

### `devport run` — Start a supervised service in the foreground

Runs the supervisor in the current terminal, blocking until stopped. Use this inside an existing tmux window or in environments where you manage your own process lifecycle (CI, systemd, etc.). `devport start` calls `devport run` internally.

```bash
devport run --key myapp -- npm run dev
devport run -- go run ./cmd/server
devport run --no-port --key worker -- python3 worker.py
```

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

Output fields: `hash`, `hashid`, `key`, `status` (running/stopped/unknown), `port`, `no_port`, `tailnet`, `url`, `cwd`, `cmd`, `last_up`.

### `devport stop` — Stop a service

```bash
# Use hash prefix (like git) to refer to services
devport stop b7d
```

Sends SIGTERM to the supervisor. Port stays reserved — use `rm` to free it.

### `devport restart` — Full stop and re-launch in tmux

```bash
devport restart b7d
```

Stops the running supervisor, waits for it to exit, then re-spawns it in a tmux window using the stored state (cmd, cwd, key, env snapshot). No flags needed — everything is read from the service record.

### `devport signal` — Send a signal to a running supervisor

```bash
# Default: SIGHUP — supervisor restarts child in-place
devport signal b7d

# Send a specific signal by number
devport signal -s 10 b7d   # SIGUSR1
devport signal -s 12 b7d   # SIGUSR2
```

Sends a signal directly to the supervisor process. The default SIGHUP triggers a graceful child restart (SIGTERM → wait 5s → SIGKILL → respawn) without stopping the supervisor. The tmux window stays open and the port stays live throughout.

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
