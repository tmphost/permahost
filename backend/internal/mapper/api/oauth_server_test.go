// Copyright 2026 Contextual, Inc. https://permahost.io
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

	"github.com/tmphost/permahost/backend/internal/data/entity"
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
	assert.JSONEq(t, `{"resource":"https://t.io/mcp","authorization_servers":["https://t.io"],"bearer_methods_supported":["header"],"resource_name":"permahost.io"}`,
		string(FromGetProtectedResourceMetadataResponseEntityToHTTPResponse(&entity.GetProtectedResourceMetadataResponse{
			Resource: "https://t.io/mcp", AuthorizationServers: []string{"https://t.io"},
		})))

	// With a device authorization endpoint, the device grant is offered too.
	assert.JSONEq(t, `{
		"issuer":"https://t.io","authorization_endpoint":"https://t.io/a","token_endpoint":"https://t.io/t","registration_endpoint":"https://t.io/r",
		"response_types_supported":["code"],"grant_types_supported":["authorization_code","refresh_token","urn:ietf:params:oauth:grant-type:device_code"],
		"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none"],
		"authorization_response_iss_parameter_supported":true,"device_authorization_endpoint":"https://t.io/d"}`,
		string(FromGetOAuthServerMetadataResponseEntityToHTTPResponse(&entity.GetOAuthServerMetadataResponse{
			Issuer: "https://t.io", AuthorizationEndpoint: "https://t.io/a", TokenEndpoint: "https://t.io/t", RegistrationEndpoint: "https://t.io/r",
			DeviceAuthorizationEndpoint: "https://t.io/d",
		})))
	assert.Equal(t, []string{"authorization_code", "refresh_token"}, _grantTypes, "registered clients get no device grant")
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

func TestFromHTTPRequestToMetadataRequestEntities(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "mcp.t.io:8443"
	var server *entity.GetOAuthServerMetadataRequest
	runAs(t, 0, "/", req, func(c fiber.Ctx) { server = FromHTTPRequestToGetOAuthServerMetadataRequestEntity(c) })
	assert.Equal(t, &entity.GetOAuthServerMetadataRequest{Host: "mcp.t.io:8443"}, server)

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "mcp.t.io:8443"
	var resource *entity.GetProtectedResourceMetadataRequest
	runAs(t, 0, "/", req, func(c fiber.Ctx) { resource = FromHTTPRequestToGetProtectedResourceMetadataRequestEntity(c) })
	assert.Equal(t, &entity.GetProtectedResourceMetadataRequest{Host: "mcp.t.io:8443"}, resource)
}

func TestFromHTTPRequestToConsentRequestEntity(t *testing.T) {
	formOn := func(route string, userID int64, body string) *entity.ConsentRequest {
		var got *entity.ConsentRequest
		req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		runAs(t, userID, route, req, func(c fiber.Ctx) { got = FromHTTPRequestToConsentRequestEntity(c) })
		return got
	}
	form := func(userID int64, body string) *entity.ConsentRequest {
		return formOn("/oauth2/authorize", userID, body)
	}
	assert.Equal(t, &entity.ConsentRequest{UserID: 7, Token: "t", Allow: true, MCP: true}, formOn("/oauth2/mcp/authorize", 7, "consent=t&decision=allow"))
	assert.Equal(t, &entity.ConsentRequest{UserID: 7, Token: "t", Allow: true}, form(7, "consent=t&decision=allow"))
	assert.Equal(t, &entity.ConsentRequest{Token: "t"}, form(0, "consent=t&decision=deny"))
	assert.Nil(t, form(7, "decision=allow"))
}

func TestTokenMappers(t *testing.T) {
	var got *entity.TokenRequest
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(
		"grant_type=authorization_code&code=c&redirect_uri=r&client_id=id&code_verifier=v&refresh_token=rt&device_code=dc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	runAs(t, 0, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToTokenRequestEntity(c) })
	assert.Equal(t, &entity.TokenRequest{
		GrantType: "authorization_code", Code: "c", RedirectURI: "r", ClientID: "id", CodeVerifier: "v", RefreshToken: "rt", DeviceCode: "dc",
	}, got)

	assert.JSONEq(t, `{"access_token":"a","token_type":"Bearer","expires_in":3600,"refresh_token":"r"}`,
		string(FromTokenResponseEntityToHTTPResponse(&entity.TokenResponse{AccessToken: "a", RefreshToken: "r", ExpiresIn: time.Hour})))
	assert.JSONEq(t, `{"error":"invalid_grant","error_description":"expired"}`, string(FromOAuthErrorToHTTPResponse("invalid_grant", "expired")))
	assert.JSONEq(t, `{"error":"server_error"}`, string(FromOAuthErrorToHTTPResponse("server_error", "")))
}

func TestDeviceMappers(t *testing.T) {
	const ip = "0.0.0.0" // What app.Test reports.
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("client_id=cli"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var authorize *entity.AuthorizeDeviceRequest
	runAs(t, 0, "/", req, func(c fiber.Ctx) { authorize = FromHTTPRequestToAuthorizeDeviceRequestEntity(c) })
	assert.Equal(t, &entity.AuthorizeDeviceRequest{ClientID: "cli", IP: ip}, authorize)

	assert.JSONEq(t, `{"device_code":"dc","user_code":"WDJB-MJHT","verification_uri":"https://t.io/auth/device",
		"verification_uri_complete":"https://t.io/auth/device?user_code=WDJB-MJHT","expires_in":600,"interval":5}`,
		string(FromAuthorizeDeviceResponseEntityToHTTPResponse(&entity.AuthorizeDeviceResponse{
			DeviceCode: "dc", UserCode: "WDJB-MJHT", VerificationURI: "https://t.io/auth/device",
			VerificationURIComplete: "https://t.io/auth/device?user_code=WDJB-MJHT", ExpiresIn: 10 * time.Minute, Interval: 5 * time.Second,
		})))

	var verify *entity.VerifyDeviceRequest
	runAs(t, 7, "/", httptest.NewRequest(http.MethodGet, "/?user_code=WDJB-MJHT", nil), func(c fiber.Ctx) { verify = FromHTTPRequestToVerifyDeviceRequestEntity(c) })
	assert.Equal(t, &entity.VerifyDeviceRequest{UserID: 7, UserCode: "WDJB-MJHT", IP: ip}, verify)

	consent := func(userID int64, body string) *entity.ConsentDeviceRequest {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		var got *entity.ConsentDeviceRequest
		runAs(t, userID, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToConsentDeviceRequestEntity(c) })
		return got
	}
	assert.Equal(t, &entity.ConsentDeviceRequest{UserID: 7, Token: "t", Allow: true}, consent(7, "consent=t&decision=allow"))
	assert.Equal(t, &entity.ConsentDeviceRequest{Token: "t"}, consent(0, "consent=t&decision=deny"))
	assert.Nil(t, consent(7, "decision=allow"))
}
