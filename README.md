# devhost

[![skills.sh](https://img.shields.io/badge/skills.sh-devhost-black?logo=vercel&logoColor=white)](https://skills.sh/wickedev/devhost)
[![Release](https://img.shields.io/github/v/release/wickedev/devhost?color=e8590c)](https://github.com/wickedev/devhost/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/wickedev/devhost/ci.yml?branch=main&label=ci)](https://github.com/wickedev/devhost/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**Per-directory port virtualization for dev servers.** Every project — and
every git worktree — gets its own loopback IP, so they can all bind the same
ports at the same time. No `PORT=13001` workarounds, no dev servers killing
each other, no code changes.

```
~/work/storefront           → 127.77.60.193   → http://storefront.devhost:3000
~/work/storefront-cart-fix  → 127.77.40.164   → http://storefront-cart-fix.devhost:3000  (worktree)
~/work/storefront-i18n      → 127.77.201.7    → http://storefront-i18n.devhost:3000      (worktree)
~/work/api                  → 127.77.113.42   → http://api.devhost:8080, :9229, :5432 ...
```

devhost virtualizes the **address**, not the port — `:3000` just makes the
worktree collision vivid. Whatever your servers bind (`:5173`, `:8080`, a
web+API+debugger trio) works unchanged, all ports per project at once.

Built for the parallel-agent era: when AI coding agents run one dev server
per worktree, fixed ports stop being a convention and start being a fight.

## How it works

1. **Marker.** A project opts in with a `.devhost` file at its root (like
   `.tool-versions`). Commit it — worktrees inherit it automatically.
   `devhost init` fills it with a short comment explaining itself to anyone
   who finds it in the repo; only the file's existence matters.
2. **Deterministic IP.** The project root path is hashed into `127.77.0.0/16`.
   Same path → same IP, different worktree → different IP. On macOS the IP is
   auto-registered as an `lo0` alias; on Linux all of `127.0.0.0/8` already
   routes.
3. **Transparent rebinding.** A PATH shim (asdf-style) wraps runtime and
   build launchers (`node`, `python3`, `ruby`, `cargo`, `go`, ...). Inside a
   `.devhost` tree it loads a tiny `bind()` interposer at the final exec, so
   `next dev`, `flask run`, `rails s` — and native servers via `cargo run` /
   `go run`, since locally-built binaries accept injection — bind the project
   IP instead of `0.0.0.0`/`localhost`. The interposer computes the IP itself
   from the process's working directory: no HOST, no PORT, no app config.
   Zero project configuration. Outside a marked tree the shim is a
   pass-through. `devhost shim add TOOL` extends the set to any other
   launcher — recorded as a `shim: TOOL` line in the project's `.devhost`,
   so the repo carries its launcher list and every checkout self-provisions
   (`--global` records machine-wide instead) — and `devhost doctor` names
   any server that slipped past the shims onto plain loopback.
4. **Names.** `<dirname>.devhost` resolves to the project IP, so the browser
   URL is stable and human. On macOS a tiny built-in DNS responder serves the
   `.devhost` TLD (via `/etc/resolver/devhost`), so nothing writes to
   `/etc/hosts`; without the helper it falls back to a tagged hosts entry.
5. **`localhost` still works.** Because real servers moved to `127.77.*`,
   `127.0.0.1:<port>` is free — for every port those servers use. The
   optional `devhost daemon` mirror-listens there, identifies each caller by
   its working directory, and routes it to *its own* project's server —
   `curl localhost:3000` (or `:5173`, or any bound port) inside a worktree
   hits that worktree.

## Quick start

```sh
# once per machine — pick one:
curl -fsSL https://wickedev.github.io/devhost/install.sh | sh   # everything: binary + setup
                                                                # (update later: devhost upgrade)
brew install wickedev/tap/devhost && devhost setup              # Homebrew
go install github.com/wickedev/devhost/cmd/devhost@latest && devhost setup  # from source

# `devhost setup` is the whole machine setup: PATH shims, shell profile,
# localhost-routing daemon (launchd/systemd user service), and the narrow
# root helper (one sudo prompt). --no-profile / --no-daemon / --no-helper
# opt out of parts; the curl one-liner runs it for you.

# once per project
cd ~/work/storefront && devhost init && git add .devhost

# that's it — every port the project binds is virtualized
npm run dev              # next dev  → http://storefront.devhost:3000
                         # vite      → http://storefront.devhost:5173
                         # storybook → http://storefront.devhost:6006
```

## Browsers and test runners too — parallel E2E, for real

The mirror-router identifies each caller by the working directory of the
connecting **process**, and child processes inherit it. So it isn't just
curl: a Playwright- or Puppeteer-launched Chromium (its network process
included), `fetch` in vitest, pytest's requests, a Go test's http client —
anything started inside a worktree resolves `localhost:<port>` to *that
worktree's* server.

```sh
# ~/work/storefront                 # ~/work/storefront-cart-fix — at the same time
npx playwright test                 npx playwright test
#  webServer → 127.77.60.193:3000   #  webServer → 127.77.40.164:3000
#  chromium  → localhost:3000 ✓     #  chromium  → localhost:3000 ✓
```

Full E2E suites in N worktrees at once; nothing collides, nothing gets
killed. For clients whose working directory is *outside* the project — a
browser opened from the Dock, a shared browser over CDP — use the stable
name instead: `baseURL: "http://storefront.devhost:3000"`.

## Containers too — Docker and OrbStack

Containers publish host ports through the runtime (dockerd, OrbStack), which
devhost can't inject a `bind()` interposer into — the bind happens as root or
inside a VM. So devhost sits in front of the Docker API socket instead: it
identifies the calling `docker` / `docker compose` process by the socket peer
credential, resolves its `.devhost` project, and rewrites each published port
onto that project's IP. Two projects can then both `-p 3000:3000` without
colliding, exactly like host dev servers.

`devhost setup` wires this up for you: `devhost daemon` runs the proxy, and
setup adds a **guarded** `DOCKER_HOST` export to your shell profile — it points
Docker at the proxy only when the socket is up, and leaves an existing
`DOCKER_HOST` untouched, so it never breaks `docker` when devhost is off
(`--no-docker` opts out). The line it adds:

```sh
[ -z "$DOCKER_HOST" ] && [ -S "$HOME/.config/devhost/docker.sock" ] && \
  export DOCKER_HOST="unix://$HOME/.config/devhost/docker.sock"
```

```sh
# ~/work/storefront                    # ~/work/storefront-cart-fix — same time
docker compose up                      docker compose up
#  published → 127.77.60.193:3000      #  published → 127.77.40.164:3000
#  → http://storefront.devhost:3000    #  → http://storefront-cart-fix.devhost:3000
```

Only the host-side publish moves; the container-internal port (the right-hand
`:3000`) is untouched. A container started outside any `.devhost` tree — or one
with an explicit non-loopback `-p 1.2.3.4:3000:3000` — passes through unchanged.

## For AI coding agents

If you drive this project with an AI coding agent, teach it not to fight
devhost — install the agent skill so it runs dev servers normally and never
changes ports or kills a port to "free" it (which under devhost kills another
worktree's server):

```sh
npx skills add wickedev/devhost   # works with Claude Code, Cursor, Copilot, Cline, +14 more
```

`devhost setup` installs it for you and `devhost upgrade` keeps it in sync with
the binary; `devhost doctor` flags it when it drifts from the published version
(so a `brew upgrade` that only moves the binary doesn't leave the skill behind).
The skill covers containers too — agents run `docker compose up` normally and
never move a published port or kill a container to free one.

The skill lives at [`skills/devhost/SKILL.md`](skills/devhost/SKILL.md) —
tool-neutral, so it's just as useful pasted into an `AGENTS.md` or `CLAUDE.md`.

## Commands

| Command | What it does |
|---|---|
| `devhost init [dir]` | create the `.devhost` marker |
| `devhost ip` / `name` | print the project IP / hostname label |
| `devhost exec -- CMD` | run any command with the project env applied |
| `devhost shim add/rm/ls TOOL` | manage shimmed launchers beyond the defaults — declared in the project's `.devhost` (committable), or machine-wide with `--global` |
| `devhost setup` | one-shot machine setup: shims, PATH, daemon service, root helper, agent skill (`--no-*` flags opt out) |
| `devhost daemon` | localhost mirror-router, `.devhost` DNS, and Docker port-isolation proxy |
| `devhost ls` | active devhost listeners |
| `devhost doctor` | diagnose the installation (flags escaped listeners, an available update, or a stale agent skill) |
| `devhost upgrade` | update devhost to the latest release (and refresh the agent skill) |

## Privilege

macOS needs root for two things: `lo0` aliases and (until the DNS responder
lands) `/etc/hosts` entries. Instead of asking for broad sudo, install the
**narrow helper** once:

```sh
devhost setup --helper   # one-time password prompt
```

This installs a short validating shell script, root-owned at
`/usr/local/libexec/devhost-helper`, plus a single sudoers line allowing
exactly it — [audit the whole trust surface here](internal/privhelper/assets/devhost-helper.sh).
It refuses anything outside `127.77.0.0/16`, malformed hostnames, and
newline/comment injection.

On macOS it also installs `/etc/resolver/devhost`, which routes the
`.devhost` TLD to devhost's own DNS responder (run by `devhost daemon`).
From then on **`/etc/hosts` is never touched** — names resolve from a
registry the daemon serves over DNS. Uninstall:
`sudo /usr/local/libexec/devhost-helper resolver-remove && sudo rm /usr/local/libexec/devhost-helper /etc/sudoers.d/devhost`.

Without the helper, devhost falls back to passwordless sudo and a tagged
`/etc/hosts` entry if present, and degrades gracefully (direct-IP access
still works) if not. Linux needs no privilege for IPs at all — `127/8`
routes natively; hostname resolution there stays on `/etc/hosts` for now.

## Platform notes

| | macOS | Linux |
|---|---|---|
| loopback IPs | `lo0` alias per IP (needs one-time privileged helper or passwordless sudo) | free — `127/8` routes by default |
| transparent rebinding | `bind()` interposer via `DYLD_INSERT_LIBRARIES`, applied by the shim at the final exec (see [docs/architecture.md](docs/architecture.md)) | `LD_PRELOAD` interposer, plus an optional eBPF kernel backend |
| `localhost` routing | caller lookup via `lsof` | unique-candidate fallback |

**Runtime coverage.** The `bind()` interposer covers every dynamically-linked
runtime — Node, Python, Ruby, the JVM, and (on macOS) even Go binaries, since
darwin routes syscalls through libSystem. It rewrites both IPv4 and IPv6
wildcard/loopback binds (a `::` bind becomes the IPv4-mapped project address,
reachable over v4). Node additionally gets a `NODE_OPTIONS` listen patch as
belt-and-braces. The one gap: binaries signed with the hardened runtime +
library validation refuse injection (rare for dev tooling installed via
brew/asdf/mise) — for those, `devhost exec --proxy` mirrors whatever loopback
port they open onto the project IP so they stay reachable at `<name>.devhost`.

On **Linux**, static binaries that issue raw syscalls past libc (Go) slip the
`LD_PRELOAD` interposer — so `devhost exec` also attaches a kernel-level
**eBPF `cgroup/bind4`+`bind6`** program when it can (root / `CAP_BPF` +
`CAP_NET_ADMIN` and a writable cgroup2). That rewrites bind at the syscall
boundary, catching everything including static Go servers. Prefer zero env
vars? `sudo devhost setup --preload` registers the interposer in
`/etc/ld.so.preload` so dynamically-linked servers rebind with nothing in the
environment at all.

Windows has no preload primitive; use WSL2, where the Linux path works as-is.

## License

[MIT](LICENSE)
