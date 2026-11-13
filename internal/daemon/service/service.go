// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

// Package service registers the daemon as a user service (launchd, systemd --user, Windows scheduled task).
package service

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
)

// Spec describes where and how to install. Paths use the separator of GOOS.
// User is the Windows account (DOMAIN\user); Env holds KEY=VALUE pairs for the service, see Environ.
type Spec struct {
	GOOS, Exe, Home, DataDir, User string
	Env                            []string
}

// allowed lists the only variables persisted into a service definition: the ones the daemon reads
// (internal/daemon/paths) and no secrets. Everything else, the server's BRAID_* settings included, is dropped.
var allowed = map[string]bool{"CLAUDE_CONFIG_DIR": true, "XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
	"XDG_CACHE_HOME": true, "BRAID_DATA_DIR": true, "BRAID_CONFIG_DIR": true}

var keyRE = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

func checkPair(k, v string) error {
	if !keyRE.MatchString(k) {
		return fmt.Errorf("service: invalid variable name %q", k)
	}
	if strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("service: %s contains a line break or NUL", k)
	}
	return nil
}

// Environ picks the allowed variables that change where the daemon looks for files; a bad value is an error.
func Environ(environ []string) ([]string, error) {
	var out []string
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" || !allowed[k] {
			continue
		}
		if err := checkPair(k, v); err != nil {
			return nil, err
		}
		out = append(out, kv)
	}
	sort.Strings(out)
	return out, nil
}

func (s Spec) pairs() ([][2]string, error) {
	var out [][2]string
	for _, kv := range s.Env {
		k, v, _ := strings.Cut(kv, "=")
		if err := checkPair(k, v); err != nil {
			return nil, err
		}
		out = append(out, [2]string{k, v})
	}
	return out, nil
}

const label = "dev.usebraid.braid"

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// Render returns the file path and content for the OS (path is "" on windows: the task XML is imported, not stored).
func Render(s Spec) (path string, content []byte, err error) {
	env, err := s.pairs()
	if err != nil {
		return "", nil, err
	}
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
%s	<key>StandardOutPath</key>
	<string>%s/braid.log</string>
	<key>StandardErrorPath</key>
	<string>%s/braid.log</string>
</dict>
</plist>
`, label, esc(s.Exe), plistEnv(env), esc(s.DataDir), esc(s.DataDir)), nil
	case "linux":
		if strings.ContainsAny(s.Exe, " \t\"\\%$\n") {
			return "", nil, fmt.Errorf("service: unsupported characters in executable path %q", s.Exe)
		}
		return s.Home + "/.config/systemd/user/braid.service", fmt.Appendf(nil, `[Unit]
Description=Braid sync daemon

[Service]
%sExecStart=%s run
Restart=always
RestartSec=10

[Install]
WantedBy=default.target
`, systemdEnv(env), s.Exe), nil
	case "windows":
		// shortcut: braid.exe is a console program, so a console window opens at logon and closing it stops the daemon;
		// start it hidden (conhost --headless or a windowsgui build) once Windows is tested for real.
		if s.User == "" {
			return "", nil, errors.New("service: windows account name missing")
		}
		cmd, args, err := windowsCommand(s.Exe, env)
		if err != nil {
			return "", nil, err
		}
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
      <Arguments>%s</Arguments>
    </Exec>
  </Actions>
</Task>
`, esc(s.User), esc(s.User), esc(cmd), esc(args)), nil
	}
	return "", nil, fmt.Errorf("service: unsupported OS %q", s.GOOS)
}

func plistEnv(env [][2]string) string {
	if len(env) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
	for _, e := range env {
		fmt.Fprintf(&b, "\t\t<key>%s</key>\n\t\t<string>%s</string>\n", esc(e[0]), esc(e[1]))
	}
	b.WriteString("\t</dict>\n")
	return b.String()
}

// systemdEnv quotes each assignment as a whole; % is a specifier in unit files.
func systemdEnv(env [][2]string) string {
	var b strings.Builder
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%")
	for _, e := range env {
		fmt.Fprintf(&b, "Environment=\"%s=%s\"\n", e[0], r.Replace(e[1]))
	}
	return b.String()
}

// windowsCommand runs braid directly, or through cmd /c set when variables must be passed (tasks have no environment).
func windowsCommand(exe string, env [][2]string) (cmd, args string, err error) {
	if len(env) == 0 {
		return exe, "run", nil
	}
	if strings.Contains(exe, "%") { // cmd expands % inside quotes too
		return "", "", fmt.Errorf("service: %% in the executable path %q cannot be passed to the Windows task", exe)
	}
	var b strings.Builder
	for _, e := range env {
		// cmd expands % even inside quotes and treats " as a terminator: refuse instead of mis-quoting.
		if strings.ContainsAny(e[0]+e[1], `"%`) {
			return "", "", fmt.Errorf("service: %s contains a character that cannot be passed to the Windows task", e[0])
		}
		fmt.Fprintf(&b, `set "%s=%s" && `, e[0], e[1])
	}
	return "cmd.exe", fmt.Sprintf(`/d /c "%s"%s" run"`, b.String(), exe), nil
}

func write(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	// The manager runs as the same user, so 0600 suffices; WriteFile keeps the mode of an existing file, hence the Chmod.
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
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
