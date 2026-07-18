// Package skills keeps the devhost agent skill current alongside the binary.
//
// The skill is distributed through skills.sh (`npx skills add wickedev/devhost`),
// which installs it into every configured agent (Claude Code, Cursor, Copilot,
// …) — not something devhost should reimplement or fork into its binary. So
// devhost delegates install/refresh to that CLI, and only *detects* drift
// itself, so `devhost setup`/`devhost upgrade` can refresh the skill and
// `devhost doctor` can flag a stale one.
package skills

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Pkg is the skills.sh package identifier for the devhost skill.
const Pkg = "wickedev/devhost"

// Name is the installed skill's local name (as `skills update <name>` sees it).
const Name = "devhost"

// canonicalURL is the skill as published on the repo's default branch — what a
// fresh `skills add` installs, and the reference for drift detection.
const canonicalURL = "https://raw.githubusercontent.com/wickedev/devhost/main/skills/devhost/SKILL.md"

// Available reports whether the skills CLI can be run (npx on PATH).
func Available() bool {
	_, err := exec.LookPath("npx")
	return err == nil
}

// Refresh installs or updates the devhost skill via skills.sh, writing progress
// to out. Best-effort: returns an error the caller can downgrade to a warning.
func Refresh(out io.Writer) error {
	if !Available() {
		return fmt.Errorf("npx not found — install Node, then: npx skills add %s", Pkg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// -g: user-level (devhost is a machine tool, not a per-project skill);
	// -y: non-interactive, safe under `curl | sh` and CI.
	cmd := exec.CommandContext(ctx, "npx", "-y", "skills", "add", Pkg, "-g", "-y")
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// ClaudePath is where the Claude Code copy of the skill lives.
func ClaudePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "skills", "devhost", "SKILL.md")
}

// Installed reports whether the skill is present for Claude Code.
func Installed() bool {
	_, err := os.Stat(ClaudePath())
	return err == nil
}

// Outdated reports whether the installed skill differs from the canonical
// published version. Best-effort and network-dependent: any lookup failure
// returns (false, err) so callers treat "unknown" as "don't nag".
func Outdated() (bool, error) {
	local, err := os.ReadFile(ClaudePath())
	if err != nil {
		return false, err
	}
	c := &http.Client{Timeout: 4 * time.Second}
	req, _ := http.NewRequest("GET", canonicalURL, nil)
	req.Header.Set("User-Agent", "devhost-doctor")
	res, err := c.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false, fmt.Errorf("fetch canonical skill: %s", res.Status)
	}
	remote, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return false, err
	}
	return !bytes.Equal(norm(local), norm(remote)), nil
}

// norm collapses CRLF and trailing-newline differences so cosmetic EOL variance
// between the skills store and the source doesn't read as drift.
func norm(b []byte) []byte {
	return []byte(strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") + "\n")
}
