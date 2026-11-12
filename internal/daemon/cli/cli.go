// SPDX-License-Identifier: AGPL-3.0-or-later
// Copyright (C) 2026 The Braid Authors

package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/usebraid/braid/internal/daemon/auth"
	"github.com/usebraid/braid/internal/daemon/client"
	"github.com/usebraid/braid/internal/daemon/paths"
	"github.com/usebraid/braid/internal/daemon/service"
	"github.com/usebraid/braid/internal/daemon/spool"
	"github.com/usebraid/braid/internal/daemon/state"
	"github.com/usebraid/braid/internal/daemon/syncer"
	"github.com/usebraid/braid/internal/daemon/watch"
	"github.com/usebraid/braid/internal/protocol"
)

// Version and Commit are set with -ldflags -X.
var (
	Version = "dev"
	Commit  = "dev"
)

const usage = `usage: braid <command>

  login <server-url>   connect this device
  logout               revoke the token and delete it locally
  status               show login, backlog and warnings
  import               sync everything now (or follow the running service)
  run                  foreground sync loop (what the service runs)
  install | uninstall  register / remove the user service
  stats --json         not implemented yet
  version
`

type env struct {
	ctx      context.Context
	in       io.Reader
	out, err io.Writer
	h        Hooks
	dirs     paths.Dirs
}

// RunWithHooks is Run with test hooks.
func RunWithHooks(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer, h Hooks) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	home, _ := os.UserHomeDir()
	e := &env{ctx: ctx, in: stdin, out: stdout, err: stderr, h: h, dirs: paths.Resolve(runtime.GOOS, home, os.Getenv)}
	if h.ConfigDir != "" {
		e.dirs.Config = h.ConfigDir
	}
	if h.DataDir != "" {
		e.dirs.Data = h.DataDir
	}
	var err error
	switch args[0] {
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(stdout, usage)
		return 0
	case "version":
		_, _ = fmt.Fprintf(stdout, "braid %s (%s) protocol %d\n", Version, Commit, protocol.Version)
		return 0
	case "stats":
		_, _ = fmt.Fprintln(stdout, `{"error":"not implemented yet"}`)
		return 2
	case "status":
		return e.status()
	case "login":
		if len(args) != 2 {
			_, _ = fmt.Fprint(stderr, usage)
			return 2
		}
		err = e.login(args[1])
	case "logout":
		err = e.logout()
	case "import":
		err = e.importAll()
	case "run":
		err = e.run()
	case "install":
		err = e.install(service.Install)
	case "uninstall":
		err = e.install(service.Uninstall)
	default:
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "braid:", err)
		return 1
	}
	return 0
}

func normalizeURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", errors.New("invalid server URL")
	}
	local := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && (u.Scheme != "http" || !local) {
		return "", errors.New("server URL must use https")
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/"), nil
}

func (e *env) statePath() string { return filepath.Join(e.dirs.Data, "state.db") }

func (e *env) openState() (*state.DB, error) { return state.Open(e.statePath()) }

func (e *env) store(kind string) auth.Store {
	if kind == "file" {
		return auth.NewFile(e.dirs.Config)
	}
	if e.h.NoKeychain {
		return noKeychain{}
	}
	return auth.NewKeychain()
}

// noKeychain simulates a machine without keychain (tests).
type noKeychain struct{ auth.Store }

func (noKeychain) Set(string, string) error { return auth.ErrNoKeychain }
func (noKeychain) Kind() string             { return "keychain" }

func openBrowser(ctx context.Context, u string) {
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		return
	}
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.CommandContext(ctx, "open", u) //nolint:gosec // G204: u is an http(s) URL from our own server, passed as one argument
	case "windows":
		c = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", u) //nolint:gosec // G204: see above
	default:
		c = exec.CommandContext(ctx, "xdg-open", u) //nolint:gosec // G204: see above
	}
	_ = c.Start() // best effort
}

func (e *env) login(raw string) error {
	server, err := normalizeURL(raw)
	if err != nil {
		return err
	}
	var tr protocol.TokenResponse
	if e.h.FakeToken != "" {
		tr = protocol.TokenResponse{AccessToken: e.h.FakeToken, DeviceID: "fake"}
	} else {
		host, _ := os.Hostname()
		osUser := "unknown"
		if u, err := user.Current(); err == nil {
			osUser = u.Username
		}
		info := protocol.DeviceCodeRequest{Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH, OSUser: osUser, DaemonVersion: Version}
		if err := info.Validate(); err != nil {
			return err
		}
		show := func(r protocol.DeviceCodeResponse) {
			_, _ = fmt.Fprintf(e.out, "Open %s and confirm code %s\n", r.VerificationURIComplete, r.UserCode)
			openBrowser(e.ctx, r.VerificationURIComplete)
		}
		tr, err = auth.Login(e.ctx, &http.Client{Timeout: 30 * time.Second}, server, info, show, sleepCtx)
		if err != nil {
			return err
		}
	}
	st := e.store("keychain")
	err = st.Set(server, tr.AccessToken)
	if errors.Is(err, auth.ErrNoKeychain) {
		st = auth.NewFile(e.dirs.Config)
		_, _ = fmt.Fprintf(e.out, "No OS keychain found. Store the token in %s (readable only by you)? [y/N] ", filepath.Join(e.dirs.Config, "token"))
		ans, _ := bufio.NewReader(e.in).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(ans)); a != "y" && a != "yes" {
			return errors.New("aborted: token not stored")
		}
		err = st.Set(server, tr.AccessToken)
	}
	if err != nil {
		return fmt.Errorf("store token: %w", err)
	}
	db, err := e.openState()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	for k, v := range map[string]string{"server_url": server, "device_id": tr.DeviceID, "token_store": st.Kind(), "last_error": ""} {
		if err := db.SetSetting(e.ctx, k, v); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintln(e.out, "Logged in. If the service is running, run `braid install` again to restart it with the new token.")
	return nil
}

// creds returns the stored server, token and settings DB (caller closes db).
func (e *env) creds() (db *state.DB, server, token string, err error) {
	db, err = e.openState()
	if err != nil {
		return nil, "", "", err
	}
	server, _, _ = db.Setting(e.ctx, "server_url")
	kind, _, _ := db.Setting(e.ctx, "token_store")
	if server == "" {
		_ = db.Close()
		return nil, "", "", auth.ErrNoToken
	}
	token, err = e.store(kind).Get(server)
	if err != nil {
		_ = db.Close()
		return nil, "", "", err
	}
	return db, server, token, nil
}

func (e *env) logout() error {
	db, server, token, err := e.creds()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
	defer cancel()
	if err := client.New(server, token, Version, nil).Revoke(ctx); err != nil {
		_, _ = fmt.Fprintln(e.err, "warning: could not revoke the token on the server:", err)
	}
	kind, _, _ := db.Setting(e.ctx, "token_store")
	if err := e.store(kind).Delete(server); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(e.out, "Logged out. Local sync state kept.")
	return nil
}

func (e *env) status() int {
	db, err := e.openState()
	if err != nil {
		_, _ = fmt.Fprintln(e.err, "braid:", err)
		return 2
	}
	defer func() { _ = db.Close() }()
	server, _, _ := db.Setting(e.ctx, "server_url")
	kind, _, _ := db.Setting(e.ctx, "token_store")
	lastErr, _, _ := db.Setting(e.ctx, "last_error")
	code := 0
	warn := func(c int, msg string) {
		_, _ = fmt.Fprintln(e.out, "warning:", msg)
		code = max(code, c)
	}
	if server == "" {
		_, _ = fmt.Fprintln(e.out, "server: -\nlogin: not logged in")
		warn(2, "not logged in, run `braid login <server-url>`")
		return code
	}
	_, _ = fmt.Fprintln(e.out, "server:", server)
	if _, err := e.store(kind).Get(server); err != nil {
		_, _ = fmt.Fprintln(e.out, "login: not logged in")
		warn(2, "not logged in, run `braid login`")
	} else {
		_, _ = fmt.Fprintln(e.out, "login: ok (token in "+kind+")")
	}
	b, err := db.Backlog(e.ctx)
	if err != nil {
		_, _ = fmt.Fprintln(e.err, "braid:", err)
		return 2
	}
	_, _ = fmt.Fprintf(e.out, "backlog: %d files, %d bytes\n", b.Files, b.Bytes)
	if b.LastAck.IsZero() {
		_, _ = fmt.Fprintln(e.out, "last ack: never")
	} else {
		_, _ = fmt.Fprintln(e.out, "last ack:", b.LastAck.Format(time.RFC3339))
	}
	if b.Files > 0 && time.Since(b.LastAck) > 7*24*time.Hour {
		warn(1, "no ack for more than 7 days while files are waiting")
	}
	if lastErr != "" {
		warn(1, lastErr)
	}
	return code
}

func (e *env) install(f func(service.Spec, func(string, ...string) error) error) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	home, _ := os.UserHomeDir()
	return f(service.Spec{GOOS: runtime.GOOS, Exe: exe, Home: home, DataDir: e.dirs.Data}, func(name string, args ...string) error {
		return exec.CommandContext(e.ctx, name, args...).Run() //nolint:gosec // fixed service-manager commands
	})
}

// trackUp records token rejection in the settings for `braid status`.
type trackUp struct {
	syncer.Uploader
	db *state.DB
}

func (t trackUp) Ingest(ctx context.Context, req protocol.IngestRequest) (protocol.IngestResponse, error) {
	resp, err := t.Uploader.Ingest(ctx, req)
	switch {
	case err == nil:
		_ = t.db.SetSetting(context.WithoutCancel(ctx), "last_error", "")
	case errors.Is(err, client.ErrUnauthorized):
		_ = t.db.SetSetting(context.WithoutCancel(ctx), "last_error", "token rejected, run `braid login` again")
	}
	return resp, err
}

func (e *env) engine() (*syncer.Engine, *state.DB, error) {
	db, server, token, err := e.creds()
	if err != nil {
		return nil, nil, err
	}
	return &syncer.Engine{Root: e.dirs.ClaudeProjects, State: db, Spool: spool.New(filepath.Join(e.dirs.Data, "spool")),
		Up: trackUp{client.New(server, token, Version, nil), db}}, db, nil
}

func (e *env) run() error {
	release, err := AcquireLock(e.dirs.Data)
	if err != nil {
		return err
	}
	defer release()
	eng, db, err := e.engine()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	trig, err := watch.Triggers(e.ctx, eng.Root, 60*time.Second)
	if err != nil {
		return err
	}
	r := &Runner{Engine: eng, Trigger: trig, Backoff: client.Backoff{Min: time.Second, Max: 5 * time.Minute}, Log: slog.New(slog.NewTextHandler(e.err, nil))}
	return r.Run(e.ctx)
}

func (e *env) importAll() error {
	if err := os.MkdirAll(e.dirs.Data, 0o700); err != nil {
		return err
	}
	release, err := AcquireLock(e.dirs.Data)
	if errors.Is(err, ErrLocked) {
		return e.follow()
	}
	if err != nil {
		return err
	}
	defer release()
	eng, db, err := e.engine()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	for {
		res, err := eng.Pass(e.ctx)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(e.out, "uploaded %d bytes, %d files pending\n", res.Uploaded, res.PendingFiles)
		if res.PendingFiles == 0 {
			return nil
		}
		if res.Uploaded == 0 {
			return errors.New("no progress, files still pending")
		}
	}
}

// follow prints the backlog of the running service until it is empty.
func (e *env) follow() error {
	db, err := e.openState()
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	for {
		b, err := db.Backlog(e.ctx)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(e.out, "backlog: %d files, %d bytes\n", b.Files, b.Bytes)
		if b.Files == 0 {
			return nil
		}
		if sleepCtx(e.ctx, 2*time.Second) != nil {
			return nil
		}
	}
}
