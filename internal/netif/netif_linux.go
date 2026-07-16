//go:build linux

// Package netif manages the loopback interface aliases that back devhost's
// per-project IPs.
package netif

// Linux routes the entire 127.0.0.0/8 block on the lo device out of the box,
// so no alias registration (and no privilege) is needed.

// HasAlias reports whether ip is reachable on loopback. Always true on Linux.
func HasAlias(string) bool { return true }

// EnsureAlias is a no-op on Linux.
func EnsureAlias(string) error { return nil }
