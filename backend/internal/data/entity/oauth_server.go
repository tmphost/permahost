// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

import "time"

type (
	// GetOAuthServerMetadataResponse is the authorization server metadata
	// (RFC 8414).
	GetOAuthServerMetadataResponse struct {
		Issuer                string
		AuthorizationEndpoint string
		TokenEndpoint         string
		RegistrationEndpoint  string
		// DeviceAuthorizationEndpoint is empty where there is none.
		DeviceAuthorizationEndpoint string
	}

	// GetOAuthServerMetadataRequest asks for the metadata as seen on Host
	// (the Host header, port included).
	GetOAuthServerMetadataRequest struct {
		Host string
	}

	// GetProtectedResourceMetadataRequest asks for the metadata as seen
	// on Host (the Host header, port included).
	GetProtectedResourceMetadataRequest struct {
		Host string
	}

	// GetProtectedResourceMetadataResponse is the protected resource
	// metadata (RFC 9728).
	GetProtectedResourceMetadataResponse struct {
		Resource             string
		AuthorizationServers []string
	}

	// RegisterClientRequest registers a public OAuth client (RFC 7591).
	RegisterClientRequest struct {
		ClientName              string
		RedirectURIs            []string
		GrantTypes              []string
		ResponseTypes           []string
		TokenEndpointAuthMethod string
	}

	// RegisterClientResponse carries the registered client.
	RegisterClientResponse struct {
		ClientID     string
		ClientName   string
		RedirectURIs []string
		IssuedAt     time.Time
	}

	// AuthorizeRequest is an authorization request (RFC 6749 §4.1.1).
	AuthorizeRequest struct {
		// UserID is the signed-in user, or 0.
		UserID              int64
		ResponseType        string
		ClientID            string
		RedirectURI         string
		CodeChallenge       string
		CodeChallengeMethod string
		State               string
	}

	// AuthorizeResponse either sends the browser back to the client with
	// an error (RedirectURL) or asks the user to consent (Consent).
	AuthorizeResponse struct {
		RedirectURL string
		Consent     *Consent
	}

	// Consent is what the consent page shows.
	Consent struct {
		ClientName string
		// Verified is true for permahost's own client, whose name is not
		// just its own claim.
		Verified bool
		// Destination is where the browser goes back to, for people.
		Destination string
		Email       string
		// Token carries the request to the user's answer.
		Token string
	}

	// ConsentRequest is the user's answer on the consent page.
	ConsentRequest struct {
		UserID int64
		Token  string
		Allow  bool
		// MCP is set for an answer to the MCP host issuer's authorization
		// endpoint, so it goes back with that issuer.
		MCP bool
	}

	// ConsentResponse sends the browser back to the client.
	ConsentResponse struct {
		RedirectURL string
	}

	// TokenRequest is a token request (RFC 6749 §4.1.3 and §6).
	TokenRequest struct {
		GrantType    string
		Code         string
		RedirectURI  string
		ClientID     string
		CodeVerifier string
		RefreshToken string
		DeviceCode   string
	}

	// TokenResponse carries the issued tokens.
	TokenResponse struct {
		AccessToken  string
		RefreshToken string
		ExpiresIn    time.Duration
	}

	// AuthorizeDeviceRequest asks for a device authorization (RFC 8628
	// §3.1), from IP.
	AuthorizeDeviceRequest struct {
		ClientID string
		IP       string
	}

	// AuthorizeDeviceResponse is a device authorization (RFC 8628 §3.2).
	AuthorizeDeviceResponse struct {
		DeviceCode              string
		UserCode                string
		VerificationURI         string
		VerificationURIComplete string
		ExpiresIn               time.Duration
		Interval                time.Duration
	}

	// VerifyDeviceRequest is a user code the signed-in user typed, from IP.
	VerifyDeviceRequest struct {
		// UserID is the signed-in user, or 0.
		UserID   int64
		UserCode string
		IP       string
	}

	// VerifyDeviceResponse asks the user to consent.
	VerifyDeviceResponse struct {
		Consent *DeviceConsent
	}

	// DeviceConsent is what the device consent page shows.
	DeviceConsent struct {
		ClientName string
		// Verified is true for permahost's own client.
		Verified bool
		// UserCode is the code the device shows, to compare.
		UserCode string
		Email    string
		// Token carries the authorization to the user's answer.
		Token string
	}

	// ConsentDeviceRequest is the user's answer on the device consent page.
	ConsentDeviceRequest struct {
		UserID int64
		Token  string
		Allow  bool
	}

	// ConsentDeviceResponse tells the user what happens next.
	ConsentDeviceResponse struct {
		ClientName string
		Allowed    bool
	}
)
