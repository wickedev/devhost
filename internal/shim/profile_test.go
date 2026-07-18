package shim

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProfileLine(t *testing.T) {
	cases := []struct {
		shell, goos     string
		wantRC, wantSub string
	}{
		{"/bin/zsh", "darwin", ".zshrc", `export PATH="/x/shims:$PATH"`},
		{"/bin/bash", "darwin", ".bash_profile", `export PATH="/x/shims:$PATH"`},
		{"/usr/bin/bash", "linux", ".bashrc", `export PATH="/x/shims:$PATH"`},
		{"/opt/homebrew/bin/fish", "darwin", "config.fish", `fish_add_path --path "/x/shims"`},
	}
	for _, c := range cases {
		profile, line, err := profileLine(c.shell, "/home/u", c.goos, "/x/shims")
		if err != nil {
			t.Fatalf("%s: %v", c.shell, err)
		}
		if filepath.Base(profile) != c.wantRC {
			t.Errorf("%s: profile = %s, want base %s", c.shell, profile, c.wantRC)
		}
		if line != c.wantSub {
			t.Errorf("%s: line = %q, want %q", c.shell, line, c.wantSub)
		}
	}
	if _, _, err := profileLine("/bin/tcsh", "/home/u", "linux", "/x"); err == nil {
		t.Error("tcsh: want error for unsupported shell")
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

	profile, added, err := AppendLineToProfile(marker, line)
	if err != nil || !added {
		t.Fatalf("first append: added=%v err=%v", added, err)
	}
	if filepath.Base(profile) != ".zshrc" {
		t.Fatalf("profile = %s, want .zshrc", profile)
	}
	if data, _ := os.ReadFile(profile); !strings.Contains(string(data), line) {
		t.Fatalf("line missing:\n%s", data)
	}

	// Idempotent: the marker is already present, so no second line.
	_, added, err = AppendLineToProfile(marker, line)
	if err != nil || added {
		t.Fatalf("second append: added=%v err=%v", added, err)
	}
}
