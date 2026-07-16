# devhost

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

1. **Marker.** A project opts in with an empty `.devhost` file at its root
   (like `.tool-versions`). Commit it — worktrees inherit it automatically.
2. **Deterministic IP.** The project root path is hashed into `127.77.0.0/16`.
   Same path → same IP, different worktree → different IP. On macOS the IP is
   auto-registered as an `lo0` alias; on Linux all of `127.0.0.0/8` already
   routes.
3. **Transparent rebinding.** A PATH shim (asdf-style) wraps runtime
   launchers (`node`, `python3`, `ruby`, ...). Inside a `.devhost` tree it
   loads a tiny `bind()` interposer at the final exec, so `next dev`,
   `flask run`, `rails s` — anything on a dynamically-linked runtime — binds
   the project IP instead of `0.0.0.0`/`localhost`. The interposer computes
   the IP itself from the process's working directory: no HOST, no PORT, no
   app config. Zero project configuration. Outside a marked tree the shim is
   a pass-through.
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
curl -fsSL https://wickedev.github.io/devhost/install.sh | sh   # prebuilt binary, no sudo
                                                                # (update later: devhost upgrade)
brew install wickedev/tap/devhost                               # Homebrew
go install github.com/wickedev/devhost/cmd/devhost@latest       # from source

devhost setup            # installs shims, prints the PATH line to add

# once per project
cd ~/work/storefront && devhost init && git add .devhost

# that's it — every port the project binds is virtualized
npm run dev              # next dev  → http://storefront.devhost:3000
                         # vite      → http://storefront.devhost:5173
                         # storybook → http://storefront.devhost:6006
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
# ~/work/storefront                 # ~/work/storefront-cart-fix — at the same time
npx playwright test                 npx playwright test
#  webServer → 127.77.60.193:3000   #  webServer → 127.77.40.164:3000
#  chromium  → localhost:3000 ✓     #  chromium  → localhost:3000 ✓
```

Full E2E suites in N worktrees at once; nothing collides, nothing gets
killed. For clients whose working directory is *outside* the project — a
browser opened from the Dock, a shared browser over CDP — use the stable
name instead: `baseURL: "http://storefront.devhost:3000"`.

## For AI coding agents

If you drive this project with an AI coding agent, teach it not to fight
devhost — install the agent skill so it runs dev servers normally and never
changes ports or kills a port to "free" it (which under devhost kills another
worktree's server):

```sh
npx skills add wickedev/devhost   # works with Claude Code, Cursor, Copilot, Cline, +14 more
```

The skill lives at [`skills/devhost/SKILL.md`](skills/devhost/SKILL.md) —
tool-neutral, so it's just as useful pasted into an `AGENTS.md` or `CLAUDE.md`.

## Commands

| Command | What it does |
|---|---|
| `devhost init [dir]` | create the `.devhost` marker |
| `devhost ip` / `name` | print the project IP / hostname label |
| `devhost exec -- CMD` | run any command with the project env applied |
| `devhost setup` | install PATH shims |
| `devhost daemon` | localhost mirror-router |
| `devhost ls` | active devhost listeners |
| `devhost doctor` | diagnose the installation (also mentions when an update is available) |
| `devhost upgrade` | update devhost to the latest release |

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

## Roadmap

The core is complete — every tier below is implemented and tested:

- [x] Deterministic per-directory IPs, `.devhost` marker, PATH shims
- [x] Universal `bind()` interposer (macOS DYLD / Linux LD_PRELOAD), IPv4 + IPv6
- [x] `localhost` mirror-router with cwd-based caller routing (in-process libproc on macOS)
- [x] Built-in DNS responder + `/etc/resolver/devhost` (no `/etc/hosts` writes)
- [x] Narrow privileged helper (`devhost setup --helper`)
- [x] Linux eBPF `cgroup/bind4`+`bind6` backend for static binaries
- [x] `/etc/ld.so.preload` opt-in (env-free rebinding, Linux)
- [x] `devhost exec --proxy` for un-injectable binaries
- [x] `devhost upgrade` self-update + automated goreleaser releases

Possible future work: a `connect4` eBPF hook for localhost routing on Linux,
and native Windows support beyond WSL2.

## License

[MIT](LICENSE)
