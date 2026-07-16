//go:build !linux

package interpose

import "errors"

// The /etc/ld.so.preload mechanism is Linux-only.

// PreloadInstalled is always false off Linux.
func PreloadInstalled() bool { return false }

// InstallPreload is unsupported off Linux.
func InstallPreload() error {
	return errors.New("--preload is linux-only (macOS uses the shim + DYLD_INSERT_LIBRARIES)")
}

// PreloadRemove is unsupported off Linux.
func PreloadRemove() error { return errors.New("--preload is linux-only") }
