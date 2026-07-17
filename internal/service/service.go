// Package service registers the devhost daemon (the localhost
// mirror-router) with the user's service manager — a launchd agent on
// macOS, a systemd user unit on Linux — so it starts at login and restarts
// on failure without a hand-written plist or unit file.
package service

import "fmt"

// Label names the service in launchctl / systemctl output.
const Label = "dev.devhost.mirror"

func launchdPlist(exe, logPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, Label, exe, logPath, logPath)
}

func systemdUnit(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=devhost localhost mirror-router

[Service]
ExecStart=%s daemon
Restart=on-failure

[Install]
WantedBy=default.target
`, exe)
}
