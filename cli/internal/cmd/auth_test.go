// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmphost/permahost/cli/internal/config"
)

const tokenJSON = `{"access_token":"at","token_type":"Bearer","expires_in":3600,"refresh_token":"rt"}`

// approve makes the harness browser approve the login at once, by following
// the redirect URI with a code and the request's state.
func (h *harness) approve() {
	h.browser = func(raw string) error {
		u, err := url.Parse(raw)
		require.NoError(h.t, err)
		q := u.Query()
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?" + url.Values{"code": {"c1"}, "state": {q.Get("state")}}.Encode())
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLogin(t *testing.T) {
	h := newHarness(t)
	h.saveConfig(config.Config{})
	h.reply("POST /oauth2/token", 200, tokenJSON)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 200, usageJSON)
	h.approve()

	assert.Equal(t, 0, h.run("login", "--server", h.srv.URL+"/"), h.stderr.String())
	assert.Equal(t, "Logged in to "+h.srv.URL+" as ada@example.com (40/100 credits)\n", h.stdout.String())
	assert.Contains(t, h.stderr.String(), h.srv.URL+"/oauth2/authorize?")
	assert.Equal(t, config.Config{Server: h.srv.URL, AccessToken: "at", RefreshToken: "rt", Expiry: now.Add(time.Hour)}, h.loadConfig())

	// The saved server is used next time, and --json prints the user.
	assert.Equal(t, 0, h.run("login", "--json"), h.stderr.String())
	assert.Equal(t, "ada@example.com", h.decode()["user"].(map[string]any)["email"])
}

func TestLogin_device(t *testing.T) {
	h := newHarness(t)
	h.saveConfig(config.Config{})
	// interval 0 polls after the CLI's minimum of a second.
	h.reply("POST /oauth2/device/code", 200, `{"device_code":"dc","user_code":"WDJB-MJHT","verification_uri":"`+h.srv.URL+`/auth/device","expires_in":600,"interval":0}`)
	h.reply("POST /oauth2/token", 200, tokenJSON)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.browser = func(string) error { return errors.New("no browser here") }

	// Over SSH the device flow is the default.
	h.env["SSH_CONNECTION"] = "1.2.3.4 5 6.7.8.9 22"
	assert.Equal(t, 0, h.run("login", "--server", h.srv.URL), h.stderr.String())
	assert.Equal(t, "Logged in to "+h.srv.URL+" as ada@example.com (40/100 credits)\n", h.stdout.String())
	assert.Contains(t, h.stderr.String(), "and enter the code:\n\n  WDJB-MJHT\n")
	assert.Equal(t, config.Config{Server: h.srv.URL, AccessToken: "at", RefreshToken: "rt", Expiry: now.Add(time.Hour)}, h.loadConfig())

	// --browser overrides it, and the flags exclude each other.
	h.approve()
	assert.Equal(t, 0, h.run("login", "--browser"), h.stderr.String())
	assert.Contains(t, h.stderr.String(), "/oauth2/authorize?")
	assert.Equal(t, 1, h.run("login", "--device", "--browser"))
	assert.Contains(t, h.stderr.String(), "none of the others can be")
}

func TestHeadless(t *testing.T) {
	orig := goos
	t.Cleanup(func() { goos = orig })
	for _, tc := range []struct {
		goos string
		env  map[string]string
		want bool
	}{
		{"linux", map[string]string{"DISPLAY": ":0"}, false},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, false},
		{"linux", map[string]string{}, true},
		{"linux", map[string]string{"DISPLAY": ":0", "SSH_TTY": "/dev/pts/0"}, true},
		{"darwin", map[string]string{}, false},
		{"darwin", map[string]string{"SSH_CONNECTION": "x"}, true},
		{"windows", map[string]string{}, false},
	} {
		goos = tc.goos
		a := &app{opts: Options{Getenv: func(k string) string { return tc.env[k] }}}
		assert.Equal(t, tc.want, a.headless(), "%s %v", tc.goos, tc.env)
	}
}

func TestLogin_errors(t *testing.T) {
	h := newHarness(t)
	h.reply("POST /oauth2/token", 200, tokenJSON)
	h.mux.HandleFunc("GET /api/v1/users/me", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"error":"db down"}`)
	})

	// The user denies access in the browser.
	h.browser = func(raw string) error {
		u, _ := url.Parse(raw)
		go func() {
			resp, err := http.Get(u.Query().Get("redirect_uri") + "?error=access_denied&state=" + u.Query().Get("state"))
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
	assert.Equal(t, 1, h.run("login"))
	assert.Contains(t, h.stderr.String(), "error: sign-in failed: access_denied\n")

	h.approve()
	assert.Equal(t, 1, h.run("login"))
	assert.Contains(t, h.stderr.String(), "error: db down\n")

	h.env["PERMAHOSTIO_CONFIG"] = filepath.Join(readOnlyDir(t), "sub", "config.json")
	assert.Equal(t, 1, h.run("login", "--server", h.srv.URL))
	assert.Contains(t, h.stderr.String(), "create config directory")
}

func TestConfigErrors(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, os.WriteFile(h.env["PERMAHOSTIO_CONFIG"], []byte("{"), 0o600))
	for _, args := range [][]string{{"whoami"}, {"login"}, {"logout"}} {
		assert.Equal(t, 1, h.run(args...), args)
		assert.Contains(t, h.stderr.String(), "parse config", args)
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t)
	assert.Equal(t, 0, h.run("logout"))
	assert.Equal(t, "Logged out.\n", h.stdout.String())
	assert.Equal(t, config.Config{Server: h.srv.URL}, h.loadConfig())

	assert.Equal(t, 0, h.run("logout", "--json"), "already logged out")
	assert.Equal(t, true, h.decode()["loggedOut"])

	h = newHarness(t)
	require.NoError(t, os.Chmod(filepath.Dir(h.env["PERMAHOSTIO_CONFIG"]), 0o500))
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(h.env["PERMAHOSTIO_CONFIG"]), 0o700) })
	require.NoError(t, os.Chmod(h.env["PERMAHOSTIO_CONFIG"], 0o400))
	assert.Equal(t, 1, h.run("logout"))
	assert.Contains(t, h.stderr.String(), "write config")
}

func TestWhoami(t *testing.T) {
	h := newHarness(t)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 200, usageJSON)
	assert.Equal(t, 0, h.run("whoami"))
	renew := formatTime(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	assert.Equal(t, "Email:         ada@example.com\nName:          Ada\nCredits:       40/100\nRenews at:     "+renew+"\n"+
		"Hosting:       1 folders, 6 B in 2 storage blocks, 1 domains\nMonthly cost:  27 credits\n"+
		"Prices:        10 credits a month per folder, 5 per started 4 B stored, 7 per domain (+3 to set it up)\n", h.stdout.String())

	assert.Equal(t, 0, h.run("whoami", "--json"))
	v := h.decode()
	assert.Equal(t, float64(40), v["user"].(map[string]any)["credits"])
	assert.Equal(t, float64(27), v["usage"].(map[string]any)["monthlyCredits"])

	h = newHarness(t)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 500, `{"error":"usage down"}`)
	assert.Equal(t, 1, h.run("whoami"))
	assert.Equal(t, "error: usage down\n", h.stderr.String())
}

func TestNotLoggedIn(t *testing.T) {
	h := newHarness(t)
	h.saveConfig(config.Config{Server: h.srv.URL})
	for _, args := range [][]string{
		{"whoami"}, {"file", "ls"}, {"file", "get", "--id", "rs"}, {"file", "url", "--id", "rs"},
		{"file", "rm", "--id", "rs"}, {"file", "upload", "."}, {"file", "download", "--id", "rs", "a"},
		{"domain", "add", "--id", "rs", "a.example"}, {"domain", "ls", "--id", "rs"},
		{"domain", "verify", "--id", "rs", "d1"}, {"domain", "rm", "--id", "rs", "d1"},
	} {
		assert.Equal(t, 1, h.run(args...), args)
		assert.Equal(t, `error: not logged in to `+h.srv.URL+`: run "permahost login"`+"\n", h.stderr.String(), args)
	}
}

func TestTokensStayWithTheirServer(t *testing.T) {
	h := newHarness(t)
	h.env["PERMAHOSTIO_SERVER"] = "https://elsewhere.example/"
	assert.Equal(t, 1, h.run("whoami"))
	assert.Equal(t, "error: not logged in to https://elsewhere.example: run \"permahost login\"\n", h.stderr.String())

	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 200, usageJSON)
	assert.Equal(t, 0, h.run("whoami", "--server", h.srv.URL))
}

func TestSession_refreshesNearExpiry(t *testing.T) {
	h := newHarness(t)
	h.saveConfig(config.Config{Server: h.srv.URL, AccessToken: "at", RefreshToken: "rt", Expiry: now.Add(30 * time.Second)})
	h.validToken = "at2"
	var form url.Values
	h.mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		form = r.PostForm
		_, _ = io.WriteString(w, `{"access_token":"at2","token_type":"Bearer","expires_in":600,"refresh_token":"rt2"}`)
	})
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 200, usageJSON)

	assert.Equal(t, 0, h.run("whoami"), h.stderr.String())
	assert.Equal(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"rt"}, "client_id": {"00000000001"}}, form)
	assert.Equal(t, config.Config{Server: h.srv.URL, AccessToken: "at2", RefreshToken: "rt2", Expiry: now.Add(10 * time.Minute)}, h.loadConfig())
}

func TestSession_refreshesAfter401(t *testing.T) {
	h := newHarness(t)
	h.validToken = "at2"
	// A server that does not rotate refresh tokens keeps the old one valid.
	h.reply("POST /oauth2/token", 200, `{"access_token":"at2","token_type":"Bearer","expires_in":600}`)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 200, usageJSON)

	assert.Equal(t, 0, h.run("whoami"), h.stderr.String())
	assert.Equal(t, config.Config{Server: h.srv.URL, AccessToken: "at2", RefreshToken: "rt", Expiry: now.Add(10 * time.Minute)}, h.loadConfig())
}

func TestSession_refreshFailures(t *testing.T) {
	h := newHarness(t)
	h.validToken = "never"
	h.reply("POST /oauth2/token", 400, `{"error":"invalid_grant","error_description":"refresh_token is invalid or expired"}`)
	assert.Equal(t, 1, h.run("whoami"))
	assert.Equal(t, "error: session expired: run \"permahost login\" (invalid_grant: refresh_token is invalid or expired)\n", h.stderr.String())

	// Without a refresh token there is nothing to try.
	h.saveConfig(config.Config{Server: h.srv.URL, AccessToken: "at"})
	assert.Equal(t, 1, h.run("whoami"))
	assert.Equal(t, "error: session expired: run \"permahost login\"\n", h.stderr.String())

	// A server that rejects even fresh tokens gets the login hint.
	h = newHarness(t)
	h.validToken = "never"
	h.reply("POST /oauth2/token", 200, tokenJSON)
	assert.Equal(t, 1, h.run("whoami"))
	assert.Equal(t, "error: invalid token\nRun \"permahost login\" to sign in again.\n", h.stderr.String())
}

func TestSession_refreshNetworkError(t *testing.T) {
	h := newHarness(t)
	h.saveConfig(config.Config{Server: h.srv.URL, RefreshToken: "rt"})
	h.srv.Close()
	assert.Equal(t, 1, h.run("whoami"))
	assert.Contains(t, h.stderr.String(), "error: refresh session: ")
}

func TestSession_saveWarning(t *testing.T) {
	h := newHarness(t)
	h.saveConfig(config.Config{Server: h.srv.URL, RefreshToken: "rt"})
	h.reply("POST /oauth2/token", 200, tokenJSON)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 200, usageJSON)
	dir := filepath.Dir(h.env["PERMAHOSTIO_CONFIG"])
	require.NoError(t, os.Chmod(h.env["PERMAHOSTIO_CONFIG"], 0o400))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	assert.Equal(t, 0, h.run("whoami"))
	assert.Contains(t, h.stderr.String(), "warning: could not save the refreshed session")
}
