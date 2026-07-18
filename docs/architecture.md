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
| 4 | Linux eBPF `cgroup/bind4` rewrite (opt-in, needs privilege) | everything, kernel-level — including static Go binaries |

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

The same late injection covers **native dev servers**. A binary you build
locally (`cargo run`, `go run`) is not an Apple platform binary and carries
no hardened runtime, so dyld injects into it happily — only the Apple-signed
hops in the middle of a chain (`/usr/bin/make`, `/bin/sh`) strip the
variable, and a shim past them re-arms it. `cargo` and `go` are therefore
shimmed by default, and `devhost shim add TOOL` extends the set to any other
launcher. The shim *file* is machine-global by necessity — PATH is a process
concept, not a directory one, and the re-injection point must be visible at
every spawn site (make recipes, IDEs, agents) — but the *declaration* is
project-scoped: `shim add` records a `shim: TOOL` line in the `.devhost`
marker, committable, so every checkout and worktree self-provisions the shim
on activation (`--global` opts into a machine-wide declaration instead).
Because the shim set is an enumeration, `devhost doctor` closes the loop on
anything missed: it
flags listeners bound to plain loopback or the wildcard by a process whose
cwd sits inside a `.devhost` project, naming the process and port so the fix
(`devhost exec` or a new shim) is one command away.

The interposer rewrites IPv6 too: a `::`/`::1` bind becomes the IPv4-mapped
project address `::ffff:127.77.x.y` (with `IPV6_V6ONLY` cleared), which a v4
client reaches at the project IP — so dual-stack servers isolate the same way.

Honest limits: binaries signed with hardened runtime + library validation
refuse injection (rare for brew/asdf/mise-installed dev tooling); Go on
*Linux* makes raw syscalls past libc. The eBPF tier closes the Linux-static
gap, and `devhost exec --proxy` (which watches the child's loopback listeners
and mirrors each onto the project IP) gives reachability — though not
same-port isolation — for anything that can't be injected at all.

Below the shim, macOS offers nothing: pf can't fix `EADDRINUSE` (the
collision happens at the syscall, before any packet exists), Network
Extensions intercept the outbound side only, kexts are gone. The shim is the
practical floor; past it lies a Linux VM.

### The Linux endgame (implemented)

Linux has the kernel primitive macOS lacks: an eBPF program attached to a
cgroup (`BPF_CGROUP_INET4_BIND`) rewrites bind addresses in-kernel, below
libc — so it catches even static Go binaries that issue raw syscalls, the one
runtime the `LD_PRELOAD` interposer misses. `devhost exec` implements this:
when it has privilege (`CAP_BPF` + `CAP_NET_ADMIN`) and a writable cgroup2, it
loads a bind4 program carrying the project IP, attaches it to a per-project
cgroup under `/sys/fs/cgroup/devhost/`, and moves itself in before exec so the
server and all its children are governed. The program only rewrites
wildcard/loopback binds, matching the interposer's policy.

The whole loader is pure Go — hand-assembled BPF instructions and raw `bpf(2)`
syscalls, no cgo and no dependency. Two things bit us and are worth recording:
`BPF_PROG_TYPE_CGROUP_SOCK_ADDR` is 18 (17 is `RAW_TRACEPOINT`, whose context
has no `user_ip4`), and the `bpf_attr` passed to `BPF_PROG_LOAD` must be the
full 144 bytes — a shorter buffer truncates `expected_attach_type`, and the
verifier then rejects the write to `user_ip4` as an invalid context access.
Both were found by diffing against a C loader built from the kernel headers.

It's opt-in by privilege, not the default: unprivileged runs skip it and rely
on the interposer. A `connect4` hook could give localhost routing at the same
layer; not yet implemented (the lsof/registry daemon covers it today).

## Addressing

- **`<name>.devhost`** — the sanitized project basename, mapped to the
  project IP. On macOS the daemon runs a small DNS responder (RFC 1035, just
  enough to answer one A question) and `/etc/resolver/devhost` — installed
  once by the helper — routes only that TLD to it. `/etc/hosts` is never
  mutated: each activation records its `name -> ip` in a registry the
  responder serves. Because the interposer derives IPs one-way from paths,
  `name -> ip` isn't computable, so the registry is the source of truth.
  Without the helper, devhost falls back to a tagged `/etc/hosts` line; Linux
  stays on that path for now (its per-domain DNS story is distro-specific).
  A product-scoped TLD keeps devhost out of `.test`, which other local-dev
  tools (puma-dev and friends) conventionally claim. The honest trade-off:
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

Caller lookup uses the `proc_info(2)` syscall in-process on macOS (source
port → PID → cwd, no fork+exec), which is sub-millisecond; it falls back to
`lsof` if the struct offsets ever drift. Trade-off, stated plainly: while a
devhost project holds a port, unrelated apps can't bind `127.0.0.1:<that
port>`. The daemon is an optional convenience layer — IP virtualization and
`.devhost` names work without it.

## Privilege model

macOS needs root for `lo0` aliases and, once, to drop the
`/etc/resolver/devhost` stub (after which `/etc/hosts` is never touched). The
supported path is `devhost setup --helper`: a short root-owned validating
shell script (`/usr/local/libexec/devhost-helper`, auditable in one
screenful) allowed by a single NOPASSWD sudoers line that names exactly it.
The helper accepts `alias <ip>`, `hosts <ip> <name> <root>`,
`resolver <port>`, and `resolver-remove` — validating every field (IP must be inside
`127.77.0.0/16`, the hostname must be one DNS label, the root path may not
contain control characters, `#`, or backslashes, and embedded newlines are
refused everywhere since per-line regex matching would otherwise let a
second line through), and composes the `/etc/hosts` line itself so callers
can never write arbitrary content. The sudoers file is checked with
`visudo -cf` before install — a malformed sudoers file can lock sudo up.
Plain passwordless sudo works as a fallback; without either, hostname
registration degrades and direct-IP access keeps working. Linux needs no
privilege for IPs at all.
