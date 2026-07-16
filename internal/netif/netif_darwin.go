//go:build darwin

// Package netif manages the loopback interface aliases that back devhost's
// per-project IPs.
package netif

import (
	"fmt"
	"os/exec"
	"strings"
)

// helper is the root-owned privileged helper installed by `devhost setup
// --helper`. It validates its argument against the devhost range and runs
// ifconfig, so a narrow NOPASSWD sudoers rule for it is safe.
const helper = "/usr/local/libexec/devhost-alias"

// HasAlias reports whether lo0 already carries ip.
func HasAlias(ip string) bool {
	out, err := exec.Command("/sbin/ifconfig", "lo0").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "inet "+ip+" ")
}

// EnsureAlias registers ip on lo0. macOS only routes 127.0.0.1 by default,
// so each project IP must be added as an interface alias. Tries the
// privileged helper first, then plain passwordless sudo. Aliases don't
// survive reboot; the next activation recreates them.
func EnsureAlias(ip string) error {
	if HasAlias(ip) {
		return nil
	}
	attempts := [][]string{
		{"sudo", "-n", helper, ip},
		{"sudo", "-n", "/sbin/ifconfig", "lo0", "alias", ip, "up"},
	}
	for _, a := range attempts {
		if exec.Command(a[0], a[1:]...).Run() == nil && HasAlias(ip) {
			return nil
		}
	}
	return fmt.Errorf(
		"could not add lo0 alias %s — install the privileged helper (devhost setup --helper) or run: sudo ifconfig lo0 alias %s up",
		ip, ip)
}
