// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHostsClassify(t *testing.T) {
	h := newHosts("https://App.Example.com", "Example.com")
	assert.False(t, h.open)
	cases := map[string]hostKind{
		"app.example.com":               hostApp,
		"APP.example.com:443":           hostApp,
		"localhost:2027":                hostApp,
		"127.0.0.1":                     hostApp,
		"[::1]:2027":                    hostApp,
		"f0123456789a.example.com":      hostSite,
		"f0123456789a.example.com:8443": hostSite,
		"F0123456789A.EXAMPLE.COM":      hostSite,
		"f0123456789.example.com":       hostUnknown, // 10 characters
		"g0123456789a.example.com":      hostUnknown,
		"f0123456789a.a.example.com":    hostUnknown,
		"example.com":                   hostUnknown,
		"evil.com":                      hostUnknown,
	}
	for host, want := range cases {
		got, id := h.classify(host)
		assert.Equal(t, want, got, host)
		if want == hostSite {
			assert.Equal(t, "f0123456789a", id)
		}
	}

	// An app URL on this machine or an IP answers any host, as in
	// development; dynamic subdomains still win.
	for _, appURL := range []string{"http://localhost:2027", "http://192.168.1.2:2027"} {
		open := newHosts(appURL, "example.com")
		assert.True(t, open.open)
		kind, _ := open.classify("anything.lan")
		assert.Equal(t, hostApp, kind)
		kind, _ = open.classify("f0123456789a.example.com")
		assert.Equal(t, hostSite, kind)
	}

	assert.True(t, h.isSite("f0123456789a.example.com"))
	assert.False(t, h.isSite("app.example.com"))

	// No sites domain, no dynamic subdomains.
	kind, _ := newHosts("https://app.example.com", "").classify("f0123456789a.example.com")
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

	res, body := request("f0123456789a.example.com")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.JSONEq(t, `{"error":"site not found"}`, body)

	res, body = request("evil.com")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, fiber.MIMEApplicationJSON, res.Header.Get(fiber.HeaderContentType))
	assert.JSONEq(t, `{"error":"host not allowed"}`, body)

	// An https app URL makes the cookies Secure.
	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.Host = "app.example.com"
	res, err = a.Handler().Test(logout)
	require.NoError(t, err)
	require.NotEmpty(t, res.Cookies())
	assert.True(t, res.Cookies()[0].Secure)
}

func TestHTTPSRedirect(t *testing.T) {
	h := newHosts("https://app.example.com", "example.com")
	cases := []struct {
		host string
		port int
		want string
	}{
		{"app.example.com", 443, "https://app.example.com/x?y=1"},
		{"app.example.com:2027", 8443, "https://app.example.com:8443/x?y=1"},
		{"f0123456789a.example.com", 443, "https://f0123456789a.example.com/x?y=1"},
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
