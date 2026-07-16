package addr

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestForDirDeterministicAndInRange(t *testing.T) {
	paths := []string{"/tmp/a", "/tmp/b", "/Users/x/Workspace/app", "/Users/x/Workspace/app-wt-feature"}
	seen := map[string]string{}
	for _, p := range paths {
		ip := ForDir(p)
		if ip != ForDir(p) {
			t.Fatalf("ForDir(%q) not deterministic", p)
		}
		if !strings.HasPrefix(ip, Prefix+".") {
			t.Fatalf("ForDir(%q) = %s, want prefix %s", p, ip, Prefix)
		}
		var x, y int
		if _, err := fmtSscanf(ip, &x, &y); err != nil {
			t.Fatalf("ForDir(%q) = %s: %v", p, ip, err)
		}
		if x < 0 || x > 255 || y < 1 || y > 254 {
			t.Fatalf("ForDir(%q) = %s: octets out of range", p, ip)
		}
		if prev, dup := seen[ip]; dup {
			t.Fatalf("collision between %q and %q on %s", prev, p, ip)
		}
		seen[ip] = p
	}
}

func fmtSscanf(ip string, x, y *int) (int, error) {
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		return 0, errFormat(ip)
	}
	n1, err1 := atoi(parts[2])
	n2, err2 := atoi(parts[3])
	if err1 != nil || err2 != nil {
		return 0, errFormat(ip)
	}
	*x, *y = n1, n2
	return 2, nil
}

type errFormat string

func (e errFormat) Error() string { return "bad ip format: " + string(e) }

func atoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errFormat(s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// TestForDirMatchesShellShim cross-checks the Go hash against the macOS
// /sbin/md5 string mode the shell shim uses. Protocol compatibility guard.
func TestForDirMatchesShellShim(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin-only cross-check")
	}
	const path = "/Users/example/Workspace/some-app"
	out, err := exec.Command("/sbin/md5", "-qs", path).Output()
	if err != nil {
		t.Skipf("/sbin/md5 unavailable: %v", err)
	}
	h := strings.TrimSpace(string(out))
	x := hexByte(t, h[0:2])
	y := hexByte(t, h[2:4])%254 + 1
	want := Prefix + "." + itoa(x) + "." + itoa(y)
	if got := ForDir(path); got != want {
		t.Fatalf("ForDir(%q) = %s, shell scheme yields %s", path, got, want)
	}
}

func hexByte(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range strings.ToLower(s) {
		n *= 16
		switch {
		case c >= '0' && c <= '9':
			n += int(c - '0')
		case c >= 'a' && c <= 'f':
			n += int(c-'a') + 10
		default:
			t.Fatalf("bad hex %q", s)
		}
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestName(t *testing.T) {
	cases := map[string]string{
		"/Users/x/Workspace/My App":   "my-app",
		"/Users/x/Workspace/repo.git": "repo-git",
		"/Users/x/Workspace/---":      "",
		"/Users/x/Workspace/a__b":     "a-b",
		"/Users/x/Workspace/carrier":  "carrier",
	}
	for in, want := range cases {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q, want %q", in, got, want)
		}
	}
}
