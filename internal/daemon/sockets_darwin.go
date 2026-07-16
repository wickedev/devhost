//go:build darwin

package daemon

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// lsof-based socket introspection. ~50-100ms per lookup, which only affects
// connection setup on the localhost mirror path (keep-alive amortizes it).
// Roadmap: replace with libproc (proc_pidinfo / proc_pidfdinfo) for sub-ms
// lookups.

const lsof = "/usr/sbin/lsof"

var listenerRe = regexp.MustCompile(`^n(127\.77\.\d+\.\d+):(\d+)$`)

// DevhostListeners returns port -> set of devhost-range IPs currently
// listening on TCP.
func DevhostListeners() (map[int]map[string]bool, error) {
	out, err := exec.Command(lsof, "-nP", "-iTCP", "-sTCP:LISTEN", "-Fn").Output()
	if err != nil && len(out) == 0 {
		// lsof exits non-zero when nothing matches — that's an empty result
		return map[int]map[string]bool{}, nil
	}
	res := map[int]map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		m := listenerRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		port, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if res[port] == nil {
			res[port] = map[string]bool{}
		}
		res[port][m[1]] = true
	}
	return res, nil
}

// pidBySrcPort finds the PID owning the client end of a connection from
// 127.0.0.1:srcPort to 127.0.0.1:dstPort.
func pidBySrcPort(srcPort, dstPort int) (int, error) {
	out, err := exec.Command(lsof, "-nP", "-iTCP:"+strconv.Itoa(srcPort), "-Fpn").Output()
	if err != nil && len(out) == 0 {
		return 0, fmt.Errorf("lsof: %w", err)
	}
	needle := fmt.Sprintf(":%d->127.0.0.1:%d", srcPort, dstPort)
	pid := 0
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && strings.Contains(line, needle) && pid != 0:
			return pid, nil
		}
	}
	return 0, errors.New("caller pid not found")
}

// cwdOfPid returns the working directory of a process.
func cwdOfPid(pid int) (string, error) {
	out, err := exec.Command(lsof, "-a", "-p", strconv.Itoa(pid), "-d", "cwd", "-Fn").Output()
	if err != nil && len(out) == 0 {
		return "", fmt.Errorf("lsof: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return line[1:], nil
		}
	}
	return "", errors.New("cwd not found")
}
