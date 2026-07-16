//go:build linux

package daemon

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// /proc-based socket introspection. Listener discovery is fully implemented;
// caller-PID resolution would need an inode scan across /proc/*/fd and is
// intentionally left to the planned eBPF backend (see docs/architecture.md),
// so routing falls back to the unique-candidate rule on Linux for now.

// DevhostListeners parses /proc/net/tcp for LISTEN sockets in 127.77.0.0/16.
func DevhostListeners() (map[int]map[string]bool, error) {
	b, err := os.ReadFile("/proc/net/tcp")
	if err != nil {
		return nil, err
	}
	res := map[int]map[string]bool{}
	lines := strings.Split(string(b), "\n")
	for _, l := range lines[1:] {
		f := strings.Fields(l)
		if len(f) < 4 || f[3] != "0A" { // 0A = TCP_LISTEN
			continue
		}
		hostPort := strings.Split(f[1], ":")
		if len(hostPort) != 2 || len(hostPort[0]) != 8 {
			continue
		}
		v, err := strconv.ParseUint(hostPort[0], 16, 32)
		if err != nil {
			continue
		}
		// /proc/net/tcp stores IPv4 little-endian: low byte = first octet
		o1, o2 := byte(v), byte(v>>8)
		if o1 != 127 || o2 != 77 {
			continue
		}
		port64, err := strconv.ParseUint(hostPort[1], 16, 16)
		if err != nil {
			continue
		}
		ip := fmt.Sprintf("127.77.%d.%d", byte(v>>16), byte(v>>24))
		port := int(port64)
		if res[port] == nil {
			res[port] = map[string]bool{}
		}
		res[port][ip] = true
	}
	return res, nil
}

func pidBySrcPort(int, int) (int, error) {
	return 0, errors.New("caller lookup not implemented on linux yet (eBPF backend planned)")
}

func cwdOfPid(pid int) (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
}
