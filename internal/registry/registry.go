// Package registry persists the name -> IP mapping the DNS responder serves.
// The interposer derives IPs one-way from paths, so "<name>.devhost" can't be
// resolved by computation alone; each activation records its mapping here.
// This is the source of truth that replaces per-project /etc/hosts lines.
package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"

	"github.com/wickedev/devhost/internal/config"
)

// Entry is one project's DNS mapping.
type Entry struct {
	Name string `json:"name"` // hostname label, e.g. "storefront"
	IP   string `json:"ip"`   // 127.77.x.y
	Root string `json:"root"` // project root that produced both
}

func path() string { return filepath.Join(config.Dir(), "registry.json") }

// Add records (name, ip, root), replacing any prior entry for the same root
// and any stale entry that collides on name (last activation wins, matching
// the old /etc/hosts behavior). Concurrency-safe across processes via flock.
func Add(name, ip, root string) error {
	if err := os.MkdirAll(config.Dir(), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(path()+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)

	entries := load()
	kept := entries[:0]
	for _, e := range entries {
		if e.Root == root || e.Name == name {
			continue
		}
		kept = append(kept, e)
	}
	kept = append(kept, Entry{Name: name, IP: ip, Root: root})
	return save(kept)
}

// Lookup returns the IP for a hostname label, or "" if unregistered.
func Lookup(name string) string {
	for _, e := range load() {
		if e.Name == name {
			return e.IP
		}
	}
	return ""
}

// All returns every registered entry.
func All() []Entry { return load() }

func load() []Entry {
	b, err := os.ReadFile(path())
	if err != nil {
		return nil
	}
	var entries []Entry
	if json.Unmarshal(b, &entries) != nil {
		return nil
	}
	return entries
}

func save(entries []Entry) error {
	b, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path())
}
