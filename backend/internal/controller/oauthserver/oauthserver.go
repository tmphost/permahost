// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package oauthserver is tmphost's OAuth 2.1 authorization server, which
// MCP clients and the CLI use to get an access token for a user.
//
// Clients register dynamically (RFC 7591) as public clients. Nothing is
// stored: the client ID is a signed token carrying the client's name and
// redirect URIs, and the consent, code and tokens are signed tokens too.
// The tmphost CLI needs no registration: it uses the fixed CLIClientID.
// Every authorization uses PKCE with S256, and the user approves each
// client on a consent page.
package oauthserver

//go:generate mockgen -source=oauthserver.go -destination=mocks/oauthserver.go -package=mocks

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/mustafaturan/monoflake"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

// The server's endpoints, below the issuer URL.
const (
	AuthorizePath = "/oauth2/authorize"
	TokenPath     = "/oauth2/token"
	RegisterPath  = "/oauth2/register"
)

// Grant and response types, and the PKCE method.
const (
	GrantAuthorizationCode = "authorization_code"
	GrantRefreshToken      = "refresh_token"
	ResponseTypeCode       = "code"
	MethodS256             = "S256"
	// AuthMethodNone is the only token endpoint auth method: every client
	// is public and proves itself with PKCE.
	AuthMethodNone = "none"
	// CLIClientID is the tmphost CLI's own client ID. It is public, like
	// every client, and may only redirect to a loopback address.
	CLIClientID = "00000000001"
)

// _cliClient is the tmphost CLI. Its loopback redirect URIs match on any
// port.
var _cliClient = token.OAuthClient{
	Name:         "tmphost CLI",
	RedirectURIs: []string{"http://127.0.0.1/callback", "http://localhost/callback", "http://[::1]/callback"},
}

const (
	_maxRedirectURIs   = 10
	_maxRedirectURILen = 2000
	_maxClientName     = 100
)

var (
	// ErrLoginRequired is returned when the user must sign in first.
	ErrLoginRequired = errors.New("login required")
	// ErrInvalidClient is returned for a client ID this server did not
	// issue.
	ErrInvalidClient = errors.New("invalid_client")
	// ErrInvalidRedirectURI is returned for a redirect URI the client did
	// not register, or cannot register.
	ErrInvalidRedirectURI = errors.New("invalid_redirect_uri")
	// ErrInvalidClientMetadata is returned for a registration asking for
	// something this server does not offer.
	ErrInvalidClientMetadata = errors.New("invalid_client_metadata")
	// ErrInvalidConsent is returned for a consent answer that is forged,
	// expired or someone else's.
	ErrInvalidConsent = errors.New("invalid consent")
	// ErrInvalidRequest is returned for a token request missing a
	// parameter.
	ErrInvalidRequest = errors.New("invalid_request")
	// ErrInvalidGrant is returned for a code or refresh token that is
	// invalid, expired, or not this client's, and for a wrong PKCE
	// verifier.
	ErrInvalidGrant = errors.New("invalid_grant")
	// ErrUnsupportedGrantType is returned for a grant type other than
	// authorization_code and refresh_token.
	ErrUnsupportedGrantType = errors.New("unsupported_grant_type")
)

var (
	// A PKCE S256 challenge is a base64url SHA-256 digest; a verifier is
	// 43 to 128 unreserved characters (RFC 7636 §4.1, §4.2).
	_challenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	_verifier  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
	_scheme    = regexp.MustCompile(`^[a-z][a-z0-9+.-]*$`)
	_hostname  = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	// Schemes a browser would run or open in place rather than hand to an
	// app.
	_forbiddenSchemes = []string{"javascript", "data", "file", "vbscript", "about", "blob", "ftp", "ws", "wss", "mailto", "tel", "sms"}
)

type (
	// Controller is the authorization server's business logic.
	Controller interface {
		GetServerMetadata(ctx context.Context) *entity.GetOAuthServerMetadataResponse
		GetProtectedResourceMetadata(ctx context.Context) *entity.GetProtectedResourceMetadataResponse
		RegisterClient(ctx context.Context, rq *entity.RegisterClientRequest) (*entity.RegisterClientResponse, error)
		// Authorize checks an authorization request and prepares the
		// consent page. Errors are for the user's eyes: the client and
		// redirect URI could not be trusted, so nothing goes back to it.
		Authorize(ctx context.Context, rq *entity.AuthorizeRequest) (*entity.AuthorizeResponse, error)
		// Consent turns the user's answer into a code, or a refusal, for
		// the client.
		Consent(ctx context.Context, rq *entity.ConsentRequest) (*entity.ConsentResponse, error)
		Token(ctx context.Context, rq *entity.TokenRequest) (*entity.TokenResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Token token.Service
		Users repository.UserRepository
		// Issuer is the server's public base URL.
		Issuer string
		// Resource is the protected resource's URL, the MCP endpoint.
		Resource string
		Now      func() time.Time
	}

	controller struct {
		token    token.Service
		users    repository.UserRepository
		issuer   string
		resource string
		now      func() time.Time
	}
)

// New returns the authorization server controller.
func New(p Params) Controller {
	c := &controller{
		token:    p.Token,
		users:    p.Users,
		issuer:   strings.TrimRight(p.Issuer, "/"),
		resource: p.Resource,
		now:      p.Now,
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

func (c *controller) GetServerMetadata(_ context.Context) *entity.GetOAuthServerMetadataResponse {
	return &entity.GetOAuthServerMetadataResponse{
		Issuer:                c.issuer,
		AuthorizationEndpoint: c.issuer + AuthorizePath,
		TokenEndpoint:         c.issuer + TokenPath,
		RegistrationEndpoint:  c.issuer + RegisterPath,
	}
}

func (c *controller) GetProtectedResourceMetadata(_ context.Context) *entity.GetProtectedResourceMetadataResponse {
	return &entity.GetProtectedResourceMetadataResponse{Resource: c.resource, AuthorizationServers: []string{c.issuer}}
}

func (c *controller) RegisterClient(_ context.Context, rq *entity.RegisterClientRequest) (*entity.RegisterClientResponse, error) {
	if len(rq.RedirectURIs) == 0 || len(rq.RedirectURIs) > _maxRedirectURIs {
		return nil, fmt.Errorf("%w: register 1 to %d redirect_uris", ErrInvalidRedirectURI, _maxRedirectURIs)
	}
	for _, u := range rq.RedirectURIs {
		if err := checkRedirectURI(u); err != nil {
			return nil, err
		}
	}
	for _, g := range rq.GrantTypes {
		if g != GrantAuthorizationCode && g != GrantRefreshToken {
			return nil, fmt.Errorf("%w: unsupported grant type %q", ErrInvalidClientMetadata, g)
		}
	}
	for _, r := range rq.ResponseTypes {
		if r != ResponseTypeCode {
			return nil, fmt.Errorf("%w: unsupported response type %q", ErrInvalidClientMetadata, r)
		}
	}
	client := token.OAuthClient{Name: cleanName(rq.ClientName), RedirectURIs: rq.RedirectURIs}
	id, err := c.token.CreateClientID(client)
	if err != nil {
		return nil, fmt.Errorf("create client id: %w", err)
	}
	return &entity.RegisterClientResponse{ClientID: id, ClientName: client.Name, RedirectURIs: client.RedirectURIs, IssuedAt: c.now().UTC()}, nil
}

func (c *controller) Authorize(ctx context.Context, rq *entity.AuthorizeRequest) (*entity.AuthorizeResponse, error) {
	client, err := c.client(rq.ClientID)
	if err != nil {
		return nil, err
	}
	redirect, err := matchRedirectURI(rq.RedirectURI, client.RedirectURIs)
	if err != nil {
		return nil, err
	}
	// From here on the redirect URI is the client's own, so errors go back
	// to it (RFC 6749 §4.1.2.1).
	if rq.ResponseType != ResponseTypeCode {
		return &entity.AuthorizeResponse{RedirectURL: withParams(redirect, "error", "unsupported_response_type", "state", rq.State)}, nil
	}
	if rq.CodeChallengeMethod != MethodS256 || !_challenge.MatchString(rq.CodeChallenge) {
		return &entity.AuthorizeResponse{RedirectURL: withParams(redirect,
			"error", "invalid_request", "error_description", "PKCE with code_challenge_method=S256 is required", "state", rq.State)}, nil
	}
	if rq.UserID == 0 {
		return nil, ErrLoginRequired
	}
	user, err := c.users.FindUserByID(ctx, rq.UserID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrLoginRequired
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	consent, err := c.token.CreateConsentToken(token.OAuthGrant{
		UserID:        monoflake.ID(user.ID).String(),
		ClientID:      clientKey(rq.ClientID),
		RedirectURI:   redirect,
		CodeChallenge: rq.CodeChallenge,
		State:         rq.State,
	})
	if err != nil {
		return nil, fmt.Errorf("create consent token: %w", err)
	}
	return &entity.AuthorizeResponse{Consent: &entity.Consent{
		ClientName:  client.Name,
		Verified:    rq.ClientID == CLIClientID,
		Destination: destination(redirect),
		Email:       user.Email,
		Token:       consent,
	}}, nil
}

func (c *controller) Consent(_ context.Context, rq *entity.ConsentRequest) (*entity.ConsentResponse, error) {
	grant, err := c.token.ValidateConsentToken(rq.Token)
	// The answer must come from the user who was asked.
	if err != nil || rq.UserID == 0 || grant.UserID != monoflake.ID(rq.UserID).String() {
		return nil, ErrInvalidConsent
	}
	if !rq.Allow {
		return &entity.ConsentResponse{RedirectURL: withParams(grant.RedirectURI,
			"error", "access_denied", "state", grant.State, "iss", c.issuer)}, nil
	}
	code, err := c.token.CreateAuthorizationCode(grant)
	if err != nil {
		return nil, fmt.Errorf("create code: %w", err)
	}
	// iss lets the client tell this server's answer from another's
	// (RFC 9207).
	return &entity.ConsentResponse{RedirectURL: withParams(grant.RedirectURI, "code", code, "state", grant.State, "iss", c.issuer)}, nil
}

func (c *controller) Token(ctx context.Context, rq *entity.TokenRequest) (*entity.TokenResponse, error) {
	switch rq.GrantType {
	case GrantAuthorizationCode:
		return c.exchangeCode(ctx, rq)
	case GrantRefreshToken:
		return c.refresh(ctx, rq)
	default:
		return nil, ErrUnsupportedGrantType
	}
}

func (c *controller) exchangeCode(ctx context.Context, rq *entity.TokenRequest) (*entity.TokenResponse, error) {
	if rq.Code == "" || rq.ClientID == "" || rq.CodeVerifier == "" {
		return nil, fmt.Errorf("%w: code, client_id and code_verifier are required", ErrInvalidRequest)
	}
	if _, err := c.client(rq.ClientID); err != nil {
		return nil, err
	}
	grant, err := c.token.ValidateAuthorizationCode(rq.Code)
	if err != nil {
		return nil, fmt.Errorf("%w: code is invalid or expired", ErrInvalidGrant)
	}
	if grant.ClientID != clientKey(rq.ClientID) {
		return nil, fmt.Errorf("%w: code was issued to another client", ErrInvalidGrant)
	}
	if rq.RedirectURI != "" && rq.RedirectURI != grant.RedirectURI {
		return nil, fmt.Errorf("%w: redirect_uri does not match", ErrInvalidGrant)
	}
	if !verifyPKCE(rq.CodeVerifier, grant.CodeChallenge) {
		return nil, fmt.Errorf("%w: code_verifier does not match", ErrInvalidGrant)
	}
	return c.issue(ctx, grant)
}

func (c *controller) refresh(ctx context.Context, rq *entity.TokenRequest) (*entity.TokenResponse, error) {
	if rq.RefreshToken == "" {
		return nil, fmt.Errorf("%w: refresh_token is required", ErrInvalidRequest)
	}
	grant, err := c.token.ValidateRefreshToken(rq.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("%w: refresh_token is invalid or expired", ErrInvalidGrant)
	}
	if rq.ClientID != "" && grant.ClientID != clientKey(rq.ClientID) {
		return nil, fmt.Errorf("%w: refresh_token was issued to another client", ErrInvalidGrant)
	}
	return c.issue(ctx, grant)
}

// client finds a client: the CLI, or one registered dynamically.
func (c *controller) client(clientID string) (token.OAuthClient, error) {
	if clientID == CLIClientID {
		return _cliClient, nil
	}
	client, err := c.token.ValidateClientID(clientID)
	if err != nil {
		return token.OAuthClient{}, ErrInvalidClient
	}
	return client, nil
}

// issue mints tokens for a grant's user, as long as the account exists.
func (c *controller) issue(ctx context.Context, grant token.OAuthGrant) (*entity.TokenResponse, error) {
	id := monoflake.IDFromBase62(grant.UserID).Int64()
	_, err := c.users.FindUserByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("%w: user not found", ErrInvalidGrant)
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	access, err := c.token.CreateAccessToken(grant.UserID, grant.ClientID)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}
	refresh, err := c.token.CreateRefreshToken(grant.UserID, grant.ClientID)
	if err != nil {
		return nil, fmt.Errorf("create refresh token: %w", err)
	}
	return &entity.TokenResponse{AccessToken: access, RefreshToken: refresh, ExpiresIn: token.AccessTokenTTL}, nil
}

// checkRedirectURI accepts what a client may register: an https URL, an
// http URL on a loopback address (RFC 8252 §7.3), or a native app's
// private-use scheme (RFC 8252 §7.1). Hosts must be ASCII so the consent
// page cannot be fooled by lookalike letters.
func checkRedirectURI(raw string) error {
	invalid := func(why string) error { return fmt.Errorf("%w: %s: %q", ErrInvalidRedirectURI, why, raw) }
	if len(raw) > _maxRedirectURILen {
		return invalid("too long")
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return invalid("must be an absolute URL")
	}
	if strings.Contains(raw, "#") {
		return invalid("must not have a fragment")
	}
	if u.User != nil {
		return invalid("must not have a user name or password")
	}
	if !_scheme.MatchString(u.Scheme) || slices.Contains(_forbiddenSchemes, u.Scheme) {
		return invalid("scheme not allowed")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil
	}
	if !validHost(u.Hostname()) {
		return invalid("host is not a valid host name")
	}
	if u.Scheme == "http" && !isLoopback(u) {
		return invalid("http is only allowed for localhost")
	}
	return nil
}

// matchRedirectURI returns the redirect URI to use: the requested one when
// the client registered it, or the client's only one when none is asked
// for. A loopback URI matches on any port, since native apps get one from
// the OS at run time (RFC 8252 §7.3).
func matchRedirectURI(requested string, registered []string) (string, error) {
	if requested == "" {
		if len(registered) != 1 {
			return "", fmt.Errorf("%w: redirect_uri is required", ErrInvalidRedirectURI)
		}
		return registered[0], nil
	}
	if slices.Contains(registered, requested) {
		return requested, nil
	}
	ru, err := url.Parse(requested)
	if err != nil || ru.Scheme != "http" || !isLoopback(ru) || ru.User != nil || strings.Contains(requested, "#") {
		return "", fmt.Errorf("%w: not registered for this client", ErrInvalidRedirectURI)
	}
	for _, r := range registered {
		u, _ := url.Parse(r) // Registered URIs were parsed at registration.
		if isLoopback(u) && u.Scheme == ru.Scheme && u.Hostname() == ru.Hostname() && u.Path == ru.Path && u.RawQuery == ru.RawQuery {
			return requested, nil
		}
	}
	return "", fmt.Errorf("%w: not registered for this client", ErrInvalidRedirectURI)
}

func validHost(host string) bool {
	return net.ParseIP(host) != nil || (len(host) <= 253 && _hostname.MatchString(host))
}

func isLoopback(u *url.URL) bool {
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// destination is where a redirect URI goes, for the consent page: a web
// address's origin, an app on this computer, or a native app's scheme.
func destination(redirect string) string {
	u, _ := url.Parse(redirect) // Checked at registration.
	switch {
	case u.Scheme == "http" && isLoopback(u):
		return "an app on this computer (" + u.Host + ")"
	case u.Scheme == "https":
		return u.Host
	default:
		return "the " + u.Scheme + ": app"
	}
}

// withParams adds query parameters to a URL, skipping empty values.
// kv alternates names and values.
func withParams(raw string, kv ...string) string {
	u, _ := url.Parse(raw) // Checked at registration.
	q := u.Query()
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			q.Set(kv[i], kv[i+1])
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// verifyPKCE checks a verifier against an S256 challenge (RFC 7636 §4.6).
func verifyPKCE(verifier, challenge string) bool {
	if !_verifier.MatchString(verifier) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// clientKey is a short, fixed-size stand-in for a client ID inside the
// tokens issued to it.
func clientKey(clientID string) string {
	sum := sha256.Sum256([]byte(clientID))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// cleanName keeps a client name printable and short.
func cleanName(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, name)
	if r := []rune(strings.TrimSpace(name)); len(r) > _maxClientName {
		return strings.TrimSpace(string(r[:_maxClientName]))
	}
	return strings.TrimSpace(name)
}
