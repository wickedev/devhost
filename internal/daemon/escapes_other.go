//go:build !darwin

package daemon

// EscapedListeners needs per-PID cwd resolution for arbitrary listeners,
// which on Linux would take an inode scan across /proc/*/fd; the eBPF tier
// already covers native binaries there, so this check is macOS-only for now.
func EscapedListeners() []Escape { return nil }
