# devhost

**Per-directory port virtualization for dev servers.** Every project — and
every git worktree — gets its own loopback IP, so they can all bind `:3000`
at the same time. No `PORT=13001` workarounds, no dev servers killing each
other, no code changes.

```
~/work/app        → 127.77.60.193   → http://app.test:3000
~/work/app-wt-a   → 127.77.40.164   → http://app-wt-a.test:3000
~/work/app-wt-b   → 127.77.201.7    → http://app-wt-b.test:3000
```

Built for the parallel-agent era: when AI coding agents run one dev server
per worktree, fixed ports stop being a convention and start being a fight.

## How it works

1. **Marker.** A project opts in with an empty `.devhost` file at its root
   (like `.tool-versions`). Commit it — worktrees inherit it automatically.
2. **Deterministic IP.** The project root path is hashed into `127.77.0.0/16`.
   Same path → same IP, different worktree → different IP. On macOS the IP is
   auto-registered as an `lo0` alias; on Linux all of `127.0.0.0/8` already
   routes.
3. **Transparent rebinding.** A PATH shim (asdf-style) wraps `node`. Inside a
   `.devhost` tree it injects a tiny `net.Server.listen` patch via
   `NODE_OPTIONS`, so `next dev`, `vite`, plain `http.createServer(...)
   .listen(3000)` — anything — binds the project IP instead of
   `0.0.0.0`/`localhost`. Zero project configuration. Outside a marked tree
   the shim is a pass-through.
4. **Names.** `<dirname>.test` resolves to the project IP, so the browser
   URL is stable and human.
5. **`localhost` still works.** Because real servers moved to `127.77.*`,
   `127.0.0.1:<port>` is free. The optional `devhost daemon` mirror-listens
   there, identifies each caller by its working directory, and routes it to
   *its own* project's server — `curl localhost:3000` inside a worktree hits
   that worktree.

## Quick start

```sh
# once per machine
go install github.com/wickedev/devhost/cmd/devhost@latest
devhost setup            # installs shims, prints the PATH line to add

# once per project
cd ~/work/app && devhost init && git add .devhost

# that's it
npm run dev              # binds 127.77.x.y:3000  →  http://app.test:3000
```

Optional localhost routing daemon (launchd/systemd):

```sh
devhost daemon
```

## Commands

| Command | What it does |
|---|---|
| `devhost init [dir]` | create the `.devhost` marker |
| `devhost ip` / `name` | print the project IP / hostname label |
| `devhost exec -- CMD` | run any command with the project env applied |
| `devhost setup` | install PATH shims |
| `devhost daemon` | localhost mirror-router |
| `devhost ls` | active devhost listeners |
| `devhost doctor` | diagnose the installation |

## Platform notes

| | macOS | Linux |
|---|---|---|
| loopback IPs | `lo0` alias per IP (needs one-time privileged helper or passwordless sudo) | free — `127/8` routes by default |
| transparent rebinding | PATH shim + env injection (SIP rules out `DYLD_*`; see [docs/architecture.md](docs/architecture.md)) | same, until the eBPF backend lands |
| `localhost` routing | caller lookup via `lsof` | unique-candidate fallback (eBPF backend planned) |

**Runtime coverage.** The injection tier covers Node today (and everything
with a `#!/usr/bin/env node` shebang: npm, npx, next, vite...). Python and
Ruby adapters are planned. Runtimes with no injection channel (Go, Rust,
Deno) follow the `HOST`/`PORT` convention that `devhost exec` provides, until
the supervised port-watch proxy lands.

## Roadmap

- [ ] Privileged helper + narrow sudoers install (`devhost setup --helper`)
- [ ] Built-in DNS responder + `/etc/resolver/test` (stop touching `/etc/hosts`)
- [ ] `devhost exec` port-watch proxy (universal, runtime-agnostic tier)
- [ ] Python / Ruby injection adapters
- [ ] Linux eBPF backend: `cgroup/bind4` rewrite — kernel-level, catches
      everything including static Go binaries
- [ ] libproc-based caller lookup on macOS (sub-ms, replaces `lsof`)
- [ ] Homebrew tap

## License

[MIT](LICENSE)
