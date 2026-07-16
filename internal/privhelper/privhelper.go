// Package privhelper installs and locates the devhost privileged helper —
// the narrow root surface that replaces broad passwordless sudo. The helper
// is a short validating shell script (see assets/devhost-helper.sh; auditable
// in one screenful) installed root-owned, plus one sudoers.d line allowing
// exactly it and nothing else.
package privhelper

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// Path is where the helper lives; netif and hosts try it before falling back
// to plain passwordless sudo.
const Path = "/usr/local/libexec/devhost-helper"

const sudoersFile = "/etc/sudoers.d/devhost"

//go:embed assets/devhost-helper.sh
var script []byte

// Installed reports whether the helper binary is present.
func Installed() bool {
	info, err := os.Stat(Path)
	return err == nil && !info.IsDir()
}

// Install writes the helper and its sudoers rule. Requires an interactive
// sudo (the one-time password prompt this whole feature exists to confine).
func Install() error {
	tmpDir, err := os.MkdirTemp("", "devhost-helper")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	helperTmp := filepath.Join(tmpDir, "devhost-helper")
	if err := os.WriteFile(helperTmp, script, 0o755); err != nil {
		return err
	}

	u, err := user.Current()
	if err != nil {
		return err
	}
	sudoersTmp := filepath.Join(tmpDir, "sudoers")
	rule := fmt.Sprintf("%s ALL=(root) NOPASSWD: %s\n", u.Username, Path)
	if err := os.WriteFile(sudoersTmp, []byte(rule), 0o644); err != nil {
		return err
	}

	steps := [][]string{
		{"mkdir", "-p", filepath.Dir(Path)},
		{"install", "-o", "root", "-m", "0755", helperTmp, Path},
		// never install a sudoers file visudo won't accept — a broken file
		// can lock sudo up entirely
		{"visudo", "-cf", sudoersTmp},
		{"install", "-o", "root", "-m", "0440", sudoersTmp, sudoersFile},
	}
	fmt.Printf("installing %s + %s (sudo may prompt once)\n", Path, sudoersFile)
	for _, s := range steps {
		cmd := exec.Command("sudo", s...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("sudo %v: %w", s, err)
		}
	}
	fmt.Println("helper installed — devhost no longer needs broad sudo")
	fmt.Println("uninstall: sudo rm " + Path + " " + sudoersFile)
	return nil
}
