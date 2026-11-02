// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package token

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newService(t *testing.T, now func() time.Time) Service {
	t.Helper()
	svc, err := New(Config{Secret: "test-secret", Now: now})
	require.NoError(t, err)
	return svc
}

func TestNew_RequiresSecret(t *testing.T) {
	_, err := New(Config{})
	assert.Error(t, err)
}

func TestNew_DefaultsClock(t *testing.T) {
	svc, err := New(Config{Secret: "s"})
	require.NoError(t, err)
	tok, err := svc.CreateSessionToken("u1")
	require.NoError(t, err)
	id, err := svc.ValidateSessionToken(tok)
	require.NoError(t, err)
	assert.Equal(t, "u1", id)
}

func TestSessionToken(t *testing.T) {
	now := time.Now()
	svc := newService(t, func() time.Time { return now })

	tok, err := svc.CreateSessionToken("abc")
	require.NoError(t, err)

	id, err := svc.ValidateSessionToken(tok)
	require.NoError(t, err)
	assert.Equal(t, "abc", id)

	// Expired.
	later := newService(t, func() time.Time { return now.Add(SessionTTL + time.Minute) })
	_, err = later.ValidateSessionToken(tok)
	assert.ErrorIs(t, err, ErrInvalidToken)

	// Wrong secret.
	other, err := New(Config{Secret: "other", Now: func() time.Time { return now }})
	require.NoError(t, err)
	_, err = other.ValidateSessionToken(tok)
	assert.ErrorIs(t, err, ErrInvalidToken)

	// A state token is not a session.
	state, err := svc.CreateStateToken("google", "/")
	require.NoError(t, err)
	_, err = svc.ValidateSessionToken(state)
	assert.ErrorIs(t, err, ErrInvalidToken)

	// Garbage.
	_, err = svc.ValidateSessionToken("not-a-jwt")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestSessionToken_EmptySubject(t *testing.T) {
	svc := newService(t, nil).(*service)
	tok, err := svc.sign(jwt.RegisteredClaims{
		Audience:  jwt.ClaimStrings{_sessionAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	require.NoError(t, err)
	_, err = svc.ValidateSessionToken(tok)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestSessionToken_RejectsOtherAlgorithms(t *testing.T) {
	svc := newService(t, nil)
	claims := jwt.RegisteredClaims{
		Subject:   "abc",
		Audience:  jwt.ClaimStrings{_sessionAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte("test-secret"))
	require.NoError(t, err)
	_, err = svc.ValidateSessionToken(tok)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestStateToken(t *testing.T) {
	now := time.Now()
	svc := newService(t, func() time.Time { return now })

	tok, err := svc.CreateStateToken("github", "/settings")
	require.NoError(t, err)

	rurl, err := svc.ValidateStateToken(tok, "github")
	require.NoError(t, err)
	assert.Equal(t, "/settings", rurl)

	_, err = svc.ValidateStateToken(tok, "google")
	assert.ErrorIs(t, err, ErrInvalidToken)

	later := newService(t, func() time.Time { return now.Add(StateTTL + time.Second) })
	_, err = later.ValidateStateToken(tok, "github")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestStateToken_WrongSubject(t *testing.T) {
	svc := newService(t, nil).(*service)
	tok, err := svc.sign(jwt.RegisteredClaims{
		Subject:   "something-else",
		Audience:  jwt.ClaimStrings{"google"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	require.NoError(t, err)
	_, err = svc.ValidateStateToken(tok, "google")
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestRootToken(t *testing.T) {
	now := time.Now()
	svc := newService(t, func() time.Time { return now })

	tok, err := svc.CreateRootToken()
	require.NoError(t, err)
	require.NoError(t, svc.ValidateRootToken(tok))

	// A root session is not a user session and vice versa.
	_, err = svc.ValidateSessionToken(tok)
	assert.ErrorIs(t, err, ErrInvalidToken)
	user, err := svc.CreateSessionToken("u1")
	require.NoError(t, err)
	assert.ErrorIs(t, svc.ValidateRootToken(user), ErrInvalidToken)

	later := newService(t, func() time.Time { return now.Add(RootTTL + time.Second) })
	assert.ErrorIs(t, later.ValidateRootToken(tok), ErrInvalidToken)
}

func TestRootToken_WrongSubject(t *testing.T) {
	svc := newService(t, nil).(*service)
	tok, err := svc.sign(jwt.RegisteredClaims{
		Subject:   "someone",
		Audience:  jwt.ClaimStrings{_rootAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	require.NoError(t, err)
	assert.ErrorIs(t, svc.ValidateRootToken(tok), ErrInvalidToken)
}

func TestClientID(t *testing.T) {
	now := time.Now()
	svc := newService(t, func() time.Time { return now })
	client := OAuthClient{Name: "Claude", RedirectURIs: []string{"http://127.0.0.1/cb"}}

	id, err := svc.CreateClientID(client)
	require.NoError(t, err)
	got, err := svc.ValidateClientID(id)
	require.NoError(t, err)
	assert.Equal(t, client, got)

	_, err = newService(t, func() time.Time { return now.Add(ClientTTL + time.Minute) }).ValidateClientID(id)
	assert.ErrorIs(t, err, ErrInvalidToken, "expired")

	noURIs, err := svc.CreateClientID(OAuthClient{Name: "x"})
	require.NoError(t, err)
	_, err = svc.ValidateClientID(noURIs)
	assert.ErrorIs(t, err, ErrInvalidToken, "no redirect URIs")

	wrongSubject := sign(t, clientClaims{RegisteredClaims: claimsFor("someone", _clientAudience, now), OAuthClient: client})
	_, err = svc.ValidateClientID(wrongSubject)
	assert.ErrorIs(t, err, ErrInvalidToken)

	access, err := svc.CreateAccessToken("u1", "c1")
	require.NoError(t, err)
	_, err = svc.ValidateClientID(access)
	assert.ErrorIs(t, err, ErrInvalidToken, "an access token is not a client ID")
}

func TestGrantTokens(t *testing.T) {
	now := time.Now()
	svc := newService(t, func() time.Time { return now })
	grant := OAuthGrant{UserID: "u1", ClientID: "c1", RedirectURI: "http://127.0.0.1/cb", CodeChallenge: "ch", State: "st"}

	consent, err := svc.CreateConsentToken(grant)
	require.NoError(t, err)
	got, err := svc.ValidateConsentToken(consent)
	require.NoError(t, err)
	assert.Equal(t, grant, got)

	code, err := svc.CreateAuthorizationCode(grant)
	require.NoError(t, err)
	got, err = svc.ValidateAuthorizationCode(code)
	require.NoError(t, err)
	assert.Equal(t, grant, got)

	access, err := svc.CreateAccessToken("u1", "c1")
	require.NoError(t, err)
	user, err := svc.ValidateAccessToken(access)
	require.NoError(t, err)
	assert.Equal(t, "u1", user)

	refresh, err := svc.CreateRefreshToken("u1", "c1")
	require.NoError(t, err)
	got, err = svc.ValidateRefreshToken(refresh)
	require.NoError(t, err)
	assert.Equal(t, OAuthGrant{UserID: "u1", ClientID: "c1"}, got)

	// No kind passes for another.
	_, err = svc.ValidateAuthorizationCode(consent)
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = svc.ValidateAccessToken(refresh)
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = svc.ValidateRefreshToken(access)
	assert.ErrorIs(t, err, ErrInvalidToken)
	session, err := svc.CreateSessionToken("u1")
	require.NoError(t, err)
	_, err = svc.ValidateAccessToken(session)
	assert.ErrorIs(t, err, ErrInvalidToken)
	_, err = svc.ValidateConsentToken(code)
	assert.ErrorIs(t, err, ErrInvalidToken)

	// Expiry.
	later := newService(t, func() time.Time { return now.Add(AuthorizationCodeTTL + time.Second) })
	_, err = later.ValidateAuthorizationCode(code)
	assert.ErrorIs(t, err, ErrInvalidToken)
	later = newService(t, func() time.Time { return now.Add(AccessTokenTTL + time.Second) })
	_, err = later.ValidateAccessToken(access)
	assert.ErrorIs(t, err, ErrInvalidToken)

	// A grant needs a user and a client.
	noClient := sign(t, grantClaims{RegisteredClaims: claimsFor("u1", _accessAudience, now)})
	_, err = svc.ValidateAccessToken(noClient)
	assert.ErrorIs(t, err, ErrInvalidToken)
	noUser := sign(t, grantClaims{RegisteredClaims: claimsFor("", _accessAudience, now), ClientID: "c1"})
	_, err = svc.ValidateAccessToken(noUser)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func claimsFor(subject, audience string, now time.Time) jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Subject:   subject,
		Audience:  jwt.ClaimStrings{audience},
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
}

func sign(t *testing.T, claims jwt.Claims) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return tok
}
