// Package daemon implements devhostd, the localhost mirror-router.
//
// Project servers bind 127.77.x.y:<port>, which leaves 127.0.0.1:<port> free.
// The daemon watches for devhost listeners and mirror-listens on the same
// port at 127.0.0.1. Each incoming connection is routed by identifying the
// calling process (from its source port), reading its working directory, and
// resolving the nearest .devhost project — so `curl localhost:3000` inside a
// workspace reaches that workspace's server, with zero client changes.
//
// The daemon never squats ports: mirrors exist only while a devhost listener
// does, and if a real application already owns 127.0.0.1:<port> the bind
// fails and is respected.
package daemon

import (
	"context"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/wickedev/devhost/internal/addr"
	"github.com/wickedev/devhost/internal/dnsserver"
	"github.com/wickedev/devhost/internal/project"
)

const (
	scanInterval = 2 * time.Second
	dialTimeout  = 3 * time.Second
)

// Run starts the mirror-router (and the DNS responder) and blocks until ctx
// is done.
func Run(ctx context.Context) error {
	go func() {
		if err := dnsserver.ListenAndServe(); err != nil {
			log.Printf("dns responder: %v (hostnames fall back to /etc/hosts)", err)
		}
	}()

	r := &router{mirrors: map[int]net.Listener{}}
	log.Printf("mirror-router started (scan every %s); dns responder on 127.0.0.1:%d", scanInterval, dnsserver.Port)
	ticker := time.NewTicker(scanInterval)
	defer ticker.Stop()
	for {
		r.reconcile()
		select {
		case <-ctx.Done():
			r.shutdown()
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type router struct {
	mu      sync.Mutex
	ports   map[int]map[string]bool // port -> set of devhost IPs listening
	mirrors map[int]net.Listener
}

func (r *router) reconcile() {
	ports, err := DevhostListeners()
	if err != nil {
		return // transient scan failure — keep current state
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ports = ports
	for port := range ports {
		if _, ok := r.mirrors[port]; ok {
			continue
		}
		ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			continue // a real app owns 127.0.0.1:<port> — respect it
		}
		r.mirrors[port] = ln
		go r.serve(port, ln)
		log.Printf("mirroring 127.0.0.1:%d", port)
	}
	for port, ln := range r.mirrors {
		if _, ok := ports[port]; !ok {
			ln.Close()
			delete(r.mirrors, port)
			log.Printf("released 127.0.0.1:%d", port)
		}
	}
}

func (r *router) shutdown() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for port, ln := range r.mirrors {
		ln.Close()
		delete(r.mirrors, port)
	}
}

func (r *router) serve(port int, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed by reconcile/shutdown
		}
		go r.handle(port, conn)
	}
}

func (r *router) handle(port int, client net.Conn) {
	defer client.Close()
	ra, ok := client.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}
	ip := r.target(ra.Port, port)
	if ip == "" {
		return // ambiguous or unknown caller — drop, like a refused connection
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(port)), dialTimeout)
	if err != nil {
		return
	}
	defer upstream.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(upstream, client); done <- struct{}{} }() //nolint:errcheck
	go func() { io.Copy(client, upstream); done <- struct{}{} }() //nolint:errcheck
	<-done
}

// target picks the upstream project IP for a client connection: route by the
// calling process's working directory when identifiable, else fall back to
// the only candidate, else give up.
func (r *router) target(srcPort, dstPort int) string {
	r.mu.Lock()
	candidates := r.ports[dstPort]
	r.mu.Unlock()
	if len(candidates) == 0 {
		return ""
	}
	if pid, err := pidBySrcPort(srcPort, dstPort); err == nil {
		if cwd, err := cwdOfPid(pid); err == nil {
			if root := project.FindRoot(cwd); root != "" {
				if ip := addr.ForDir(root); candidates[ip] {
					return ip
				}
			}
		}
	}
	if len(candidates) == 1 {
		for ip := range candidates {
			return ip
		}
	}
	return ""
}
