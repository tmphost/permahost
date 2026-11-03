// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authctrl "github.com/tmphost/permahost/backend/internal/controller/auth"
	mockauth "github.com/tmphost/permahost/backend/internal/controller/auth/mocks"
	mockfolder "github.com/tmphost/permahost/backend/internal/controller/folder/mocks"
	"github.com/tmphost/permahost/backend/internal/data/entity"
)

const _endpoint = "https://mcp.t.io/mcp"

func newSetupServer(t *testing.T) *httptest.Server {
	t.Helper()
	mc := gomock.NewController(t)
	authn := mockauth.NewMockController(mc)
	authn.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{AccessToken: "bad"}).Return(nil, authctrl.ErrUnauthorized).AnyTimes()
	srv := httptest.NewServer(New(Params{Folders: mockfolder.NewMockController(mc), Auth: authn, EndpointURL: _endpoint, ResourceMetadataURL: "https://mcp.t.io/.well-known/oauth-protected-resource/mcp"}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSetupPage(t *testing.T) {
	srv := newSetupServer(t)
	for name, accept := range map[string]string{"no accept": "", "browser": "text/html,application/xhtml+xml,*/*;q=0.8", "any": "*/*"} {
		t.Run(name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			if accept != "" {
				req.Header.Set("Accept", accept)
			}
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer func() { _ = res.Body.Close() }()
			body, _ := io.ReadAll(res.Body)

			assert.Equal(t, http.StatusOK, res.StatusCode)
			assert.Equal(t, "text/html; charset=utf-8", res.Header.Get("Content-Type"))
			assert.Equal(t, _setupPageCSP, res.Header.Get("Content-Security-Policy"))
			assert.Equal(t, "DENY", res.Header.Get("X-Frame-Options"))
			page := string(body)
			assert.Contains(t, page, "<title>permahost.io MCP server</title>")
			assert.Contains(t, page, "claude mcp add --transport http permahost "+_endpoint)
			assert.Contains(t, page, "codex mcp add permahost --url "+_endpoint)
			assert.Contains(t, page, "gemini mcp add --transport http permahost "+_endpoint)
			assert.Contains(t, page, `<img src="data:image/svg&#43;xml;base64,`)
			assert.NotContains(t, page, "<script")
			for _, action := range _actionSummaries {
				assert.Contains(t, page, "<code>"+action.Name+"</code>: "+template.HTMLEscapeString(action.Summary))
			}
		})
	}

	res, err := http.Head(srv.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode, "HEAD")
}

// TestSetupPage_MCPClients checks that MCP clients still get the 401 that
// starts their sign-in.
func TestSetupPage_MCPClients(t *testing.T) {
	srv := newSetupServer(t)
	for name, req := range map[string]func() *http.Request{
		"post": func() *http.Request {
			r, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{}`))
			r.Header.Set("Accept", "*/*")
			return r
		},
		"event stream": func() *http.Request {
			r, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			r.Header.Set("Accept", "text/event-stream")
			return r
		},
		"json": func() *http.Request {
			r, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			r.Header.Set("Accept", "Application/JSON")
			return r
		},
		"token": func() *http.Request {
			r, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
			r.Header.Set("Authorization", "Bearer bad")
			return r
		},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := http.DefaultClient.Do(req())
			require.NoError(t, err)
			_ = res.Body.Close()
			assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
			assert.Contains(t, res.Header.Get("WWW-Authenticate"), "resource_metadata=")
		})
	}
}

// TestSetupPage_ListsEveryAction keeps the page's action list in step with
// the file tool's.
func TestSetupPage_ListsEveryAction(t *testing.T) {
	e := newEnv(t)
	rs, err := e.session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, rs.Tools, 1)
	raw, err := json.Marshal(rs.Tools[0].InputSchema)
	require.NoError(t, err)
	var schema struct {
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	var listed []string
	for _, action := range _actionSummaries {
		listed = append(listed, action.Name)
	}
	assert.Equal(t, schema.Properties.Action.Enum, listed)
}

// TestSetupPage_Icon keeps the page's icon equal to the app's favicon.
func TestSetupPage_Icon(t *testing.T) {
	favicon, err := os.ReadFile("../../../../frontend/public/favicon.svg")
	require.NoError(t, err)
	assert.Equal(t, string(favicon), string(_icon))
}
