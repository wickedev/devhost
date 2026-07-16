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
	"github.com/wickedev/devhost/internal/shim"
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
	for _, a := range args {
		switch a {
		case "--helper":
			return privhelper.Install()
		case "--preload":
			return interpose.InstallPreload()
		case "--preload-remove":
			return interpose.PreloadRemove()
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
	fmt.Println("\nAdd to your shell profile, AFTER any version manager init")
	fmt.Println("(the shim must win the PATH race, then hand off to it):")
	fmt.Printf("\n  export PATH=\"%s:$PATH\"\n\n", dir)
	fmt.Println("Optional — localhost routing (`curl localhost:3000` inside a workspace):")
	fmt.Println("  run `devhost daemon` under your service manager (launchd/systemd)")
	if !privhelper.Installed() {
		fmt.Println("\nRecommended — a narrow root helper instead of broad sudo:")
		fmt.Println("  devhost setup --helper   (one-time password prompt)")
	}
	return nil
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
