package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

// Installed reports whether the launch agent file is present.
func Installed() bool {
	p, err := plistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Install writes the launch agent and (re)loads it, so the daemon is
// running now and at every login. Overwrites an existing agent — that's
// how the plist follows the binary when it moves.
func Install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p, err := plistPath()
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	logPath := filepath.Join(home, "Library", "Logs", "devhostd.log")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(launchdPlist(exe, logPath)), 0o644); err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	// bootout first so bootstrap re-reads a changed plist; fails harmlessly
	// when the agent wasn't loaded.
	exec.Command("launchctl", "bootout", domain+"/"+Label).Run() //nolint:errcheck
	if out, err := exec.Command("launchctl", "bootstrap", domain, p).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	return nil
}

// Remove unloads the launch agent and deletes its plist.
func Remove() error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	exec.Command("launchctl", "bootout", domain+"/"+Label).Run() //nolint:errcheck
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Kind describes the service manager for user-facing messages.
func Kind() string { return "launchd agent " + Label }
