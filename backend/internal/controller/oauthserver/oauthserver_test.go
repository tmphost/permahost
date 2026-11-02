// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthserver

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	mockrepo "github.com/tmphost/tmphost/backend/internal/repository/mocks"
	"github.com/tmphost/tmphost/backend/internal/service/token"
	mocktoken "github.com/tmphost/tmphost/backend/internal/service/token/mocks"
)

const (
	_issuer       = "https://tmphost.io"
	_userID       = int64(42)
	_pkceVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	_redirect     = "http://127.0.0.1:8123/callback"
)

var (
	errBoom   = errors.New("boom")
	_now      = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	_userB62  = monoflake.ID(_userID).String()
	_user     = model.User{ID: _userID, Email: "a@b.c"}
	challenge = func(v string) string {
		sum := sha256.Sum256([]byte(v))
		return base64.RawURLEncoding.EncodeToString(sum[:])
	}
)

type env struct {
	ctrl   Controller
	tokens token.Service
	users  *mockrepo.MockUserRepository
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tokens, err := token.New(token.Config{Secret: "s", Now: func() time.Time { return _now }})
	require.NoError(t, err)
	users := mockrepo.NewMockUserRepository(gomock.NewController(t))
	return &env{
		ctrl:   New(Params{Token: tokens, Users: users, Issuer: _issuer + "/", Resource: _issuer + "/mcp", Now: func() time.Time { return _now }}),
		tokens: tokens,
		users:  users,
	}
}

// withMockToken builds a controller whose token service is a mock.
func withMockToken(t *testing.T) (Controller, *mocktoken.MockService, *mockrepo.MockUserRepository) {
	t.Helper()
	mc := gomock.NewController(t)
	tokens, users := mocktoken.NewMockService(mc), mockrepo.NewMockUserRepository(mc)
	return New(Params{Token: tokens, Users: users, Issuer: _issuer}), tokens, users
}

func (e *env) register(t *testing.T, uris ...string) string {
	t.Helper()
	rs, err := e.ctrl.RegisterClient(context.Background(), &entity.RegisterClientRequest{ClientName: "CLI", RedirectURIs: uris})
	require.NoError(t, err)
	return rs.ClientID
}

func (e *env) authorize(clientID string) *entity.AuthorizeRequest {
	return &entity.AuthorizeRequest{
		UserID: _userID, ResponseType: "code", ClientID: clientID, RedirectURI: _redirect,
		CodeChallenge: challenge(_pkceVerifier), CodeChallengeMethod: "S256", State: "xyz",
	}
}

// code runs authorize and consent and returns the code.
func (e *env) code(t *testing.T, clientID string) string {
	t.Helper()
	ctx := context.Background()
	e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
	rs, err := e.ctrl.Authorize(ctx, e.authorize(clientID))
	require.NoError(t, err)
	consent, err := e.ctrl.Consent(ctx, &entity.ConsentRequest{UserID: _userID, Token: rs.Consent.Token, Allow: true})
	require.NoError(t, err)
	u, err := url.Parse(consent.RedirectURL)
	require.NoError(t, err)
	return u.Query().Get("code")
}

func TestNew_DefaultsClock(t *testing.T) {
	ctrl := New(Params{}).(*controller)
	assert.NotNil(t, ctrl.now)
}

func TestMetadata(t *testing.T) {
	e := newEnv(t)
	assert.Equal(t, &entity.GetOAuthServerMetadataResponse{
		Issuer:                _issuer,
		AuthorizationEndpoint: _issuer + "/oauth2/authorize",
		TokenEndpoint:         _issuer + "/oauth2/token",
		RegistrationEndpoint:  _issuer + "/oauth2/register",
	}, e.ctrl.GetServerMetadata(context.Background()))
	assert.Equal(t, &entity.GetProtectedResourceMetadataResponse{
		Resource: _issuer + "/mcp", AuthorizationServers: []string{_issuer},
	}, e.ctrl.GetProtectedResourceMetadata(context.Background()))
}

func TestRegisterClient(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	rs, err := e.ctrl.RegisterClient(ctx, &entity.RegisterClientRequest{
		ClientName:    "  Claude\u0000 Code\u200b ",
		RedirectURIs:  []string{_redirect, "https://claude.ai/api/mcp/auth_callback", "cursor://anysphere.cursor-retrieval/oauth/callback"},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Claude Code", rs.ClientName)
	assert.Equal(t, _now, rs.IssuedAt)
	client, err := e.tokens.ValidateClientID(rs.ClientID)
	require.NoError(t, err)
	assert.Equal(t, rs.RedirectURIs, client.RedirectURIs)

	long, err := e.ctrl.RegisterClient(ctx, &entity.RegisterClientRequest{ClientName: strings.Repeat("é", 150), RedirectURIs: []string{_redirect}})
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("é", 100), long.ClientName)

	tooMany := make([]string, 11)
	for i := range tooMany {
		tooMany[i] = _redirect
	}
	cases := map[string]struct {
		rq   entity.RegisterClientRequest
		want error
	}{
		"no redirect":     {entity.RegisterClientRequest{}, ErrInvalidRedirectURI},
		"too many":        {entity.RegisterClientRequest{RedirectURIs: tooMany}, ErrInvalidRedirectURI},
		"too long":        {entity.RegisterClientRequest{RedirectURIs: []string{"https://x.io/" + strings.Repeat("a", 2000)}}, ErrInvalidRedirectURI},
		"relative":        {entity.RegisterClientRequest{RedirectURIs: []string{"/callback"}}, ErrInvalidRedirectURI},
		"unparsable":      {entity.RegisterClientRequest{RedirectURIs: []string{"http://[::1"}}, ErrInvalidRedirectURI},
		"fragment":        {entity.RegisterClientRequest{RedirectURIs: []string{"https://x.io/cb#f"}}, ErrInvalidRedirectURI},
		"userinfo":        {entity.RegisterClientRequest{RedirectURIs: []string{"https://u:p@x.io/cb"}}, ErrInvalidRedirectURI},
		"javascript":      {entity.RegisterClientRequest{RedirectURIs: []string{"javascript:alert(1)"}}, ErrInvalidRedirectURI},
		"data":            {entity.RegisterClientRequest{RedirectURIs: []string{"data:text/html,x"}}, ErrInvalidRedirectURI},
		"http remote":     {entity.RegisterClientRequest{RedirectURIs: []string{"http://evil.example/cb"}}, ErrInvalidRedirectURI},
		"unicode host":    {entity.RegisterClientRequest{RedirectURIs: []string{"https://gооgle.com/cb"}}, ErrInvalidRedirectURI},
		"empty host":      {entity.RegisterClientRequest{RedirectURIs: []string{"https:///cb"}}, ErrInvalidRedirectURI},
		"grant type":      {entity.RegisterClientRequest{RedirectURIs: []string{_redirect}, GrantTypes: []string{"client_credentials"}}, ErrInvalidClientMetadata},
		"response type":   {entity.RegisterClientRequest{RedirectURIs: []string{_redirect}, ResponseTypes: []string{"token"}}, ErrInvalidClientMetadata},
		"uppercase https": {entity.RegisterClientRequest{RedirectURIs: []string{"https://X.io/cb"}}, ErrInvalidRedirectURI},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := e.ctrl.RegisterClient(ctx, &tc.rq)
			assert.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("signing fails", func(t *testing.T) {
		ctrl, tokens, _ := withMockToken(t)
		tokens.EXPECT().CreateClientID(gomock.Any()).Return("", errBoom)
		_, err := ctrl.RegisterClient(ctx, &entity.RegisterClientRequest{RedirectURIs: []string{_redirect}})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestAuthorizeAndConsent(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	clientID := e.register(t, "http://127.0.0.1/callback", "https://app.example.com/cb")

	e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
	rq := e.authorize(clientID) // a loopback port the client did not register
	rs, err := e.ctrl.Authorize(ctx, rq)
	require.NoError(t, err)
	require.NotNil(t, rs.Consent)
	assert.Equal(t, "CLI", rs.Consent.ClientName)
	assert.Equal(t, "an app on this computer (127.0.0.1:8123)", rs.Consent.Destination)
	assert.Equal(t, "a@b.c", rs.Consent.Email)

	t.Run("allow", func(t *testing.T) {
		out, err := e.ctrl.Consent(ctx, &entity.ConsentRequest{UserID: _userID, Token: rs.Consent.Token, Allow: true})
		require.NoError(t, err)
		u, err := url.Parse(out.RedirectURL)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1:8123", u.Host)
		assert.Equal(t, "xyz", u.Query().Get("state"))
		assert.Equal(t, _issuer, u.Query().Get("iss"))
		grant, err := e.tokens.ValidateAuthorizationCode(u.Query().Get("code"))
		require.NoError(t, err)
		assert.Equal(t, _redirect, grant.RedirectURI)
		assert.Equal(t, _userB62, grant.UserID)
	})

	t.Run("deny", func(t *testing.T) {
		out, err := e.ctrl.Consent(ctx, &entity.ConsentRequest{UserID: _userID, Token: rs.Consent.Token})
		require.NoError(t, err)
		assert.Equal(t, _redirect+"?error=access_denied&iss=https%3A%2F%2Ftmphost.io&state=xyz", out.RedirectURL)
	})

	t.Run("someone else's consent", func(t *testing.T) {
		_, err := e.ctrl.Consent(ctx, &entity.ConsentRequest{UserID: 7, Token: rs.Consent.Token, Allow: true})
		assert.ErrorIs(t, err, ErrInvalidConsent)
		_, err = e.ctrl.Consent(ctx, &entity.ConsentRequest{Token: rs.Consent.Token, Allow: true})
		assert.ErrorIs(t, err, ErrInvalidConsent)
	})

	t.Run("forged consent", func(t *testing.T) {
		_, err := e.ctrl.Consent(ctx, &entity.ConsentRequest{UserID: _userID, Token: "x", Allow: true})
		assert.ErrorIs(t, err, ErrInvalidConsent)
	})

	t.Run("https destination", func(t *testing.T) {
		e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
		r := e.authorize(clientID)
		r.RedirectURI = "https://app.example.com/cb"
		rs, err := e.ctrl.Authorize(ctx, r)
		require.NoError(t, err)
		assert.Equal(t, "app.example.com", rs.Consent.Destination)
	})
}

func TestAuthorize_Errors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	clientID := e.register(t, _redirect)
	twoURIs := e.register(t, _redirect, "cursor://cb")

	t.Run("unknown client", func(t *testing.T) {
		_, err := e.ctrl.Authorize(ctx, e.authorize("forged"))
		assert.ErrorIs(t, err, ErrInvalidClient)
	})

	redirectCases := map[string]string{
		"other host":       "https://evil.example/cb",
		"other path":       "http://127.0.0.1:9/other",
		"other query":      "http://127.0.0.1:9/callback?x=1",
		"loopback name":    "http://localhost:9/callback",
		"fragment":         "http://127.0.0.1:9/callback#x",
		"userinfo":         "http://u@127.0.0.1:9/callback",
		"unparsable":       "http://[::1",
		"custom on a port": "cursor://cb:1",
	}
	for name, uri := range redirectCases {
		t.Run("redirect "+name, func(t *testing.T) {
			rq := e.authorize(twoURIs)
			rq.RedirectURI = uri
			_, err := e.ctrl.Authorize(ctx, rq)
			assert.ErrorIs(t, err, ErrInvalidRedirectURI)
		})
	}

	t.Run("redirect required with two registered", func(t *testing.T) {
		rq := e.authorize(twoURIs)
		rq.RedirectURI = ""
		_, err := e.ctrl.Authorize(ctx, rq)
		assert.ErrorIs(t, err, ErrInvalidRedirectURI)
	})

	t.Run("the only registered redirect is the default", func(t *testing.T) {
		e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
		rq := e.authorize(clientID)
		rq.RedirectURI = ""
		rs, err := e.ctrl.Authorize(ctx, rq)
		require.NoError(t, err)
		assert.NotNil(t, rs.Consent)
	})

	t.Run("custom scheme destination", func(t *testing.T) {
		e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
		rq := e.authorize(twoURIs)
		rq.RedirectURI = "cursor://cb"
		rs, err := e.ctrl.Authorize(ctx, rq)
		require.NoError(t, err)
		assert.Equal(t, "the cursor: app", rs.Consent.Destination)
	})

	t.Run("response type goes back to the client", func(t *testing.T) {
		rq := e.authorize(clientID)
		rq.ResponseType = "token"
		rs, err := e.ctrl.Authorize(ctx, rq)
		require.NoError(t, err)
		assert.Equal(t, _redirect+"?error=unsupported_response_type&state=xyz", rs.RedirectURL)
	})

	for name, mutate := range map[string]func(*entity.AuthorizeRequest){
		"no pkce":    func(rq *entity.AuthorizeRequest) { rq.CodeChallenge, rq.CodeChallengeMethod = "", "" },
		"plain pkce": func(rq *entity.AuthorizeRequest) { rq.CodeChallengeMethod = "plain" },
		"short":      func(rq *entity.AuthorizeRequest) { rq.CodeChallenge = "abc" },
	} {
		t.Run(name, func(t *testing.T) {
			rq := e.authorize(clientID)
			mutate(rq)
			rq.State = ""
			rs, err := e.ctrl.Authorize(ctx, rq)
			require.NoError(t, err)
			u, err := url.Parse(rs.RedirectURL)
			require.NoError(t, err)
			assert.Equal(t, "invalid_request", u.Query().Get("error"))
			assert.False(t, u.Query().Has("state"))
		})
	}

	t.Run("signed out", func(t *testing.T) {
		rq := e.authorize(clientID)
		rq.UserID = 0
		_, err := e.ctrl.Authorize(ctx, rq)
		assert.ErrorIs(t, err, ErrLoginRequired)
	})

	t.Run("deleted user", func(t *testing.T) {
		e.users.EXPECT().FindUserByID(ctx, _userID).Return(model.User{}, repository.ErrNotFound)
		_, err := e.ctrl.Authorize(ctx, e.authorize(clientID))
		assert.ErrorIs(t, err, ErrLoginRequired)
	})

	t.Run("user lookup fails", func(t *testing.T) {
		e.users.EXPECT().FindUserByID(ctx, _userID).Return(model.User{}, errBoom)
		_, err := e.ctrl.Authorize(ctx, e.authorize(clientID))
		assert.ErrorIs(t, err, errBoom)
	})

	t.Run("signing fails", func(t *testing.T) {
		ctrl, tokens, users := withMockToken(t)
		tokens.EXPECT().ValidateClientID("c").Return(token.OAuthClient{RedirectURIs: []string{_redirect}}, nil)
		users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
		tokens.EXPECT().CreateConsentToken(gomock.Any()).Return("", errBoom)
		_, err := ctrl.Authorize(ctx, e.authorize("c"))
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestConsent_SigningFails(t *testing.T) {
	ctrl, tokens, _ := withMockToken(t)
	tokens.EXPECT().ValidateConsentToken("t").Return(token.OAuthGrant{UserID: _userB62, RedirectURI: _redirect}, nil)
	tokens.EXPECT().CreateAuthorizationCode(gomock.Any()).Return("", errBoom)
	_, err := ctrl.Consent(context.Background(), &entity.ConsentRequest{UserID: _userID, Token: "t", Allow: true})
	assert.ErrorIs(t, err, errBoom)
}

func TestToken_AuthorizationCode(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	clientID := e.register(t, "http://127.0.0.1/callback")
	code := e.code(t, clientID)

	exchange := func() *entity.TokenRequest {
		return &entity.TokenRequest{GrantType: "authorization_code", Code: code, ClientID: clientID, RedirectURI: _redirect, CodeVerifier: _pkceVerifier}
	}

	e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
	rs, err := e.ctrl.Token(ctx, exchange())
	require.NoError(t, err)
	assert.Equal(t, time.Hour, rs.ExpiresIn)
	user, err := e.tokens.ValidateAccessToken(rs.AccessToken)
	require.NoError(t, err)
	assert.Equal(t, _userB62, user)
	_, err = e.tokens.ValidateRefreshToken(rs.RefreshToken)
	require.NoError(t, err)

	otherClient := e.register(t, _redirect)
	cases := map[string]struct {
		mutate func(*entity.TokenRequest)
		want   error
	}{
		"no code":         {func(rq *entity.TokenRequest) { rq.Code = "" }, ErrInvalidRequest},
		"no client":       {func(rq *entity.TokenRequest) { rq.ClientID = "" }, ErrInvalidRequest},
		"no verifier":     {func(rq *entity.TokenRequest) { rq.CodeVerifier = "" }, ErrInvalidRequest},
		"forged client":   {func(rq *entity.TokenRequest) { rq.ClientID = "forged" }, ErrInvalidClient},
		"forged code":     {func(rq *entity.TokenRequest) { rq.Code = "forged" }, ErrInvalidGrant},
		"other client":    {func(rq *entity.TokenRequest) { rq.ClientID = otherClient }, ErrInvalidGrant},
		"other redirect":  {func(rq *entity.TokenRequest) { rq.RedirectURI = "http://127.0.0.1:9/callback" }, ErrInvalidGrant},
		"wrong verifier":  {func(rq *entity.TokenRequest) { rq.CodeVerifier = strings.Repeat("a", 43) }, ErrInvalidGrant},
		"short verifier":  {func(rq *entity.TokenRequest) { rq.CodeVerifier = "abc" }, ErrInvalidGrant},
		"unknown grant":   {func(rq *entity.TokenRequest) { rq.GrantType = "password" }, ErrUnsupportedGrantType},
		"verifier as hex": {func(rq *entity.TokenRequest) { rq.CodeVerifier = challenge(_pkceVerifier) + "!" }, ErrInvalidGrant},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rq := exchange()
			tc.mutate(rq)
			_, err := e.ctrl.Token(ctx, rq)
			assert.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("redirect_uri may be left out", func(t *testing.T) {
		e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
		rq := exchange()
		rq.RedirectURI = ""
		_, err := e.ctrl.Token(ctx, rq)
		assert.NoError(t, err)
	})

	t.Run("deleted user", func(t *testing.T) {
		e.users.EXPECT().FindUserByID(ctx, _userID).Return(model.User{}, repository.ErrNotFound)
		_, err := e.ctrl.Token(ctx, exchange())
		assert.ErrorIs(t, err, ErrInvalidGrant)
	})

	t.Run("user lookup fails", func(t *testing.T) {
		e.users.EXPECT().FindUserByID(ctx, _userID).Return(model.User{}, errBoom)
		_, err := e.ctrl.Token(ctx, exchange())
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestToken_Refresh(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	clientID := e.register(t, _redirect)
	refresh, err := e.tokens.CreateRefreshToken(_userB62, clientKey(clientID))
	require.NoError(t, err)

	e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil).Times(2)
	rs, err := e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: refresh, ClientID: clientID})
	require.NoError(t, err)
	assert.NotEmpty(t, rs.AccessToken)
	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: refresh})
	require.NoError(t, err, "client_id is optional")

	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token"})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: rs.AccessToken})
	assert.ErrorIs(t, err, ErrInvalidGrant, "an access token is not a refresh token")
	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: refresh, ClientID: e.register(t, _redirect+"2")})
	assert.ErrorIs(t, err, ErrInvalidGrant)
}

func TestToken_SigningFails(t *testing.T) {
	ctx := context.Background()
	grant := token.OAuthGrant{UserID: _userB62, ClientID: "k"}

	t.Run("access", func(t *testing.T) {
		ctrl, tokens, users := withMockToken(t)
		tokens.EXPECT().ValidateRefreshToken("r").Return(grant, nil)
		users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
		tokens.EXPECT().CreateAccessToken(_userB62, "k").Return("", errBoom)
		_, err := ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: "r"})
		assert.ErrorIs(t, err, errBoom)
	})

	t.Run("refresh", func(t *testing.T) {
		ctrl, tokens, users := withMockToken(t)
		tokens.EXPECT().ValidateRefreshToken("r").Return(grant, nil)
		users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
		tokens.EXPECT().CreateAccessToken(_userB62, "k").Return("a", nil)
		tokens.EXPECT().CreateRefreshToken(_userB62, "k").Return("", errBoom)
		_, err := ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: "r"})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestCLIClient(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)

	e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
	rq := e.authorize(CLIClientID)
	rq.RedirectURI = "http://localhost:54321/callback"
	rs, err := e.ctrl.Authorize(ctx, rq)
	require.NoError(t, err)
	assert.Equal(t, "tmphost CLI", rs.Consent.ClientName)
	assert.True(t, rs.Consent.Verified)

	for _, uri := range []string{"https://evil.example/callback", "http://127.0.0.1:1/other", "cursor://callback"} {
		rq.RedirectURI = uri
		_, err := e.ctrl.Authorize(ctx, rq)
		assert.ErrorIs(t, err, ErrInvalidRedirectURI, uri)
	}
	rq.RedirectURI = ""
	_, err = e.ctrl.Authorize(ctx, rq)
	assert.ErrorIs(t, err, ErrInvalidRedirectURI, "the CLI has several redirect URIs")

	code := e.code(t, CLIClientID)
	e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
	tokens, err := e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: "authorization_code", Code: code, ClientID: CLIClientID, CodeVerifier: _pkceVerifier})
	require.NoError(t, err)
	e.users.EXPECT().FindUserByID(ctx, _userID).Return(_user, nil)
	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: tokens.RefreshToken, ClientID: CLIClientID})
	require.NoError(t, err)

	other := e.register(t, _redirect)
	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: "refresh_token", RefreshToken: tokens.RefreshToken, ClientID: other})
	assert.ErrorIs(t, err, ErrInvalidGrant, "the CLI's tokens are not another client's")
}
