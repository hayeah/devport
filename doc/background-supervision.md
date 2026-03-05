# Background Supervision Design

## Goal

Turn devport into a simple nohup-style background process supervisor. Processes run in tmux sessions, file locks remain the authoritative source of truth for liveness, and the existing port allocation and state machinery is preserved unchanged.

## Architecture

### Layers

```
devport start    ← new: tmux launcher, returns immediately
    └─ tmux new-session -d -s devport-<hash>
        └─ devport run    ← unchanged: foreground supervisor (the engine)
            └─ child process (your actual dev service)
```

`devport run` is unchanged — it blocks, supervises, handles restarts, and holds the file lock. `devport start` is a thin wrapper that launches `devport run` inside a detached tmux session and returns.

### Why tmux instead of self-daemonizing

Spawning inside tmux gives us:
- **Free backgrounding** — tmux survives terminal close, SSH drops, etc.
- **Free interactive TTY** — processes that need a terminal (REPLs, watchers with color output) work naturally
- **Free scrollback** — tmux history buffer
- **Free attach** — `devport attach <id>` is just `tmux attach`
- **Free liveness signal** — when `devport run` exits, the tmux session ends; there is no session without a running supervisor

No double-fork, no pidfile management, no log redirection infrastructure needed.

## File Lock as Truth

The `flock` on `~/.local/share/devport/locks/<hash>.lock` remains the single authoritative signal that a service is alive. A locked file means the supervisor is running. An unlocked (or absent) lock file means it is not.

tmux session existence is a convenience check only — e.g. to get the session name for `attach`. The lock is always the truth.

**Stale lock behavior**: `flock` is automatically released by the kernel when the process holding it exits for any reason (clean exit, crash, kill -9, OOM). There are no stale locks.

**Stale tmux sessions**: if `devport run` exits (e.g. user manually kills the supervisor inside the session), the tmux session either exits immediately (if no `remain-on-exit` is set) or shows a dead window. Since the lock is released, `devport start` will correctly identify the service as not running and can start a fresh session. A stale dead tmux session is harmless — `tmux new-session` with the same name will fail, so `devport start` should kill any dead session with the same hash before creating a new one.

## Commands

### `devport start [flags] -- <cmd> [args...]`

New command. Launches `devport run` inside a detached tmux session.

```
devport start -- npm start
devport start --key api -- uvicorn main:app --reload
devport start --no-port -- redis-server
```

Steps:
1. Compute hash (same as `devport run`)
2. Try to acquire identity lock (non-blocking)
   - If acquired: release it immediately — just checking. Proceed to spawn.
   - If not acquired: service is already running. Print service info and exit.
3. Determine tmux session name: `devport-<hashid>`
4. Kill any dead tmux session with that name (session exists but lock is free — leftover shell)
5. Spawn: `tmux new-session -d -s devport-<hashid> -- devport run [flags] -- <cmd> [args...]`
6. Wait briefly for `devport run` to write its state (poll until `store.Load(hash)` succeeds, timeout ~2s)
7. Print service JSON and return

The lock is acquired inside `devport run`, not in `devport start`. `devport start` is purely a launcher.

### `devport run [flags] -- <cmd> [args...]`

Unchanged foreground supervisor. Can still be used directly (e.g. inside an existing tmux session, or in a CI environment where backgrounding is not needed).

The only addition: snapshot the environment at startup and store it in the service state (see Env Snapshot below).

### `devport attach <id>`

Attach to a running service's tmux session.

```
devport attach a3f2
```

- Resolve `<id>` to a hash (same prefix-matching as today)
- If inside tmux: `tmux switch-client -t devport-<hashid>`
- If outside tmux: `tmux attach-session -t devport-<hashid>`

### `devport ls`

Unchanged. Liveness: lock held = running, lock free = stopped (but state file still present).

### `devport stop <id>`

Unchanged. Sends SIGTERM to the process holding the lock (via `lsof` → PID → kill), or `tmux kill-session` as an alternative path.

### `devport restart <id>`

Unchanged. Sends SIGHUP to the supervisor process (already handled in `supervisor.go`).

### `devport rm <id>`

Unchanged. Stops if running, deletes state file and lock file.

## Port Allocation

Port allocation is unchanged and remains the default. A `--no-port` flag is added for services that don't bind to a port (background workers, compilers, etc.).

```
devport start --no-port -- watchexec -e go go build ./...
```

When `--no-port` is set:
- No port is allocated
- `PORT` env var is not injected
- `Port` field in service state is `0`
- `devport ls` shows `-` for the port column

## Env Snapshot

At `devport run` startup, snapshot `os.Environ()` and store it in the service state file. This allows:
- Reliable restart: re-run the service with the same env it was originally started with
- Inspection: `devport env <id>` to see what env a service is running with

Secrets loaded via `.env` mechanics are included in the snapshot. This is intentional — the snapshot lives in `~/.local/share/devport/services/<hash>.json`, which is local to the machine and user.

Add `Env []string` to the `Service` struct. Populated once on first registration, not updated on restart (the registered env is the canonical env).

## State Directory Layout

```
~/.local/share/devport/
  services/
    <hash>.json       ← service metadata + env snapshot
  locks/
    <hash>.lock       ← flock held by running supervisor
    register.lock     ← flock held during port allocation
```

No new directories needed.

## Service Struct Changes

```go
type Service struct {
    Hash    string    `json:"hash"`
    HashID  string    `json:"hashid"`
    Key     string    `json:"key,omitempty"`
    Port    int       `json:"port"`           // 0 = no port (--no-port)
    NoPort  bool      `json:"no_port,omitempty"`
    Tailnet bool      `json:"tailnet"`
    CWD     string    `json:"cwd"`
    CMD     []string  `json:"cmd"`
    Env     []string  `json:"env"`            // snapshot of os.Environ() at registration
    LastUp  time.Time `json:"last_up"`
}
```

## tmux Session Naming

Session name: `devport-<hashid>` where `<hashid>` is the shortest unique prefix (e.g. `devport-a3f2`).

If hashid grows (more services registered), the session name stays stable — it is fixed at registration time and stored in `HashID`.

## What Is Not Changing

- Hash computation (`ComputeHash`)
- Port allocation (`AllocatePort`, port range)
- File locking mechanism (`FileLock`, `flock`)
- Store layout and serialization
- Supervisor restart/backoff logic
- Tailscale integration
- `devport run` behavior (still usable standalone)
