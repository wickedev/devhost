// Package selfupdate implements `devhost upgrade`: replace the running
// binary with the latest GitHub release. Deliberately explicit — devhost
// never updates itself in the background; behavior only changes when the
// user asks it to.
package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	repo    = "wickedev/devhost"
	apiURL  = "https://api.github.com/repos/" + repo + "/releases/latest"
	maxSize = 64 << 20 // sanity cap for downloads
)

var client = &http.Client{Timeout: 60 * time.Second}

// Latest returns the newest released version ("0.2.0"), using a short
// timeout so callers like `devhost doctor` stay snappy offline.
func Latest() (string, error) {
	quick := &http.Client{Timeout: 4 * time.Second}
	tag, err := latestTagWith(quick)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(tag, "v"), nil
}

// Upgrade replaces the current executable with the latest release build.
// current is the running version ("dev" for source builds).
func Upgrade(current string, out io.Writer) error {
	tag, err := latestTag()
	if err != nil {
		return fmt.Errorf("could not determine the latest release: %w", err)
	}
	latest := strings.TrimPrefix(tag, "v")

	if current == latest {
		fmt.Fprintf(out, "devhost %s is already the latest release\n", current)
		return nil
	}
	if current == "dev" {
		fmt.Fprintf(out, "running a source build; installing latest release %s\n", latest)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return err
	}
	// A Homebrew cellar binary must be updated through brew, or the two
	// package states diverge.
	if strings.Contains(exe, "/Cellar/") {
		return fmt.Errorf("%s is managed by Homebrew — run: brew upgrade devhost", exe)
	}

	asset := fmt.Sprintf("devhost_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	base := "https://github.com/" + repo + "/releases/download/" + tag + "/"

	fmt.Fprintf(out, "downloading %s (%s)\n", asset, tag)
	archive, err := fetch(base + asset)
	if err != nil {
		return err
	}
	if err := verifyChecksum(base+"checksums.txt", asset, archive); err != nil {
		return err
	}
	bin, err := extractBinary(archive)
	if err != nil {
		return err
	}

	// Write next to the target so the final rename is atomic (same fs).
	tmp := filepath.Join(filepath.Dir(exe), fmt.Sprintf(".devhost-upgrade-%d", os.Getpid()))
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("could not replace %s: %w", exe, err)
	}
	fmt.Fprintf(out, "upgraded %s → %s (%s)\n", current, latest, exe)
	if current != "dev" {
		fmt.Fprintln(out, "note: a running `devhost daemon` keeps the old version until restarted")
	}
	return nil
}

func latestTag() (string, error) { return latestTagWith(client) }

func latestTagWith(c *http.Client) (string, error) {
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("User-Agent", "devhost-upgrade")
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", res.Status)
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&r); err != nil {
		return "", err
	}
	if r.TagName == "" {
		return "", errors.New("release has no tag name")
	}
	return r.TagName, nil
}

func fetch(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "devhost-upgrade")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxSize))
}

func verifyChecksum(sumsURL, asset string, data []byte) error {
	sums, err := fetch(sumsURL)
	if err != nil {
		return fmt.Errorf("could not fetch checksums: %w", err)
	}
	got := hex.EncodeToString(func() []byte { s := sha256.Sum256(data); return s[:] }())
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == asset {
			if f[0] != got {
				return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", asset, f[0], got)
			}
			return nil
		}
	}
	return fmt.Errorf("no checksum entry for %s", asset)
}

func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == "devhost" && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, maxSize))
		}
	}
	return nil, errors.New("devhost binary not found in the release archive")
}
