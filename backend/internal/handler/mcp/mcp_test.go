// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/permahost/backend/internal/brand"
	authctrl "github.com/tmphost/permahost/backend/internal/controller/auth"
	mockauth "github.com/tmphost/permahost/backend/internal/controller/auth/mocks"
	folderctrl "github.com/tmphost/permahost/backend/internal/controller/folder"
	mockfolder "github.com/tmphost/permahost/backend/internal/controller/folder/mocks"
	"github.com/tmphost/permahost/backend/internal/data/entity"
)

const _key = "access-token"

var (
	errBoom = errors.New("boom")
	// Folder 1000 is "000000rs" as an 11-character folder ID.
	_folder = entity.Folder{ID: 1000, UserID: 7, URL: "https://000000rs.file.tmp.host/"}
)

type env struct {
	folders *mockfolder.MockController
	session *mcp.ClientSession
}

type bearer struct{ key string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.key)
	return http.DefaultTransport.RoundTrip(r)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	mc := gomock.NewController(t)
	authn := mockauth.NewMockController(mc)
	authn.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{AccessToken: _key}).Return(&entity.AuthenticateResponse{UserID: 7}, nil).AnyTimes()
	e := &env{folders: mockfolder.NewMockController(mc)}
	srv := httptest.NewServer(New(Params{Folders: e.folders, Auth: authn, MaxBodyBytes: 1 << 20}))
	t.Cleanup(srv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             srv.URL,
		HTTPClient:           &http.Client{Transport: bearer{_key}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	e.session = session
	return e
}

// call runs the tool name, decoding its structured output into out, or
// returns its error text.
func (e *env) call(t *testing.T, name string, args map[string]any, out any) string {
	t.Helper()
	rs, err := e.session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	if rs.IsError {
		return rs.Content[0].(*mcp.TextContent).Text
	}
	b, _ := json.Marshal(rs.StructuredContent)
	require.NoError(t, json.Unmarshal(b, out))
	return ""
}

func TestUnauthorized(t *testing.T) {
	mc := gomock.NewController(t)
	authn := mockauth.NewMockController(mc)
	authn.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{AccessToken: "bad"}).Return(nil, authctrl.ErrUnauthorized)
	srv := httptest.NewServer(New(Params{Folders: mockfolder.NewMockController(mc), Auth: authn, ResourceMetadataURL: "https://t.io/.well-known/oauth-protected-resource/mcp"}))
	defer srv.Close()

	res, err := http.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode, "no token")
	assert.Contains(t, res.Header.Get("WWW-Authenticate"), `resource_metadata="https://t.io/.well-known/oauth-protected-resource/mcp"`)

	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer bad")
	res, err = http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func TestListTools(t *testing.T) {
	e := newEnv(t)
	rs, err := e.session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	byName := map[string]*mcp.Tool{}
	for _, tool := range rs.Tools {
		byName[tool.Name] = tool
		assertIcons(t, tool.Icons)
		require.NotNil(t, tool.Annotations)
		assert.Equal(t, tool.Title, tool.Annotations.Title)
		assert.NotEmpty(t, tool.Description)
	}
	assert.ElementsMatch(t, []string{"uploadFile", "downloadFile", "listFiles", "getUrl", "listFolders"}, slices.Collect(maps.Keys(byName)))

	for name, want := range map[string]struct {
		properties []string
		required   []string
	}{
		"uploadFile":   {[]string{"folderId", "fileName", "path", "format", "base64Content", "textContent"}, []string{"fileName"}},
		"downloadFile": {[]string{"folderId", "fileName", "path", "format"}, []string{"folderId", "fileName"}},
		"listFiles":    {[]string{"folderId"}, []string{"folderId"}},
		"getUrl":       {[]string{"folderId", "path", "fileName"}, []string{"folderId"}},
		"listFolders":  {nil, nil},
	} {
		t.Run(name, func(t *testing.T) {
			var schema struct {
				Properties map[string]struct {
					Enum []string `json:"enum"`
				} `json:"properties"`
				Required []string `json:"required"`
			}
			b, _ := json.Marshal(byName[name].InputSchema)
			require.NoError(t, json.Unmarshal(b, &schema))
			assert.ElementsMatch(t, want.properties, slices.Collect(maps.Keys(schema.Properties)), "only the tool's own inputs")
			assert.ElementsMatch(t, want.required, schema.Required)
			for prop := range schema.Properties {
				assert.Regexp(t, `^[a-z][a-zA-Z0-9]*$`, prop, "camelCase")
			}
			if f, ok := schema.Properties["format"]; ok {
				assert.Equal(t, []string{"base64Content", "textContent"}, f.Enum)
			}
			assert.NotNil(t, byName[name].OutputSchema)
		})
	}

	for _, name := range []string{"downloadFile", "listFiles", "getUrl", "listFolders"} {
		a := byName[name].Annotations
		assert.True(t, a.ReadOnlyHint, name)
		assert.True(t, a.IdempotentHint, name)
		assert.Equal(t, new(false), a.DestructiveHint, name)
		assert.Equal(t, new(false), a.OpenWorldHint, name)
	}
	a := byName["uploadFile"].Annotations
	assert.False(t, a.ReadOnlyHint)
	assert.False(t, a.IdempotentHint)
	assert.Equal(t, new(true), a.DestructiveHint)
	assert.Equal(t, new(true), a.OpenWorldHint)

	// The upload description spells out that only a new folder pays folder
	// credits.
	desc := byName["uploadFile"].Description
	assert.Contains(t, desc, "A new folder costs 500 credits a month; only uploadFile without folderId creates one.")
	assert.Contains(t, desc, "Uploading with folderId into an existing folder never costs folder credits again")
	assert.Contains(t, desc, "Storage costs 500 credits a month per started 25 MB the whole account stores")
	assert.Contains(t, desc, "keep the folderId it returns and pass it for every other file")
	assert.Contains(t, desc, "find its folder with listFolders")
	schema, _ := json.Marshal(byName["uploadFile"].InputSchema)
	assert.Contains(t, string(schema), "at no folder cost")
}

func TestListTools_SingleTypes(t *testing.T) {
	rs, err := newEnv(t).session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	schemas := map[string]map[string]any{}
	for _, tool := range rs.Tools {
		for kind, schema := range map[string]any{"input": tool.InputSchema, "output": tool.OutputSchema} {
			b, _ := json.Marshal(schema)
			var v map[string]any
			require.NoError(t, json.Unmarshal(b, &v))
			assertSingleTypes(t, tool.Name+" "+kind, v)
			schemas[tool.Name+" "+kind] = v
		}
	}

	nullableString := map[string]any{"anyOf": []any{map[string]any{"type": "null"}, map[string]any{"type": "string"}}}
	download := schemas["downloadFile output"]["properties"].(map[string]any)
	for _, field := range []string{"base64Content", "textContent"} {
		assert.Equal(t, nullableString["anyOf"], download[field].(map[string]any)["anyOf"])
		assert.NotEmpty(t, download[field].(map[string]any)["description"], "the description stays on the property")
	}
	upload := schemas["uploadFile input"]["properties"].(map[string]any)
	assert.Equal(t, nullableString["anyOf"], upload["textContent"].(map[string]any)["anyOf"])
	assert.NotEmpty(t, upload["textContent"].(map[string]any)["description"], "the description stays on the property")

	for _, list := range [][2]string{{"listFiles output", "files"}, {"listFolders output", "folders"}} {
		field := schemas[list[0]]["properties"].(map[string]any)[list[1]].(map[string]any)
		branches := field["anyOf"].([]any)
		require.Len(t, branches, 2, list[0])
		assert.Equal(t, map[string]any{"type": "null"}, branches[0])
		assert.Equal(t, "array", branches[1].(map[string]any)["type"])
		assert.Equal(t, "object", branches[1].(map[string]any)["items"].(map[string]any)["type"], "the array branch keeps the items")
		assert.NotEmpty(t, branches[1].(map[string]any)["items"].(map[string]any)["description"], "each %s entry is described", list[1])
	}
}

// assertSingleTypes fails when any schema in v has a type array.
func assertSingleTypes(t *testing.T, where string, v any) {
	t.Helper()
	switch v := v.(type) {
	case map[string]any:
		if typ, ok := v["type"]; ok {
			assert.IsType(t, "", typ, where)
		}
		for key, child := range v {
			assertSingleTypes(t, where+"."+key, child)
		}
	case []any:
		for _, child := range v {
			assertSingleTypes(t, where, child)
		}
	}
}

func TestInitialize(t *testing.T) {
	e := newEnv(t)
	rs := e.session.InitializeResult()
	assert.Equal(t, "permahost", rs.ServerInfo.Name)
	assert.Equal(t, "permahost.io", rs.ServerInfo.Title)
	assert.NotEmpty(t, rs.ServerInfo.Version)
	assert.Equal(t, brand.Version, rs.ServerInfo.Version)
	assert.Equal(t, "https://permahost.io", rs.ServerInfo.WebsiteURL)
	assert.NotEmpty(t, rs.ServerInfo.Description)
	assertIcons(t, rs.ServerInfo.Icons)
	assert.Equal(t, _instructions, rs.Instructions)
	assert.Contains(t, rs.Instructions, "find its folder with listFolders and upload with its folderId")
	assert.Contains(t, rs.Instructions, "never costs folder credits again")
	assert.Contains(t, rs.Instructions, _skillURI)
	assert.Equal(t, "2026-07-28", rs.ProtocolVersion, "a client that speaks the newest protocol gets it")
	assert.Equal(t, &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}, Resources: &mcp.ResourceCapabilities{}, Extensions: map[string]any{ExtensionSkills: map[string]any{"directoryRead": true}}}, rs.Capabilities)
}

// _maxDescription is the most text some clients, such as Claude, keep of the
// server instructions and of a tool's or parameter's description.
const _maxDescription = 2000

// TestDescriptionLengths keeps the instructions and every tool, input and
// output description within what clients show in full.
func TestDescriptionLengths(t *testing.T) {
	e := newEnv(t)
	assert.LessOrEqual(t, len(e.session.InitializeResult().Instructions), _maxDescription, "instructions")
	rs, err := e.session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tool := range rs.Tools {
		assert.LessOrEqual(t, len(tool.Description), _maxDescription, tool.Name)
		for kind, schema := range map[string]any{"input": tool.InputSchema, "output": tool.OutputSchema} {
			b, _ := json.Marshal(schema)
			var v any
			require.NoError(t, json.Unmarshal(b, &v))
			assertDescriptionLengths(t, tool.Name+" "+kind, v)
		}
	}
}

// assertDescriptionLengths fails when any description in schema v is too
// long.
func assertDescriptionLengths(t *testing.T, where string, v any) {
	t.Helper()
	switch v := v.(type) {
	case map[string]any:
		if d, ok := v["description"].(string); ok {
			assert.LessOrEqual(t, len(d), _maxDescription, where)
		}
		for key, child := range v {
			assertDescriptionLengths(t, where+"."+key, child)
		}
	case []any:
		for _, child := range v {
			assertDescriptionLengths(t, where, child)
		}
	}
}

// rpc posts one JSON-RPC request as a client speaking version would,
// returning the result object.
func rpc(t *testing.T, version, method, params string) map[string]any {
	t.Helper()
	result, rpcErr := rpcCall(t, version, method, params)
	require.Nil(t, rpcErr)
	return result
}

// rpcCall is rpc that returns the error object too.
func rpcCall(t *testing.T, version, method, params string) (map[string]any, map[string]any) {
	t.Helper()
	mc := gomock.NewController(t)
	authn := mockauth.NewMockController(mc)
	authn.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(&entity.AuthenticateResponse{UserID: 7}, nil)
	srv := httptest.NewServer(New(Params{Folders: mockfolder.NewMockController(mc), Auth: authn}))
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
	req.Header.Set("Authorization", "Bearer "+_key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if version != "" {
		req.Header.Set("Mcp-Protocol-Version", version)
		req.Header.Set("Mcp-Method", method)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	assert.Empty(t, res.Header.Get("Mcp-Session-Id"), "stateless: no session")
	var rs struct {
		Result map[string]any `json:"result"`
		Error  map[string]any `json:"error"`
	}
	require.NoError(t, json.NewDecoder(res.Body).Decode(&rs))
	return rs.Result, rs.Error
}

// _wantCapabilities is what every handshake advertises.
var _wantCapabilities = map[string]any{
	"tools":      map[string]any{},
	"resources":  map[string]any{},
	"extensions": map[string]any{"io.modelcontextprotocol/skills": map[string]any{"directoryRead": true}},
}

// The stateless 2026-07-28 protocol has no handshake: server/discover
// advertises the versions, newest first, and the capabilities.
func TestDiscover(t *testing.T) {
	meta := `{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{},"io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"}}}`
	rs := rpc(t, "2026-07-28", "server/discover", meta)
	assert.Equal(t, []any{"2026-07-28", "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"}, rs["supportedVersions"])
	assert.Equal(t, _wantCapabilities, rs["capabilities"], "tools and skills, no list_changed or logging notifications")
	assert.Equal(t, _instructions, rs["instructions"])
	serverInfo := rs["_meta"].(map[string]any)["io.modelcontextprotocol/serverInfo"].(map[string]any)
	assert.Equal(t, "permahost", serverInfo["name"])
	assert.Equal(t, brand.Version, serverInfo["version"], "the release, in every 2026-07-28 result")

	rs = rpc(t, "2026-07-28", "tools/list", meta)
	assert.Len(t, rs["tools"], 5, "served without initialize")
}

// The legacy handshake answers with the client's version when it is
// known, else the newest that handshake can negotiate.
func TestInitialize_Negotiation(t *testing.T) {
	for asked, want := range map[string]string{
		"2025-06-18": "2025-06-18",
		"2025-11-25": "2025-11-25",
		"2099-01-01": "2025-11-25",
	} {
		rs := rpc(t, "", "initialize", `{"protocolVersion":"`+asked+`","capabilities":{},"clientInfo":{"name":"test","version":"1"}}`)
		assert.Equal(t, want, rs["protocolVersion"], asked)
		assert.Equal(t, _wantCapabilities, rs["capabilities"], asked)
		assert.Equal(t, brand.Version, rs["serverInfo"].(map[string]any)["version"], asked)
	}
}

func assertIcons(t *testing.T, icons []mcp.Icon) {
	t.Helper()
	require.Len(t, icons, 2)
	assert.Equal(t, "image/svg+xml", icons[0].MIMEType)
	assert.True(t, strings.HasPrefix(icons[0].Source, "data:image/svg+xml;base64,"))
	assert.Equal(t, []string{"any"}, icons[0].Sizes)
	assert.Equal(t, "image/png", icons[1].MIMEType)
	assert.True(t, strings.HasPrefix(icons[1].Source, "data:image/png;base64,"))
	assert.Equal(t, []string{"128x128"}, icons[1].Sizes)
}

func TestUnknownTool(t *testing.T) {
	e := newEnv(t)
	_, err := e.session.CallTool(context.Background(), &mcp.CallToolParams{Name: "file", Arguments: map[string]any{"action": "get"}})
	assert.ErrorContains(t, err, "unknown tool")
}

func TestUnknownArgument(t *testing.T) {
	e := newEnv(t)
	var out ListFilesOutput
	msg := e.call(t, "listFiles", map[string]any{"folderId": "000000rs", "fileName": "a"}, &out)
	assert.Contains(t, msg, "fileName", "a tool rejects another tool's inputs")
}

func TestContentFormat(t *testing.T) {
	// The schema rejects unknown formats; the handlers check too.
	_, err := contentFormat("hex")
	assert.EqualError(t, err, `unknown format "hex": use base64Content or textContent`)
	_, err = uploadContent(UploadFileInput{Format: "hex"})
	assert.Error(t, err)
	_, err = (&tools{}).downloadFile(context.Background(), 7, DownloadFileInput{Format: "hex"})
	assert.EqualError(t, err, `unknown format "hex": use base64Content or textContent`)
}

func TestUploadFile_NewFolder(t *testing.T) {
	e := newEnv(t)
	gomock.InOrder(
		e.folders.EXPECT().CreateFolder(gomock.Any(), &entity.CreateFolderRequest{UserID: 7}).
			Return(&entity.CreateFolderResponse{Folder: _folder, CreditsSpent: 500}, nil),
		e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "site/index.html", Data: []byte("<p>")}}}).
			Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "site/index.html", Size: 3, URL: "https://000000rs.file.tmp.host/site/index.html"}}, CreditsSpent: 4}, nil),
	)
	var out UploadFileOutput
	msg := e.call(t, "uploadFile", map[string]any{
		"path": "/site/", "fileName": "index.html",
		"base64Content": base64.StdEncoding.EncodeToString([]byte("<p>")),
	}, &out)
	require.Empty(t, msg)
	assert.Equal(t, UploadFileOutput{
		FolderID: "000000rs", URL: "https://000000rs.file.tmp.host/site/index.html",
		FileName: "index.html", Path: "site", Size: 3, CreditsSpent: 504,
	}, out)
}

func TestUploadFile_ExistingFolder(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "a.txt", Data: []byte{}}}}).
		Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "a.txt", URL: "https://000000rs.file.tmp.host/a.txt"}}, CreditsSpent: 1}, nil)
	var out UploadFileOutput
	msg := e.call(t, "uploadFile", map[string]any{"folderId": "000000rs", "fileName": "a.txt", "base64Content": ""}, &out)
	require.Empty(t, msg)
	assert.Equal(t, UploadFileOutput{FolderID: "000000rs", URL: "https://000000rs.file.tmp.host/a.txt", FileName: "a.txt", CreditsSpent: 1}, out)
}

func TestUploadFile_TextContent(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "index.html", Data: []byte("<p>héllo</p>")}}}).
		Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "index.html", Size: 13, URL: "https://000000rs.file.tmp.host/index.html"}}, CreditsSpent: 1}, nil)
	var out UploadFileOutput
	msg := e.call(t, "uploadFile", map[string]any{"folderId": "000000rs", "fileName": "index.html", "format": "textContent", "textContent": "<p>héllo</p>"}, &out)
	require.Empty(t, msg)
	assert.Equal(t, UploadFileOutput{FolderID: "000000rs", URL: "https://000000rs.file.tmp.host/index.html", FileName: "index.html", Size: 13, CreditsSpent: 1}, out)

	e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "empty.txt", Data: []byte{}}}}).
		Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "empty.txt", URL: "https://000000rs.file.tmp.host/empty.txt"}}, CreditsSpent: 1}, nil)
	msg = e.call(t, "uploadFile", map[string]any{"folderId": "000000rs", "fileName": "empty.txt", "format": "textContent", "textContent": ""}, &out)
	require.Empty(t, msg, "an empty text file")
}

func TestUploadFile_Errors(t *testing.T) {
	e := newEnv(t)
	content := base64.StdEncoding.EncodeToString([]byte("x"))
	text := "textContent"
	for name, tc := range map[string]struct {
		args map[string]any
		msg  string
	}{
		"no file name":   {map[string]any{"base64Content": content}, "fileName"},
		"empty name":     {map[string]any{"fileName": "", "base64Content": content}, "fileName is required"},
		"slash in name":  {map[string]any{"fileName": "a/b", "base64Content": content}, "fileName is required"},
		"bad path":       {map[string]any{"fileName": "a", "path": "x/../y", "base64Content": content}, "invalid file path"},
		"not base64":     {map[string]any{"fileName": "a", "base64Content": "!!"}, "base64Content must be base64"},
		"no content":     {map[string]any{"fileName": "a"}, "base64Content is required with format base64Content"},
		"text as base64": {map[string]any{"fileName": "a", "textContent": "x"}, "textContent needs format textContent"},
		"no text":        {map[string]any{"fileName": "a", "format": text}, "textContent is required with format textContent"},
		"base64 as text": {map[string]any{"fileName": "a", "format": text, "textContent": "x", "base64Content": content}, "base64Content needs format base64Content"},
		"bad format":     {map[string]any{"fileName": "a", "format": "hex", "base64Content": content}, "enum"},
		"bad folderId":   {map[string]any{"fileName": "a", "base64Content": content, "folderId": "RS"}, "folderId must be a folder id"},
	} {
		t.Run(name, func(t *testing.T) {
			var out UploadFileOutput
			assert.Contains(t, e.call(t, "uploadFile", tc.args, &out), tc.msg)
		})
	}
}

func TestUploadFile_ControllerErrors(t *testing.T) {
	args := func() map[string]any {
		return map[string]any{"fileName": "a", "base64Content": ""}
	}
	var out UploadFileOutput

	t.Run("create fails", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().CreateFolder(gomock.Any(), &entity.CreateFolderRequest{UserID: 7}).Return(nil, folderctrl.ErrInsufficientCredits)
		assert.Contains(t, e.call(t, "uploadFile", args(), &out), "insufficient credits")
	})

	t.Run("put fails and the paid-for folder is kept", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().CreateFolder(gomock.Any(), gomock.Any()).Return(&entity.CreateFolderResponse{Folder: _folder, CreditsSpent: 500}, nil)
		e.folders.EXPECT().PutFiles(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrInsufficientCredits)
		msg := e.call(t, "uploadFile", args(), &out)
		assert.Contains(t, msg, "insufficient credits")
		assert.Contains(t, msg, "folder 000000rs was created for 500 credits: pass it as folderId to retry")
	})

	t.Run("put into an existing folder fails", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().PutFiles(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
		a := args()
		a["folderId"] = "000000rs"
		assert.Contains(t, e.call(t, "uploadFile", a, &out), "not found")
	})
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errBoom }

func TestDownloadFile(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().OpenFile(gomock.Any(), &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "site/a.bin"}).
		Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("hi"))}, nil)
	var out DownloadFileOutput
	msg := e.call(t, "downloadFile", map[string]any{"folderId": "000000rs", "path": "site", "fileName": "a.bin"}, &out)
	require.Empty(t, msg)
	assert.Equal(t, DownloadFileOutput{FolderID: "000000rs", FileName: "a.bin", Path: "site", Size: 2, Base64Content: ptr(base64.StdEncoding.EncodeToString([]byte("hi")))}, out)
}

func TestDownloadFile_TextContent(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().OpenFile(gomock.Any(), &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "a.txt"}).
		Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("héllo"))}, nil)
	var out DownloadFileOutput
	msg := e.call(t, "downloadFile", map[string]any{"folderId": "000000rs", "fileName": "a.txt", "format": "textContent"}, &out)
	require.Empty(t, msg)
	assert.Equal(t, DownloadFileOutput{FolderID: "000000rs", FileName: "a.txt", Size: 6, TextContent: ptr("héllo")}, out)

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader(""))}, nil)
	out = DownloadFileOutput{}
	msg = e.call(t, "downloadFile", map[string]any{"folderId": "000000rs", "fileName": "a.txt", "format": "textContent"}, &out)
	require.Empty(t, msg)
	assert.Equal(t, ptr(""), out.TextContent, "an empty file still carries its content field")

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("\xff\xfe"))}, nil)
	msg = e.call(t, "downloadFile", map[string]any{"folderId": "000000rs", "fileName": "a.bin", "format": "textContent"}, &out)
	assert.Contains(t, msg, "not UTF-8 text: download it with format base64Content")
}

func ptr(s string) *string { return &s }

func TestDownloadFile_Errors(t *testing.T) {
	e := newEnv(t)
	var out DownloadFileOutput
	assert.Contains(t, e.call(t, "downloadFile", map[string]any{"folderId": "", "fileName": "a"}, &out), "folderId must be a folder id")
	assert.Contains(t, e.call(t, "downloadFile", map[string]any{"folderId": "000000rs", "fileName": ""}, &out), "fileName is required")
	assert.Contains(t, e.call(t, "downloadFile", map[string]any{"folderId": "000000rs", "fileName": "a", "format": "hex"}, &out), "enum", "the schema rejects an unknown format")

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	assert.Contains(t, e.call(t, "downloadFile", map[string]any{"folderId": "000000rs", "fileName": "a"}, &out), "not found")

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(&entity.OpenFileResponse{Body: io.NopCloser(failingReader{})}, nil)
	assert.Contains(t, e.call(t, "downloadFile", map[string]any{"folderId": "000000rs", "fileName": "a"}, &out), "read file: boom")
}

func TestListFolders(t *testing.T) {
	e := newEnv(t)
	created := time.Date(2026, 11, 2, 9, 30, 0, 0, time.FixedZone("x", 3600))
	e.folders.EXPECT().ListFolders(gomock.Any(), &entity.ListFoldersRequest{UserID: 7}).Return(&entity.ListFoldersResponse{Folders: []entity.Folder{
		{ID: 1000, CreatedAt: created, SizeBytes: 2048, URL: "https://000000rs.file.tmp.host/", Title: "Site", DefaultTitle: "index.html"},
		{ID: 1001, CreatedAt: created, URL: "https://000000rt.file.tmp.host/", DefaultTitle: "report.pdf"},
		{ID: 1002, CreatedAt: created, URL: "https://000000ru.file.tmp.host/", DefaultTitle: "000000ru"},
	}}, nil)
	var out ListFoldersOutput
	msg := e.call(t, "listFolders", nil, &out)
	require.Empty(t, msg)
	assert.Equal(t, ListFoldersOutput{Folders: []FolderInfo{
		{FolderID: "000000rs", Title: "Site", URL: "https://000000rs.file.tmp.host/", SizeBytes: 2048, CreatedAt: "2026-11-02T08:30:00Z"},
		{FolderID: "000000rt", Title: "report.pdf", URL: "https://000000rt.file.tmp.host/", CreatedAt: "2026-11-02T08:30:00Z"},
		{FolderID: "000000ru", URL: "https://000000ru.file.tmp.host/", CreatedAt: "2026-11-02T08:30:00Z"},
	}}, out)

	e.folders.EXPECT().ListFolders(gomock.Any(), gomock.Any()).Return(&entity.ListFoldersResponse{}, nil)
	out = ListFoldersOutput{}
	msg = e.call(t, "listFolders", map[string]any{}, &out)
	require.Empty(t, msg)
	assert.Equal(t, ListFoldersOutput{Folders: []FolderInfo{}}, out, "an empty array, never null")

	e.folders.EXPECT().ListFolders(gomock.Any(), gomock.Any()).Return(nil, errors.New("db down"))
	assert.Contains(t, e.call(t, "listFolders", nil, &out), "db down")
}

func TestListFiles(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().GetFolder(gomock.Any(), &entity.GetFolderRequest{UserID: 7, FolderID: 1000}).Return(&entity.GetFolderResponse{
		Folder: _folder,
		Files: []entity.File{
			{Path: "index.html", Size: 1, URL: "https://000000rs.file.tmp.host/index.html"},
			{Path: "site/css/a.css", Size: 2, URL: "https://000000rs.file.tmp.host/site/css/a.css"},
		},
	}, nil)
	var out ListFilesOutput
	msg := e.call(t, "listFiles", map[string]any{"folderId": "000000rs"}, &out)
	require.Empty(t, msg)
	assert.Equal(t, ListFilesOutput{FolderID: "000000rs", URL: "https://000000rs.file.tmp.host/", Files: []FileInfo{
		{FileName: "index.html", Path: "", Size: 1, URL: "https://000000rs.file.tmp.host/index.html"},
		{FileName: "a.css", Path: "site/css", Size: 2, URL: "https://000000rs.file.tmp.host/site/css/a.css"},
	}}, out)

	e.folders.EXPECT().GetFolder(gomock.Any(), gomock.Any()).Return(&entity.GetFolderResponse{Folder: _folder}, nil)
	out = ListFilesOutput{}
	require.Empty(t, e.call(t, "listFiles", map[string]any{"folderId": "000000rs"}, &out))
	assert.Equal(t, []FileInfo{}, out.Files, "an empty array, never null")

	assert.Contains(t, e.call(t, "listFiles", map[string]any{"folderId": ""}, &out), "folderId must be a folder id")

	e.folders.EXPECT().GetFolder(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	assert.Contains(t, e.call(t, "listFiles", map[string]any{"folderId": "000000rs"}, &out), "not found")
}

func TestGetURL(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().GetFolder(gomock.Any(), gomock.Any()).Return(&entity.GetFolderResponse{Folder: _folder}, nil).Times(4)
	for _, tc := range []struct {
		args map[string]any
		url  string
	}{
		{map[string]any{}, "https://000000rs.file.tmp.host/"},
		{map[string]any{"path": "site/"}, "https://000000rs.file.tmp.host/site/"},
		{map[string]any{"fileName": "a b.txt"}, "https://000000rs.file.tmp.host/a%20b.txt"},
		{map[string]any{"path": "site", "fileName": "index.html"}, "https://000000rs.file.tmp.host/site/index.html"},
	} {
		tc.args["folderId"] = "000000rs"
		var out GetURLOutput
		require.Empty(t, e.call(t, "getUrl", tc.args, &out))
		assert.Equal(t, tc.url, out.URL)
		assert.Equal(t, "000000rs", out.FolderID)
	}
}

func TestGetURL_Errors(t *testing.T) {
	e := newEnv(t)
	var out GetURLOutput
	assert.Contains(t, e.call(t, "getUrl", map[string]any{"folderId": "!"}, &out), "folderId must be a folder id")
	assert.Contains(t, e.call(t, "getUrl", map[string]any{"folderId": "000000rs", "fileName": "a/b"}, &out), "invalid file path")
	assert.Contains(t, e.call(t, "getUrl", map[string]any{"folderId": "000000rs", "path": "a//b"}, &out), "invalid file path")
	assert.Contains(t, e.call(t, "getUrl", map[string]any{"folderId": "000000rs", "path": "1/2/3/4/5/6", "fileName": "f"}, &out), "invalid file path", "deeper than 5 directories")
}
