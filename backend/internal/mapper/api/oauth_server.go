// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"time"

	"github.com/gofiber/fiber/v3"

	oauthctrl "github.com/tmphost/tmphost/backend/internal/controller/oauthserver"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/view"
)

// ConsentDecisionAllow is the consent form's decision to grant access.
const ConsentDecisionAllow = "allow"

var _grantTypes = []string{oauthctrl.GrantAuthorizationCode, oauthctrl.GrantRefreshToken}

// FromGetOAuthServerMetadataResponseEntityToHTTPResponse maps the
// authorization server metadata.
func FromGetOAuthServerMetadataResponseEntityToHTTPResponse(rs *entity.GetOAuthServerMetadataResponse) []byte {
	return marshal(view.OAuthServerMetadata{
		Issuer:                                     rs.Issuer,
		AuthorizationEndpoint:                      rs.AuthorizationEndpoint,
		TokenEndpoint:                              rs.TokenEndpoint,
		RegistrationEndpoint:                       rs.RegistrationEndpoint,
		ResponseTypesSupported:                     []string{oauthctrl.ResponseTypeCode},
		GrantTypesSupported:                        _grantTypes,
		CodeChallengeMethodsSupported:              []string{oauthctrl.MethodS256},
		TokenEndpointAuthMethodsSupported:          []string{oauthctrl.AuthMethodNone},
		AuthorizationResponseIssParameterSupported: true,
	})
}

// FromGetProtectedResourceMetadataResponseEntityToHTTPResponse maps the
// protected resource metadata.
func FromGetProtectedResourceMetadataResponseEntityToHTTPResponse(rs *entity.GetProtectedResourceMetadataResponse) []byte {
	return marshal(view.ProtectedResourceMetadata{
		Resource:               rs.Resource,
		AuthorizationServers:   rs.AuthorizationServers,
		BearerMethodsSupported: []string{"header"},
		ResourceName:           "tmphost.io",
	})
}

// FromHTTPRequestToRegisterClientRequestEntity reads client metadata from
// a JSON body.
func FromHTTPRequestToRegisterClientRequestEntity(c fiber.Ctx) *entity.RegisterClientRequest {
	var v view.RegisterClientRequest
	if err := c.Bind().JSON(&v); err != nil {
		return nil
	}
	return &entity.RegisterClientRequest{
		ClientName:              v.ClientName,
		RedirectURIs:            v.RedirectURIs,
		GrantTypes:              v.GrantTypes,
		ResponseTypes:           v.ResponseTypes,
		TokenEndpointAuthMethod: v.TokenEndpointAuthMethod,
	}
}

// FromRegisterClientResponseEntityToHTTPResponse maps a registered client.
// Every client is public, whatever auth method it asked for.
func FromRegisterClientResponseEntityToHTTPResponse(rs *entity.RegisterClientResponse) []byte {
	return marshal(view.RegisterClientResponse{
		ClientID:                rs.ClientID,
		ClientIDIssuedAt:        rs.IssuedAt.Unix(),
		ClientName:              rs.ClientName,
		RedirectURIs:            rs.RedirectURIs,
		GrantTypes:              _grantTypes,
		ResponseTypes:           []string{oauthctrl.ResponseTypeCode},
		TokenEndpointAuthMethod: oauthctrl.AuthMethodNone,
	})
}

// FromHTTPRequestToAuthorizeRequestEntity reads an authorization request
// from the query, with the signed-in user if any. It returns nil without a
// client_id.
func FromHTTPRequestToAuthorizeRequestEntity(c fiber.Ctx) *entity.AuthorizeRequest {
	if c.Query("client_id") == "" {
		return nil
	}
	return &entity.AuthorizeRequest{
		UserID:              localUserID(c),
		ResponseType:        c.Query("response_type"),
		ClientID:            c.Query("client_id"),
		RedirectURI:         c.Query("redirect_uri"),
		CodeChallenge:       c.Query("code_challenge"),
		CodeChallengeMethod: c.Query("code_challenge_method"),
		State:               c.Query("state"),
	}
}

// FromHTTPRequestToConsentRequestEntity reads the consent form, with the
// signed-in user if any. It returns nil without a consent token.
func FromHTTPRequestToConsentRequestEntity(c fiber.Ctx) *entity.ConsentRequest {
	consent := c.FormValue("consent")
	if consent == "" {
		return nil
	}
	return &entity.ConsentRequest{UserID: localUserID(c), Token: consent, Allow: c.FormValue("decision") == ConsentDecisionAllow}
}

// FromHTTPRequestToTokenRequestEntity reads a form-encoded token request.
func FromHTTPRequestToTokenRequestEntity(c fiber.Ctx) *entity.TokenRequest {
	return &entity.TokenRequest{
		GrantType:    c.FormValue("grant_type"),
		Code:         c.FormValue("code"),
		RedirectURI:  c.FormValue("redirect_uri"),
		ClientID:     c.FormValue("client_id"),
		CodeVerifier: c.FormValue("code_verifier"),
		RefreshToken: c.FormValue("refresh_token"),
	}
}

// FromTokenResponseEntityToHTTPResponse maps issued tokens.
func FromTokenResponseEntityToHTTPResponse(rs *entity.TokenResponse) []byte {
	return marshal(view.TokenResponse{
		AccessToken:  rs.AccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(rs.ExpiresIn / time.Second),
		RefreshToken: rs.RefreshToken,
	})
}

// FromOAuthErrorToHTTPResponse maps an OAuth error code and description.
func FromOAuthErrorToHTTPResponse(code, description string) []byte {
	return marshal(view.OAuthError{Error: code, ErrorDescription: description})
}
