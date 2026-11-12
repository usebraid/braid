// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package service registers the daemon as a user service (launchd, systemd --user, Windows scheduled task).
package service

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

// Spec describes where and how to install. Paths use the separator of GOOS.
type Spec struct{ GOOS, Exe, Home, DataDir string }

const label = "dev.usebraid.braid"

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Render returns the file path and content for the OS (path is "" on windows: the task XML is imported, not stored).
func Render(s Spec) (path string, content []byte, err error) {
	switch s.GOOS {
	case "darwin":
		return s.Home + "/Library/LaunchAgents/" + label + ".plist", fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s/braid.log</string>
	<key>StandardErrorPath</key>
	<string>%s/braid.log</string>
</dict>
</plist>
`, label, esc(s.Exe), esc(s.DataDir), esc(s.DataDir)), nil
	case "linux":
		if strings.ContainsAny(s.Exe, " \t\"\\%$\n") {
			return "", nil, fmt.Errorf("service: unsupported characters in executable path %q", s.Exe)
		}
		return s.Home + "/.config/systemd/user/braid.service", fmt.Appendf(nil, `[Unit]
Description=Braid sync daemon

[Service]
ExecStart=%s run
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`, s.Exe), nil
	case "windows":
		// shortcut: user name derived from the home directory's last element, not the domain account; fix if schtasks rejects it.
		user := s.Home[strings.LastIndexByte(s.Home, '\\')+1:]
		return "", fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>3</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>run</Arguments>
    </Exec>
  </Actions>
</Task>
`, esc(user), esc(user), esc(s.Exe)), nil
	}
	return "", nil, fmt.Errorf("service: unsupported OS %q", s.GOOS)
}

func write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644) //nolint:gosec // unit files must be readable by the service manager
}

// utf16le encodes with BOM, as schtasks expects for XML declared as UTF-16.
func utf16le(b []byte) []byte {
	out := []byte{0xFF, 0xFE}
	for _, c := range utf16.Encode([]rune(string(b))) {
		out = binary.LittleEndian.AppendUint16(out, c)
	}
	return out
}

// Install writes the service definition and starts it via run.
func Install(s Spec, run func(name string, args ...string) error) error {
	path, content, err := Render(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.DataDir, 0o700); err != nil {
		return err
	}
	switch s.GOOS {
	case "darwin":
		if err := write(path, content); err != nil {
			return err
		}
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		_ = run("launchctl", "bootout", domain, path) // not loaded yet is fine
		return run("launchctl", "bootstrap", domain, path)
	case "linux":
		if err := write(path, content); err != nil {
			return err
		}
		if err := run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return run("systemctl", "--user", "enable", "--now", "braid.service")
	default: // windows; Render rejected everything else
		tmp, err := os.CreateTemp("", "braid-task-*.xml")
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(tmp.Name()) }()
		if _, err := tmp.Write(utf16le(content)); err != nil {
			_ = tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		if err := run("schtasks", "/Create", "/TN", "Braid", "/XML", tmp.Name(), "/F"); err != nil {
			return err
		}
		return run("schtasks", "/Run", "/TN", "Braid")
	}
}

// Uninstall stops and removes the service. Stop errors are ignored (it may not be running).
func Uninstall(s Spec, run func(name string, args ...string) error) error {
	path, _, err := Render(s)
	if err != nil {
		return err
	}
	switch s.GOOS {
	case "darwin":
		_ = run("launchctl", "bootout", fmt.Sprintf("gui/%d", os.Getuid()), path)
	case "linux":
		_ = run("systemctl", "--user", "disable", "--now", "braid.service")
	default:
		_ = run("schtasks", "/End", "/TN", "Braid")
		return run("schtasks", "/Delete", "/TN", "Braid", "/F")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if s.GOOS == "linux" {
		return run("systemctl", "--user", "daemon-reload")
	}
	return nil
}
