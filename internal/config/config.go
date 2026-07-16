// Package config locates devhost's per-user configuration directory.
package config

import (
	"os"
	"path/filepath"
)

// Dir returns ~/.config/devhost, where generated assets (shims, injectors,
// the compiled interposer) live.
func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "devhost")
}
