package interpose

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wickedev/devhost/internal/addr"
	"github.com/wickedev/devhost/internal/project"
)

const clientSrc = `
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <netinet/in.h>
#include <arpa/inet.h>
int main(void) {
  int fd = socket(AF_INET, SOCK_STREAM, 0);
  struct sockaddr_in a; memset(&a, 0, sizeof a);
  a.sin_family = AF_INET;
  a.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
  a.sin_port = 0;
  if (bind(fd, (struct sockaddr *)&a, sizeof a) != 0) { perror("bind"); return 1; }
  socklen_t l = sizeof a;
  getsockname(fd, (struct sockaddr *)&a, &l);
  printf("%s\n", inet_ntoa(a.sin_addr));
  return 0;
}
`

// TestInterposerRewritesBind is the full-stack check: compile the embedded
// interposer, preload it into a plain C client that binds 127.0.0.1, run it
// inside a .devhost-marked directory, and require the socket to land on the
// project IP computed by internal/addr.
func TestInterposerRewritesBind(t *testing.T) {
	cc, err := findCompiler()
	if err != nil {
		t.Skipf("no C compiler: %v", err)
	}

	lib, err := Ensure()
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	dir := t.TempDir()
	client := filepath.Join(dir, "client")
	src := filepath.Join(dir, "client.c")
	if err := os.WriteFile(src, []byte(clientSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(cc, "-O2", "-o", client, src).CombinedOutput(); err != nil {
		t.Fatalf("compiling client: %v\n%s", err, out)
	}

	marked, err := filepath.EvalSymlinks(t.TempDir()) // getcwd(3) resolves symlinks
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marked, project.Marker), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	want := addr.ForDir(marked)

	if runtime.GOOS == "darwin" {
		// macOS routes only 127.0.0.1 by default; the target needs an lo0
		// alias. CI runners have passwordless sudo — locally, skip if absent.
		if err := exec.Command("sudo", "-n", "/sbin/ifconfig", "lo0", "alias", want, "up").Run(); err != nil {
			if out, _ := exec.Command("/sbin/ifconfig", "lo0").Output(); !strings.Contains(string(out), "inet "+want+" ") {
				t.Skipf("cannot add lo0 alias %s without passwordless sudo", want)
			}
		}
	}

	cmd := exec.Command(client)
	cmd.Dir = marked
	cmd.Env = append(os.Environ(), EnvVar()+"="+lib)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("client: %v\n%s", err, out)
	}
	got := strings.TrimSpace(string(out))
	if got != want {
		t.Fatalf("client bound %s, want project IP %s", got, want)
	}

	// Outside a marked tree the interposer must be a strict no-op.
	plain := t.TempDir()
	cmd = exec.Command(client)
	cmd.Dir = plain
	cmd.Env = append(os.Environ(), EnvVar()+"="+lib)
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("client (unmarked): %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "127.0.0.1" {
		t.Fatalf("unmarked dir bound %s, want untouched 127.0.0.1", got)
	}
}
