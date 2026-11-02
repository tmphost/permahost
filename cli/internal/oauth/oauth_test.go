// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// tokenServer answers /oauth2/token with status and body and records the
// last form it received.
func tokenServer(t *testing.T, status int, body string) (*Client, *url.Values) {
	t.Helper()
	var form url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth2/token", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		form = r.PostForm
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return &Client{Server: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}, &form
}

const tokenJSON = `{"access_token":"at","token_type":"Bearer","expires_in":3600,"refresh_token":"rt"}`

func TestExchange(t *testing.T) {
	c, form := tokenServer(t, 200, tokenJSON)
	tok, err := c.Exchange(context.Background(), "code1", "http://127.0.0.1:1/callback", "ver")
	require.NoError(t, err)
	assert.Equal(t, &Token{AccessToken: "at", RefreshToken: "rt", Expiry: now.Add(time.Hour)}, tok)
	assert.Equal(t, url.Values{
		"grant_type": {"authorization_code"}, "code": {"code1"}, "client_id": {ClientID},
		"redirect_uri": {"http://127.0.0.1:1/callback"}, "code_verifier": {"ver"},
	}, *form)
}

func TestRefresh(t *testing.T) {
	c, form := tokenServer(t, 200, `{"access_token":"at2","token_type":"bearer","expires_in":60,"refresh_token":"rt2"}`)
	tok, err := c.Refresh(context.Background(), "rt1")
	require.NoError(t, err)
	assert.Equal(t, &Token{AccessToken: "at2", RefreshToken: "rt2", Expiry: now.Add(time.Minute)}, tok)
	assert.Equal(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"rt1"}, "client_id": {ClientID}}, *form)
}

func TestTokenErrors(t *testing.T) {
	ctx := context.Background()

	c, _ := tokenServer(t, 400, `{"error":"invalid_grant","error_description":"refresh_token is invalid or expired"}`)
	_, err := c.Refresh(ctx, "rt")
	var oe *Error
	require.ErrorAs(t, err, &oe)
	assert.EqualError(t, err, "invalid_grant: refresh_token is invalid or expired")

	c, _ = tokenServer(t, 400, `{"error":"invalid_request"}`)
	_, err = c.Refresh(ctx, "rt")
	assert.EqualError(t, err, "invalid_request")

	c, _ = tokenServer(t, 502, `<html>`)
	_, err = c.Refresh(ctx, "rt")
	assert.EqualError(t, err, "server_error: token endpoint responded 502 Bad Gateway")

	c, _ = tokenServer(t, 200, `nope`)
	_, err = c.Refresh(ctx, "rt")
	assert.ErrorContains(t, err, "decode token response")

	c, _ = tokenServer(t, 200, `{"access_token":"at","token_type":"mac"}`)
	_, err = c.Refresh(ctx, "rt")
	assert.EqualError(t, err, "token endpoint returned no bearer access token")

	_, err = (&Client{Server: "://bad", HTTP: http.DefaultClient}).Refresh(ctx, "rt")
	assert.Error(t, err)

	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	_, err = (&Client{Server: srv.URL, HTTP: http.DefaultClient}).Refresh(ctx, "rt")
	assert.Error(t, err)
}

func TestAuthorizeURL(t *testing.T) {
	c := &Client{Server: "https://tmphost.io"}
	u, err := url.Parse(c.AuthorizeURL("http://127.0.0.1:5/callback", "chal", "st"))
	require.NoError(t, err)
	assert.Equal(t, "https://tmphost.io/oauth2/authorize", u.Scheme+"://"+u.Host+u.Path)
	assert.Equal(t, url.Values{
		"response_type": {"code"}, "client_id": {ClientID}, "redirect_uri": {"http://127.0.0.1:5/callback"},
		"code_challenge": {"chal"}, "code_challenge_method": {"S256"}, "state": {"st"},
	}, u.Query())
}

func TestChallenge(t *testing.T) {
	// RFC 7636 appendix B.
	assert.Equal(t, "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM", Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"))
	v := randomString()
	assert.Len(t, v, 43)
	assert.NotEqual(t, v, randomString())
}

// browser simulates the user approving in a browser: it checks the
// authorize URL and follows the redirect with the given query.
func browser(t *testing.T, query func(authQuery url.Values) url.Values) func(string) error {
	return func(raw string) error {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		q := u.Query()
		redirect, err := url.Parse(q.Get("redirect_uri"))
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", redirect.Hostname())
		assert.Equal(t, "/callback", redirect.Path)
		go func() {
			resp, err := http.Get(redirect.String() + "?" + query(q).Encode())
			if assert.NoError(t, err) {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLogin(t *testing.T) {
	var challenge, verifier string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		verifier = r.PostForm.Get("code_verifier")
		assert.Equal(t, "the-code", r.PostForm.Get("code"))
		_, _ = io.WriteString(w, tokenJSON)
	}))
	defer srv.Close()
	c := &Client{Server: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}

	var out bytes.Buffer
	tok, err := c.Login(context.Background(), LoginOptions{
		Out:     &out,
		Timeout: 10 * time.Second,
		Open: browser(t, func(q url.Values) url.Values {
			challenge = q.Get("code_challenge")
			return url.Values{"code": {"the-code"}, "state": {q.Get("state")}, "iss": {srv.URL}}
		}),
	})
	require.NoError(t, err)
	assert.Equal(t, "at", tok.AccessToken)
	assert.Equal(t, Challenge(verifier), challenge, "PKCE verifier matches the challenge")
	assert.Contains(t, out.String(), srv.URL+"/oauth2/authorize?")
}

func TestLogin_callbackErrors(t *testing.T) {
	c := &Client{Server: "http://unused", HTTP: http.DefaultClient}
	for name, tc := range map[string]struct {
		query func(url.Values) url.Values
		err   string
	}{
		"state": {func(url.Values) url.Values { return url.Values{"code": {"c"}, "state": {"forged"}} }, "wrong state"},
		"error": {func(q url.Values) url.Values {
			return url.Values{"error": {"access_denied"}, "error_description": {"user said no"}, "state": {q.Get("state")}}
		}, "sign-in failed: access_denied: user said no"},
		"code": {func(q url.Values) url.Values { return url.Values{"state": {q.Get("state")}} }, "no code"},
	} {
		_, err := c.Login(context.Background(), LoginOptions{Out: io.Discard, Timeout: 10 * time.Second, Open: browser(t, tc.query)})
		assert.ErrorContains(t, err, tc.err, name)
	}
}

func TestCallbackHandler(t *testing.T) {
	out := make(chan result, 1)
	h := callbackHandler("st", out)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/callback?code=c&state=st", nil))
	assert.Equal(t, 200, rec.Code)
	assert.Contains(t, rec.Body.String(), "You can close this tab.")

	// A second hit gets a page but does not block or replace the first.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/callback?state=x", nil))
	assert.Equal(t, 400, rec.Code)
	assert.Contains(t, rec.Body.String(), "Sign-in failed")
	assert.Equal(t, result{code: "c"}, <-out)
}

func TestLogin_browserFailsAndTimeout(t *testing.T) {
	var out bytes.Buffer
	c := &Client{Server: "http://unused"}
	_, err := c.Login(context.Background(), LoginOptions{
		Out:     &out,
		Timeout: 50 * time.Millisecond,
		Open:    func(string) error { return errors.New("no display") },
	})
	assert.EqualError(t, err, "timed out after 50ms waiting for the browser sign-in")
	assert.Contains(t, out.String(), "Could not open the browser: no display")
}

func TestLogin_canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{Server: "http://unused"}
	_, err := c.Login(ctx, LoginOptions{Out: io.Discard, Timeout: time.Minute, Open: func(string) error { cancel(); return nil }})
	assert.EqualError(t, err, "login canceled")
}

func TestLogin_listenError(t *testing.T) {
	orig := listen
	listen = func(string, string) (net.Listener, error) { return nil, errors.New("no sockets") }
	defer func() { listen = orig }()
	_, err := (&Client{}).Login(context.Background(), LoginOptions{})
	assert.EqualError(t, err, "listen for the login callback: no sockets")
}

func TestBrowserCommand(t *testing.T) {
	for goos, want := range map[string][]string{
		"darwin":  {"open", "u"},
		"windows": {"rundll32", "url.dll,FileProtocolHandler", "u"},
		"linux":   {"xdg-open", "u"},
	} {
		assert.Equal(t, want, browserCommand(goos, "u").Args, goos)
	}

	// With an empty PATH no opener is found, so nothing is launched.
	t.Setenv("PATH", "")
	var execErr *exec.Error
	assert.ErrorAs(t, OpenBrowser("http://example.invalid"), &execErr)
	assert.True(t, strings.Contains(execErr.Error(), "executable file not found"))
}
