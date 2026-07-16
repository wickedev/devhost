// Package interpose builds and locates the bind() interposer library — the
// universal transparent-rebinding tier. The C source is embedded and compiled
// on the user's machine with the system compiler (clang ships with Xcode CLT,
// gcc with build-essential); the result is cached in the config dir and
// rebuilt automatically whenever the embedded source changes. Missing
// compiler degrades this one tier — Node injection and the rest of devhost
// keep working.
package interpose

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/wickedev/devhost/internal/config"
)

//go:embed assets/bind_interpose.c
var source []byte

// EnvVar is the loader variable that injects the library on this platform.
func EnvVar() string {
	if runtime.GOOS == "darwin" {
		return "DYLD_INSERT_LIBRARIES"
	}
	return "LD_PRELOAD"
}

// LibPath returns where the compiled library lives (it may not exist yet).
func LibPath() string {
	ext := ".so"
	if runtime.GOOS == "darwin" {
		ext = ".dylib"
	}
	return filepath.Join(config.Dir(), "libdevhost-bind"+ext)
}

// Ensure compiles the interposer if it is missing or its source changed,
// returning the library path.
func Ensure() (string, error) {
	dir := config.Dir()
	srcPath := filepath.Join(dir, "bind_interpose.c")
	libPath := LibPath()

	cur, _ := os.ReadFile(srcPath)
	if bytes.Equal(cur, source) {
		if _, err := os.Stat(libPath); err == nil {
			return libPath, nil
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(srcPath, source, 0o644); err != nil {
		return "", err
	}

	cc, err := findCompiler()
	if err != nil {
		return "", err
	}
	args := []string{"-O2", "-Wall", "-o", libPath, srcPath}
	if runtime.GOOS == "darwin" {
		args = append([]string{"-dynamiclib"}, args...)
	} else {
		args = append([]string{"-shared", "-fPIC"}, args...)
		args = append(args, "-ldl")
	}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		os.Remove(libPath)
		return "", fmt.Errorf("compiling interposer with %s: %v\n%s", cc, err, out)
	}
	return libPath, nil
}

func findCompiler() (string, error) {
	for _, cand := range []string{"cc", "clang", "gcc"} {
		if p, err := exec.LookPath(cand); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no C compiler found (install Xcode command line tools or build-essential)")
}
