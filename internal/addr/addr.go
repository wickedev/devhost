// Package addr maps project root paths to deterministic loopback IPs and
// hostname labels. The scheme is compatibility-critical: the shell shim and
// the Node injector must produce identical results, so any change here is a
// breaking protocol change.
package addr

import (
	"crypto/md5"
	"fmt"
	"path/filepath"
	"strings"
)

// Prefix is the loopback range devhost assigns project IPs from
// (127.77.0.0/16). On macOS each IP is registered as an lo0 alias; on Linux
// the whole 127.0.0.0/8 block is routable out of the box.
const Prefix = "127.77"

// ForDir returns the deterministic loopback IP for a project root path:
// md5 over the exact path string, first byte -> third octet, second byte
// mod 254 plus 1 -> fourth octet (avoids .0 and .255). Equivalent shell:
// `/sbin/md5 -qs "$root"`.
func ForDir(root string) string {
	sum := md5.Sum([]byte(root))
	return fmt.Sprintf("%s.%d.%d", Prefix, sum[0], int(sum[1])%254+1)
}

// Name returns the hostname label for a project root (served as
// "<label>.devhost"): the lowercased basename with every run of characters
// outside [a-z0-9-] collapsed to a single '-', trimmed of leading and
// trailing dashes.
func Name(root string) string {
	base := strings.ToLower(filepath.Base(filepath.Clean(root)))
	var b strings.Builder
	dash := false
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
