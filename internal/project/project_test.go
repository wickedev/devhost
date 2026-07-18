package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "apps", "web", "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Marker), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if got := FindRoot(sub); got != root {
		t.Errorf("FindRoot(subdir) = %q, want %q", got, root)
	}
	if got := FindRoot(root); got != root {
		t.Errorf("FindRoot(root) = %q, want %q", got, root)
	}

	outside := t.TempDir()
	if got := FindRoot(outside); got != "" {
		t.Errorf("FindRoot(outside) = %q, want empty", got)
	}
}

func TestShimToolsRoundTrip(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, Marker)
	if err := os.WriteFile(marker, []byte(Contents), 0o644); err != nil {
		t.Fatal(err)
	}

	// The commented example in Contents must not parse as a declaration.
	if got := ShimTools(root); len(got) != 0 {
		t.Fatalf("fresh marker: ShimTools = %v, want empty", got)
	}

	if added, err := AddShimTool(root, "cargo"); err != nil || !added {
		t.Fatalf("AddShimTool: added=%v err=%v", added, err)
	}
	if added, _ := AddShimTool(root, "cargo"); added {
		t.Error("second AddShimTool reported added=true")
	}
	if added, err := AddShimTool(root, "dotnet"); err != nil || !added {
		t.Fatalf("AddShimTool dotnet: added=%v err=%v", added, err)
	}
	got := ShimTools(root)
	want := []string{"cargo", "dotnet"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ShimTools = %v, want %v", got, want)
	}

	if removed, err := RemoveShimTool(root, "cargo"); err != nil || !removed {
		t.Fatalf("RemoveShimTool: removed=%v err=%v", removed, err)
	}
	if removed, _ := RemoveShimTool(root, "cargo"); removed {
		t.Error("second RemoveShimTool reported removed=true")
	}
	if got := ShimTools(root); len(got) != 1 || got[0] != "dotnet" {
		t.Fatalf("after remove: ShimTools = %v, want [dotnet]", got)
	}
	// The human comment survives edits.
	data, _ := os.ReadFile(marker)
	if !strings.Contains(string(data), "wickedev.github.io/devhost") {
		t.Fatalf("comment lost after edits:\n%s", data)
	}
}

func TestShimToolsMultiPerLine(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, Marker)
	if err := os.WriteFile(marker, []byte("shim: cargo dotnet\nshim: go\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ShimTools(root); len(got) != 3 {
		t.Fatalf("ShimTools = %v, want 3 tools", got)
	}
	// Removing one tool from a multi-tool line keeps the rest.
	if removed, err := RemoveShimTool(root, "cargo"); err != nil || !removed {
		t.Fatalf("RemoveShimTool: removed=%v err=%v", removed, err)
	}
	got := ShimTools(root)
	if len(got) != 2 || got[0] != "dotnet" || got[1] != "go" {
		t.Fatalf("after remove: ShimTools = %v, want [dotnet go]", got)
	}
}
