// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

func TestOAuthMetadataMappers(t *testing.T) {
	assert.JSONEq(t, `{
		"issuer":"https://t.io","authorization_endpoint":"https://t.io/a","token_endpoint":"https://t.io/t","registration_endpoint":"https://t.io/r",
		"response_types_supported":["code"],"grant_types_supported":["authorization_code","refresh_token"],
		"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none"],
		"authorization_response_iss_parameter_supported":true}`,
		string(FromGetOAuthServerMetadataResponseEntityToHTTPResponse(&entity.GetOAuthServerMetadataResponse{
			Issuer: "https://t.io", AuthorizationEndpoint: "https://t.io/a", TokenEndpoint: "https://t.io/t", RegistrationEndpoint: "https://t.io/r",
		})))
	assert.JSONEq(t, `{"resource":"https://t.io/mcp","authorization_servers":["https://t.io"],"bearer_methods_supported":["header"],"resource_name":"tmphost.io"}`,
		string(FromGetProtectedResourceMetadataResponseEntityToHTTPResponse(&entity.GetProtectedResourceMetadataResponse{
			Resource: "https://t.io/mcp", AuthorizationServers: []string{"https://t.io"},
		})))
}

func TestRegisterClientMappers(t *testing.T) {
	var got *entity.RegisterClientRequest
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"client_name":"CLI","redirect_uris":["http://127.0.0.1/cb"],
		"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`))
	req.Header.Set("Content-Type", "application/json")
	runAs(t, 0, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToRegisterClientRequestEntity(c) })
	assert.Equal(t, &entity.RegisterClientRequest{
		ClientName: "CLI", RedirectURIs: []string{"http://127.0.0.1/cb"}, GrantTypes: []string{"authorization_code"},
		ResponseTypes: []string{"code"}, TokenEndpointAuthMethod: "none",
	}, got)

	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`nope`))
	req.Header.Set("Content-Type", "application/json")
	runAs(t, 0, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToRegisterClientRequestEntity(c) })
	assert.Nil(t, got)

	assert.JSONEq(t, `{"client_id":"cid","client_id_issued_at":10,"client_name":"CLI","redirect_uris":["http://127.0.0.1/cb"],
		"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`,
		string(FromRegisterClientResponseEntityToHTTPResponse(&entity.RegisterClientResponse{
			ClientID: "cid", ClientName: "CLI", RedirectURIs: []string{"http://127.0.0.1/cb"}, IssuedAt: time.Unix(10, 0),
		})))
}

func TestFromHTTPRequestToAuthorizeRequestEntity(t *testing.T) {
	var got *entity.AuthorizeRequest
	runAs(t, 7, "/", httptest.NewRequest(http.MethodGet,
		"/?response_type=code&client_id=c&redirect_uri=r&code_challenge=ch&code_challenge_method=S256&state=s", nil),
		func(c fiber.Ctx) { got = FromHTTPRequestToAuthorizeRequestEntity(c) })
	assert.Equal(t, &entity.AuthorizeRequest{
		UserID: 7, ResponseType: "code", ClientID: "c", RedirectURI: "r", CodeChallenge: "ch", CodeChallengeMethod: "S256", State: "s",
	}, got)

	runAs(t, 7, "/", httptest.NewRequest(http.MethodGet, "/?response_type=code", nil),
		func(c fiber.Ctx) { got = FromHTTPRequestToAuthorizeRequestEntity(c) })
	assert.Nil(t, got)
}

func TestFromHTTPRequestToConsentRequestEntity(t *testing.T) {
	form := func(userID int64, body string) *entity.ConsentRequest {
		var got *entity.ConsentRequest
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		runAs(t, userID, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToConsentRequestEntity(c) })
		return got
	}
	assert.Equal(t, &entity.ConsentRequest{UserID: 7, Token: "t", Allow: true}, form(7, "consent=t&decision=allow"))
	assert.Equal(t, &entity.ConsentRequest{Token: "t"}, form(0, "consent=t&decision=deny"))
	assert.Nil(t, form(7, "decision=allow"))
}

func TestTokenMappers(t *testing.T) {
	var got *entity.TokenRequest
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		"grant_type=authorization_code&code=c&redirect_uri=r&client_id=id&code_verifier=v&refresh_token=rt"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	runAs(t, 0, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToTokenRequestEntity(c) })
	assert.Equal(t, &entity.TokenRequest{
		GrantType: "authorization_code", Code: "c", RedirectURI: "r", ClientID: "id", CodeVerifier: "v", RefreshToken: "rt",
	}, got)

	assert.JSONEq(t, `{"access_token":"a","token_type":"Bearer","expires_in":3600,"refresh_token":"r"}`,
		string(FromTokenResponseEntityToHTTPResponse(&entity.TokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: time.Hour})))
	assert.JSONEq(t, `{"error":"invalid_grant","error_description":"expired"}`, string(FromOAuthErrorToHTTPResponse("invalid_grant", "expired")))
	assert.JSONEq(t, `{"error":"server_error"}`, string(FromOAuthErrorToHTTPResponse("server_error", "")))
}
