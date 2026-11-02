// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	oauthctrl "github.com/tmphost/tmphost/backend/internal/controller/oauthserver"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// send makes a request with an optional session cookie and content type.
func (e *filesEnv) send(t *testing.T, method, target, contentType, body, session string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if session != "" {
		req.AddCookie(&http.Cookie{Name: mapper.SessionCookie, Value: session})
	}
	res, err := e.app.Test(req)
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(b)
}

func (e *filesEnv) signedIn() {
	e.auth.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{SessionToken: "s"}).Return(&entity.AuthenticateResponse{UserID: 7}, nil)
}

func TestOAuth_Metadata(t *testing.T) {
	e := newFilesApp(t)
	e.oauth.EXPECT().GetServerMetadata(gomock.Any()).Return(&entity.GetOAuthServerMetadataResponse{Issuer: "https://t.io"})
	res, body := e.send(t, http.MethodGet, ServerMetadataPath, "", "", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"issuer":"https://t.io"`)

	e.oauth.EXPECT().GetProtectedResourceMetadata(gomock.Any()).Return(&entity.GetProtectedResourceMetadataResponse{Resource: "https://t.io/mcp"}).Times(2)
	for _, path := range []string{ResourceMetadataPath, ResourceMetadataPath + "/mcp"} {
		res, body = e.send(t, http.MethodGet, path, "", "", "")
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Contains(t, body, `"resource":"https://t.io/mcp"`)
	}

	req := httptest.NewRequest(http.MethodOptions, oauthctrl.TokenPath, nil)
	req.Header.Set("Origin", "https://inspector.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	res, err := e.app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, res.StatusCode, "preflight")
	assert.Equal(t, "*", res.Header.Get("Access-Control-Allow-Origin"))
}

func TestOAuth_Register(t *testing.T) {
	e := newFilesApp(t)
	const body = `{"client_name":"C","redirect_uris":["http://127.0.0.1/cb"]}`
	e.oauth.EXPECT().RegisterClient(gomock.Any(), &entity.RegisterClientRequest{ClientName: "C", RedirectURIs: []string{"http://127.0.0.1/cb"}}).
		Return(&entity.RegisterClientResponse{ClientID: "cid", ClientName: "C", RedirectURIs: []string{"http://127.0.0.1/cb"}, IssuedAt: time.Unix(5, 0)}, nil)
	res, out := e.send(t, http.MethodPost, oauthctrl.RegisterPath, "application/json", body, "")
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	assert.Contains(t, out, `"client_id":"cid"`)

	res, out = e.send(t, http.MethodPost, oauthctrl.RegisterPath, "application/json", `nope`, "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.JSONEq(t, `{"error":"invalid_client_metadata","error_description":"body must be JSON client metadata"}`, out)

	for _, tc := range []struct {
		err    error
		status int
		want   string
	}{
		{fmt.Errorf("%w: http is only allowed for localhost", oauthctrl.ErrInvalidRedirectURI), http.StatusBadRequest,
			`{"error":"invalid_redirect_uri","error_description":"http is only allowed for localhost"}`},
		{fmt.Errorf("%w: unsupported grant type", oauthctrl.ErrInvalidClientMetadata), http.StatusBadRequest,
			`{"error":"invalid_client_metadata","error_description":"unsupported grant type"}`},
		{errBoom, http.StatusInternalServerError, `{"error":"server_error"}`},
	} {
		e.oauth.EXPECT().RegisterClient(gomock.Any(), gomock.Any()).Return(nil, tc.err)
		res, out = e.send(t, http.MethodPost, oauthctrl.RegisterPath, "application/json", body, "")
		assert.Equal(t, tc.status, res.StatusCode)
		assert.JSONEq(t, tc.want, out)
	}
}

func TestOAuth_Authorize(t *testing.T) {
	const target = "/oauth2/authorize?client_id=c&response_type=code&state=s"

	t.Run("consent page", func(t *testing.T) {
		e := newFilesApp(t)
		e.signedIn()
		e.oauth.EXPECT().Authorize(gomock.Any(), &entity.AuthorizeRequest{UserID: 7, ClientID: "c", ResponseType: "code", State: "s"}).
			Return(&entity.AuthorizeResponse{Consent: &entity.Consent{ClientName: "<b>Evil</b>", Destination: "app.example", Email: "a@b.c", Token: "ct"}}, nil)
		res, body := e.send(t, http.MethodGet, target, "", "", "s")
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, _pageCSP, res.Header.Get("Content-Security-Policy"))
		assert.Equal(t, "DENY", res.Header.Get("X-Frame-Options"))
		assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
		assert.Contains(t, body, "&lt;b&gt;Evil&lt;/b&gt;", "escaped")
		assert.Contains(t, body, `name="consent" value="ct"`)
		assert.Contains(t, body, `data-test="consent-unverified"`)
		assert.Contains(t, body, "app.example")
		assert.Contains(t, body, "a@b.c")
		assert.Contains(t, body, `value="allow"`)
		assert.Contains(t, body, "<script>"+_themeScript+"</script>", "the script the CSP hash allows, byte for byte")
		sum := sha256.Sum256([]byte(_themeScript))
		assert.Contains(t, res.Header.Get("Content-Security-Policy"), "script-src 'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"';")
	})

	t.Run("verified, unnamed", func(t *testing.T) {
		e := newFilesApp(t)
		e.oauth.EXPECT().Authorize(gomock.Any(), gomock.Any()).
			Return(&entity.AuthorizeResponse{Consent: &entity.Consent{Verified: true, Token: "ct"}}, nil)
		_, body := e.send(t, http.MethodGet, target, "", "", "")
		assert.Contains(t, body, `data-test="consent-verified"`)
		assert.Contains(t, body, "An unnamed app")
	})

	t.Run("signed out", func(t *testing.T) {
		e := newFilesApp(t)
		e.auth.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{SessionToken: "old"}).Return(nil, authctrl.ErrUnauthorized)
		e.oauth.EXPECT().Authorize(gomock.Any(), &entity.AuthorizeRequest{ClientID: "c", ResponseType: "code", State: "s"}).Return(nil, oauthctrl.ErrLoginRequired)
		res, _ := e.send(t, http.MethodGet, target, "", "", "old")
		assert.Equal(t, http.StatusFound, res.StatusCode)
		assert.Equal(t, "/base/login?redirect=%2Foauth2%2Fauthorize%3Fclient_id%3Dc%26response_type%3Dcode%26state%3Ds", res.Header.Get("Location"))
	})

	t.Run("error back to the client", func(t *testing.T) {
		e := newFilesApp(t)
		e.oauth.EXPECT().Authorize(gomock.Any(), gomock.Any()).Return(&entity.AuthorizeResponse{RedirectURL: "http://127.0.0.1/cb?error=x"}, nil)
		res, _ := e.send(t, http.MethodGet, target, "", "", "")
		assert.Equal(t, http.StatusFound, res.StatusCode)
		assert.Equal(t, "http://127.0.0.1/cb?error=x", res.Header.Get("Location"))
	})

	t.Run("no client", func(t *testing.T) {
		e := newFilesApp(t)
		res, body := e.send(t, http.MethodGet, "/oauth2/authorize", "", "", "")
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Contains(t, body, "no client_id")
	})

	for name, tc := range map[string]struct {
		err    error
		status int
		text   string
	}{
		"unknown client": {oauthctrl.ErrInvalidClient, http.StatusBadRequest, "not registered"},
		"bad redirect":   {oauthctrl.ErrInvalidRedirectURI, http.StatusBadRequest, "did not register"},
		"failure":        {errBoom, http.StatusInternalServerError, "Something went wrong"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newFilesApp(t)
			e.oauth.EXPECT().Authorize(gomock.Any(), gomock.Any()).Return(nil, tc.err)
			res, body := e.send(t, http.MethodGet, target, "", "", "")
			assert.Equal(t, tc.status, res.StatusCode)
			assert.Contains(t, body, tc.text)
			assert.Contains(t, body, `data-test="consent-error"`)
		})
	}
}

func TestOAuth_Consent(t *testing.T) {
	const (
		target = "/oauth2/authorize?client_id=c"
		form   = "application/x-www-form-urlencoded"
	)

	t.Run("allow", func(t *testing.T) {
		e := newFilesApp(t)
		e.signedIn()
		e.oauth.EXPECT().Consent(gomock.Any(), &entity.ConsentRequest{UserID: 7, Token: "ct", Allow: true}).
			Return(&entity.ConsentResponse{RedirectURL: "http://127.0.0.1/cb?code=x"}, nil)
		res, _ := e.send(t, http.MethodPost, target, form, "consent=ct&decision=allow", "s")
		assert.Equal(t, http.StatusSeeOther, res.StatusCode)
		assert.Equal(t, "http://127.0.0.1/cb?code=x", res.Header.Get("Location"))
	})

	t.Run("no token", func(t *testing.T) {
		e := newFilesApp(t)
		res, body := e.send(t, http.MethodPost, target, form, "decision=allow", "")
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Contains(t, body, "incomplete")
	})

	t.Run("signed out", func(t *testing.T) {
		e := newFilesApp(t)
		res, _ := e.send(t, http.MethodPost, target, form, "consent=ct&decision=allow", "")
		assert.Equal(t, http.StatusFound, res.StatusCode)
		assert.Equal(t, "/base/login?redirect=%2Foauth2%2Fauthorize%3Fclient_id%3Dc", res.Header.Get("Location"))
	})

	t.Run("invalid", func(t *testing.T) {
		e := newFilesApp(t)
		e.signedIn()
		e.oauth.EXPECT().Consent(gomock.Any(), gomock.Any()).Return(nil, oauthctrl.ErrInvalidConsent)
		res, body := e.send(t, http.MethodPost, target, form, "consent=ct", "s")
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.Contains(t, body, "expired")
	})

	t.Run("failure", func(t *testing.T) {
		e := newFilesApp(t)
		e.signedIn()
		e.oauth.EXPECT().Consent(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		res, _ := e.send(t, http.MethodPost, target, form, "consent=ct", "s")
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	})
}

func TestOAuth_Token(t *testing.T) {
	const form = "application/x-www-form-urlencoded"
	e := newFilesApp(t)
	e.oauth.EXPECT().Token(gomock.Any(), &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: "r"}).
		Return(&entity.TokenResponse{AccessToken: "a", RefreshToken: "r2", ExpiresIn: time.Hour}, nil)
	res, body := e.send(t, http.MethodPost, oauthctrl.TokenPath, form, "grant_type=refresh_token&refresh_token=r", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "no-store", res.Header.Get("Cache-Control"))
	assert.JSONEq(t, `{"access_token":"a","token_type":"Bearer","expires_in":3600,"refresh_token":"r2"}`, body)

	for _, tc := range []struct {
		err    error
		status int
		want   string
	}{
		{oauthctrl.ErrInvalidClient, http.StatusUnauthorized, `{"error":"invalid_client"}`},
		{fmt.Errorf("%w: code is required", oauthctrl.ErrInvalidRequest), http.StatusBadRequest, `{"error":"invalid_request","error_description":"code is required"}`},
		{fmt.Errorf("%w: expired", oauthctrl.ErrInvalidGrant), http.StatusBadRequest, `{"error":"invalid_grant","error_description":"expired"}`},
		{oauthctrl.ErrUnsupportedGrantType, http.StatusBadRequest, `{"error":"unsupported_grant_type"}`},
		{errBoom, http.StatusInternalServerError, `{"error":"server_error"}`},
	} {
		e.oauth.EXPECT().Token(gomock.Any(), gomock.Any()).Return(nil, tc.err)
		res, body = e.send(t, http.MethodPost, oauthctrl.TokenPath, form, "", "")
		assert.Equal(t, tc.status, res.StatusCode)
		assert.JSONEq(t, tc.want, body)
	}
}
