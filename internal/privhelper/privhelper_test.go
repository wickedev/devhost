package privhelper

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// The helper's whole job is refusing bad input before touching anything
// privileged — refusal paths run fine as an unprivileged user.
func TestHelperValidation(t *testing.T) {
	sh := filepath.Join(t.TempDir(), "devhost-helper")
	if err := os.WriteFile(sh, script, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		args []string
		exit int
	}{
		{"no args", nil, 64},
		{"unknown verb", []string{"pwn"}, 64},
		{"alias wrong ip range", []string{"alias", "10.0.0.1"}, 65},
		{"alias not an ip", []string{"alias", "127.77.1.1; rm -rf /"}, 65},
		{"alias octet overflow", []string{"alias", "127.77.256.1"}, 65},
		{"alias network address", []string{"alias", "127.77.1.0"}, 65},
		{"hosts bad name", []string{"hosts", "127.77.1.2", "UPPER_case", "/tmp"}, 65},
		{"hosts name injection", []string{"hosts", "127.77.1.2", "a b", "/tmp"}, 65},
		{"hosts relative root", []string{"hosts", "127.77.1.2", "app", "tmp"}, 65},
		{"hosts root newline", []string{"hosts", "127.77.1.2", "app", "/tmp\nevil"}, 65},
		{"hosts root hash", []string{"hosts", "127.77.1.2", "app", "/tmp#x"}, 65},
		{"hosts missing args", []string{"hosts", "127.77.1.2"}, 64},
	}
	for _, c := range cases {
		err := exec.Command("/bin/sh", append([]string{sh}, c.args...)...).Run()
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Errorf("%s: expected refusal exit %d, got err=%v", c.name, c.exit, err)
			continue
		}
		if ee.ExitCode() != c.exit {
			t.Errorf("%s: exit = %d, want %d", c.name, ee.ExitCode(), c.exit)
		}
	}

	if runtime.GOOS == "linux" {
		// valid alias on linux is a privileged-op-free no-op — must succeed
		if err := exec.Command("/bin/sh", sh, "alias", "127.77.1.2").Run(); err != nil {
			t.Errorf("linux alias no-op failed: %v", err)
		}
	}
}
