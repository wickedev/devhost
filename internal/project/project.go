// Package project locates devhost project roots. A project is any directory
// tree opted in with a `.devhost` marker file — an empty, committable file,
// so git worktrees inherit devhost behavior automatically.
package project

import (
	"os"
	"path/filepath"
)

// Marker is the file that opts a directory tree into devhost.
const Marker = ".devhost"

// FindRoot walks up from dir and returns the nearest ancestor (including dir
// itself) containing Marker, or "" if none exists.
func FindRoot(dir string) string {
	dir = filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(dir, Marker)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
