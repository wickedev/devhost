// Package inject builds the environment that activates devhost for a project:
// the DEVHOST/HOST convention variables, the Node NODE_OPTIONS channel, and
// the universal bind() interposer via DYLD_INSERT_LIBRARIES / LD_PRELOAD.
// SIP-stripped variables (DYLD_*) are safe here because Env is applied by
// `devhost shim-exec` at the FINAL exec — past every /bin/sh and
// /usr/bin/env hop a script chain can throw at it.
package inject

import (
	_ "embed"
	"os"
	"path/filepath"
	"strings"

	"github.com/wickedev/devhost/internal/addr"
	"github.com/wickedev/devhost/internal/config"
	"github.com/wickedev/devhost/internal/interpose"
)

//go:embed assets/node-inject.cjs
var nodeInjector []byte

// ConfigDir is where devhost keeps its runtime assets and shims.
func ConfigDir() string { return config.Dir() }

// EnsureInjector writes the embedded Node injector to the config dir if it
// is missing or stale, and returns its path. Self-healing: every activation
// repairs a deleted or outdated file.
func EnsureInjector() (string, error) {
	p := filepath.Join(ConfigDir(), "node-inject.cjs")
	if cur, err := os.ReadFile(p); err == nil && string(cur) == string(nodeInjector) {
		return p, nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, nodeInjector, 0o644); err != nil {
		return "", err
	}
	return p, nil
}

// Env returns base with devhost variables applied for the given project root.
func Env(base []string, root string) []string {
	ip := addr.ForDir(root)
	env := setVar(base, "DEVHOST", ip)
	env = setVar(env, "HOST", ip)
	if p, err := EnsureInjector(); err == nil {
		env = appendNodeOptions(env, "--require "+p)
	}
	// Universal tier: the bind() interposer covers every dynamically-linked
	// runtime. Missing compiler just skips this tier; doctor surfaces it.
	if lib, err := interpose.Ensure(); err == nil {
		env = appendPathList(env, interpose.EnvVar(), lib)
	}
	return env
}

func setVar(env []string, key, val string) []string {
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			env[i] = prefix + val
			return env
		}
	}
	return append(env, prefix+val)
}

// appendPathList adds entry to a colon-separated list variable, idempotently.
func appendPathList(env []string, key, entry string) []string {
	prefix := key + "="
	for i, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			if strings.Contains(kv, entry) {
				return env
			}
			env[i] = kv + ":" + entry
			return env
		}
	}
	return append(env, prefix+entry)
}

func appendNodeOptions(env []string, opt string) []string {
	const key = "NODE_OPTIONS="
	for i, kv := range env {
		if strings.HasPrefix(kv, key) {
			if strings.Contains(kv, "node-inject.cjs") {
				return env // already injected (e.g. shim under direnv) — stay idempotent
			}
			env[i] = kv + " " + opt
			return env
		}
	}
	return append(env, key+opt)
}
