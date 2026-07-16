//go:build !linux

// Package ebpf is a no-op outside Linux; the kernel primitive it needs
// (CGROUP_INET4_BIND) exists only there. macOS uses the DYLD interposer tier.
package ebpf

// Available reports whether the eBPF backend can run. Never on non-Linux.
func Available() bool { return false }

// Activate is unavailable off Linux.
func Activate(string) (string, error) { return "", errUnsupported }

// JoinCgroup is unavailable off Linux.
func JoinCgroup(string, int) error { return errUnsupported }

type unsupported struct{}

func (unsupported) Error() string { return "ebpf backend is linux-only" }

var errUnsupported = unsupported{}
