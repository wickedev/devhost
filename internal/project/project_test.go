package project

import (
	"os"
	"path/filepath"
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
