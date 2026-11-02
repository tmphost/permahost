// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mustafaturan/monoflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/controller/credit"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/service/encryption"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		MasterKey: "a-master-key-of-at-least-32-characters",
		App: AppConfig{
			BaseURL: "http://localhost:3000/", BasePath: "/base/",
			SettingsFile: filepath.Join(t.TempDir(), "settings.json"),
		},
		Auth: AuthConfig{
			JWTSecret:       "secret",
			RootAccessToken: "root-token",
			Google:          OAuthAppConfig{ClientID: "g", ClientSecret: "gs"},
			GitHub:          OAuthAppConfig{ClientID: "h", ClientSecret: "hs"},
		},
		SQLite: SQLiteConfig{
			Enabled: true, DSN: filepath.Join(t.TempDir(), "nested", "tmphost.db"),
			PoolConfig: PoolConfig{MaxIdleConns: 2, MaxOpenConns: 4, MaxConnLifetime: time.Hour},
		},
		Storage: StorageConfig{Driver: "local", LocalPath: filepath.Join(t.TempDir(), "files")},
	}
}

func get(t *testing.T, a *App, target string) (*http.Response, string) {
	t.Helper()
	res, err := a.Handler().Test(httptest.NewRequest(http.MethodGet, target, nil))
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(b)
}

func TestNew_WiresProvidersAndRoutes(t *testing.T) {
	a, err := New(testConfig(t), fstest.MapFS{})
	require.NoError(t, err)

	res, body := get(t, a, "/auth/config")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"providers":[{"id":"google","name":"Google","icon":""},{"id":"github","name":"GitHub","icon":""}]}`, body)

	res, _ = get(t, a, "/auth/github/login")
	assert.Equal(t, http.StatusFound, res.StatusCode)
	assert.Contains(t, res.Header.Get("Location"), "redirect_uri=http%3A%2F%2Flocalhost%3A3000%2Fauth%2Fgithub%2Fcallback")

	res, _ = get(t, a, "/api/v1/users/me")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func TestNew_NoProvidersConfigured(t *testing.T) {
	cfg := testConfig(t)
	cfg.Auth.Google, cfg.Auth.GitHub = OAuthAppConfig{}, OAuthAppConfig{}
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)
	_, body := get(t, a, "/auth/config")
	var got struct{ Providers []any }
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	assert.Empty(t, got.Providers)
}

func TestNew_Errors(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))
	garbage := filepath.Join(t.TempDir(), "garbage.db")
	require.NoError(t, os.WriteFile(garbage, []byte("this is definitely not a sqlite database file at all"), 0o600))

	cases := map[string]func(*Config){
		"missing secret":   func(c *Config) { c.Auth.JWTSecret = "" },
		"missing dsn":      func(c *Config) { c.SQLite.DSN = "" },
		"no master key":    func(c *Config) { c.MasterKey = "" },
		"short master key": func(c *Config) { c.MasterKey = "too-short" },
		"postgres dsn":     func(c *Config) { c.Postgres = PostgresConfig{Enabled: true} },
		"postgres down": func(c *Config) {
			c.Postgres = PostgresConfig{Enabled: true, DSN: "postgres://x:x@127.0.0.1:1/x?sslmode=disable"}
		},
		"bad settings file": func(c *Config) { c.App.SettingsFile = t.TempDir() },
		"mkdir fails":       func(c *Config) { c.SQLite.DSN = filepath.Join(notADir, "db", "x.db") },
		"open fails":        func(c *Config) { c.SQLite.DSN = t.TempDir() },
		"not a database":    func(c *Config) { c.SQLite.DSN = garbage },
		"storage driver":    func(c *Config) { c.Storage.Driver = "ftp" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig(t)
			mutate(&cfg)
			_, err := New(cfg, fstest.MapFS{})
			assert.Error(t, err)
		})
	}
}

func TestListenAndShutdown(t *testing.T) {
	cfg := testConfig(t)
	cfg.App.Port = 0
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- a.Listen() }()
	require.Eventually(t, func() bool { return a.fiber.Server() != nil }, time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, a.Shutdown(ctx))
	assert.NoError(t, <-done)
}

func TestSelectDialector(t *testing.T) {
	cfg := testConfig(t)
	cfg.Postgres = PostgresConfig{Enabled: true, DSN: "host=db", PoolConfig: PoolConfig{MaxOpenConns: 9}}
	d, pool, err := selectDialector(cfg)
	require.NoError(t, err)
	assert.Equal(t, "postgres", d.Name(), "postgres wins when both are enabled")
	assert.Equal(t, 9, pool.MaxOpenConns)

	cfg.Postgres.Enabled = false
	cfg.SQLite.DSN = "file::memory:"
	d, _, err = selectDialector(cfg)
	require.NoError(t, err)
	assert.Equal(t, "sqlite", d.Name())

	_, _, err = selectDialector(Config{})
	assert.ErrorContains(t, err, "no database enabled")
}

func TestRenewCredits(t *testing.T) {
	cfg := testConfig(t)
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	due := time.Now().UTC().Truncate(24 * time.Hour)
	require.NoError(t, a.db.Create(&model.User{ID: 1, Email: "a@x", Credits: 3, CreditsRenewAt: due}).Error)
	a.renewCredits()

	var u model.User
	require.NoError(t, a.db.First(&u, 1).Error)
	assert.Equal(t, credit.DefaultMonthlyGrant, u.Credits)
	assert.True(t, due.Add(credit.Period).Equal(u.CreditsRenewAt))

	// A failing run is logged, not fatal.
	sqlDB, err := a.db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	a.renewCredits()
}

func TestSetupFlow_SettingsApplyAndPersist(t *testing.T) {
	cfg := testConfig(t)
	cfg.Credits.MonthlyGrant = 0 // falls back to the default
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	login := httptest.NewRequest(http.MethodPost, "/api/v1/setup/login", strings.NewReader(`{"token":"root-token"}`))
	res, err := a.Handler().Test(login)
	require.NoError(t, err)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	root := res.Cookies()[0]

	req := httptest.NewRequest(http.MethodGet, "/api/v1/setup/settings", nil)
	req.AddCookie(root)
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	var got struct {
		Settings struct {
			Credits  struct{ MonthlyGrant int }
			Database struct{ Driver string }
		}
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
	assert.Equal(t, 250, got.Settings.Credits.MonthlyGrant)
	assert.Equal(t, "sqlite", got.Settings.Database.Driver)

	body := `{"settings":{"credits":{"monthlyGrant":500},"database":{"driver":"sqlite","sqlite":{"dsn":"` + cfg.SQLite.DSN + `"}},
		"domain":{"appUrl":"http://localhost:3000"},
		"storage":{"driver":"local","local":{"path":"` + cfg.Storage.LocalPath + `"}},"files":{"maxTtlMinutes":60,"maxUploadMb":1,"anonymousMaxTtlMinutes":10},
		"auth":{"github":{"clientId":"h"},"providers":[{"name":"Okta","icon":"<svg></svg>","clientId":"o","clientSecret":"okta-client-secret","authUrl":"https://okta.example.com"}]}}}`
	req = httptest.NewRequest(http.MethodPut, "/api/v1/setup/settings", strings.NewReader(body))
	req.AddCookie(root)
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	msg, _ := io.ReadAll(res.Body)
	require.Equal(t, http.StatusOK, res.StatusCode, string(msg))

	// The grant applies to the running server immediately...
	require.NoError(t, a.db.Create(&model.User{ID: 1, Email: "a@x", CreditsRenewAt: time.Now().Add(-time.Hour)}).Error)
	a.renewCredits()
	var u model.User
	require.NoError(t, a.db.First(&u, 1).Error)
	assert.Equal(t, 500, u.Credits)

	// ...and saved settings override the config on the next start.
	b, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)
	_, cfgBody := get(t, b, "/auth/config")
	assert.JSONEq(t, `{"providers":[{"id":"github","name":"GitHub","icon":""},{"id":"okta","name":"Okta","icon":"<svg></svg>"}]}`,
		cfgBody, "google cleared, github secret kept, okta added")

	// The file on disk is encrypted, and only the same master key opens it.
	raw, err := os.ReadFile(cfg.App.SettingsFile)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "monthlyGrant")
	assert.NotContains(t, string(raw), "okta-client-secret")
	// Quoted, so random base64 ciphertext can never match it by chance.
	assert.NotContains(t, string(raw), `"hs"`)
	cfg.MasterKey = "another-master-key-of-32-characters!"
	_, err = New(cfg, fstest.MapFS{})
	assert.ErrorIs(t, err, encryption.ErrDecrypt)
}

func TestSettingsFromConfig(t *testing.T) {
	cfg := testConfig(t)
	cfg.Credits.MonthlyGrant = 42
	cfg.Postgres = PostgresConfig{Enabled: true, DSN: "postgres://u:p@h:1/d"}
	cfg.S3 = S3Config{Endpoint: "e", Region: "r", Bucket: "b", AccessKeyID: "a", SecretAccessKey: "s", UsePathStyle: true}
	cfg.Storage = StorageConfig{Driver: "s3", LocalPath: "/files"}
	cfg.Files = FilesConfig{MaxTTLMinutes: 60, MaxUploadMB: 2, AnonymousMaxTTLMinutes: 15, Subdomains: true, PublicDomain: "tmp.site"}
	cfg.App.SitesDomain = "tmphost.io"
	cfg.TLS = TLSConfig{Enabled: true, Email: "e@x.io", Lego: LegoConfig{Enabled: true, CloudflareAPIToken: "cf"}}
	s := settingsFromConfig(cfg)
	assert.Equal(t, 443, s.TLS.Port, "default TLS port")
	assert.Equal(t, 42, s.Credits.MonthlyGrant)
	assert.Equal(t, "postgres", s.Database.Driver)

	defaults := settingsFromConfig(Config{})
	assert.Equal(t, "local", defaults.Storage.Driver)
	assert.Equal(t, "./_storage/files", defaults.Storage.Local.Path)
	assert.Equal(t, entity.FileSettings{MaxTTLMinutes: 7200 * 60, MaxUploadMB: 16, AnonymousMaxTTLMinutes: 10}, defaults.Files)
	withBase := settingsFromConfig(Config{App: AppConfig{BaseURL: "https://TmpHost.io:8443/x"}})
	assert.Equal(t, "tmphost.io:8443", withBase.Files.PublicDomain, "defaults to the base URL host")
	withSites := settingsFromConfig(Config{App: AppConfig{BaseURL: "https://app.tmphost.io", SitesDomain: "Sites.io"}})
	assert.Equal(t, "sites.io", withSites.Files.PublicDomain, "defaults to the sites domain")

	round := applySettings(Config{}, s)
	assert.Equal(t, cfg.Postgres.DSN, round.Postgres.DSN)
	assert.True(t, round.Postgres.Enabled)
	assert.False(t, round.SQLite.Enabled)
	assert.Equal(t, cfg.S3, round.S3)
	assert.Equal(t, cfg.Storage, round.Storage)
	assert.Equal(t, cfg.Files, round.Files)
	assert.Equal(t, cfg.Auth.Google, round.Auth.Google)
	assert.Equal(t, 42, round.Credits.MonthlyGrant)
	assert.Equal(t, cfg.App.BaseURL, round.App.BaseURL)
	assert.Equal(t, "tmphost.io", round.App.SitesDomain)
	cfg.TLS.Port = 443
	assert.Equal(t, cfg.TLS, round.TLS)

	cfg.TLS.Port = 8443
	assert.Equal(t, 8443, settingsFromConfig(cfg).TLS.Port)
}

func TestRootAccessToken(t *testing.T) {
	assert.Equal(t, "set", rootAccessToken(Config{Auth: AuthConfig{RootAccessToken: "set"}}))
	a, b := rootAccessToken(Config{}), rootAccessToken(Config{})
	assert.Len(t, a, 48)
	assert.NotEqual(t, a, b)
}

func TestSettingsFile(t *testing.T) {
	assert.Equal(t, "./_storage/settings.json", settingsFile(Config{}))
	assert.Equal(t, "/x.json", settingsFile(Config{App: AppConfig{SettingsFile: "/x.json"}}))
}

func TestOpenStorage(t *testing.T) {
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "none"))
	s, err := openStorage(Config{Storage: StorageConfig{Driver: "s3"}, S3: S3Config{Region: "auto", Bucket: "b", Endpoint: "http://127.0.0.1:1"}})
	require.NoError(t, err)
	assert.NotNil(t, s)

	notADir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notADir, nil, 0o600))
	_, err = openStorage(Config{Storage: StorageConfig{Driver: "local", LocalPath: filepath.Join(notADir, "x")}})
	assert.Error(t, err)
}

// TestOAuth runs the OAuth server end to end: discovery, registration,
// sign-in, consent, code exchange and refresh.
func TestOAuth(t *testing.T) {
	a, err := New(testConfig(t), fstest.MapFS{})
	require.NoError(t, err)
	require.NoError(t, a.db.Create(&model.User{ID: 7, Email: "a@x", Credits: 10, CreditsRenewAt: time.Now().Add(time.Hour)}).Error)

	req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	req.Header.Set("Origin", "https://inspector.example")
	res, err := a.Handler().Test(req)
	require.NoError(t, err)
	body, _ := io.ReadAll(res.Body)
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, string(body), `"token_endpoint":"http://localhost:3000/oauth2/token"`)
	assert.Equal(t, "*", res.Header.Get("Access-Control-Allow-Origin"))
	res, metadata := get(t, a, "/.well-known/oauth-protected-resource/mcp")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, metadata, `"resource":"http://localhost:3000/mcp"`)

	res, err = a.Handler().Test(httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(`{}`)))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Contains(t, res.Header.Get("WWW-Authenticate"), `resource_metadata="http://localhost:3000/.well-known/oauth-protected-resource/mcp"`)

	req = httptest.NewRequest(http.MethodPost, "/oauth2/register", strings.NewReader(`{"client_name":"Claude","redirect_uris":["http://127.0.0.1/callback"]}`))
	req.Header.Set("Content-Type", "application/json")
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, res.StatusCode)
	var client struct {
		ClientID string `json:"client_id"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&client))

	access := accessToken(t, a, client.ClientID)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode, "access tokens work on the API too")

	// Signed out, the authorize page sends the browser to sign in first.
	target := "/oauth2/authorize?response_type=code&client_id=00000000001&redirect_uri=http%3A%2F%2F127.0.0.1%3A9%2Fcallback" +
		"&code_challenge_method=S256&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	res, _ = get(t, a, target)
	assert.Equal(t, http.StatusFound, res.StatusCode)
	assert.Equal(t, "/base/login?redirect="+url.QueryEscape(target), res.Header.Get("Location"))
}

var _consentInput = regexp.MustCompile(`name="consent" value="([^"]+)"`)

// accessToken signs user 7 in, allows clientID on the consent page and
// exchanges the code, then refreshes once.
func accessToken(t *testing.T, a *App, clientID string) string {
	t.Helper()
	tokens, err := token.New(token.Config{Secret: "secret"})
	require.NoError(t, err)
	session, err := tokens.CreateSessionToken(monoflake.ID(7).String())
	require.NoError(t, err)
	const verifier = "a-pkce-verifier-that-is-long-enough-to-be-valid-0123"
	sum := sha256.Sum256([]byte(verifier))
	redirect := "http://127.0.0.1:8765/callback"

	authorize := "/oauth2/authorize?" + url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirect}, "state": {"st"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}.Encode()
	req := httptest.NewRequest(http.MethodGet, authorize, nil)
	req.AddCookie(&http.Cookie{Name: "tmphost_session", Value: session})
	res, err := a.Handler().Test(req)
	require.NoError(t, err)
	page, _ := io.ReadAll(res.Body)
	require.Equal(t, http.StatusOK, res.StatusCode, string(page))
	assert.Contains(t, res.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'")
	consent := _consentInput.FindStringSubmatch(string(page))
	require.Len(t, consent, 2)

	req = httptest.NewRequest(http.MethodPost, authorize, strings.NewReader(url.Values{"consent": {consent[1]}, "decision": {"allow"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "tmphost_session", Value: session})
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusSeeOther, res.StatusCode)
	back, err := url.Parse(res.Header.Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "st", back.Query().Get("state"))

	exchange := func(form url.Values) (string, string) {
		req := httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := a.Handler().Test(req)
		require.NoError(t, err)
		var out struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		require.Equal(t, http.StatusOK, res.StatusCode)
		require.NoError(t, json.NewDecoder(res.Body).Decode(&out))
		return out.AccessToken, out.RefreshToken
	}
	_, refresh := exchange(url.Values{
		"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "client_id": {clientID},
		"redirect_uri": {redirect}, "code_verifier": {verifier},
	})
	access, _ := exchange(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}})
	return access
}

// TestFileHosting runs the whole flow through the real stack: an OAuth
// access token, a folder, an upload, the public URL, the MCP tool and the
// sweep.
func TestFileHosting(t *testing.T) {
	cfg := testConfig(t)
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)
	require.NoError(t, a.db.Create(&model.User{ID: 7, Email: "a@x", Credits: 10, CreditsRenewAt: time.Now().Add(time.Hour)}).Error)
	key := accessToken(t, a, "00000000001")

	call := func(method, target, host, body string) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if host != "" {
			req.Host = host
		}
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := a.Handler().Test(req)
		require.NoError(t, err)
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	res, body := call(http.MethodPost, "/api/v1/folders", "", `{"ttlMinutes":60}`)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var created struct{ Folder struct{ ID, URL string } }
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	id := created.Folder.ID
	assert.Equal(t, "http://localhost:3000/files/7/"+id+"/", created.Folder.URL)

	res, body = call(http.MethodPut, "/api/v1/folders/"+id+"/files/site/index%20page.html", "", "<h1>hi</h1>")
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	assert.Contains(t, body, `"creditsSpent":2`)
	res, _ = call(http.MethodPut, "/api/v1/folders/"+id+"/files/1/2/3/4/5/6/x.txt", "", "x")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode, "deeper than 5 directories")

	res, body = call(http.MethodGet, "/files/7/"+id+"/site/index%20page.html", "", "")
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	assert.Equal(t, "<h1>hi</h1>", body)
	assert.Equal(t, "text/html; charset=utf-8", res.Header.Get("Content-Type"))
	assert.Contains(t, res.Header.Get("Content-Security-Policy"), "sandbox")
	res, _ = call(http.MethodGet, "/files/8/"+id+"/site/index%20page.html", "", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode, "the path names another owner")
	res, _ = call(http.MethodGet, "/files/7/"+id, "", "")
	assert.Equal(t, http.StatusMovedPermanently, res.StatusCode)
	assert.Equal(t, id+"/", res.Header.Get("Location"))
	res, _ = call(http.MethodGet, "/site/index%20page.html", "f"+id+".localhost:3000", "")
	assert.NotEqual(t, http.StatusOK, res.StatusCode, "no folder hosts without subdomains")

	res, body = call(http.MethodPut, "/api/v1/folders/"+id+"/files/site/index.html", "", "<h1>index</h1>")
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	res, body = call(http.MethodGet, "/files/7/"+id+"/site/", "", "")
	require.Equal(t, http.StatusOK, res.StatusCode, "a directory serves its index.html")
	assert.Equal(t, "<h1>index</h1>", body)

	res, body = call(http.MethodGet, "/api/v1/folders/"+id+"/files/site/index%20page.html", "", "")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "application/octet-stream", res.Header.Get("Content-Type"), "never rendered on the app origin")
	assert.Equal(t, "<h1>hi</h1>", body)

	mcpCall := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"file","arguments":{"action":"get","id":"` + id + `"}}}`
	req := httptest.NewRequest(http.MethodPost, MCPPath, strings.NewReader(mcpCall))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Protocol-Version", "2025-06-18")
	res, err = a.Handler().Test(req)
	require.NoError(t, err)
	b, _ := io.ReadAll(res.Body)
	require.Equal(t, http.StatusOK, res.StatusCode, string(b))
	assert.Contains(t, string(b), `"fileName":"index page.html"`)

	var u model.User
	require.NoError(t, a.db.First(&u, 7).Error)
	assert.Equal(t, 6, u.Credits, "two files, two blocks each")

	// Once expired, the sweep removes the record and the files.
	require.NoError(t, a.db.Model(&model.Folder{}).Where("1 = 1").Update("expires_at", time.Now().Add(-time.Second)).Error)
	a.sweepFolders()
	var n int64
	require.NoError(t, a.db.Model(&model.Folder{}).Count(&n).Error)
	assert.Zero(t, n)
	entries, err := os.ReadDir(filepath.Join(cfg.Storage.LocalPath, "u7"))
	require.NoError(t, err)
	assert.Empty(t, entries)

	a.sweepFolders() // nothing to do
	// A failing run is logged, not fatal.
	sqlDB, err := a.db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	a.sweepFolders()
}

// TestFileHosting_Subdomains serves folders on their own hosts.
func TestFileHosting_Subdomains(t *testing.T) {
	cfg := testConfig(t)
	cfg.Files = FilesConfig{Subdomains: true}
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)
	require.NoError(t, a.db.Create(&model.User{ID: 7, Email: "a@x", Credits: 10, CreditsRenewAt: time.Now().Add(time.Hour)}).Error)
	key := accessToken(t, a, "00000000001")

	call := func(method, target, host, body string) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host = host
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := a.Handler().Test(req)
		require.NoError(t, err)
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}
	res, body := call(http.MethodPost, "/api/v1/folders", "localhost:3000", `{}`)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var created struct{ Folder struct{ ID, URL string } }
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	id := created.Folder.ID
	assert.Len(t, id, 11)
	assert.Equal(t, "http://f"+id+".localhost:3000/", created.Folder.URL)

	res, body = call(http.MethodPut, "/api/v1/folders/"+id+"/files/index.html", "localhost:3000", "<p>x</p>")
	require.Equal(t, http.StatusOK, res.StatusCode, body)

	res, body = call(http.MethodGet, "/", "f"+id+".localhost:3000", "")
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	assert.Equal(t, "<p>x</p>", body)
	res, _ = call(http.MethodGet, "/", "f00000000001.localhost:3000", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode, "no such folder")
	_, body = call(http.MethodGet, "/files/7/"+id+"/index.html", "localhost:3000", "")
	assert.NotEqual(t, "<p>x</p>", body, "no folder paths with subdomains")
}

// TestAnonymousUploads uploads from the web app without signing in, from
// behind a trusted proxy.
func TestAnonymousUploads(t *testing.T) {
	cfg := testConfig(t)
	cfg.App.TrustedProxies = " 0.0.0.0 , 10.0.0.0/8,"
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	call := func(method, target, ip, origin, body string) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		// A spoofed hop on the left is ignored: the client IP is the
		// rightmost one that is not a trusted proxy.
		req.Header.Set("X-Forwarded-For", "192.0.2.99, "+ip+", 10.0.0.1")
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := a.Handler().Test(req)
		require.NoError(t, err)
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}
	const web = "http://localhost:3000"

	res, _ := call(http.MethodPost, "/api/v1/folders", "203.0.113.7", "", "")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode, "not from the web app, e.g. the CLI")
	res, _ = call(http.MethodPost, "/api/v1/folders", "203.0.113.7", "https://evil.example", "")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)

	res, body := call(http.MethodGet, "/api/v1/limits", "203.0.113.7", web, "")
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	assert.JSONEq(t, `{"defaultTtlMinutes":10,"maxTtlMinutes":10,"maxUploadBytes":4194304,"billingBlockMinutes":30,"billingUnitBytes":1048576,"anonymous":true,"fileOps":5,"fileOpsWindowMinutes":30}`, body)

	res, body = call(http.MethodPost, "/api/v1/folders", "203.0.113.7", web, `{"ttlMinutes":11}`)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode, body)
	res, body = call(http.MethodPost, "/api/v1/folders", "203.0.113.7", web, "")
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var created struct{ Folder struct{ ID, URL string } }
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	user := idgen.FormatBase36(idgen.AnonymousUserID("203.0.113.7"))
	assert.Equal(t, web+"/files/"+user+"/"+created.Folder.ID+"/", created.Folder.URL)
	files := "/api/v1/folders/" + created.Folder.ID + "/files/"

	res, _ = call(http.MethodPut, files+"big.bin", "203.0.113.7", web, strings.Repeat("x", 4<<20+1))
	assert.Equal(t, http.StatusRequestEntityTooLarge, res.StatusCode)
	res, _ = call(http.MethodPut, files+"a.txt", "203.0.113.8", web, "hi")
	assert.Equal(t, http.StatusNotFound, res.StatusCode, "another IP is another user")
	for i := range 5 {
		res, body = call(http.MethodPut, files+strconv.Itoa(i)+".txt", "203.0.113.7", web, "hi")
		require.Equal(t, http.StatusOK, res.StatusCode, body)
		assert.Contains(t, body, `"creditsSpent":0`)
	}
	res, body = call(http.MethodPut, files+"5.txt", "203.0.113.7", web, "hi")
	assert.Equal(t, http.StatusTooManyRequests, res.StatusCode, body)

	res, body = call(http.MethodGet, "/files/"+user+"/"+created.Folder.ID+"/4.txt", "198.51.100.1", "", "")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "hi", body, "anyone can download")
	res, _ = call(http.MethodGet, "/api/v1/folders", "203.0.113.7", web, "")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode, "listing needs a sign-in")
}

func TestFiberConfig(t *testing.T) {
	cfg := fiberConfig(AppConfig{}, 10)
	assert.False(t, cfg.TrustProxy)
	assert.Equal(t, 10, cfg.BodyLimit)

	cfg = fiberConfig(AppConfig{TrustedProxies: "10.0.0.0/8, 127.0.0.1", ProxyHeader: "CF-Connecting-IP"}, 10)
	assert.True(t, cfg.TrustProxy)
	assert.Equal(t, []string{"10.0.0.0/8", "127.0.0.1"}, cfg.TrustProxyConfig.Proxies)
	assert.Equal(t, "CF-Connecting-IP", cfg.ProxyHeader)
	assert.True(t, cfg.EnableIPValidation)

	assert.Equal(t, "X-Forwarded-For", fiberConfig(AppConfig{TrustedProxies: "10.0.0.1"}, 10).ProxyHeader)
}
