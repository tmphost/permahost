// Copyright 2026 Contextual, Inc. https://tmphost.io
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
		// Verified is true for tmphost's own client, whose name is not
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
	}

	// TokenResponse carries the issued tokens.
	TokenResponse struct {
		AccessToken  string
		RefreshToken string
		ExpiresIn    time.Duration
	}
)
