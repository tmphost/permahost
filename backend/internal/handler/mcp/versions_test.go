// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockauth "github.com/tmphost/permahost/backend/internal/controller/auth/mocks"
	mockfolder "github.com/tmphost/permahost/backend/internal/controller/folder/mocks"
	"github.com/tmphost/permahost/backend/internal/data/entity"
)

const _meta2026 = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}`

// rpcResponse is a decoded JSON-RPC response.
type rpcResponse struct {
	ID     any            `json:"id"`
	Result map[string]any `json:"result"`
	Error  struct {
		Code int `json:"code"`
		Data struct {
			Supported []string `json:"supported"`
			Requested string   `json:"requested"`
		} `json:"data"`
	} `json:"error"`
}

// post sends body to a real server with the given headers, besides the
// token and content type.
func post(t *testing.T, headers map[string]string, body string) (*http.Response, rpcResponse) {
	t.Helper()
	mc := gomock.NewController(t)
	authn := mockauth.NewMockController(mc)
	authn.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(&entity.AuthenticateResponse{UserID: 7}, nil)
	srv := httptest.NewServer(New(Params{Folders: mockfolder.NewMockController(mc), Auth: authn, MaxBodyBytes: 1 << 20}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+_key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	var rs rpcResponse
	require.NoError(t, json.NewDecoder(res.Body).Decode(&rs))
	return res, rs
}

func TestNegotiate_EchoesVersion(t *testing.T) {
	res, rs := post(t, map[string]string{_headerVersion: "2026-07-28", _headerMethod: "tools/list"},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{`+_meta2026+`}}`)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "2026-07-28", res.Header.Get(_headerVersion))
	assert.Contains(t, rs.Result, "ttlMs", "a 2026-07-28 client gets the cache hints")
}

func TestNegotiate_Unsupported(t *testing.T) {
	for name, tc := range map[string]struct {
		headers map[string]string
		body    string
		id      any
	}{
		"header": {map[string]string{_headerVersion: "1999-01-01", _headerMethod: "tools/list"},
			`{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"1999-01-01"}}}`, float64(3)},
		"meta only": {nil, `{"jsonrpc":"2.0","id":"a","method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"1999-01-01"}}}`, "a"},
		"no id":     {map[string]string{_headerVersion: "1999-01-01"}, `{"jsonrpc":"2.0","method":"tools/list"}`, nil},
	} {
		res, rs := post(t, tc.headers, tc.body)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode, name)
		assert.Equal(t, "application/json", res.Header.Get("Content-Type"), name)
		assert.Equal(t, tc.id, rs.ID, name)
		assert.Equal(t, mcp.CodeUnsupportedProtocolVersion, rs.Error.Code, name)
		assert.Equal(t, mcp.SupportedProtocolVersions(), rs.Error.Data.Supported, name+": the versions server/discover lists")
		assert.Equal(t, "1999-01-01", rs.Error.Data.Requested, name)
	}
}

func TestNegotiate_VersionlessDiscover(t *testing.T) {
	for name, params := range map[string]string{
		"no params":    ``,
		"empty params": `,"params":{}`,
		"other meta":   `,"params":{"_meta":{"io.modelcontextprotocol/clientCapabilities":{"x":{}}}}`,
	} {
		res, rs := post(t, nil, `{"jsonrpc":"2.0","id":1,"method":"server/discover"`+params+`}`)
		assert.Equal(t, http.StatusOK, res.StatusCode, name)
		assert.Equal(t, "2026-07-28", res.Header.Get(_headerVersion), name)
		assert.Equal(t, []any{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}, rs.Result["supportedVersions"], name)
	}
}

func TestNegotiate_OlderClientGetsNoNewerFields(t *testing.T) {
	for name, meta := range map[string]string{
		"header only": `{}`,
		"with _meta":  `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2025-11-25","io.modelcontextprotocol/clientCapabilities":{}}}`,
	} {
		res, rs := post(t, map[string]string{_headerVersion: "2025-11-25", _headerMethod: "tools/list"},
			`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":`+meta+`}`)
		assert.Equal(t, http.StatusOK, res.StatusCode, name)
		assert.Equal(t, "2025-11-25", res.Header.Get(_headerVersion), name)
		require.Len(t, rs.Result["tools"], 5, name)
		for _, f := range _newerResultFields {
			assert.NotContains(t, rs.Result, f, name)
		}
	}
}

// serve runs negotiate around next on one request.
func serve(next http.HandlerFunc, max int64, method string, body io.Reader) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	negotiate(next, max).ServeHTTP(w, httptest.NewRequest(method, "/", body))
	return w
}

func TestNegotiate_PassesThrough(t *testing.T) {
	var got string
	next := func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = r.Method + " " + string(b)
		w.WriteHeader(http.StatusTeapot)
	}
	assert.Equal(t, http.StatusTeapot, serve(next, 0, http.MethodGet, nil).Code)
	assert.Equal(t, "GET ", got, "only POST bodies are read")

	w := serve(next, 0, http.MethodPost, strings.NewReader(`[{"jsonrpc":"2.0"}]`))
	assert.Equal(t, http.StatusTeapot, w.Code)
	assert.Equal(t, `POST [{"jsonrpc":"2.0"}]`, got, "a batch goes to the SDK as it is")
}

func TestNegotiate_BodyErrors(t *testing.T) {
	next := func(http.ResponseWriter, *http.Request) { t.Fatal("not served") }
	assert.Equal(t, http.StatusRequestEntityTooLarge, serve(next, 4, http.MethodPost, strings.NewReader(`{"jsonrpc":"2.0"}`)).Code)
	assert.Equal(t, http.StatusBadRequest, serve(next, 0, http.MethodPost, failingReader{}).Code)
}

func TestNegotiate_OlderResponses(t *testing.T) {
	for name, body := range map[string]string{
		"not JSON": `not json`,
		"error":    `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"no"}}`,
	} {
		next := func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(body))
		}
		w := serve(next, 0, http.MethodPost, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
		assert.Equal(t, http.StatusAccepted, w.Code, name)
		assert.Equal(t, body, w.Body.String(), name+": left as it is")
		assert.Empty(t, w.Header().Get(_headerVersion), name+": no version to echo")
	}
}
