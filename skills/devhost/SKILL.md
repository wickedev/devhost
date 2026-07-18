---
name: devhost
description: Work correctly in a repo that uses devhost for per-directory port virtualization — run dev servers and containers (docker compose / docker run) normally, never change ports to avoid conflicts, and never kill a process or container to free a port. Use when a project has a .devhost marker, its dev servers are reached at <name>.devhost, or you are about to run Docker/OrbStack in such a repo.
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

The same isolation extends to **containers**: when Docker is pointed at
devhost's proxy, `docker compose up` or `docker run -p 3000:3000` in different
projects publish to different loopback IPs, so containers never fight over a
host port either.

Detect it: the project (or an ancestor) contains an empty `.devhost` marker
file, and/or `devhost` is on PATH.

## Rules

1. **Run dev servers normally.** Just `npm run dev`, `flask run`, `rails s`,
   `go run ./...`, `cargo run`, etc. devhost transparently rebinds the server
   — including locally-built native binaries — to this project's IP. Do not
   pass a custom host or IP. If a server ends up on plain `127.0.0.1` anyway
   (`devhost doctor` lists it under "escaped isolation"), its launcher isn't
   shimmed: run it via `devhost exec -- CMD`, or add a shim for the launcher
   with `devhost shim add TOOL` and restart the server.

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

5. **Containers publish per project — treat them the same.** Run
   `docker compose up`, `docker run -p 3000:3000`, etc. normally. When
   `DOCKER_HOST` points at devhost's proxy (set by `devhost setup`; socket at
   `~/.config/devhost/docker.sock`), devhost rewrites each container's
   published port onto this project's IP, so `:3000` in two projects coexists.
   Do NOT change the published port and do NOT `docker kill`/`docker rm` a
   container just to free a host port — a busy port is another project's
   container. Reach a container at `http://<name>.devhost:<port>` or the
   project IP. Leave the container port (the right-hand `:3000`) alone. If two
   projects genuinely collide on a host port, `DOCKER_HOST` isn't pointed at
   the proxy — surface that rather than moving the port.

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
`devhost setup`. Container isolation additionally needs `DOCKER_HOST` pointed
at the devhost proxy; without it, containers bind the shared host as usual.
