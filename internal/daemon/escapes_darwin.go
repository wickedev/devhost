//go:build darwin

package daemon

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/wickedev/devhost/internal/project"
)

// escapedHosts are listener addresses that should have been rewritten to the
// project IP: plain v4/v6 loopback and the wildcards.
var escapedHosts = map[string]bool{
	"127.0.0.1": true, "0.0.0.0": true, "*": true, "[::]": true, "[::1]": true,
}

// EscapedListeners scans TCP listeners for processes whose working directory
// lies inside a .devhost project but whose socket is not on the project IP.
func EscapedListeners() []Escape {
	out, err := exec.Command(lsof, "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcn").Output()
	if err != nil && len(out) == 0 {
		return nil
	}
	var (
		escapes []Escape
		pid     int
		cmd     string
		cwds    = map[int]string{} // pid -> cwd ("" = lookup failed)
		seen    = map[[2]int]bool{}
	)
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
			cmd = ""
		case 'c':
			cmd = line[1:]
		case 'n':
			i := strings.LastIndex(line, ":")
			if i < 1 || !escapedHosts[line[1:i]] {
				continue
			}
			port, err := strconv.Atoi(line[i+1:])
			if err != nil || pid == 0 || seen[[2]int{pid, port}] {
				continue
			}
			cwd, ok := cwds[pid]
			if !ok {
				cwd, _ = cwdOfPid(pid)
				cwds[pid] = cwd
			}
			if cwd == "" {
				continue
			}
			root := project.FindRoot(cwd)
			if root == "" {
				continue
			}
			seen[[2]int{pid, port}] = true
			escapes = append(escapes, Escape{PID: pid, Command: cmd, Port: port, Root: root})
		}
	}
	return escapes
}
