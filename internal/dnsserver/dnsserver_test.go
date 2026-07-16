package dnsserver

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestResolveAViaRealResolver(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	table := map[string]string{"storefront": "127.77.60.193", "api": "127.77.113.42"}
	go Serve(conn, func(label string) string { return table[label] })

	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "udp", conn.LocalAddr().String())
		},
	}

	cases := []struct {
		host string
		want string // "" => expect a resolution error
	}{
		{"storefront.devhost", "127.77.60.193"},
		{"api.devhost", "127.77.113.42"},
		{"unknown.devhost", ""},  // known TLD, unregistered -> NXDOMAIN
		{"nested.x.devhost", ""}, // devhost hostnames never nest
	}
	for _, c := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ips, err := r.LookupHost(ctx, c.host)
		cancel()
		if c.want == "" {
			if err == nil {
				t.Errorf("%s: expected no resolution, got %v", c.host, ips)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", c.host, err)
			continue
		}
		if len(ips) != 1 || ips[0] != c.want {
			t.Errorf("%s = %v, want [%s]", c.host, ips, c.want)
		}
	}
}
