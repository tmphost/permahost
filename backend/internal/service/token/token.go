// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package token issues and validates the signed JWTs used for browser
// sessions, for OAuth state, and for tmphost's own OAuth server: client
// IDs, consents, authorization codes, access and refresh tokens. All of
// them are stateless; each kind has its own audience, so one can never
// pass for another.
package token

//go:generate mockgen -source=token.go -destination=mocks/token.go -package=mocks

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// SessionTTL is how long a browser session lasts.
	SessionTTL = 7 * 24 * time.Hour
	// StateTTL is how long a user has to finish an OAuth round trip.
	StateTTL = 5 * time.Minute
	// RootTTL is how long a root (setup) session lasts.
	RootTTL = 12 * time.Hour
	// ClientTTL is how long a dynamically registered client ID is valid.
	ClientTTL = 10 * 365 * 24 * time.Hour
	// ConsentTTL is how long a user has to answer the consent page.
	ConsentTTL = 10 * time.Minute
	// AuthorizationCodeTTL is how long a client has to redeem a code.
	AuthorizationCodeTTL = 2 * time.Minute
	// AccessTokenTTL is how long an OAuth access token lasts.
	AccessTokenTTL = time.Hour
	// RefreshTokenTTL is how long an OAuth refresh token lasts.
	RefreshTokenTTL = 30 * 24 * time.Hour

	_sessionAudience = "session:human"
	_rootAudience    = "session:root"
	_rootSubject     = "root"
	_stateSubject    = "oauth_state"
	_clientAudience  = "oauth:client"
	_clientSubject   = "client"
	_consentAudience = "oauth:consent"
	_codeAudience    = "oauth:code"
	_accessAudience  = "oauth:access"
	_refreshAudience = "oauth:refresh"
)

// ErrInvalidToken is returned for any token that fails validation.
var ErrInvalidToken = errors.New("invalid token")

type (
	// Service creates and validates tokens.
	Service interface {
		// CreateSessionToken signs a session for the given user ID.
		CreateSessionToken(userID string) (string, error)
		// ValidateSessionToken returns the user ID a session belongs to.
		ValidateSessionToken(token string) (string, error)
		// CreateStateToken signs an OAuth state bound to a provider and the
		// in-app path to return to.
		CreateStateToken(provider, redirectURL string) (string, error)
		// ValidateStateToken returns the redirect path carried by a state
		// issued for provider.
		ValidateStateToken(token, provider string) (string, error)
		// CreateRootToken signs a root (setup) session.
		CreateRootToken() (string, error)
		// ValidateRootToken checks a root session.
		ValidateRootToken(token string) error
		// CreateClientID signs a registered OAuth client into its ID.
		CreateClientID(client OAuthClient) (string, error)
		// ValidateClientID returns the client an ID was issued for.
		ValidateClientID(clientID string) (OAuthClient, error)
		// CreateConsentToken signs an authorization request awaiting the
		// user's answer.
		CreateConsentToken(grant OAuthGrant) (string, error)
		// ValidateConsentToken returns the request a consent token holds.
		ValidateConsentToken(consent string) (OAuthGrant, error)
		// CreateAuthorizationCode signs an approved authorization request.
		CreateAuthorizationCode(grant OAuthGrant) (string, error)
		// ValidateAuthorizationCode returns the request a code approves.
		ValidateAuthorizationCode(code string) (OAuthGrant, error)
		// CreateAccessToken signs an access token for a user and client.
		CreateAccessToken(userID, clientID string) (string, error)
		// ValidateAccessToken returns the user an access token is for.
		ValidateAccessToken(access string) (string, error)
		// CreateRefreshToken signs a refresh token for a user and client.
		CreateRefreshToken(userID, clientID string) (string, error)
		// ValidateRefreshToken returns the user and client of a refresh
		// token.
		ValidateRefreshToken(refresh string) (OAuthGrant, error)
	}

	// OAuthClient is a dynamically registered OAuth client.
	OAuthClient struct {
		Name         string   `json:"name,omitempty"`
		RedirectURIs []string `json:"ruris"`
	}

	// OAuthGrant is what a user grants a client, as it moves from the
	// consent page through the code to the tokens.
	OAuthGrant struct {
		UserID string
		// ClientID identifies the client; callers pass a digest, since a
		// client ID is long.
		ClientID      string
		RedirectURI   string
		CodeChallenge string
		State         string
	}

	// Config holds the token signing settings.
	Config struct {
		Secret string
		Now    func() time.Time
	}

	service struct {
		secret []byte
		now    func() time.Time
	}

	stateClaims struct {
		jwt.RegisteredClaims
		RedirectURL string `json:"rurl,omitempty"`
	}

	clientClaims struct {
		jwt.RegisteredClaims
		OAuthClient
	}

	grantClaims struct {
		jwt.RegisteredClaims
		ClientID      string `json:"cid"`
		RedirectURI   string `json:"ruri,omitempty"`
		CodeChallenge string `json:"cch,omitempty"`
		State         string `json:"st,omitempty"`
	}
)

// New returns a token service. The secret must not be empty.
func New(cfg Config) (Service, error) {
	if cfg.Secret == "" {
		return nil, errors.New("token: secret is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{secret: []byte(cfg.Secret), now: now}, nil
}

func (s *service) CreateSessionToken(userID string) (string, error) {
	now := s.now()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		Audience:  jwt.ClaimStrings{_sessionAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(SessionTTL)),
	}
	return s.sign(claims)
}

func (s *service) ValidateSessionToken(token string) (string, error) {
	var claims jwt.RegisteredClaims
	if err := s.parse(token, &claims, _sessionAudience); err != nil {
		return "", err
	}
	if claims.Subject == "" {
		return "", ErrInvalidToken
	}
	return claims.Subject, nil
}

func (s *service) CreateStateToken(provider, redirectURL string) (string, error) {
	now := s.now()
	claims := stateClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   _stateSubject,
			Audience:  jwt.ClaimStrings{provider},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(StateTTL)),
		},
		RedirectURL: redirectURL,
	}
	return s.sign(claims)
}

func (s *service) ValidateStateToken(token, provider string) (string, error) {
	var claims stateClaims
	if err := s.parse(token, &claims, provider); err != nil {
		return "", err
	}
	if claims.Subject != _stateSubject {
		return "", ErrInvalidToken
	}
	return claims.RedirectURL, nil
}

func (s *service) CreateRootToken() (string, error) {
	now := s.now()
	return s.sign(jwt.RegisteredClaims{
		Subject:   _rootSubject,
		Audience:  jwt.ClaimStrings{_rootAudience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(RootTTL)),
	})
}

func (s *service) ValidateRootToken(token string) error {
	var claims jwt.RegisteredClaims
	if err := s.parse(token, &claims, _rootAudience); err != nil {
		return err
	}
	if claims.Subject != _rootSubject {
		return ErrInvalidToken
	}
	return nil
}

func (s *service) CreateClientID(client OAuthClient) (string, error) {
	return s.sign(clientClaims{RegisteredClaims: s.registered(_clientSubject, _clientAudience, ClientTTL), OAuthClient: client})
}

func (s *service) ValidateClientID(clientID string) (OAuthClient, error) {
	var claims clientClaims
	if err := s.parse(clientID, &claims, _clientAudience); err != nil {
		return OAuthClient{}, err
	}
	if claims.Subject != _clientSubject || len(claims.RedirectURIs) == 0 {
		return OAuthClient{}, ErrInvalidToken
	}
	return claims.OAuthClient, nil
}

func (s *service) CreateConsentToken(grant OAuthGrant) (string, error) {
	return s.signGrant(grant, _consentAudience, ConsentTTL)
}

func (s *service) ValidateConsentToken(consent string) (OAuthGrant, error) {
	return s.parseGrant(consent, _consentAudience)
}

func (s *service) CreateAuthorizationCode(grant OAuthGrant) (string, error) {
	return s.signGrant(grant, _codeAudience, AuthorizationCodeTTL)
}

func (s *service) ValidateAuthorizationCode(code string) (OAuthGrant, error) {
	return s.parseGrant(code, _codeAudience)
}

func (s *service) CreateAccessToken(userID, clientID string) (string, error) {
	return s.signGrant(OAuthGrant{UserID: userID, ClientID: clientID}, _accessAudience, AccessTokenTTL)
}

func (s *service) ValidateAccessToken(token string) (string, error) {
	grant, err := s.parseGrant(token, _accessAudience)
	return grant.UserID, err
}

func (s *service) CreateRefreshToken(userID, clientID string) (string, error) {
	return s.signGrant(OAuthGrant{UserID: userID, ClientID: clientID}, _refreshAudience, RefreshTokenTTL)
}

func (s *service) ValidateRefreshToken(refresh string) (OAuthGrant, error) {
	return s.parseGrant(refresh, _refreshAudience)
}

func (s *service) signGrant(grant OAuthGrant, audience string, ttl time.Duration) (string, error) {
	return s.sign(grantClaims{
		RegisteredClaims: s.registered(grant.UserID, audience, ttl),
		ClientID:         grant.ClientID,
		RedirectURI:      grant.RedirectURI,
		CodeChallenge:    grant.CodeChallenge,
		State:            grant.State,
	})
}

func (s *service) parseGrant(token, audience string) (OAuthGrant, error) {
	var claims grantClaims
	if err := s.parse(token, &claims, audience); err != nil {
		return OAuthGrant{}, err
	}
	if claims.Subject == "" || claims.ClientID == "" {
		return OAuthGrant{}, ErrInvalidToken
	}
	return OAuthGrant{
		UserID:        claims.Subject,
		ClientID:      claims.ClientID,
		RedirectURI:   claims.RedirectURI,
		CodeChallenge: claims.CodeChallenge,
		State:         claims.State,
	}, nil
}

func (s *service) registered(subject, audience string, ttl time.Duration) jwt.RegisteredClaims {
	now := s.now()
	return jwt.RegisteredClaims{
		Subject:   subject,
		Audience:  jwt.ClaimStrings{audience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
}

func (s *service) sign(claims jwt.Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

func (s *service) parse(token string, claims jwt.Claims, audience string) error {
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return s.secret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithAudience(audience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(s.now),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	return nil
}
