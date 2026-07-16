package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"

	"github.com/wickedev/devhost/internal/addr"
	"github.com/wickedev/devhost/internal/daemon"
	"github.com/wickedev/devhost/internal/hosts"
	"github.com/wickedev/devhost/internal/inject"
	"github.com/wickedev/devhost/internal/interpose"
	"github.com/wickedev/devhost/internal/netif"
	"github.com/wickedev/devhost/internal/project"
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
// alias plus hosts entry. Each step is best-effort so a missing privilege
// degrades one feature (e.g. the .devhost hostname), not the whole run.
func activate(root string) error {
	ip := addr.ForDir(root)
	var errs []error
	if err := netif.EnsureAlias(ip); err != nil {
		errs = append(errs, err)
	}
	if err := hosts.Ensure(ip, addr.Name(root), root); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func cmdExec(args []string) error {
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return errors.New("usage: devhost exec -- CMD [ARGS...]")
	}
	env := os.Environ()
	if cwd, err := os.Getwd(); err == nil {
		if root := project.FindRoot(cwd); root != "" {
			if err := activate(root); err != nil {
				log.Printf("warning: %v", err)
			}
			env = inject.Env(env, root)
		}
	}
	path, err := exec.LookPath(args[0])
	if err != nil {
		return err
	}
	cmd := exec.Command(path, args[1:]...)
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		os.Exit(exitErr.ExitCode())
	}
	return err
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
	names := hosts.Names()
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

	cwd, _ := os.Getwd()
	root := project.FindRoot(cwd)
	report(root != "", "project marker ("+project.Marker+")", "run `devhost init` in your project root")
	if root != "" {
		ip := addr.ForDir(root)
		report(netif.HasAlias(ip), "loopback alias "+ip,
			"created on first activation; manual: sudo ifconfig lo0 alias "+ip+" up")
		name := addr.Name(root)
		report(hosts.Has(ip, name), "hosts entry "+hosts.FQDN(name),
			"created on first activation (needs passwordless sudo or the helper)")
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
