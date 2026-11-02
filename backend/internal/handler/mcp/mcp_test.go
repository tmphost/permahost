// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	mockauth "github.com/tmphost/tmphost/backend/internal/controller/auth/mocks"
	folderctrl "github.com/tmphost/tmphost/backend/internal/controller/folder"
	mockfolder "github.com/tmphost/tmphost/backend/internal/controller/folder/mocks"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

const _key = "access-token"

var (
	errBoom  = errors.New("boom")
	_expires = time.Date(2026, 10, 18, 12, 30, 0, 0, time.UTC)
	// Folder 1000 is "000000000rs" as an 11-character folder ID.
	_folder = entity.Folder{ID: 1000, UserID: 7, ExpiresAt: _expires, URL: "https://f000000000rs.tmp.host/"}
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

// call runs the file tool, returning its structured output or its error
// text.
func (e *env) call(t *testing.T, args map[string]any) (FileOutput, string) {
	t.Helper()
	rs, err := e.session.CallTool(context.Background(), &mcp.CallToolParams{Name: "file", Arguments: args})
	require.NoError(t, err)
	if rs.IsError {
		return FileOutput{}, rs.Content[0].(*mcp.TextContent).Text
	}
	var out FileOutput
	b, _ := json.Marshal(rs.StructuredContent)
	require.NoError(t, json.Unmarshal(b, &out))
	return out, ""
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
	require.Len(t, rs.Tools, 1)
	assert.Equal(t, "file", rs.Tools[0].Name)
	schema, _ := json.Marshal(rs.Tools[0].InputSchema)
	assert.Contains(t, string(schema), `"enum":["upload","download","get","url"]`)
	assert.Contains(t, string(schema), `"enum":["base64Content","textContent"]`)
}

func TestUnknownAction(t *testing.T) {
	e := newEnv(t)
	_, msg := e.call(t, map[string]any{"action": "rm"})
	assert.Contains(t, msg, "enum", "the schema rejects it")

	// The handler checks too, in case the schema is ever loosened.
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{UserID: "7"}}}
	_, _, err := (&tool{}).handle(context.Background(), req, FileInput{Action: "rm"})
	assert.EqualError(t, err, `unknown action "rm": use upload, download, get or url`)
}

func TestContentFormat(t *testing.T) {
	// The schema rejects unknown formats; the handler checks too.
	_, err := contentFormat("hex")
	assert.EqualError(t, err, `unknown format "hex": use base64Content or textContent`)
	_, err = uploadContent(FileInput{Format: "hex"})
	assert.Error(t, err)
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{TokenInfo: &auth.TokenInfo{UserID: "7"}}}
	_, _, err = (&tool{}).handle(context.Background(), req, FileInput{Action: ActionDownload, Format: "hex"})
	assert.EqualError(t, err, `unknown format "hex": use base64Content or textContent`)
}

func TestUpload_NewFolder(t *testing.T) {
	e := newEnv(t)
	gomock.InOrder(
		e.folders.EXPECT().CreateFolder(gomock.Any(), &entity.CreateFolderRequest{UserID: 7, TTLMinutes: 120}).
			Return(&entity.CreateFolderResponse{Folder: _folder}, nil),
		e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "site/index.html", Data: []byte("<p>")}}}).
			Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "site/index.html", Size: 3, URL: "https://f000000000rs.tmp.host/site/index.html"}}, CreditsSpent: 4}, nil),
	)
	out, msg := e.call(t, map[string]any{
		"action": "upload", "ttl": "2h", "path": "/site/", "fileName": "index.html",
		"base64Content": base64.StdEncoding.EncodeToString([]byte("<p>")),
	})
	require.Empty(t, msg)
	assert.Equal(t, FileOutput{
		ID: "000000000rs", URL: "https://f000000000rs.tmp.host/site/index.html", ExpiresAt: "2026-10-18T12:30:00Z",
		FileName: "index.html", Path: "site", Size: 3, CreditsSpent: 4,
	}, out)
}

func TestUpload_ExistingFolder(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "a.txt", Data: []byte{}}}}).
		Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "a.txt", URL: "https://f000000000rs.tmp.host/a.txt"}}, CreditsSpent: 1}, nil)
	out, msg := e.call(t, map[string]any{"action": "upload", "id": "000000000rs", "fileName": "a.txt", "base64Content": ""})
	require.Empty(t, msg)
	assert.Equal(t, FileOutput{ID: "000000000rs", URL: "https://f000000000rs.tmp.host/a.txt", FileName: "a.txt", CreditsSpent: 1}, out)
}

func TestUpload_TextContent(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "index.html", Data: []byte("<p>héllo</p>")}}}).
		Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "index.html", Size: 13, URL: "https://f000000000rs.tmp.host/index.html"}}, CreditsSpent: 1}, nil)
	out, msg := e.call(t, map[string]any{"action": "upload", "id": "000000000rs", "fileName": "index.html", "format": "textContent", "textContent": "<p>héllo</p>"})
	require.Empty(t, msg)
	assert.Equal(t, FileOutput{ID: "000000000rs", URL: "https://f000000000rs.tmp.host/index.html", FileName: "index.html", Size: 13, CreditsSpent: 1}, out)

	e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "empty.txt", Data: []byte{}}}}).
		Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "empty.txt", URL: "https://f000000000rs.tmp.host/empty.txt"}}, CreditsSpent: 1}, nil)
	_, msg = e.call(t, map[string]any{"action": "upload", "id": "000000000rs", "fileName": "empty.txt", "format": "textContent", "textContent": ""})
	require.Empty(t, msg, "an empty text file")
}

func TestUpload_Errors(t *testing.T) {
	e := newEnv(t)
	content := base64.StdEncoding.EncodeToString([]byte("x"))
	text := "textContent"
	for name, tc := range map[string]struct {
		args map[string]any
		msg  string
	}{
		"no file name":   {map[string]any{"base64Content": content}, "fileName is required"},
		"slash in name":  {map[string]any{"fileName": "a/b", "base64Content": content}, "fileName is required"},
		"bad path":       {map[string]any{"fileName": "a", "path": "x/../y", "base64Content": content}, "invalid file path"},
		"not base64":     {map[string]any{"fileName": "a", "base64Content": "!!"}, "base64Content must be base64"},
		"no content":     {map[string]any{"fileName": "a"}, "base64Content is required with format base64Content"},
		"text as base64": {map[string]any{"fileName": "a", "textContent": "x"}, "textContent needs format textContent"},
		"no text":        {map[string]any{"fileName": "a", "format": text}, "textContent is required with format textContent"},
		"base64 as text": {map[string]any{"fileName": "a", "format": text, "textContent": "x", "base64Content": content}, "base64Content needs format base64Content"},
		"bad format":     {map[string]any{"fileName": "a", "format": "hex", "base64Content": content}, "enum"},
		"ttl with id":    {map[string]any{"fileName": "a", "base64Content": content, "id": "000000000rs", "ttl": "1h"}, "ttl only applies"},
		"bad id":         {map[string]any{"fileName": "a", "base64Content": content, "id": "RS"}, "id must be a folder id"},
		"bad ttl":        {map[string]any{"fileName": "a", "base64Content": content, "ttl": "soon"}, "whole minutes"},
		"fractional ttl": {map[string]any{"fileName": "a", "base64Content": content, "ttl": "90s"}, "whole minutes"},
	} {
		t.Run(name, func(t *testing.T) {
			tc.args["action"] = "upload"
			_, msg := e.call(t, tc.args)
			assert.Contains(t, msg, tc.msg)
		})
	}
}

func TestUpload_ControllerErrors(t *testing.T) {
	args := func() map[string]any {
		return map[string]any{"action": "upload", "fileName": "a", "base64Content": ""}
	}

	t.Run("create fails", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().CreateFolder(gomock.Any(), &entity.CreateFolderRequest{UserID: 7}).Return(nil, folderctrl.ErrInvalidTTL)
		_, msg := e.call(t, args())
		assert.Contains(t, msg, "invalid ttl")
	})

	t.Run("put fails and the new folder is removed", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().CreateFolder(gomock.Any(), gomock.Any()).Return(&entity.CreateFolderResponse{Folder: _folder}, nil)
		e.folders.EXPECT().PutFiles(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrInsufficientCredits)
		e.folders.EXPECT().DeleteFolder(gomock.Any(), &entity.DeleteFolderRequest{UserID: 7, FolderID: 1000}).Return(nil)
		_, msg := e.call(t, args())
		assert.Contains(t, msg, "insufficient credits")
	})

	t.Run("put into an existing folder fails", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().PutFiles(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
		a := args()
		a["id"] = "000000000rs"
		_, msg := e.call(t, a)
		assert.Contains(t, msg, "not found")
	})
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errBoom }

func TestDownload(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().OpenFile(gomock.Any(), &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "site/a.bin"}).
		Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("hi"))}, nil)
	out, msg := e.call(t, map[string]any{"action": "download", "id": "000000000rs", "path": "site", "fileName": "a.bin"})
	require.Empty(t, msg)
	assert.Equal(t, FileOutput{ID: "000000000rs", FileName: "a.bin", Path: "site", Size: 2, Base64Content: ptr(base64.StdEncoding.EncodeToString([]byte("hi")))}, out)
}

func TestDownload_TextContent(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().OpenFile(gomock.Any(), &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "a.txt"}).
		Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("héllo"))}, nil)
	out, msg := e.call(t, map[string]any{"action": "download", "id": "000000000rs", "fileName": "a.txt", "format": "textContent"})
	require.Empty(t, msg)
	assert.Equal(t, FileOutput{ID: "000000000rs", FileName: "a.txt", Size: 6, TextContent: ptr("héllo")}, out)

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader(""))}, nil)
	out, msg = e.call(t, map[string]any{"action": "download", "id": "000000000rs", "fileName": "a.txt", "format": "textContent"})
	require.Empty(t, msg)
	assert.Equal(t, ptr(""), out.TextContent, "an empty file still carries its content field")

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("\xff\xfe"))}, nil)
	_, msg = e.call(t, map[string]any{"action": "download", "id": "000000000rs", "fileName": "a.bin", "format": "textContent"})
	assert.Contains(t, msg, "not UTF-8 text: download it with format base64Content")
}

func ptr(s string) *string { return &s }

func TestDownload_Errors(t *testing.T) {
	e := newEnv(t)
	_, msg := e.call(t, map[string]any{"action": "download", "fileName": "a"})
	assert.Contains(t, msg, "id must be a folder id")

	_, msg = e.call(t, map[string]any{"action": "download", "id": "000000000rs"})
	assert.Contains(t, msg, "fileName is required")

	_, msg = e.call(t, map[string]any{"action": "download", "id": "000000000rs", "fileName": "a", "format": "hex"})
	assert.Contains(t, msg, "enum", "the schema rejects an unknown format")

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	_, msg = e.call(t, map[string]any{"action": "download", "id": "000000000rs", "fileName": "a"})
	assert.Contains(t, msg, "not found")

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(&entity.OpenFileResponse{Body: io.NopCloser(failingReader{})}, nil)
	_, msg = e.call(t, map[string]any{"action": "download", "id": "000000000rs", "fileName": "a"})
	assert.Contains(t, msg, "read file: boom")
}

func TestGet(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().GetFolder(gomock.Any(), &entity.GetFolderRequest{UserID: 7, FolderID: 1000}).Return(&entity.GetFolderResponse{
		Folder: _folder,
		Files: []entity.File{
			{Path: "index.html", Size: 1, URL: "https://f000000000rs.tmp.host/index.html"},
			{Path: "site/css/a.css", Size: 2, URL: "https://f000000000rs.tmp.host/site/css/a.css"},
		},
	}, nil)
	out, msg := e.call(t, map[string]any{"action": "get", "id": "000000000rs"})
	require.Empty(t, msg)
	assert.Equal(t, FileOutput{ID: "000000000rs", URL: "https://f000000000rs.tmp.host/", ExpiresAt: "2026-10-18T12:30:00Z", Files: []FileInfo{
		{FileName: "index.html", Path: "", Size: 1, URL: "https://f000000000rs.tmp.host/index.html"},
		{FileName: "a.css", Path: "site/css", Size: 2, URL: "https://f000000000rs.tmp.host/site/css/a.css"},
	}}, out)

	_, msg = e.call(t, map[string]any{"action": "get", "id": ""})
	assert.Contains(t, msg, "id must be a folder id")

	e.folders.EXPECT().GetFolder(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	_, msg = e.call(t, map[string]any{"action": "get", "id": "000000000rs"})
	assert.Contains(t, msg, "not found")
}

func TestURL(t *testing.T) {
	e := newEnv(t)
	e.folders.EXPECT().GetFolder(gomock.Any(), gomock.Any()).Return(&entity.GetFolderResponse{Folder: _folder}, nil).Times(4)
	for _, tc := range []struct {
		args map[string]any
		url  string
	}{
		{map[string]any{}, "https://f000000000rs.tmp.host/"},
		{map[string]any{"path": "site/"}, "https://f000000000rs.tmp.host/site/"},
		{map[string]any{"fileName": "a b.txt"}, "https://f000000000rs.tmp.host/a%20b.txt"},
		{map[string]any{"path": "site", "fileName": "index.html"}, "https://f000000000rs.tmp.host/site/index.html"},
	} {
		tc.args["action"], tc.args["id"] = "url", "000000000rs"
		out, msg := e.call(t, tc.args)
		require.Empty(t, msg)
		assert.Equal(t, tc.url, out.URL)
		assert.Equal(t, "2026-10-18T12:30:00Z", out.ExpiresAt)
	}
}

func TestURL_Errors(t *testing.T) {
	e := newEnv(t)
	_, msg := e.call(t, map[string]any{"action": "url", "id": "!"})
	assert.Contains(t, msg, "id must be a folder id")

	_, msg = e.call(t, map[string]any{"action": "url", "id": "000000000rs", "fileName": "a/b"})
	assert.Contains(t, msg, "invalid file path")
	_, msg = e.call(t, map[string]any{"action": "url", "id": "000000000rs", "path": "a//b"})
	assert.Contains(t, msg, "invalid file path")
	_, msg = e.call(t, map[string]any{"action": "url", "id": "000000000rs", "path": "1/2/3/4/5/6", "fileName": "f"})
	assert.Contains(t, msg, "invalid file path", "deeper than 5 directories")
}
