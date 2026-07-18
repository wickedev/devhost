package shim

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestCustomToolsRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // config.Dir derives from the home dir

	if got := CustomTools(); len(got) != 0 {
		t.Fatalf("fresh home: CustomTools = %v, want empty", got)
	}

	added, err := AddCustom("cargo-nightly")
	if err != nil || !added {
		t.Fatalf("AddCustom: added=%v err=%v", added, err)
	}
	// Re-adding (or adding a default) is a no-op.
	if added, _ := AddCustom("cargo-nightly"); added {
		t.Error("second AddCustom reported added=true")
	}
	if added, _ := AddCustom("node"); added {
		t.Error("AddCustom of a default tool reported added=true")
	}

	if got := CustomTools(); !slices.Equal(got, []string{"cargo-nightly"}) {
		t.Fatalf("CustomTools = %v, want [cargo-nightly]", got)
	}
	all := AllTools()
	if !slices.Contains(all, "cargo-nightly") || !slices.Contains(all, "node") {
		t.Fatalf("AllTools = %v, want defaults + custom", all)
	}

	removed, err := RemoveCustom("cargo-nightly")
	if err != nil || !removed {
		t.Fatalf("RemoveCustom: removed=%v err=%v", removed, err)
	}
	if removed, _ := RemoveCustom("cargo-nightly"); removed {
		t.Error("second RemoveCustom reported removed=true")
	}
	if got := CustomTools(); len(got) != 0 {
		t.Fatalf("after remove: CustomTools = %v, want empty", got)
	}
}

func TestRemoveCustomDeletesShim(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// A tool the machine "has": drop a fake binary on PATH.
	bin := t.TempDir()
	fake := filepath.Join(bin, "mylauncher")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, err := AddCustom("mylauncher"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Install(AllTools()); err != nil {
		t.Fatal(err)
	}
	shimPath := filepath.Join(Dir(), "mylauncher")
	if _, err := os.Stat(shimPath); err != nil {
		t.Fatalf("shim not installed: %v", err)
	}

	if removed, err := RemoveCustom("mylauncher"); err != nil || !removed {
		t.Fatalf("RemoveCustom: removed=%v err=%v", removed, err)
	}
	if _, err := os.Stat(shimPath); !os.IsNotExist(err) {
		t.Fatalf("shim still present after RemoveCustom (err=%v)", err)
	}
}
