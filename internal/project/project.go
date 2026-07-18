// Package project locates devhost project roots. A project is any directory
// tree opted in with a `.devhost` marker file — a committable file, so git
// worktrees inherit devhost behavior automatically. Detection is purely by
// existence; the content is a comment for humans plus optional `shim:`
// declarations, so a repo can carry its own launcher list.
package project

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Marker is the file that opts a directory tree into devhost.
const Marker = ".devhost"

// Contents is what `devhost init` writes into the marker: a pointer for
// anyone who stumbles on the file in a repo. Detection ignores it — an
// empty or hand-edited marker works the same.
const Contents = `# This tree is port-virtualized by devhost: it gets its own loopback IP, so
# every project and git worktree can bind the same dev port at once.
# https://wickedev.github.io/devhost
#
# Optional: declare extra launchers to shim for this project's dev servers
# (or run ` + "`devhost shim add TOOL`" + ` here), e.g.:
# shim: cargo
`

// ShimTools returns launchers declared in root's marker with `shim: TOOL
// [TOOL...]` lines. Comments and anything else are ignored, so the marker
// stays freeform.
func ShimTools(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, Marker))
	if err != nil {
		return nil
	}
	var tools []string
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "shim:")
		if !ok {
			continue
		}
		for _, t := range strings.Fields(rest) {
			if !slices.Contains(tools, t) {
				tools = append(tools, t)
			}
		}
	}
	return tools
}

// AddShimTool declares tool in root's marker. Returns false if already
// declared.
func AddShimTool(root, tool string) (bool, error) {
	if slices.Contains(ShimTools(root), tool) {
		return false, nil
	}
	path := filepath.Join(root, Marker)
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	s := string(data)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return true, os.WriteFile(path, []byte(s+"shim: "+tool+"\n"), 0o644)
}

// RemoveShimTool drops tool from root's marker `shim:` lines, preserving
// everything else. Returns false if it wasn't declared.
func RemoveShimTool(root, tool string) (bool, error) {
	path := filepath.Join(root, Marker)
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	changed := false
	lines := strings.Split(string(data), "\n")
	out := lines[:0:0]
	for _, line := range lines {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "shim:"); ok {
			tools := strings.Fields(rest)
			kept := slices.DeleteFunc(slices.Clone(tools), func(t string) bool { return t == tool })
			if len(kept) != len(tools) {
				changed = true
				if len(kept) == 0 {
					continue // the line declared only this tool — drop it
				}
				line = "shim: " + strings.Join(kept, " ")
			}
		}
		out = append(out, line)
	}
	if !changed {
		return false, nil
	}
	return true, os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}

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
