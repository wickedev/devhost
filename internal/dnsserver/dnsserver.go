// Package dnsserver answers A queries for "<name>.devhost" from the registry,
// so hostnames resolve without any /etc/hosts entry. It speaks just enough of
// the DNS wire format (RFC 1035) to parse a single question and return an A
// record — the daemon runs it, and macOS routes the .devhost TLD to it via
// /etc/resolver/devhost.
package dnsserver

import (
	"errors"
	"net"
	"strings"

	"github.com/wickedev/devhost/internal/registry"
)

// Port is the fixed loopback UDP port the responder listens on and that the
// /etc/resolver/devhost stub points at. High enough to bind without root.
const Port = 53530

// TLD without the leading dot.
const tld = "devhost"

// Serve runs the responder on conn until conn is closed. Resolver maps a
// hostname label to an IP; nil uses the on-disk registry.
func Serve(conn net.PacketConn, resolver func(string) string) error {
	if resolver == nil {
		resolver = registry.Lookup
	}
	buf := make([]byte, 512) // classic DNS UDP message ceiling
	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if resp := handle(buf[:n], resolver); resp != nil {
			conn.WriteTo(resp, addr) //nolint:errcheck
		}
	}
}

// ListenAndServe binds 127.0.0.1:Port and serves until the listener errors.
func ListenAndServe() error {
	conn, err := net.ListenPacket("udp", net.JoinHostPort("127.0.0.1", itoa(Port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	return Serve(conn, nil)
}

func handle(msg []byte, resolve func(string) string) []byte {
	if len(msg) < 12 {
		return nil
	}
	qd := int(msg[4])<<8 | int(msg[5])
	if qd != 1 { // exactly one question, as resolvers send
		return nil
	}

	name, off, ok := readName(msg, 12)
	if !ok || off+4 > len(msg) {
		return nil
	}
	qtype := int(msg[off])<<8 | int(msg[off+1])
	qend := off + 4 // past QTYPE + QCLASS

	label, matches := devhostLabel(name)
	var ip net.IP
	if matches {
		ip = net.ParseIP(resolve(label)).To4()
	}

	// Response header: copy ID, set QR + RA, echo question count.
	out := make([]byte, 0, len(msg)+16)
	out = append(out, msg[0], msg[1])
	const A = 1
	answer := matches && qtype == A && ip != nil
	switch {
	case !matches:
		out = append(out, 0x81, 0x83) // QR + RD/RA + NXDOMAIN
	case answer:
		out = append(out, 0x81, 0x80) // QR + RD/RA + NOERROR
	case ip == nil:
		out = append(out, 0x81, 0x83) // known TLD, unknown name -> NXDOMAIN
	default:
		out = append(out, 0x81, 0x80) // e.g. AAAA for a real name -> NOERROR, no answer
	}
	out = append(out, 0x00, 0x01) // QDCOUNT
	anCount := 0
	if answer {
		anCount = 1
	}
	out = append(out, byte(anCount>>8), byte(anCount))
	out = append(out, 0, 0, 0, 0) // NSCOUNT, ARCOUNT

	out = append(out, msg[12:qend]...) // echo the question verbatim

	if answer {
		out = append(out, 0xC0, 0x0C)             // name pointer to the question
		out = append(out, 0x00, 0x01)             // TYPE A
		out = append(out, 0x00, 0x01)             // CLASS IN
		out = append(out, 0x00, 0x00, 0x00, 0x1E) // TTL 30s
		out = append(out, 0x00, 0x04)             // RDLENGTH
		out = append(out, ip[0], ip[1], ip[2], ip[3])
	}
	return out
}

// devhostLabel returns the leading label of "<label>.devhost" and whether the
// name is in the devhost TLD (single-label subdomains only, e.g. no
// a.b.devhost — devhost hostnames never nest).
func devhostLabel(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	suffix := "." + tld
	if !strings.HasSuffix(name, suffix) {
		return "", false
	}
	label := strings.TrimSuffix(name, suffix)
	if label == "" || strings.Contains(label, ".") {
		return "", false
	}
	return label, true
}

// readName decodes a QNAME (no compression in questions) to a dotted string,
// returning the offset just past it.
func readName(msg []byte, off int) (string, int, bool) {
	var labels []string
	for {
		if off >= len(msg) {
			return "", 0, false
		}
		n := int(msg[off])
		off++
		if n == 0 {
			return strings.Join(labels, "."), off, true
		}
		if n > 63 || off+n > len(msg) {
			return "", 0, false // compression pointer or overrun — reject
		}
		labels = append(labels, string(msg[off:off+n]))
		off += n
	}
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
