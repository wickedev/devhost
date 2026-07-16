//go:build darwin

package daemon

import (
	"net"
	"os"
	"testing"
)

// Open a real loopback connection and confirm proc_info finds this very
// process by the client socket's local port — proving the offsets are right
// (not just that the lsof fallback works).
func TestPidBySrcPortProc(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	dst := ln.Addr().(*net.TCPAddr).Port

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	src := c.LocalAddr().(*net.TCPAddr).Port

	pid, err := pidBySrcPortProc(src, dst)
	if err != nil {
		t.Fatalf("pidBySrcPortProc: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("pid = %d, want this process %d", pid, os.Getpid())
	}

	cwd, err := cwdOfPidProc(pid)
	if err != nil {
		t.Fatalf("cwdOfPidProc: %v", err)
	}
	want, _ := os.Getwd()
	if cwd != want {
		t.Fatalf("cwd = %q, want %q", cwd, want)
	}
}
