//go:build linux

package interpose

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

// ldSoPreload loads a library into every dynamically-linked process on the
// system, with no environment variable anywhere. The devhost interposer is a
// no-op outside .devhost trees, so this is safe in principle — but it is
// still a system-wide hook, hence strictly opt-in.
const ldSoPreload = "/etc/ld.so.preload"

// PreloadInstalled reports whether the interposer is registered in
// /etc/ld.so.preload.
func PreloadInstalled() bool {
	b, err := os.ReadFile(ldSoPreload)
	if err != nil {
		return false
	}
	return bytes.Contains(b, []byte(LibPath()))
}

// InstallPreload adds the interposer to /etc/ld.so.preload (requires root).
// It compiles the library first if needed. This makes rebinding env-free
// system-wide; use PreloadRemove to undo.
func InstallPreload() error {
	lib, err := Ensure()
	if err != nil {
		return fmt.Errorf("compile interposer: %w", err)
	}
	if PreloadInstalled() {
		fmt.Println("already registered in", ldSoPreload)
		return nil
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("writing %s needs root — rerun: sudo devhost setup --preload", ldSoPreload)
	}
	existing, _ := os.ReadFile(ldSoPreload)
	lines := strings.TrimRight(string(existing), "\n")
	if lines != "" {
		lines += "\n"
	}
	lines += lib + "\n"
	if err := os.WriteFile(ldSoPreload, []byte(lines), 0o644); err != nil {
		return err
	}
	fmt.Printf("registered %s in %s — rebinding is now env-free system-wide\n", lib, ldSoPreload)
	fmt.Println("undo: sudo devhost setup --preload-remove")
	return nil
}

// PreloadRemove removes the interposer line from /etc/ld.so.preload.
func PreloadRemove() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("writing %s needs root — rerun: sudo devhost setup --preload-remove", ldSoPreload)
	}
	b, err := os.ReadFile(ldSoPreload)
	if err != nil {
		return nil // nothing to remove
	}
	var kept []string
	for _, l := range strings.Split(string(b), "\n") {
		if l != "" && l != LibPath() {
			kept = append(kept, l)
		}
	}
	out := strings.Join(kept, "\n")
	if out != "" {
		out += "\n"
	}
	return os.WriteFile(ldSoPreload, []byte(out), 0o644)
}
