package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/wickedev/devhost/internal/addr"
	"github.com/wickedev/devhost/internal/daemon"
	"github.com/wickedev/devhost/internal/dnsserver"
	"github.com/wickedev/devhost/internal/ebpf"
	"github.com/wickedev/devhost/internal/hosts"
	"github.com/wickedev/devhost/internal/inject"
	"github.com/wickedev/devhost/internal/interpose"
	"github.com/wickedev/devhost/internal/netif"
	"github.com/wickedev/devhost/internal/privhelper"
	"github.com/wickedev/devhost/internal/project"
	"github.com/wickedev/devhost/internal/registry"
	"github.com/wickedev/devhost/internal/selfupdate"
	"github.com/wickedev/devhost/internal/service"
	"github.com/wickedev/devhost/internal/shim"
	"github.com/wickedev/devhost/internal/skills"
)

// rootFrom resolves the project root for an optional [dir] argument,
// defaulting to the current directory.
func rootFrom(args []string) (string, error) {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	root := project.FindRoot(abs)
	if root == "" {
		return "", fmt.Errorf("no %s marker found above %s (run `devhost init`)", project.Marker, abs)
	}
	return root, nil
}

func cmdInit(args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	marker := filepath.Join(abs, project.Marker)
	if _, err := os.Stat(marker); err == nil {
		fmt.Println("already initialized:", marker)
		return nil
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return err
	}
	fmt.Printf("initialized %s\n  ip:   %s\n  host: %s\n", marker, addr.ForDir(abs), hosts.FQDN(addr.Name(abs)))
	// Activate right now instead of lazily on the first server run, so a
	// missing privilege surfaces here — where the fix is actionable — and
	// the first `npm run dev` just works.
	if err := activate(abs); err != nil {
		fmt.Printf("  activation incomplete: %v\n", err)
	} else {
		fmt.Println("  network ready: loopback IP + hostname registered")
	}
	fmt.Println("commit the marker so git worktrees inherit it")
	return nil
}

func cmdIP(args []string) error {
	root, err := rootFrom(args)
	if err != nil {
		return err
	}
	fmt.Println(addr.ForDir(root))
	return nil
}

func cmdName(args []string) error {
	root, err := rootFrom(args)
	if err != nil {
		return err
	}
	fmt.Println(addr.Name(root))
	return nil
}

// activate makes a project's loopback IP and hostname usable: interface
// alias, DNS registry entry, and — only when the DNS resolver isn't handling
// the TLD — an /etc/hosts fallback. Each step is best-effort so a missing
// privilege degrades one feature (e.g. the .devhost hostname), not the whole
// run.
func activate(root string) error {
	ip := addr.ForDir(root)
	name := addr.Name(root)
	var errs []error
	if err := netif.EnsureAlias(ip); err != nil {
		errs = append(errs, err)
	}
	// Always record for the DNS responder; harmless if unused.
	registry.Add(name, ip, root) //nolint:errcheck
	// When the resolver routes .devhost to our responder, never touch
	// /etc/hosts — that's the whole point of the DNS path.
	if !privhelper.ResolverInstalled() {
		if err := hosts.Ensure(ip, name, root); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func cmdExec(args []string) error {
	proxy := false
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		if args[0] == "--proxy" {
			proxy = true
		}
		args = args[1:]
	}
	if len(args) == 0 {
		return errors.New("usage: devhost exec [--proxy] -- CMD [ARGS...]")
	}
	env := os.Environ()
	projectIP := ""
	if cwd, err := os.Getwd(); err == nil {
		if root := project.FindRoot(cwd); root != "" {
			if err := activate(root); err != nil {
				log.Printf("warning: %v", err)
			}
			env = inject.Env(env, root)
			applyEBPF(root) // Linux kernel tier; children inherit the cgroup
			projectIP = addr.ForDir(root)
		}
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr

	// --proxy: for servers that can't be injected (hardened/static binaries),
	// mirror whatever loopback port they open onto the project IP so they are
	// still reachable at <name>.devhost. Note: this gives reachability, not
	// same-port isolation between un-injectable servers.
	if proxy && projectIP != "" {
		stop := make(chan struct{})
		defer close(stop)
		go watchAndProxy(projectIP, stop)
	}

	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	return err
}

// watchAndProxy mirrors newly-appearing 127.0.0.1 listeners onto projectIP,
// diffing against a baseline so it only touches ports opened after start.
func watchAndProxy(projectIP string, stop <-chan struct{}) {
	baseline, _ := daemon.LoopbackListenerPorts()
	proxied := map[int]bool{}
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
		}
		cur, err := daemon.LoopbackListenerPorts()
		if err != nil {
			continue
		}
		for port := range cur {
			if baseline[port] || proxied[port] {
				continue
			}
			proxied[port] = true
			go proxyPort(projectIP, port, stop)
			fmt.Fprintf(os.Stderr, "devhost: proxying %s:%d -> 127.0.0.1:%d\n", projectIP, port, port)
		}
	}
}

func proxyPort(projectIP string, port int, stop <-chan struct{}) {
	ln, err := net.Listen("tcp", net.JoinHostPort(projectIP, fmt.Sprint(port)))
	if err != nil {
		return // project IP:port already taken (e.g. the server bound it itself)
	}
	go func() { <-stop; ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer c.Close()
			u, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)))
			if err != nil {
				return
			}
			defer u.Close()
			done := make(chan struct{}, 2)
			go func() { io.Copy(u, c); done <- struct{}{} }() //nolint:errcheck
			go func() { io.Copy(c, u); done <- struct{}{} }() //nolint:errcheck
			<-done
		}()
	}
}

// cmdShimExec backs the generated PATH shims: `devhost shim-exec TOOL -- ARGS`.
// It applies the project environment (when inside a .devhost tree) and execs
// the real TOOL, replacing this process.
func cmdShimExec(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: devhost shim-exec TOOL -- [ARGS...]")
	}
	tool, rest := args[0], args[1:]
	if len(rest) > 0 && rest[0] == "--" {
		rest = rest[1:]
	}
	real, err := shim.RealBinary(tool)
	if err != nil {
		return err
	}
	env := os.Environ()
	if cwd, err := os.Getwd(); err == nil {
		if root := project.FindRoot(cwd); root != "" {
			if err := activate(root); err != nil {
				fmt.Fprintf(os.Stderr, "devhost: warning: %v\n", err)
			}
			env = inject.Env(env, root)
			applyEBPF(root) // Linux kernel tier; the exec'd process stays in the cgroup
			if runtime.GOOS == "darwin" {
				// A version-manager shim script between us and the runtime
				// would strip DYLD_* at its /bin/bash hop — exec the real
				// binary instead. Only needed inside a project.
				real = shim.ResolveThroughManagers(real, tool)
			}
		}
	}
	return syscall.Exec(real, append([]string{tool}, rest...), env)
}

func cmdSetup(args []string) error {
	noProfile, noDaemon, noHelper, noSkill, noDocker := false, false, false, false, false
	for _, a := range args {
		switch a {
		case "--helper":
			return privhelper.Install()
		case "--preload":
			return interpose.InstallPreload()
		case "--preload-remove":
			return interpose.PreloadRemove()
		case "--daemon-remove":
			return service.Remove()
		case "--skill":
			return skills.Refresh(os.Stdout)
		case "--no-profile":
			noProfile = true
		case "--no-daemon":
			noDaemon = true
		case "--no-helper":
			noHelper = true
		case "--no-skill":
			noSkill = true
		case "--no-docker":
			noDocker = true
		}
	}
	dir, installed, err := shim.Install(shim.DefaultTools)
	if err != nil {
		return err
	}
	fmt.Printf("installed shims: %s (%s)\n", dir, strings.Join(installed, ", "))
	if lib, err := interpose.Ensure(); err != nil {
		fmt.Printf("bind() interposer skipped: %v\n", err)
		fmt.Println("  (Node keeps working via NODE_OPTIONS; other runtimes need HOST)")
	} else {
		fmt.Println("compiled bind() interposer:", lib)
	}
	setupPath(dir, noProfile)

	if noDaemon {
		fmt.Println("\ndaemon service skipped (--no-daemon) — localhost routing needs `devhost daemon` running")
	} else if err := service.Install(); err != nil {
		fmt.Printf("\ndaemon service skipped: %v\n", err)
		fmt.Println("  localhost routing needs `devhost daemon` running; register it by hand or rerun setup")
	} else {
		fmt.Println("\nregistered daemon service:", service.Kind(), "(localhost routing + .devhost DNS)")
		if daemon.ResolveUpstream(daemon.ProxySocket()) != "" {
			setupDockerHost(noProfile || noDocker)
		}
	}

	switch {
	case privhelper.Installed() || noHelper:
	case sudoPossible():
		if err := privhelper.Install(); err != nil {
			fmt.Printf("root helper skipped: %v\n", err)
			fmt.Println("  finish later with: devhost setup --helper")
		}
	default:
		fmt.Println("root helper skipped (sudo unavailable here)")
		fmt.Println("  finish later with: devhost setup --helper   (one-time password prompt)")
	}

	switch {
	case noSkill:
	case skills.Available():
		fmt.Println("\nrefreshing the devhost agent skill (skills.sh)…")
		if err := skills.Refresh(os.Stdout); err != nil {
			fmt.Printf("skill refresh skipped: %v\n", err)
		}
	default:
		fmt.Println("\nagent skill skipped (Node/npx not found)")
		fmt.Printf("  install it later with: npx skills add %s\n", skills.Pkg)
	}
	return nil
}

// sudoPossible reports whether privhelper.Install's sudo calls can succeed:
// either passwordless sudo works, or a terminal exists for the password
// prompt (sudo prompts on /dev/tty, so this survives `curl | sh`).
func sudoPossible() bool {
	if exec.Command("sudo", "-n", "true").Run() == nil {
		return true
	}
	tty, err := os.Open("/dev/tty")
	if err != nil {
		return false
	}
	tty.Close()
	return true
}

// setupPath puts the shim dir (and the devhost binary's dir, when devhost
// itself isn't resolvable yet — shims exec `devhost shim-exec`) on PATH by
// editing the user's shell profile. --no-profile, or a shell we don't know
// how to edit, falls back to printing the lines to add by hand.
func setupPath(shimDir string, noProfile bool) {
	dirs := []string{shimDir}
	if _, err := exec.LookPath("devhost"); err != nil {
		if exe, err := os.Executable(); err == nil {
			dirs = append(dirs, filepath.Dir(exe))
		}
	}
	manual := dirs[:0:0]
	if noProfile {
		manual = dirs
	} else {
		for _, d := range dirs {
			profile, added, err := shim.AppendPathToProfile(d)
			switch {
			case err != nil:
				fmt.Printf("could not edit shell profile: %v\n", err)
				manual = append(manual, d)
			case added:
				fmt.Printf("added to %s: export PATH=\"%s:$PATH\"\n", profile, d)
			default:
				fmt.Printf("%s already puts %s on PATH\n", profile, d)
			}
		}
		if len(manual) < len(dirs) {
			fmt.Println("restart your shell (or source the profile) to pick it up")
		}
	}
	if len(manual) > 0 {
		fmt.Println("\nAdd to your shell profile, AFTER any version manager init")
		fmt.Println("(the shim must win the PATH race, then hand off to it):")
		fmt.Println()
		for _, d := range manual {
			fmt.Printf("  export PATH=\"%s:$PATH\"\n", d)
		}
	}
}

// cmdUpgrade updates the binary, then brings the paired agent skill along so
// the two don't drift. The skill refresh is best-effort and never fails the
// upgrade. (A Homebrew-managed binary upgrades via `brew upgrade`; the skill
// then rides along on the next `devhost setup`, and `devhost doctor` flags it
// meanwhile.)
func cmdUpgrade(args []string) error {
	if err := selfupdate.Upgrade(version, os.Stdout); err != nil {
		return err
	}
	if skills.Available() {
		fmt.Println("\nrefreshing the devhost agent skill…")
		if err := skills.Refresh(os.Stdout); err != nil {
			fmt.Printf("skill refresh skipped: %v\n", err)
		}
	}
	return nil
}

// setupDockerHost wires the shell profile to point Docker at devhost's proxy,
// so container ports isolate per project without a manual step. The exported
// line is guarded (fires only when the proxy socket exists and DOCKER_HOST is
// unset), so it never breaks `docker` when the daemon is down. skip prints the
// line instead of editing the profile (--no-profile / --no-docker).
func setupDockerHost(skip bool) {
	sock := daemon.ProxySocket()
	if skip {
		fmt.Printf("  container ports: point Docker at the proxy —\n    export DOCKER_HOST=unix://%s\n", sock)
		return
	}
	marker, line, err := shim.DockerHostProfileEntry()
	if err != nil {
		fmt.Printf("  container ports: add to your profile —\n    export DOCKER_HOST=unix://%s\n", sock)
		return
	}
	profile, added, err := shim.AppendLineToProfile(marker, line)
	switch {
	case err != nil:
		fmt.Printf("  container ports: couldn't edit profile (%v); add by hand —\n    export DOCKER_HOST=unix://%s\n", err, sock)
	case added:
		fmt.Printf("  container ports: added a guarded DOCKER_HOST export to %s\n    (Docker uses the devhost proxy when the daemon is up; restart your shell)\n", profile)
	default:
		fmt.Printf("  container ports: %s already points Docker at the devhost proxy\n", profile)
	}
}

func cmdLs(args []string) error {
	ports, err := daemon.DevhostListeners()
	if err != nil {
		return err
	}
	if len(ports) == 0 {
		fmt.Println("no active devhost listeners")
		return nil
	}
	names := hosts.Names() // /etc/hosts path
	for _, e := range registry.All() {
		if names[e.IP] == "" { // DNS path — registry is the source of truth
			names[e.IP] = e.Name
		}
	}
	var lines []string
	for port, ips := range ports {
		for ip := range ips {
			name := names[ip]
			if name != "" {
				name = hosts.FQDN(name)
			}
			lines = append(lines, fmt.Sprintf("%s:%d\t%s", ip, port, name))
		}
	}
	sort.Strings(lines)
	fmt.Println(strings.Join(lines, "\n"))
	return nil
}

// applyEBPF, on Linux with privilege, attaches the kernel-level bind4 rewrite
// to a per-project cgroup and moves this process into it so the server (and
// its children) are governed by it — covering static binaries the interposer
// can't. A no-op without privilege or off Linux; the interposer still applies.
func applyEBPF(root string) {
	if !ebpf.Available() {
		return
	}
	cg, err := ebpf.Activate(addr.ForDir(root))
	if err != nil {
		fmt.Fprintf(os.Stderr, "devhost: ebpf: %v (falling back to the interposer)\n", err)
		return
	}
	if err := ebpf.JoinCgroup(cg, os.Getpid()); err != nil {
		fmt.Fprintf(os.Stderr, "devhost: ebpf: joining cgroup: %v\n", err)
	}
}

// dnsResponderAlive probes the local responder for a known registered name.
func dnsResponderAlive() bool {
	entries := registry.All()
	if len(entries) == 0 {
		return true // nothing to resolve yet — don't cry wolf
	}
	c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", fmt.Sprint(dnsserver.Port)))
	if err != nil {
		return false
	}
	defer c.Close()
	// minimal A query for "<name>.devhost"
	name := entries[0].Name
	q := []byte{0x12, 0x34, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0}
	q = append(q, byte(len(name)))
	q = append(q, name...)
	q = append(q, byte(len("devhost")))
	q = append(q, "devhost"...)
	q = append(q, 0, 0, 1, 0, 1)
	c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write(q); err != nil {
		return false
	}
	resp := make([]byte, 64)
	n, err := c.Read(resp)
	return err == nil && n >= 12 && resp[0] == 0x12 && resp[1] == 0x34
}

func cmdDoctor(args []string) error {
	report := func(ok bool, label, hint string) {
		mark := "✓"
		if !ok {
			mark = "✗"
		}
		fmt.Printf("%s %s\n", mark, label)
		if !ok && hint != "" {
			fmt.Printf("  → %s\n", hint)
		}
	}

	_, err := exec.LookPath("devhost")
	report(err == nil, "devhost on PATH", "install the binary somewhere on PATH — shims depend on it")

	shimDir := shim.Dir()
	onPath := false
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if d == shimDir {
			onPath = true
			break
		}
	}
	report(onPath, "shim dir on PATH ("+shimDir+")", "run `devhost setup` and add the printed export line")

	if lib, err := interpose.Ensure(); err != nil {
		report(false, "bind() interposer", err.Error())
	} else {
		report(true, "bind() interposer ("+lib+")", "")
	}

	if runtime.GOOS == "linux" {
		if ebpf.Available() {
			report(true, "eBPF bind4 backend (kernel-level, catches static binaries)", "")
		} else {
			fmt.Println("- eBPF backend unavailable (needs root/CAP_BPF + writable cgroup2; the interposer covers dynamic runtimes)")
		}
	}

	if privhelper.Installed() {
		report(true, "privileged helper ("+privhelper.Path+")", "")
	} else if exec.Command("sudo", "-n", "true").Run() == nil {
		fmt.Println("- privileged helper not installed (passwordless sudo covers it; `devhost setup --helper` to narrow)")
	} else {
		report(false, "privileged helper", "run `devhost setup --helper` — without it, lo0 aliases and .devhost hostnames can't be registered")
	}

	if privhelper.ResolverInstalled() {
		// The resolver only helps if the responder is actually answering.
		alive := dnsResponderAlive()
		report(alive, "DNS resolver (.devhost via responder)",
			"resolver is configured but the responder isn't answering — start `devhost daemon`")
	} else {
		fmt.Println("- DNS resolver not configured (.devhost names use /etc/hosts; `devhost setup --helper` switches to DNS)")
	}

	if service.Installed() {
		report(true, "daemon service ("+service.Kind()+")", "")
	} else {
		fmt.Println("- daemon service not registered (localhost routing off; `devhost setup` registers it)")
	}

	switch {
	case !skills.Installed() && skills.Available():
		report(false, "agent skill (devhost)", "install it: devhost setup   (or: npx skills add "+skills.Pkg+")")
	case !skills.Installed():
		fmt.Println("- agent skill not installed (optional; Node/npx not found to install it)")
	default:
		// Installed — flag it only when we can confirm it's behind the source.
		if outdated, err := skills.Outdated(); err == nil && outdated {
			report(false, "agent skill (devhost) — differs from the published version",
				"refresh it: devhost setup   (or: npx skills update "+skills.Name+")")
		} else {
			report(true, "agent skill (devhost)", "")
		}
	}

	cwd, _ := os.Getwd()
	root := project.FindRoot(cwd)
	report(root != "", "project marker ("+project.Marker+")", "run `devhost init` in your project root")
	if root != "" {
		ip := addr.ForDir(root)
		report(netif.HasAlias(ip), "loopback alias "+ip,
			"created on first activation; manual: sudo ifconfig lo0 alias "+ip+" up")
		name := addr.Name(root)
		if privhelper.ResolverInstalled() {
			// DNS mode: the name lives in the registry, not /etc/hosts.
			report(registry.Lookup(name) == ip, "DNS entry "+hosts.FQDN(name),
				"recorded on first activation (run a dev server once in this project)")
		} else {
			report(hosts.Has(ip, name), "hosts entry "+hosts.FQDN(name),
				"created on first activation (needs passwordless sudo or the helper)")
		}
	}

	ports, err := daemon.DevhostListeners()
	report(err == nil, "listener scan", fmt.Sprint(err))
	if err == nil {
		fmt.Printf("  %d active devhost listener port(s)\n", len(ports))
	}

	if latest, err := selfupdate.Latest(); err != nil {
		fmt.Println("- version check skipped (release lookup failed)")
	} else if version == latest {
		fmt.Printf("✓ devhost %s is the latest release\n", version)
	} else {
		report(false, fmt.Sprintf("devhost %s (latest is %s)", version, latest),
			"run `devhost upgrade`")
	}
	return nil
}
