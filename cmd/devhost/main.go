// Command devhost virtualizes dev-server ports per directory: every project
// (a directory tree containing a .devhost marker) gets its own deterministic
// loopback IP, so any number of projects and git worktrees can all bind the
// same port — e.g. :3000 — at the same time without killing each other.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/wickedev/devhost/internal/daemon"
	"github.com/wickedev/devhost/internal/selfupdate"
)

// version is injected by goreleaser (-X main.version) on release builds.
var version = "dev"

func main() {
	log.SetFlags(0)
	log.SetPrefix("devhost: ")
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "init":
		err = cmdInit(args)
	case "ip":
		err = cmdIP(args)
	case "name":
		err = cmdName(args)
	case "exec":
		err = cmdExec(args)
	case "shim-exec":
		err = cmdShimExec(args)
	case "setup":
		err = cmdSetup(args)
	case "daemon":
		err = daemon.Run(context.Background())
	case "ls":
		err = cmdLs(args)
	case "doctor":
		err = cmdDoctor(args)
	case "upgrade":
		err = selfupdate.Upgrade(version, os.Stdout)
	case "version":
		fmt.Println(version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		err = fmt.Errorf("unknown command %q", cmd)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `devhost — per-directory port virtualization for dev servers

Every .devhost-marked directory tree gets its own loopback IP, so projects
and git worktrees can all bind :3000 at once. See README for the full story.

Usage:
  devhost init [dir]        opt a project in (creates a .devhost marker)
  devhost ip [dir]          print the project's loopback IP
  devhost name [dir]        print the project's hostname label (<name>.devhost)
  devhost exec -- CMD ...   run CMD with the project environment applied
  devhost setup             one-shot machine setup: shims, PATH, daemon
                            service (launchd/systemd), root helper
                            (--no-profile / --no-daemon / --no-helper
                            skip parts; --daemon-remove unregisters)
  devhost setup --helper    install the narrow root helper (one-time sudo)
  devhost setup --preload   (linux) load the interposer via /etc/ld.so.preload
  devhost daemon            run the localhost mirror-router (devhostd)
  devhost ls                list active devhost listeners
  devhost doctor            diagnose the local installation
  devhost upgrade           update devhost to the latest release
  devhost version           print version
`)
}
