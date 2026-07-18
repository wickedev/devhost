package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PathEdit records one profile file touched (or found already configured)
// while wiring the shell environment.
type PathEdit struct {
	Profile string
	Added   bool
}

// AppendPathToProfile ensures dir is put on PATH by the user's shell
// profiles. The export line is appended at the END of each file so it lands
// after any version manager init — the shim must win the PATH race against
// asdf/mise/nvm entries that those inits prepend. zsh gets the line in BOTH
// ~/.zshenv and ~/.zshrc: non-interactive shells (agents, scripts, CI) read
// only .zshenv, while in interactive shells the .zshrc copy re-prepends past
// version managers and macOS path_helper. Returns one edit per file
// (Added=false means dir was already mentioned there). Shells other than
// zsh/bash/fish return an error so the caller can fall back to printing
// manual instructions.
func AppendPathToProfile(dir string) ([]PathEdit, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	shell := os.Getenv("SHELL")
	profiles, err := profilePaths(shell, home, runtime.GOOS)
	if err != nil {
		return nil, err
	}
	line := exportLine(dir)
	if filepath.Base(shell) == "fish" {
		line = fmt.Sprintf("fish_add_path --path %q", dir)
	}
	var edits []PathEdit
	for _, p := range profiles {
		added, err := appendOnce(p, line, dir, home)
		if err != nil {
			return edits, err
		}
		edits = append(edits, PathEdit{Profile: p, Added: added})
	}
	return edits, nil
}

// profilePaths returns the rc files that must carry devhost's environment
// for the login shell. zsh needs two: .zshrc alone only covers interactive
// shells, and a dev server launched by an agent or script (`zsh -c ...`)
// reads nothing but .zshenv. bash has no file at all for non-interactive
// shells (a documented gap); fish's config.fish covers both modes.
func profilePaths(shell, home, goos string) ([]string, error) {
	switch filepath.Base(shell) {
	case "zsh":
		return []string{
			filepath.Join(home, ".zshenv"),
			filepath.Join(home, ".zshrc"),
		}, nil
	case "bash":
		rc := ".bashrc"
		if goos == "darwin" { // macOS bash login shells read .bash_profile
			rc = ".bash_profile"
		}
		return []string{filepath.Join(home, rc)}, nil
	case "fish":
		return []string{filepath.Join(home, ".config", "fish", "config.fish")}, nil
	default:
		return nil, fmt.Errorf("don't know the profile file for shell %q", shell)
	}
}

// DockerHostProfileEntry returns a guarded DOCKER_HOST export for the login
// shell, plus a stable marker for idempotent appends. The export only fires
// when the proxy socket exists and the user hasn't set DOCKER_HOST themselves,
// so it never breaks `docker` when the devhost daemon is down (Docker just
// falls back to its default socket — graceful degradation).
func DockerHostProfileEntry() (marker, line string, err error) {
	const sock = "$HOME/.config/devhost/docker.sock"
	const url = "unix://" + sock
	marker = "devhost/docker.sock" // any spelling of the line contains this
	switch filepath.Base(os.Getenv("SHELL")) {
	case "zsh", "bash", "sh":
		line = fmt.Sprintf(`[ -z "$DOCKER_HOST" ] && [ -S "%s" ] && export DOCKER_HOST="%s"`, sock, url)
	case "fish":
		line = fmt.Sprintf(`test -z "$DOCKER_HOST"; and test -S "%s"; and set -gx DOCKER_HOST "%s"`, sock, url)
	default:
		return "", "", fmt.Errorf("don't know the profile file for shell %q", os.Getenv("SHELL"))
	}
	return marker, line, nil
}

// AppendLineToProfile appends line to each of the login shell's rc files
// unless marker already appears there. For devhost-managed entries beyond
// PATH (a guarded DOCKER_HOST export) — written to the same file set as the
// PATH line, so agent/script shells get them too. Returns one edit per file.
func AppendLineToProfile(marker, line string) ([]PathEdit, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	profiles, err := profilePaths(os.Getenv("SHELL"), home, runtime.GOOS)
	if err != nil {
		return nil, err
	}
	var edits []PathEdit
	for _, p := range profiles {
		added, err := appendMarkedOnce(p, marker, line)
		if err != nil {
			return edits, err
		}
		edits = append(edits, PathEdit{Profile: p, Added: added})
	}
	return edits, nil
}

// appendMarkedOnce appends line unless the file already contains marker.
func appendMarkedOnce(path, marker, line string) (added bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if strings.Contains(string(data), marker) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	prefix := ""
	if n := len(data); n > 0 {
		if data[n-1] != '\n' {
			prefix = "\n"
		}
		prefix += "\n"
	}
	_, err = fmt.Fprintf(f, "%s# added by devhost setup\n%s\n", prefix, line)
	return err == nil, err
}

func exportLine(dir string) string {
	return fmt.Sprintf(`export PATH="%s:$PATH"`, dir)
}

// appendOnce appends line to the file unless the file already mentions dir
// in any spelling — absolute, $HOME-relative, or ~-relative — so repeated
// `devhost setup` runs and hand-written entries both count as present.
func appendOnce(path, line, dir, home string) (added bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, spelling := range spellings(dir, home) {
		if strings.Contains(string(data), spelling) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	prefix := ""
	if n := len(data); n > 0 {
		if data[n-1] != '\n' {
			prefix = "\n"
		}
		prefix += "\n"
	}
	_, err = fmt.Fprintf(f, "%s# added by devhost setup — keep after any version-manager init\n%s\n", prefix, line)
	return err == nil, err
}

// spellings lists the ways a profile may already reference dir.
func spellings(dir, home string) []string {
	out := []string{dir}
	if rel, ok := strings.CutPrefix(dir, home+string(filepath.Separator)); ok {
		out = append(out, "$HOME/"+rel, "${HOME}/"+rel, "~/"+rel)
	}
	return out
}
