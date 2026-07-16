# devhost

**Per-directory port virtualization for dev servers.** Every project — and
every git worktree — gets its own loopback IP, so they can all bind the same
ports at the same time. No `PORT=13001` workarounds, no dev servers killing
each other, no code changes.

```
~/work/app        → 127.77.60.193   → http://app.devhost:3000
~/work/app-wt-a   → 127.77.40.164   → http://app-wt-a.devhost:3000  (same repo, worktree)
~/work/app-wt-b   → 127.77.201.7    → http://app-wt-b.devhost:3000  (same repo, worktree)
~/work/api        → 127.77.113.42   → http://api.devhost:8080, :9229, :5432 ...
```

devhost virtualizes the **address**, not the port — `:3000` just makes the
worktree collision vivid. Whatever your servers bind (`:5173`, `:8080`, a
web+API+debugger trio) works unchanged, all ports per project at once.

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
4. **Names.** `<dirname>.devhost` resolves to the project IP, so the browser
   URL is stable and human.
5. **`localhost` still works.** Because real servers moved to `127.77.*`,
   `127.0.0.1:<port>` is free — for every port those servers use. The
   optional `devhost daemon` mirror-listens there, identifies each caller by
   its working directory, and routes it to *its own* project's server —
   `curl localhost:3000` (or `:5173`, or any bound port) inside a worktree
   hits that worktree.

## Quick start

```sh
# once per machine
go install github.com/wickedev/devhost/cmd/devhost@latest
devhost setup            # installs shims, prints the PATH line to add

# once per project
cd ~/work/app && devhost init && git add .devhost

# that's it — every port the project binds is virtualized
npm run dev              # next dev  → http://app.devhost:3000
                         # vite      → http://app.devhost:5173
                         # storybook → http://app.devhost:6006
```

Optional localhost routing daemon (launchd/systemd):

```sh
devhost daemon
```

## Browsers and test runners too — parallel E2E, for real

The mirror-router identifies each caller by the working directory of the
connecting **process**, and child processes inherit it. So it isn't just
curl: a Playwright- or Puppeteer-launched Chromium (its network process
included), `fetch` in vitest, pytest's requests, a Go test's http client —
anything started inside a worktree resolves `localhost:<port>` to *that
worktree's* server.

```sh
# worktree A                        # worktree B — at the same time
npx playwright test                 npx playwright test
#  webServer → 127.77.60.193:3000   #  webServer → 127.77.40.164:3000
#  chromium  → localhost:3000 → A   #  chromium  → localhost:3000 → B
```

Full E2E suites in N worktrees at once; nothing collides, nothing gets
killed. For clients whose working directory is *outside* the project — a
browser opened from the Dock, a shared browser over CDP — use the stable
name instead: `baseURL: "http://app.devhost:3000"`.

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
- [ ] Built-in DNS responder + `/etc/resolver/devhost` (stop touching `/etc/hosts`)
- [ ] `devhost exec` port-watch proxy (universal, runtime-agnostic tier)
- [ ] Python / Ruby injection adapters
- [ ] Linux eBPF backend: `cgroup/bind4` rewrite — kernel-level, catches
      everything including static Go binaries
- [ ] libproc-based caller lookup on macOS (sub-ms, replaces `lsof`)
- [ ] Homebrew tap

## License

[MIT](LICENSE)
