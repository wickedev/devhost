// Docker socket proxy: per-project container port isolation.
//
// Containers publish host ports through the container runtime (dockerd,
// OrbStack), a process devhost can't inject a bind() interposer into — the
// bind happens as root or inside a VM. So instead of rewriting the bind,
// devhost rewrites the *request*: it sits on a unix socket in front of the
// real Docker API, identifies the calling `docker`/`docker compose` process
// from the socket peer credential, resolves its .devhost project, and rewrites
// the container-create body so published ports land on that project's loopback
// IP (127.77.x.y) instead of the shared host. Two projects can then both run
// `-p 3000:3000` without colliding, exactly like host dev servers.
//
// Everything except container-create passes through untouched, including
// hijacked attach/exec streams. Point Docker at it with
// DOCKER_HOST=unix://~/.config/devhost/docker.sock.
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/wickedev/devhost/internal/addr"
	"github.com/wickedev/devhost/internal/config"
	"github.com/wickedev/devhost/internal/project"
)

type dpCtxKey int

const connCtxKey dpCtxKey = 0

// ProxySocket returns the path devhost's Docker proxy listens on.
func ProxySocket() string {
	return filepath.Join(config.Dir(), "docker.sock")
}

// ResolveUpstream finds the real Docker socket to forward to, never returning
// the proxy's own socket (which would loop). Precedence: DEVHOST_DOCKER_UPSTREAM,
// a unix:// DOCKER_HOST, then the well-known OrbStack/Docker Desktop paths.
func ResolveUpstream(listen string) string {
	unixPath := func(v string) string { return strings.TrimPrefix(v, "unix://") }
	if v := os.Getenv("DEVHOST_DOCKER_UPSTREAM"); v != "" {
		return unixPath(v)
	}
	if v := os.Getenv("DOCKER_HOST"); strings.HasPrefix(v, "unix://") {
		if p := unixPath(v); p != listen {
			return p
		}
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".orbstack/run/docker.sock"),
		filepath.Join(home, ".docker/run/docker.sock"),
		"/var/run/docker.sock",
	} {
		if p == listen {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return p
		}
	}
	return ""
}

// RunDockerProxy serves the Docker API proxy on listenPath, forwarding to the
// real daemon at upstreamPath. Blocks until ctx is done.
func RunDockerProxy(ctx context.Context, listenPath, upstreamPath string) error {
	if err := os.MkdirAll(filepath.Dir(listenPath), 0o755); err != nil {
		return err
	}
	if err := os.Remove(listenPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	ln, err := net.Listen("unix", listenPath)
	if err != nil {
		return err
	}
	os.Chmod(listenPath, 0o600) //nolint:errcheck
	defer ln.Close()

	dialer := &net.Dialer{}
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = "docker" // ignored; the transport dials the unix socket
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, "unix", upstreamPath)
			},
		},
		FlushInterval: -1, // stream logs/attach/pull progress in real time
		ErrorLog:      log.Default(),
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isContainerCreate(r) {
				if ip := projectIPForRequest(r); ip != "" {
					rewriteCreateBody(r, ip)
				}
			}
			rp.ServeHTTP(w, r)
		}),
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			return context.WithValue(ctx, connCtxKey, c)
		},
	}
	go func() { <-ctx.Done(); srv.Close() }()
	log.Printf("docker proxy: %s -> %s", listenPath, upstreamPath)
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return ctx.Err()
}

func isContainerCreate(r *http.Request) bool {
	// Path is "/containers/create" or "/v1.51/containers/create".
	return r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/containers/create")
}

// projectIPForRequest resolves the calling process (via the unix socket peer
// PID) to its .devhost project loopback IP, or "" when the caller isn't inside
// a project or can't be identified — in which case nothing is rewritten.
func projectIPForRequest(r *http.Request) string {
	uc, ok := r.Context().Value(connCtxKey).(*net.UnixConn)
	if !ok {
		return ""
	}
	pid, err := peerPID(uc)
	if err != nil {
		return ""
	}
	cwd, err := cwdOfPid(pid)
	if err != nil {
		return ""
	}
	root := project.FindRoot(cwd)
	if root == "" {
		return ""
	}
	return addr.ForDir(root)
}

type portBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// sharedHostIP reports whether a HostIp binding lands on the shared host (and
// so would collide between projects). An explicit non-shared IP the user chose
// is left untouched.
func sharedHostIP(ip string) bool {
	switch ip {
	case "", "0.0.0.0", "127.0.0.1", "::", "::1":
		return true
	}
	return false
}

// rewriteCreateBody replaces the request body with one whose shared-host port
// publishings are rebound to projectIP. Untouched fields round-trip verbatim
// (they stay json.RawMessage), so numeric limits etc. keep full precision.
func rewriteCreateBody(r *http.Request, projectIP string) {
	if r.Body == nil {
		return
	}
	raw, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		r.Body = io.NopCloser(bytes.NewReader(raw))
		return
	}
	out, changed := rewritePortBindings(raw, projectIP)
	if !changed {
		out = raw
	}
	r.Body = io.NopCloser(bytes.NewReader(out))
	r.ContentLength = int64(len(out))
	r.Header.Set("Content-Length", strconv.Itoa(len(out)))
	r.TransferEncoding = nil
}

func rewritePortBindings(body []byte, projectIP string) ([]byte, bool) {
	var top map[string]json.RawMessage
	if json.Unmarshal(body, &top) != nil {
		return body, false
	}
	hcRaw, ok := top["HostConfig"]
	if !ok {
		return body, false
	}
	var hc map[string]json.RawMessage
	if json.Unmarshal(hcRaw, &hc) != nil {
		return body, false
	}
	pbRaw, ok := hc["PortBindings"]
	if !ok {
		return body, false
	}
	var pb map[string][]portBinding
	if json.Unmarshal(pbRaw, &pb) != nil {
		return body, false
	}
	changed := false
	for _, binds := range pb {
		for i := range binds {
			if sharedHostIP(binds[i].HostIP) {
				binds[i].HostIP = projectIP
				changed = true
			}
		}
	}
	if !changed {
		return body, false
	}
	newPB, err := json.Marshal(pb)
	if err != nil {
		return body, false
	}
	hc["PortBindings"] = newPB
	newHC, err := json.Marshal(hc)
	if err != nil {
		return body, false
	}
	top["HostConfig"] = newHC
	newBody, err := json.Marshal(top)
	if err != nil {
		return body, false
	}
	return newBody, true
}
