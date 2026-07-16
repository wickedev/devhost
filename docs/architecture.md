# Architecture

## The problem

Dev servers assume they own a fixed port on localhost. The moment two copies
of a project run — classically two projects, now routinely N git worktrees
driven by parallel AI agents — they fight over `:3000`, and tooling that
"frees" the port (`kill $(lsof -ti:3000)`) turns the fight into mutual
process murder.

## The idea

Give every project its own loopback **IP** instead of its own port. The IP is
a pure function of the project root path (md5 → `127.77.x.y`), so it needs no
coordination, no registry to stay consistent across machines, and worktrees
diverge automatically. Ports stay fixed; the address varies. Note what this
buys over port remapping: the port is untouched, so *every* port a project
binds — web, API, HMR websocket, debugger — is virtualized at once, and
printed URLs keep their familiar port numbers.

Two things must then be made true:

1. servers must *bind* the project IP without being configured to;
2. clients must still *reach* the server by habit (`localhost:3000`) or by a
   stable name (`app.devhost:3000`).

## Tiered rebinding strategy

No single interception layer covers every runtime, so devhost stacks four,
each engaging only when the one above it can't:

| Tier | Mechanism | Covers |
|---|---|---|
| 1 | `bind()` interposer (`DYLD_INSERT_LIBRARIES` / `LD_PRELOAD`), self-computing | every dynamically-linked runtime — Node, Python, Ruby, JVM; Go too on macOS |
| 1b | Node `NODE_OPTIONS --require` listen patch | hardened Node builds that refuse injection |
| 2 | `HOST`/`PORT` convention env | static/hardened binaries reading 12-factor config |
| 3 | `devhost exec` supervised port-watch + reverse proxy (planned) | arbitrary binaries, no injection at all |
| 4 | Linux eBPF `cgroup/bind4` rewrite (planned) | everything, kernel-level |

### The interposer, and how the shim resurrects DYLD

A `bind()`-interposing library is the universal answer: every runtime's
socket call funnels through libc, so one small C file covers Node, Python,
Ruby, the JVM — and on macOS even Go, which links libSystem dynamically. The
library is self-contained: it computes the project IP itself (cwd → nearest
`.devhost` → md5, the same scheme as `internal/addr`), so the data path
carries no HOST/PORT/DEVHOST at all, and outside a marked tree it is a
strict no-op.

The classic objection is SIP: macOS strips `DYLD_*` variables whenever a
protected binary (`/bin/sh`, `/usr/bin/env`) execs, and every npm script
chain crosses both. The **PATH shim** (asdf's mechanism) dissolves it: a tiny
script in front of each runtime launcher delegates to `devhost shim-exec`,
which applies the variable **at the final exec** — after every SIP-protected
hop — and executes the real Mach-O binary directly (following asdf/mise shim
scripts to their target, since a bash hop there would strip the variable
again). On Linux there is nothing to resurrect: `LD_PRELOAD` survives shell
chains, and `/etc/ld.so.preload` can load the interposer with zero
environment variables anywhere.

Honest limits: binaries signed with hardened runtime + library validation
refuse injection (rare for brew/asdf/mise-installed dev tooling); Go on
*Linux* makes raw syscalls past libc (the eBPF tier closes this); IPv6
wildcard binds pass through. Those cases fall to tiers 1b/2/3.

Below the shim, macOS offers nothing: pf can't fix `EADDRINUSE` (the
collision happens at the syscall, before any packet exists), Network
Extensions intercept the outbound side only, kexts are gone. The shim is the
practical floor; past it lies a Linux VM.

### The Linux endgame

Linux has the kernel primitive macOS lacks: an eBPF program attached to a
cgroup (`BPF_CGROUP_INET4_BIND`) can rewrite bind addresses in-kernel. Pair
it with a shell hook that migrates the shell into a per-project cgroup on
`cd`, and every descendant process — including static Go binaries that
bypass libc — is virtualized with no env vars in the data path. This is the
planned `devhostd` Linux backend; `connect4` gives localhost routing for
free at the same layer.

## Addressing

- **`<name>.devhost`** — the sanitized project basename, mapped to the
  project IP. Currently a tagged `/etc/hosts` line; the roadmap replaces
  this with a built-in DNS responder behind `/etc/resolver/devhost`, so
  `/etc/hosts` is never mutated. A product-scoped TLD keeps devhost out of
  `.test`, which other local-dev tools (puma-dev and friends) conventionally
  claim — devhost owns its namespace outright. The honest trade-off:
  `.devhost` is not IANA-reserved the way `.test` is; it is vanishingly
  unlikely to be delegated, and the TLD is a single constant if it ever is.
- **`localhost:<port>`** — restored by the mirror-router below.

## devhostd: the localhost mirror-router

Moving servers off `127.0.0.1` frees `127.0.0.1:<port>`. The daemon:

1. scans for `127.77.*` TCP listeners (lsof on macOS, `/proc/net/tcp` on
   Linux) every 2s;
2. mirror-listens on `127.0.0.1:<port>` for exactly the ports that have a
   devhost listener — lazily, releasing them when the server exits, backing
   off silently if a real app owns the port;
3. per connection, resolves the **caller** (source port → PID → cwd →
   nearest `.devhost` root → IP) and proxies to that project's server;
   falls back to the unique candidate, drops when ambiguous.

So `curl localhost:3000` inside worktree A reaches A's server while the same
command inside worktree B reaches B's — client-side changes: none.

Trade-offs, stated plainly: while a devhost project holds a port, unrelated
apps can't bind `127.0.0.1:<that port>`; and the lsof caller lookup adds
~100ms to connection *setup* (libproc will cut this to sub-ms). The daemon is
an optional convenience layer — IP virtualization and `.devhost` names work
without it.

## Privilege model

macOS needs root twice: `lo0` aliases and (until the DNS responder lands)
`/etc/hosts`. The supported path is a ~20-line root-owned helper that
validates its argument against `127.77.0.0/16` and runs `ifconfig`, allowed
by a single narrow NOPASSWD sudoers line — auditable in one screenful.
Plain passwordless sudo works as a fallback. Linux needs no privilege at all.
