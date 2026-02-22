# devport — Design Doc

> **Status:** Draft
> **Date:** 2026-02-22

## Problem

Running dev services on a shared dev machine (e.g. `m4mini`) requires manually picking ports, remembering which port maps to what, and setting up Tailscale services by hand. There's no way to know if a service is already running, and ports clash when multiple projects use the same default.

## Goal

A CLI tool (`devport`) that manages the full lifecycle of dev services:

- Assign a stable, unique local port per service
- Start/stop/restart the underlying process
- Automatically expose the service via Tailscale Services
- Idempotent — safe to re-run without duplication

## Architecture

### No Central Daemon

Each `devport run` spawns its own **supervisor process** for that service. There is no central daemon or process monitor. Each supervisor:

- Manages exactly one child process
- Handles signals (ctrl-c to kill, ctrl-z to restart)
- Holds a `flock()` on a lock file for the duration of its lifetime (see Liveness below)

### Data Store — Filesystem Only

All state lives under `~/.local/share/devport/`:

```
~/.local/share/devport/
  services/<hash>.json        -- metadata per service (persistent reservation)
  locks/<hash>.lock           -- identity lock (is this service's supervisor alive?)
  locks/register.lock         -- global lock for registration (held briefly)
```

**No SQLite.** At a few dozen services, reading all JSON files into memory is instant. The filesystem is the database.

**Service metadata** (`services/<hash>.json`):
```json
{
  "hash": "b7d2f1a8c3",
  "key": "myapp",
  "svc": "b7d",
  "port": 19000,
  "cwd": "/Users/me/projects/myapp",
  "cmd": ["npm", "run", "dev"],
  "last_up": "2026-02-22T10:30:00Z"
}
```

- `key` — optional, only present when `--key` was used
- `svc` — the hash prefix registered with Tailscale (frozen at registration time)
- `port` — reserved port (persists across stop/restart, only freed on `rm`)

The JSON file **is** the reservation. As long as it exists, the port and svc prefix are taken — even if the supervisor isn't running. Written on first `devport run`, `last_up` updated every 30s while the supervisor runs. Only deleted by `devport rm`.

### Locking

Three mechanisms, each serving one purpose:

**Identity lock** (`locks/<hash>.lock`) — flock, non-blocking, held for supervisor lifetime
- Purpose: "is this service's supervisor alive?"
- Acquired on `devport run` startup — if already held, the service is already running, so print info and exit (idempotent)
- Kernel automatically releases the lock when the process exits — even on `SIGKILL` or sudden power loss (locks are in-kernel memory, not persisted to disk)
- Used by `ls --active`, `stop`, `restart` to probe liveness

**Registration lock** (`locks/register.lock`) — flock, blocking, held briefly
- Purpose: serialize the registration critical section
- Prevents two concurrent `devport run` calls from picking the same port or same svc prefix
- Held only during: scan all JSONs → pick port → compute prefix → register Tailscale → write JSON
- Released immediately after

**JSON file** (`services/<hash>.json`) — not a lock, but acts as the persistent reservation
- Purpose: "this port and svc prefix are taken"
- Survives supervisor death, reboot, everything
- Only deleted by `devport rm`
- On restart, supervisor reads its JSON and reuses the same port/prefix — no re-registration needed

### Startup Flow

```
devport run --key myapp -- npm run dev

1. hash = hash("myapp")

2. flock(locks/<hash>.lock, NON-BLOCKING)
   → fails?    already running, print info, exit
   → succeeds? hold it open forever

3. services/<hash>.json exists?
   → yes: read port + svc prefix, skip to step 5
   → no:  continue to step 4

4. flock(locks/register.lock, BLOCKING)     ← wait for other registrations
     scan all services/*.json → find used ports + prefixes
     pick lowest free port
     compute shortest unique hash prefix
     register Tailscale svc
     write services/<hash>.json
   unlock(locks/register.lock)              ← release immediately

5. start child process, update last_up every 30s
```

**Properties:**
- **Port and svc prefix are stable across restarts** — persisted in JSON, not dependent on flocks
- **Liveness is always a live query** against the kernel, never stale
- **Registration is serialized** — no races on port or prefix assignment
- **After crash or reboot**, JSON reservations remain; only the identity lock is gone, so `devport run` re-acquires it and reuses the same port/prefix

### Port Assignment

- Port range: configurable, default `19000–19999`
- On `devport run`, if `services/<hash>.json` already exists, reuse the stored port (no re-assignment)
- If no existing JSON, hold the registration lock and scan all `services/*.json` to find used ports; pick the lowest unused port in range
- A port is considered "free" after an expiration period (e.g. 30 days since `last_up`) — reclaimable but not automatically freed
- Ports are **reserved by JSON existence**, not by flocks — a stopped service still holds its port

### Service Identity: Always a Hash

The canonical identifier for every service is a **hash**. Even named keys get hashed — the key is just a human-friendly input that produces a deterministic hash.

**Input → Hash:**
- `--key myapp` → `hash("myapp")` → `e.g. b7d2f1...`
- No key → `hash(<canonical cwd> <cmd and args>)` → `e.g. a3f7c9...`

The hash is what's used for file paths, lock files, and internal lookups. The original key (if provided) is stored in the service JSON as metadata.

**Referring to services** — like git, use the shortest unambiguous hash prefix:

```bash
devport stop a3f      # works if no other hash starts with "a3f"
devport restart b7d   # use more chars if needed to disambiguate
```

When resolving a prefix, devport scans all `services/*.json` filenames. If the prefix matches multiple services, it errors with the ambiguous matches listed.

**`devport ls`** shows the key (if named) alongside the shortest unique hash prefix:

```
HASH   KEY      PORT   LAST UP               CMD
b7d    myapp    19000  2026-02-22T10:30:00Z  npm run dev
a3f    —        19001  2026-02-22T09:15:00Z  go run ./cmd/server
e91    —        19002  2026-02-22T08:00:00Z  python -m http.server
```

File paths always use the full hash:

```
services/b7d2f1a8c3.json
locks/b7d2f1a8c3.lock
```

## CLI Interface

### `devport run [--key NAME] <cmd> [args...]`

Starts a **supervisor process** in the foreground that manages the child service.

- Compute hash from `--key` or from `<cwd> <cmd>`
- Acquire `flock()` on `locks/<hash>.lock` — if already held, print existing port/URL and exit (idempotent)
- If `services/<hash>.json` exists, reuse stored port and svc prefix
- If not, hold registration lock → assign port → compute svc prefix → register Tailscale → write JSON → release lock
- Update `last_up` periodically (every 30s) while the supervisor is running
- Set `PORT=<port>` in the child's environment
- Spawn the child process, inheriting the current shell environment
- Register Tailscale service (see below)
- **The supervisor stays in the foreground**, relaying child stdout/stderr
- **Signal handling:**
  - `SIGINT` (ctrl-c) — kill child, release lock (automatic on exit), exit
  - `SIGTSTP` (ctrl-z) — graceful restart: send `SIGHUP` to child, wait timeout, hard-kill if needed, then respawn

### `devport ls [--active]`

- List all registered services with key, port, cwd, cmd, last_up
- `--active` — filter to only entries where the identity lock is held (flock probe)
- No PID stored — liveness is always a live kernel query

### `devport stop <hash>`

- Resolve hash prefix to full hash (error if ambiguous)
- Look up the lock file for the service
- Read the PID of the lock holder (via `lsof` or `/proc`) and send `SIGTERM`
- Supervisor catches SIGTERM, kills child, exits (lock auto-released)
- Does **not** free the port — the mapping is retained

### `devport restart <hash>`

- Send `SIGHUP` to the supervisor
- Supervisor gracefully restarts the child (SIGHUP, wait, hard-kill, respawn)

### `devport rm <hash>`

- If running, kill the supervisor + child
- Tear down the Tailscale service
- Delete `services/<hash>.json` and lock files (frees the port)

## Tailscale Integration

On `devport run`, after the port is assigned:

- The service name uses the **shortest unambiguous hash prefix** (e.g. `b7d` if unique)
- Register: `tailscale serve --service svc:b7d https:443 http://localhost:<port>`
- The service becomes reachable at `https://b7d.tailb63537.ts.net`
- If a new service is added that makes an existing prefix ambiguous, the existing service's Tailscale name is **not** changed — prefixes are only computed at registration time
- Requires the host to be tagged (e.g. `tag:services` — already configured on m4mini)

On `devport rm`:

- Tear down: `tailscale serve --service svc:<hash-prefix> https:443 http://localhost:<port> off`
- The hash prefix used for tear down is stored in the service JSON so it matches what was registered

### ACL Auto-Approval

The tailnet policy needs an auto-approver so devport can register services without manual approval:

```jsonc
{
  "autoApprovers": {
    "services": {
      "svc:*": ["tag:services"]
    }
  }
}
```

(Or scope it to a `tag:dev` tag if tighter control is desired.)

## Idempotency

- `devport run` with a key that's already running: flock probe detects held lock, prints existing port and Tailscale URL, exits without spawning a duplicate
- `devport stop` on an already-stopped service: no-op (lock not held)
- `devport rm` on a non-existent ID: error with helpful message

## Port Expiration

- Ports are not automatically freed on stop — they stay reserved
- The supervisor updates `last_up` every 30s while running, so it reflects actual usage, not just when `run` was invoked
- A port becomes reclaimable if `last_up` is older than a threshold (default 30 days)
- Reclaimable ports are only reused when the pool is exhausted — prefer fresh ports first
- `devport ls` shows age/staleness indicators

## Open Questions

- **Language choice** — Go? Rust? Shell script? (Go fits well: single binary, good process management, flock via syscall)

golang

- **Tailscale service naming** — Should the svc name be derived from `--key`, or allow a separate `--svc-name` override?

just use the shortest prefix

- **Non-HTTP services** — Some dev services may need raw TCP passthrough instead of HTTPS termination. Support `--proto tcp` flag?
yes

- **Port env var name** — Always `PORT`, or allow `--port-env VITE_PORT`?

yup

- **Graceful restart timeout** — Configurable? Default 5s?
 ok

## Example Session

```bash
# Named key — hash derived from "myapp"
$ devport run --key myapp -- npm run dev
{
  "hash": "b7d2f1a8c3",
  "key": "myapp",
  "svc": "b7d",
  "port": 19000,
  "url": "https://b7d.tailb63537.ts.net",
  "cwd": "/Users/me/projects/myapp",
  "cmd": ["npm", "run", "dev"]
}

# No key — hash derived from cwd + cmd
$ devport run -- go run ./cmd/server
{
  "hash": "a3f7c91b2d",
  "svc": "a3f",
  "port": 19001,
  "url": "https://a3f.tailb63537.ts.net",
  "cwd": "/Users/me/projects/api",
  "cmd": ["go", "run", "./cmd/server"]
}

# List services — JSON array
$ devport ls --active
[
  {
    "hash": "b7d2f1a8c3",
    "key": "myapp",
    "svc": "b7d",
    "status": "running",
    "port": 19000,
    "url": "https://b7d.tailb63537.ts.net",
    "cwd": "/Users/me/projects/myapp",
    "cmd": ["npm", "run", "dev"],
    "last_up": "2026-02-22T10:30:00Z"
  },
  {
    "hash": "a3f7c91b2d",
    "svc": "a3f",
    "status": "stopped",
    "port": 19001,
    "url": "https://a3f.tailb63537.ts.net",
    "cwd": "/Users/me/projects/api",
    "cmd": ["go", "run", "./cmd/server"],
    "last_up": "2026-02-22T09:15:00Z"
  }
]

# Always use hash prefix to refer to services
$ devport restart a3f
$ devport stop b7d

# Remove entirely (frees port, tears down Tailscale service)
$ devport rm b7d
```
