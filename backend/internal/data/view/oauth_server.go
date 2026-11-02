// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// OAuthServerMetadata is the authorization server metadata (RFC 8414).
	OAuthServerMetadata struct {
		Issuer                                     string   `json:"issuer"`
		AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
		TokenEndpoint                              string   `json:"token_endpoint"`
		RegistrationEndpoint                       string   `json:"registration_endpoint"`
		ResponseTypesSupported                     []string `json:"response_types_supported"`
		GrantTypesSupported                        []string `json:"grant_types_supported"`
		CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
		TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
		AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
	}

	// ProtectedResourceMetadata is the protected resource metadata
	// (RFC 9728).
	ProtectedResourceMetadata struct {
		Resource               string   `json:"resource"`
		AuthorizationServers   []string `json:"authorization_servers"`
		BearerMethodsSupported []string `json:"bearer_methods_supported"`
		ResourceName           string   `json:"resource_name"`
	}

	// RegisterClientRequest is client metadata (RFC 7591 §2).
	RegisterClientRequest struct {
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}

	// RegisterClientResponse is a registered client (RFC 7591 §3.2.1).
	RegisterClientResponse struct {
		ClientID                string   `json:"client_id"`
		ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
		ClientName              string   `json:"client_name,omitempty"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}

	// TokenResponse is a successful token response (RFC 6749 §5.1).
	TokenResponse struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}

	// OAuthError is an OAuth error response (RFC 6749 §5.2).
	OAuthError struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description,omitempty"`
	}
)
