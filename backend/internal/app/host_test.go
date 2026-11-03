// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isCustom reports the verified custom domain the tests know.
func isCustom(host string) bool { return host == "www.custom.io" }

func TestHostsClassify(t *testing.T) {
	h := newHosts("https://App.Example.com", "Example.com", "MCP.example.com", isCustom)
	assert.False(t, h.open)
	cases := map[string]hostKind{
		"app.example.com":                hostApp,
		"APP.example.com:443":            hostApp,
		"localhost:2027":                 hostApp,
		"127.0.0.1":                      hostApp,
		"[::1]:2027":                     hostApp,
		"0123abcd.file.example.com":      hostSite,
		"0123abcd.file.example.com:8443": hostSite,
		"0123ABCD.FILE.EXAMPLE.COM":      hostSite,
		"0123abc.file.example.com":       hostUnknown, // 7 characters
		"0123-bcd.file.example.com":      hostUnknown,
		"0123abcd.example.com":           hostUnknown, // not under file.
		"0123abcd.a.file.example.com":    hostUnknown,
		"file.example.com":               hostUnknown,
		"example.com":                    hostUnknown,
		"evil.com":                       hostUnknown,
		"www.custom.io":                  hostDomain,
		"WWW.custom.io:443":              hostDomain,
		"mcp.example.com":                hostMCP,
		"MCP.example.com:8443":           hostMCP,
	}
	for host, want := range cases {
		got, id := h.classify(host)
		assert.Equal(t, want, got, host)
		if want == hostSite {
			assert.Equal(t, "0123abcd", id)
		}
	}
	// Folders made before IDs shrank to 8 characters keep their 11.
	kind, id := h.classify("00100000000.file.example.com")
	assert.Equal(t, hostSite, kind)
	assert.Equal(t, "00100000000", id)
	kind, _ = h.classify("000000000rs.file.example.com")
	assert.Equal(t, hostUnknown, kind, "a new ID padded to 11 characters")

	// An app URL on this machine or an IP answers any host, as in
	// development; dynamic subdomains still win.
	for _, appURL := range []string{"http://localhost:2027", "http://192.168.1.2:2027"} {
		open := newHosts(appURL, "example.com", "", isCustom)
		assert.True(t, open.open)
		kind, _ := open.classify("anything.lan")
		assert.Equal(t, hostApp, kind)
		kind, _ = open.classify("www.custom.io")
		assert.Equal(t, hostDomain, kind, "custom domains still work")
		kind, _ = open.classify("0123abcd.file.example.com")
		assert.Equal(t, hostSite, kind)
	}

	assert.True(t, h.isSite("0123abcd.file.example.com"))
	assert.False(t, h.isSite("app.example.com"))
	assert.True(t, h.isDomain("www.custom.io"))
	assert.False(t, h.isDomain("app.example.com"))
	assert.True(t, h.isMCP("mcp.example.com"))
	assert.False(t, h.isMCP("app.example.com"))

	// No sites domain, no dynamic subdomains.
	kind, _ = newHosts("https://app.example.com", "", "", isCustom).classify("0123abcd.file.example.com")
	assert.Equal(t, hostUnknown, kind)
}

func TestHostRouter(t *testing.T) {
	cfg := testConfig(t)
	cfg.App.BaseURL = "https://app.example.com"
	cfg.App.SitesDomain = "example.com"
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	request := func(host string) (*http.Response, string) {
		req := httptest.NewRequest(http.MethodGet, "/auth/config", nil)
		req.Host = host
		res, err := a.Handler().Test(req)
		require.NoError(t, err)
		b := make([]byte, 256)
		n, _ := res.Body.Read(b)
		return res, string(b[:n])
	}

	res, _ := request("app.example.com")
	assert.Equal(t, http.StatusOK, res.StatusCode)

	res, body := request("0123abcd.file.example.com")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.JSONEq(t, `{"error":"site not found"}`, body)

	res, body = request("evil.com")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, fiber.MIMEApplicationJSON, res.Header.Get(fiber.HeaderContentType))
	assert.JSONEq(t, `{"error":"host not allowed"}`, body)

	// Without an MCP domain, no host serves only MCP.
	res, _ = request("mcp.example.com")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	// An https app URL makes the cookies Secure.
	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.Host = "app.example.com"
	res, err = a.Handler().Test(logout)
	require.NoError(t, err)
	require.NotEmpty(t, res.Cookies())
	assert.True(t, res.Cookies()[0].Secure)
}

func TestHTTPSRedirect(t *testing.T) {
	h := newHosts("https://app.example.com", "example.com", "mcp.example.com", isCustom)
	cases := []struct {
		host string
		port int
		want string
	}{
		{"app.example.com", 443, "https://app.example.com/x?y=1"},
		{"app.example.com:2027", 8443, "https://app.example.com:8443/x?y=1"},
		{"0123abcd.file.example.com", 443, "https://0123abcd.file.example.com/x?y=1"},
		{"www.custom.io", 443, "https://www.custom.io/x?y=1"},
		{"mcp.example.com", 443, "https://mcp.example.com/x?y=1"},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/x?y=1", nil)
		req.Host = tc.host
		res, err := httpsRedirect(h, tc.port).Test(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusMovedPermanently, res.StatusCode)
		assert.Equal(t, tc.want, res.Header.Get("Location"))
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "evil.com"
	res, err := httpsRedirect(h, 443).Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestMCPHost(t *testing.T) {
	cfg := testConfig(t)
	cfg.App.BaseURL = "https://app.example.com:8443"
	cfg.App.SitesDomain = "example.com"
	cfg.App.MCPDomain = "mcp.example.com"
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)

	request := func(method, host, path string) (*http.Response, string) {
		req := httptest.NewRequest(method, path, nil)
		req.Host = host
		res, err := a.Handler().Test(req)
		require.NoError(t, err)
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	// The MCP host serves the endpoint and its OAuth metadata only. It is
	// an issuer of its own; users sign in on the app host.
	res, body := request(http.MethodGet, "mcp.example.com:8443", "/.well-known/oauth-protected-resource/mcp")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"resource":"https://mcp.example.com:8443/mcp"`)
	assert.Contains(t, body, `"authorization_servers":["https://mcp.example.com:8443"]`)
	res, body = request(http.MethodGet, "mcp.example.com:8443", "/.well-known/oauth-authorization-server")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"issuer":"https://mcp.example.com:8443"`)
	assert.Contains(t, body, `"authorization_endpoint":"https://app.example.com:8443/oauth2/mcp/authorize"`)
	assert.Contains(t, body, `"token_endpoint":"https://mcp.example.com:8443/oauth2/token"`)
	assert.Contains(t, body, `"registration_endpoint":"https://mcp.example.com:8443/oauth2/register"`)
	res, _ = request(http.MethodPost, "mcp.example.com:8443", "/oauth2/register")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode, "register is served")
	res, _ = request(http.MethodPost, "mcp.example.com:8443", "/oauth2/token")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode, "token is served")
	res, _ = request(http.MethodPost, "mcp.example.com:8443", "/mcp")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Contains(t, res.Header.Get("WWW-Authenticate"), "https://mcp.example.com:8443/.well-known/oauth-protected-resource/mcp")
	for _, path := range []string{"/", "/auth/config", "/api/v1/limits", "/oauth2/authorize", "/oauth2/mcp/authorize", "/files/x/y/"} {
		res, body = request(http.MethodGet, "mcp.example.com:8443", path)
		assert.Equal(t, http.StatusNotFound, res.StatusCode, path)
		assert.JSONEq(t, `{"error":"not found"}`, body, path)
	}

	// The app host keeps serving MCP, with its own resource and issuer.
	res, body = request(http.MethodGet, "app.example.com:8443", "/.well-known/oauth-protected-resource/mcp")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"resource":"https://app.example.com:8443/mcp"`)
	assert.Contains(t, body, `"authorization_servers":["https://app.example.com:8443"]`)
	res, _ = request(http.MethodPost, "app.example.com:8443", "/mcp")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	assert.Contains(t, res.Header.Get("WWW-Authenticate"), "https://app.example.com:8443/.well-known/oauth-protected-resource/mcp")

	// The web app learns the MCP URL from /auth/config.
	res, body = request(http.MethodGet, "app.example.com:8443", "/auth/config")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"mcpUrl":"https://mcp.example.com:8443/mcp"`)
}

func TestMCPEndpointURL(t *testing.T) {
	assert.Empty(t, mcpEndpointURL("https://app.example.com", ""))
	assert.Empty(t, mcpEndpointURL("://bad", "mcp.example.com"))
	assert.Equal(t, "https://mcp.example.com/mcp", mcpEndpointURL("https://app.example.com", "MCP.example.com"))
	assert.Equal(t, "http://mcp.example.com:2027/mcp", mcpEndpointURL("http://app.example.com:2027", "mcp.example.com"))
}
