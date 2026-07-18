package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// AppendPathToProfile ensures dir is put on PATH by the user's shell
// profile. The export line is appended at the END of the file so it lands
// after any version manager init — the shim must win the PATH race against
// asdf/mise/nvm entries that those inits prepend. Returns the profile path
// and whether a line was actually added (false means dir was already
// mentioned there). Shells other than zsh/bash/fish return an error so the
// caller can fall back to printing manual instructions.
func AppendPathToProfile(dir string) (profile string, added bool, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	profile, line, err := profileLine(os.Getenv("SHELL"), home, runtime.GOOS, dir)
	if err != nil {
		return "", false, err
	}
	added, err = appendOnce(profile, line, dir, home)
	return profile, added, err
}

// profileLine picks the rc file and PATH syntax for the login shell.
func profileLine(shell, home, goos, dir string) (profile, line string, err error) {
	profile, err = profilePath(shell, home, goos)
	if err != nil {
		return "", "", err
	}
	if filepath.Base(shell) == "fish" {
		return profile, fmt.Sprintf("fish_add_path --path %q", dir), nil
	}
	return profile, exportLine(dir), nil
}

// profilePath returns the login shell's rc file (zsh/bash/fish), or an error
// for shells devhost doesn't know how to edit.
func profilePath(shell, home, goos string) (string, error) {
	switch filepath.Base(shell) {
	case "zsh":
		return filepath.Join(home, ".zshrc"), nil
	case "bash":
		rc := ".bashrc"
		if goos == "darwin" { // macOS bash login shells read .bash_profile
			rc = ".bash_profile"
		}
		return filepath.Join(home, rc), nil
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish"), nil
	default:
		return "", fmt.Errorf("don't know the profile file for shell %q", shell)
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

// AppendLineToProfile appends line to the login shell's rc file unless marker
// already appears there. For devhost-managed entries beyond PATH (a guarded
// DOCKER_HOST export). Returns the profile path and whether a line was added.
func AppendLineToProfile(marker, line string) (profile string, added bool, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	profile, err = profilePath(os.Getenv("SHELL"), home, runtime.GOOS)
	if err != nil {
		return "", false, err
	}
	added, err = appendMarkedOnce(profile, marker, line)
	return profile, added, err
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
