package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfilePaths(t *testing.T) {
	cases := []struct {
		shell, goos string
		wantRCs     []string
	}{
		// zsh needs both: non-interactive shells read only .zshenv, and the
		// .zshrc copy re-prepends past version managers in interactive ones.
		{"/bin/zsh", "darwin", []string{".zshenv", ".zshrc"}},
		{"/bin/bash", "darwin", []string{".bash_profile"}},
		{"/usr/bin/bash", "linux", []string{".bashrc"}},
		{"/opt/homebrew/bin/fish", "darwin", []string{"config.fish"}},
	}
	for _, c := range cases {
		profiles, err := profilePaths(c.shell, "/home/u", c.goos)
		if err != nil {
			t.Fatalf("%s: %v", c.shell, err)
		}
		if len(profiles) != len(c.wantRCs) {
			t.Fatalf("%s: profiles = %v, want %v", c.shell, profiles, c.wantRCs)
		}
		for i, p := range profiles {
			if filepath.Base(p) != c.wantRCs[i] {
				t.Errorf("%s: profile[%d] = %s, want base %s", c.shell, i, p, c.wantRCs[i])
			}
		}
	}
	if _, err := profilePaths("/bin/tcsh", "/home/u", "linux"); err == nil {
		t.Error("tcsh: want error for unsupported shell")
	}
}

func TestAppendPathToProfileZshBothFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	dir := filepath.Join(home, ".config", "devhost", "shims")

	edits, err := AppendPathToProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 2 {
		t.Fatalf("edits = %+v, want .zshenv and .zshrc", edits)
	}
	for _, e := range edits {
		if !e.Added {
			t.Errorf("%s: not added on first run", e.Profile)
		}
		data, err := os.ReadFile(e.Profile)
		if err != nil || !strings.Contains(string(data), exportLine(dir)) {
			t.Errorf("%s: export line missing (err=%v)", e.Profile, err)
		}
	}

	// Second run is a no-op on both files.
	edits, err = AppendPathToProfile(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edits {
		if e.Added {
			t.Errorf("%s: added again on second run", e.Profile)
		}
	}
}

func TestAppendOnce(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	dir := filepath.Join(home, ".config", "devhost", "shims")
	line := exportLine(dir)

	// First append creates the file and adds the line.
	added, err := appendOnce(rc, line, dir, home)
	if err != nil || !added {
		t.Fatalf("first append: added=%v err=%v", added, err)
	}
	data, _ := os.ReadFile(rc)
	if !strings.Contains(string(data), line) {
		t.Fatalf("line missing from profile:\n%s", data)
	}

	// Second append is a no-op.
	added, err = appendOnce(rc, line, dir, home)
	if err != nil || added {
		t.Fatalf("second append: added=%v err=%v", added, err)
	}
	if again, _ := os.ReadFile(rc); string(again) != string(data) {
		t.Fatalf("profile changed on repeat append:\n%s", again)
	}

	// A hand-written $HOME-relative entry counts as already present.
	rc3 := filepath.Join(t.TempDir(), ".zshrc")
	os.WriteFile(rc3, []byte("export PATH=\"$HOME/.config/devhost/shims:$PATH\"\n"), 0o644)
	added, err = appendOnce(rc3, line, dir, home)
	if err != nil || added {
		t.Fatalf("$HOME spelling not recognized: added=%v err=%v", added, err)
	}

	// A file without a trailing newline gets one before the block.
	rc2 := filepath.Join(t.TempDir(), ".zshrc")
	os.WriteFile(rc2, []byte("eval \"$(mise activate zsh)\""), 0o644)
	if _, err := appendOnce(rc2, line, dir, home); err != nil {
		t.Fatal(err)
	}
	data2, _ := os.ReadFile(rc2)
	if !strings.Contains(string(data2), "zsh)\"\n\n# added by devhost setup") {
		t.Fatalf("bad separation:\n%q", data2)
	}
	// The shim entry must land after the version-manager init.
	if strings.Index(string(data2), "mise activate") > strings.Index(string(data2), line) {
		t.Fatal("shim PATH line appended before version-manager init")
	}
}

func TestDockerHostProfileEntry(t *testing.T) {
	cases := []struct{ shell, wantSub string }{
		{"/bin/zsh", `[ -z "$DOCKER_HOST" ] && [ -S "$HOME/.config/devhost/docker.sock" ] && export DOCKER_HOST="unix://$HOME/.config/devhost/docker.sock"`},
		{"/usr/bin/bash", `[ -z "$DOCKER_HOST" ] && [ -S "$HOME/.config/devhost/docker.sock" ] && export DOCKER_HOST="unix://$HOME/.config/devhost/docker.sock"`},
		{"/opt/homebrew/bin/fish", `test -z "$DOCKER_HOST"; and test -S "$HOME/.config/devhost/docker.sock"; and set -gx DOCKER_HOST "unix://$HOME/.config/devhost/docker.sock"`},
	}
	for _, c := range cases {
		t.Setenv("SHELL", c.shell)
		marker, line, err := DockerHostProfileEntry()
		if err != nil {
			t.Fatalf("%s: %v", c.shell, err)
		}
		if line != c.wantSub {
			t.Errorf("%s: line = %q, want %q", c.shell, line, c.wantSub)
		}
		if !strings.Contains(line, marker) {
			t.Errorf("%s: marker %q not in line %q", c.shell, marker, line)
		}
	}
	t.Setenv("SHELL", "/bin/tcsh")
	if _, _, err := DockerHostProfileEntry(); err == nil {
		t.Error("tcsh: want error for unsupported shell")
	}
}

func TestAppendLineToProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	marker, line, err := DockerHostProfileEntry()
	if err != nil {
		t.Fatal(err)
	}

	edits, err := AppendLineToProfile(marker, line)
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 2 || filepath.Base(edits[0].Profile) != ".zshenv" || filepath.Base(edits[1].Profile) != ".zshrc" {
		t.Fatalf("edits = %+v, want .zshenv then .zshrc", edits)
	}
	for _, e := range edits {
		if !e.Added {
			t.Errorf("%s: not added on first run", e.Profile)
		}
		if data, _ := os.ReadFile(e.Profile); !strings.Contains(string(data), line) {
			t.Errorf("%s: line missing:\n%s", e.Profile, data)
		}
	}

	// Idempotent: the marker is already present, so no second line.
	edits, err = AppendLineToProfile(marker, line)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range edits {
		if e.Added {
			t.Errorf("%s: added again on second run", e.Profile)
		}
	}
}
