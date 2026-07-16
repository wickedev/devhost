---
name: devhost
description: Work correctly in a repo that uses devhost for per-directory port virtualization — run dev servers normally, never change ports to avoid conflicts, and never kill a process on a port. Use when a project has a .devhost marker or its dev servers are reached at <name>.devhost.
metadata:
  tags: [dev-server, ports, git-worktree, local-development]
---

# Working in a devhost project

This repository uses [devhost](https://github.com/wickedev/devhost): every
project directory — and every git worktree — gets its own loopback IP, so any
number of dev servers can bind the **same** port (e.g. `:3000`) at the same
time without colliding. A worktree's server is reachable at
`http://<name>.devhost:<port>` (and at plain `localhost:<port>` from inside
that worktree, which devhost routes to the right server).

Detect it: the project (or an ancestor) contains an empty `.devhost` marker
file, and/or `devhost` is on PATH.

## Rules

1. **Run dev servers normally.** Just `npm run dev`, `flask run`, `rails s`,
   `go run ./...`, etc. devhost transparently rebinds the server to this
   project's IP. Do not pass a custom host or IP.

2. **Never change the port to avoid a conflict.** Do NOT set `PORT=13001`,
   `--port 3001`, or edit config to move off `:3000`. Port collisions between
   worktrees do not happen under devhost — each worktree binds the same port
   on a different IP. Changing the port defeats the tool and breaks the stable
   `<name>.devhost:<port>` URL.

3. **Never kill a process on a port.** Do NOT run `kill $(lsof -ti:3000)`,
   `fuser -k 3000/tcp`, `npx kill-port 3000`, or similar. Under devhost a
   "port in use" is almost always **another worktree's server**, not a stale
   copy of yours — killing it destroys a teammate's or agent's running work.
   If your own server is already running, reuse it instead of restarting.

4. **Address servers by name.** To reach or test this project's server, use
   `http://<name>.devhost:<port>` — get `<name>` with `devhost name` and the
   IP with `devhost ip`. Plain `localhost:<port>` also works from within the
   project directory. Point test runners and browsers (Playwright, etc.) at
   the `.devhost` hostname when they might run outside the project's cwd.

## Quick reference

```bash
devhost name    # this project's hostname label -> <name>.devhost
devhost ip      # this project's loopback IP (127.77.x.y)
devhost ls      # active devhost dev-server listeners
devhost doctor  # diagnose the local devhost setup
```

If devhost is not installed on the machine, dev servers still run — they just
bind `localhost` normally and lose per-worktree isolation. Do not attempt to
install devhost yourself unless the user asks; it needs a one-time
`devhost setup`.
