// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

var _creds = Credentials{ClientID: "cid", ClientSecret: "secret", RedirectURL: "http://localhost/cb"}

// fakeProvider serves a token endpoint plus the given JSON routes.
func fakeProvider(t *testing.T, routes map[string]any) (*httptest.Server, oauth2.Endpoint) {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		if r.Form.Get("code") != "good" {
			http.Error(w, `{"error":"bad_verification_code"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"at","token_type":"bearer"}`))
	})
	for path, body := range routes {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer at", r.Header.Get("Authorization"))
			switch b := body.(type) {
			case int:
				w.WriteHeader(b)
			case string:
				_, _ = w.Write([]byte(b))
			default:
				_ = json.NewEncoder(w).Encode(b)
			}
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, oauth2.Endpoint{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}
}

func assertAuthURL(t *testing.T, raw string) {
	t.Helper()
	u, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "cid", u.Query().Get("client_id"))
	assert.Equal(t, "st", u.Query().Get("state"))
	assert.Equal(t, "http://localhost/cb", u.Query().Get("redirect_uri"))
}

func TestGoogle_Constructor(t *testing.T) {
	p := NewGoogle(_creds)
	assert.Equal(t, ProviderGoogle, p.Name())
	assert.Equal(t, "Google", p.Label())
	assert.Empty(t, p.Icon())
	authURL, err := p.AuthURL(context.Background(), "st")
	require.NoError(t, err)
	assertAuthURL(t, authURL)
	assert.Contains(t, authURL, "accounts.google.com")
}

func TestGoogle_Exchange(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		info    any
		want    *Identity
		wantErr error
	}{
		{
			name: "ok",
			code: "good",
			info: map[string]any{"id": "g1", "email": "a@b.c", "verified_email": true, "name": "A", "picture": "p"},
			want: &Identity{Subject: "g1", Email: "a@b.c", Name: "A", Picture: "p"},
		},
		{name: "bad code", code: "bad"},
		{name: "userinfo fails", code: "good", info: http.StatusInternalServerError},
		{name: "unverified", code: "good", info: map[string]any{"id": "g1", "email": "a@b.c"}, wantErr: ErrNoVerifiedEmail},
		{name: "no email", code: "good", info: map[string]any{"id": "g1", "verified_email": true}, wantErr: ErrNoVerifiedEmail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := map[string]any{}
			if tc.info != nil {
				routes["/userinfo"] = tc.info
			}
			srv, ep := fakeProvider(t, routes)
			p := newGoogle(_creds, ep, srv.URL+"/userinfo")
			got, err := p.Exchange(context.Background(), tc.code)
			if tc.want != nil {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
				return
			}
			assert.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestGitHub_Constructor(t *testing.T) {
	p := NewGitHub(_creds)
	assert.Equal(t, ProviderGitHub, p.Name())
	assert.Equal(t, "GitHub", p.Label())
	assert.Empty(t, p.Icon())
	authURL, err := p.AuthURL(context.Background(), "st")
	require.NoError(t, err)
	assertAuthURL(t, authURL)
	assert.Contains(t, authURL, "github.com")
}

func TestGitHub_Exchange(t *testing.T) {
	user := map[string]any{"id": 42, "login": "octo", "name": "Octo Cat", "avatar_url": "av"}
	cases := []struct {
		name    string
		code    string
		user    any
		emails  any
		want    *Identity
		wantErr error
	}{
		{
			name: "prefers verified primary",
			code: "good",
			user: user,
			emails: []map[string]any{
				{"email": "unverified@x", "primary": false, "verified": false},
				{"email": "second@x", "primary": false, "verified": true},
				{"email": "primary@x", "primary": true, "verified": true},
				{"email": "third@x", "primary": false, "verified": true},
			},
			want: &Identity{Subject: "42", Email: "primary@x", Name: "Octo Cat", Picture: "av"},
		},
		{
			name:   "falls back to first verified and login",
			code:   "good",
			user:   map[string]any{"id": 7, "login": "octo"},
			emails: []map[string]any{{"email": "first@x", "verified": true}, {"email": "second@x", "verified": true}},
			want:   &Identity{Subject: "7", Email: "first@x", Name: "octo"},
		},
		{name: "bad code", code: "bad"},
		{name: "user fails", code: "good", user: http.StatusUnauthorized},
		{name: "emails fail", code: "good", user: user, emails: http.StatusForbidden},
		{name: "bad emails json", code: "good", user: user, emails: "{not json"},
		{
			name:    "no verified email",
			code:    "good",
			user:    user,
			emails:  []map[string]any{{"email": "x@x", "primary": true, "verified": false}},
			wantErr: ErrNoVerifiedEmail,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routes := map[string]any{}
			if tc.user != nil {
				routes["/user"] = tc.user
			}
			if tc.emails != nil {
				routes["/user/emails"] = tc.emails
			}
			srv, ep := fakeProvider(t, routes)
			p := newGitHub(_creds, ep, srv.URL)
			got, err := p.Exchange(context.Background(), tc.code)
			if tc.want != nil {
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
				return
			}
			assert.Error(t, err)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
			}
		})
	}
}

func TestGetJSON_TransportError(t *testing.T) {
	var out any
	assert.Error(t, getJSON(http.DefaultClient, "http://127.0.0.1:0/unreachable", &out))
}
