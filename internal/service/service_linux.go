package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const unitName = "devhostd.service"

func unitPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "systemd", "user", unitName), nil
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName), nil
}

// Installed reports whether the systemd user unit is present.
func Installed() bool {
	p, err := unitPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// Install writes the systemd user unit and enables it now and at login.
// Overwrites an existing unit — that's how it follows the binary when it
// moves.
func Install() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not found — run `devhost daemon` under your init system by hand")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	p, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(systemdUnit(exe)), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", unitName},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("systemctl %v: %v: %s", args, err, out)
		}
	}
	return nil
}

// Remove stops the unit and deletes its file.
func Remove() error {
	p, err := unitPath()
	if err != nil {
		return err
	}
	exec.Command("systemctl", "--user", "disable", "--now", unitName).Run() //nolint:errcheck
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	exec.Command("systemctl", "--user", "daemon-reload").Run() //nolint:errcheck
	return nil
}

// Kind describes the service manager for user-facing messages.
func Kind() string { return "systemd user unit " + unitName }
