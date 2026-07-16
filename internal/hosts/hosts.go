// Package hosts manages devhost's "<name>.test -> project IP" entries in
// /etc/hosts. Each managed line carries a "# devhost:<root>" tag so entries
// can be updated or removed per project without touching anything else.
//
// Roadmap: replace file mutation with a built-in DNS responder plus an
// /etc/resolver/test stub so /etc/hosts is never written at all.
package hosts

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

const (
	file = "/etc/hosts"
	tag  = "# devhost:"
)

func entryFor(ip, name, root string) string {
	return fmt.Sprintf("%s %s.test %s%s", ip, name, tag, root)
}

// Has reports whether the exact managed entry for (ip, name) is present.
func Has(ip, name string) bool {
	b, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	prefix := ip + " " + name + ".test "
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, prefix) && strings.Contains(line, tag) {
			return true
		}
	}
	return false
}

// Ensure registers "<name>.test -> ip" for root, replacing any stale managed
// entry for the same root. /etc/hosts is root-owned, so the rewrite goes
// through `sudo -n tee`; callers should treat failure as a degraded feature
// (direct IP access still works), not a fatal error.
func Ensure(ip, name, root string) error {
	want := entryFor(ip, name, root)
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	kept := make([]string, 0, len(lines)+1)
	found, changed := false, false
	for _, l := range lines {
		if strings.HasSuffix(l, tag+root) {
			if l == want {
				found = true
				kept = append(kept, l)
			} else {
				changed = true // stale entry for this root — drop it
			}
			continue
		}
		kept = append(kept, l)
	}
	if !found {
		kept = append(kept, want)
		changed = true
	}
	if !changed {
		return nil
	}
	cmd := exec.Command("sudo", "-n", "tee", file)
	cmd.Stdin = strings.NewReader(strings.Join(kept, "\n") + "\n")
	cmd.Stdout = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not update %s (needs passwordless sudo or the privileged helper): %w", file, err)
	}
	return nil
}

// Names returns ip -> hostname label for every devhost-managed entry.
func Names() map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(file)
	if err != nil {
		return m
	}
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.Contains(l, tag) {
			continue
		}
		f := strings.Fields(l)
		if len(f) >= 2 {
			m[f[0]] = strings.TrimSuffix(f[1], ".test")
		}
	}
	return m
}
