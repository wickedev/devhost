package service

import (
	"strings"
	"testing"
)

func TestLaunchdPlist(t *testing.T) {
	p := launchdPlist("/usr/local/bin/devhost", "/tmp/devhostd.log")
	for _, want := range []string{
		"<string>" + Label + "</string>",
		"<string>/usr/local/bin/devhost</string>",
		"<string>daemon</string>",
		"<key>KeepAlive</key>",
		"<string>/tmp/devhostd.log</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q:\n%s", want, p)
		}
	}
}

func TestSystemdUnit(t *testing.T) {
	u := systemdUnit("/home/u/.local/bin/devhost")
	for _, want := range []string{
		"ExecStart=/home/u/.local/bin/devhost daemon",
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q:\n%s", want, u)
		}
	}
}
