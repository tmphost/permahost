// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeIssuer is an OpenID Connect issuer: discovery plus the token and
// userinfo endpoints of fakeProvider.
func fakeIssuer(t *testing.T, discovery string, userinfo any) (*httptest.Server, *int) {
	t.Helper()
	srv, _ := fakeProvider(t, map[string]any{"/userinfo": userinfo})
	calls := new(int)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/realms/x/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		*calls++
		_, _ = fmt.Fprint(w, strings.ReplaceAll(discovery, "{srv}", srv.URL))
	}))
	t.Cleanup(issuer.Close)
	return issuer, calls
}

const _discovery = `{"authorization_endpoint":"{srv}/auth","token_endpoint":"{srv}/token","userinfo_endpoint":"{srv}/userinfo"}`

func newTestOIDC(issuer string) Provider {
	return NewOIDC(OIDCParams{Name: "okta", Label: "Okta", Icon: "<svg/>", Issuer: issuer + "/realms/x", Credentials: _creds})
}

func TestOIDC_DiscoversOnce(t *testing.T) {
	issuer, calls := fakeIssuer(t, _discovery, map[string]any{"sub": "1"})
	p := newTestOIDC(issuer.URL)
	assert.Equal(t, "okta", p.Name())
	assert.Equal(t, "Okta", p.Label())
	assert.Equal(t, "<svg/>", p.Icon())

	for range 2 {
		authURL, err := p.AuthURL(context.Background(), "st")
		require.NoError(t, err)
		assertAuthURL(t, authURL)
		assert.Contains(t, authURL, "scope=openid+email+profile")
	}
	assert.Equal(t, 1, *calls)
}

func TestOIDC_Exchange(t *testing.T) {
	cases := []struct {
		name    string
		code    string
		info    any
		want    *Identity
		wantErr string
	}{
		{
			name: "verified bool", code: "good",
			info: map[string]any{"sub": "1", "email": "a@x.io", "email_verified": true, "name": "A", "picture": "p"},
			want: &Identity{Subject: "1", Email: "a@x.io", Name: "A", Picture: "p"},
		},
		{
			name: "verified string", code: "good",
			info: map[string]any{"sub": "1", "email": "a@x.io", "email_verified": "true"},
			want: &Identity{Subject: "1", Email: "a@x.io"},
		},
		{name: "unverified", code: "good", info: map[string]any{"sub": "1", "email": "a@x.io", "email_verified": false}, wantErr: ErrNoVerifiedEmail.Error()},
		{name: "no email", code: "good", info: map[string]any{"sub": "1", "email_verified": true}, wantErr: ErrNoVerifiedEmail.Error()},
		{name: "no subject", code: "good", info: map[string]any{"email": "a@x.io"}, wantErr: "no subject"},
		{name: "userinfo fails", code: "good", info: http.StatusUnauthorized, wantErr: "okta userinfo"},
		{name: "bad code", code: "bad", info: map[string]any{}, wantErr: "okta oauth exchange"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issuer, _ := fakeIssuer(t, _discovery, tc.info)
			got, err := newTestOIDC(issuer.URL).Exchange(context.Background(), tc.code)
			if tc.wantErr != "" {
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOIDC_DiscoveryFailures(t *testing.T) {
	cases := map[string]string{
		"not json":          `nope`,
		"missing endpoints": `{"authorization_endpoint":"{srv}/auth"}`,
	}
	for name, discovery := range cases {
		t.Run(name, func(t *testing.T) {
			issuer, calls := fakeIssuer(t, discovery, nil)
			p := newTestOIDC(issuer.URL)
			_, err := p.AuthURL(context.Background(), "st")
			assert.ErrorContains(t, err, "okta discovery")
			_, err = p.Exchange(context.Background(), "good")
			assert.Error(t, err)
			assert.Equal(t, 2, *calls, "a failed discovery is retried")
		})
	}

	t.Run("status", func(t *testing.T) {
		issuer, _ := fakeIssuer(t, _discovery, nil)
		p := NewOIDC(OIDCParams{Name: "okta", Issuer: issuer.URL + "/elsewhere"})
		_, err := p.AuthURL(context.Background(), "st")
		assert.ErrorContains(t, err, "status 404")
	})

	t.Run("unreachable", func(t *testing.T) {
		issuer, _ := fakeIssuer(t, _discovery, nil)
		issuer.Close()
		_, err := newTestOIDC(issuer.URL).AuthURL(context.Background(), "st")
		assert.ErrorContains(t, err, "okta discovery")
	})

	t.Run("bad issuer URL", func(t *testing.T) {
		_, err := NewOIDC(OIDCParams{Name: "okta", Issuer: "http://[::1"}).AuthURL(context.Background(), "st")
		assert.ErrorContains(t, err, "okta discovery")
	})
}
