// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ts = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// serve starts a server answering every request with status and body, and
// records the last request (with its body) for assertions.
func serve(t *testing.T, status int, body string) (*Client, *http.Request, *string) {
	t.Helper()
	var last http.Request
	var lastBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		last = *r.Clone(context.Background())
		lastBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL+"/", staticTokens("tok")), &last, &lastBody
}

// staticTokens is a token source that never changes.
type staticTokens string

func (s staticTokens) Token(context.Context) (string, error)   { return string(s), nil }
func (s staticTokens) Refresh(context.Context) (string, error) { return string(s), nil }

// rotatingTokens hands out "old" until refreshed, then "new".
type rotatingTokens struct {
	refreshed  bool
	tokenErr   error
	refreshErr error
}

func (r *rotatingTokens) Token(context.Context) (string, error) {
	if r.refreshed {
		return "new", r.tokenErr
	}
	return "old", r.tokenErr
}

func (r *rotatingTokens) Refresh(context.Context) (string, error) {
	r.refreshed = true
	return "new", r.refreshErr
}

func TestMe(t *testing.T) {
	c, req, _ := serve(t, 200, `{"user":{"id":"u1","email":"a@b.c","name":"A","credits":5,"creditsLimit":100,"creditsRenewAt":"2026-10-18T12:00:00Z"}}`)
	u, err := c.Me(context.Background())
	require.NoError(t, err)
	assert.Equal(t, &User{ID: "u1", Email: "a@b.c", Name: "A", Credits: 5, CreditsLimit: 100, CreditsRenewAt: ts}, u)
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "/api/v1/users/me", req.URL.Path)
	assert.Equal(t, "Bearer tok", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Accept"))
}

func TestLimits(t *testing.T) {
	c, req, _ := serve(t, 200, `{"defaultTtlMinutes":30,"maxTtlMinutes":432000,"maxUploadBytes":16777216,"billingBlockMinutes":30,"billingUnitBytes":1048576}`)
	l, err := c.Limits(context.Background())
	require.NoError(t, err)
	assert.Equal(t, &Limits{30, 432000, 16777216, 30, 1048576}, l)
	assert.Equal(t, "/api/v1/limits", req.URL.Path)
}

func TestCreateFolder(t *testing.T) {
	c, req, body := serve(t, 201, `{"folder":{"id":"f1","url":"https://h/p/f1/","expiresAt":"2026-10-18T12:00:00Z"}}`)
	f, err := c.CreateFolder(context.Background(), 60)
	require.NoError(t, err)
	assert.Equal(t, &Folder{ID: "f1", URL: "https://h/p/f1/", ExpiresAt: ts}, f)
	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "/api/v1/folders", req.URL.Path)
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.JSONEq(t, `{"ttlMinutes":60}`, *body)
}

func TestListFolders(t *testing.T) {
	c, req, _ := serve(t, 200, `{"folders":[{"id":"f1"},{"id":"f2"}]}`)
	fs, err := c.ListFolders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []Folder{{ID: "f1"}, {ID: "f2"}}, fs)
	assert.Equal(t, "/api/v1/folders", req.URL.Path)
}

func TestGetFolder(t *testing.T) {
	c, req, _ := serve(t, 200, `{"folder":{"id":"f1"},"files":[{"path":"a.txt","size":3}]}`)
	d, err := c.GetFolder(context.Background(), "f1")
	require.NoError(t, err)
	assert.Equal(t, &FolderDetail{Folder: Folder{ID: "f1"}, Files: []File{{Path: "a.txt", Size: 3}}}, d)
	assert.Equal(t, "/api/v1/folders/f1", req.URL.Path)
}

func TestDeleteFolder(t *testing.T) {
	c, req, _ := serve(t, 204, ``)
	require.NoError(t, c.DeleteFolder(context.Background(), "f1"))
	assert.Equal(t, "DELETE", req.Method)
	assert.Equal(t, "/api/v1/folders/f1", req.URL.Path)
}

func TestPutFile(t *testing.T) {
	c, req, body := serve(t, 200, `{"file":{"path":"my dir/a#b.txt","size":5},"creditsSpent":2}`)
	r, err := c.PutFile(context.Background(), "f1", "my dir/a#b.txt", strings.NewReader("hello"), 5)
	require.NoError(t, err)
	assert.Equal(t, &PutFileResult{File: File{Path: "my dir/a#b.txt", Size: 5}, CreditsSpent: 2}, r)
	assert.Equal(t, "PUT", req.Method)
	assert.Equal(t, "/api/v1/folders/f1/files/my%20dir/a%23b.txt", req.URL.EscapedPath())
	assert.Equal(t, int64(5), req.ContentLength)
	assert.Equal(t, "application/octet-stream", req.Header.Get("Content-Type"))
	assert.Equal(t, "hello", *body)
}

func TestPutFileErrors(t *testing.T) {
	c, _, _ := serve(t, 413, `{"error":"file too large"}`)
	_, err := c.PutFile(context.Background(), "f1", "a", strings.NewReader("x"), 1)
	assert.EqualError(t, err, "file too large")

	_, err = New("://bad", staticTokens("k")).PutFile(context.Background(), "f1", "a", strings.NewReader("x"), 1)
	assert.Error(t, err)
}

func TestGetFile(t *testing.T) {
	c, req, _ := serve(t, 200, "raw bytes")
	body, err := c.GetFile(context.Background(), "rs", "site/a b.txt")
	require.NoError(t, err)
	b, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	assert.Equal(t, "raw bytes", string(b))
	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "/api/v1/folders/rs/files/site/a%20b.txt", req.URL.EscapedPath())

	c, _, _ = serve(t, 404, `{"error":"file not found"}`)
	_, err = c.GetFile(context.Background(), "rs", "a")
	assert.EqualError(t, err, "file not found")

	_, err = New("://bad", staticTokens("k")).GetFile(context.Background(), "rs", "a")
	assert.Error(t, err)
}

func TestDeleteFile(t *testing.T) {
	c, req, _ := serve(t, 204, ``)
	require.NoError(t, c.DeleteFile(context.Background(), "f1", "site/index.html"))
	assert.Equal(t, "DELETE", req.Method)
	assert.Equal(t, "/api/v1/folders/f1/files/site/index.html", req.URL.Path)
}

// Every typed method shares the same error handling; each is checked
// against an API error so its error return is exercised.
func TestMethodErrors(t *testing.T) {
	c, _, _ := serve(t, 401, `{"error":"invalid token"}`)
	ctx := context.Background()
	calls := map[string]func() error{
		"Me":           func() error { _, err := c.Me(ctx); return err },
		"Limits":       func() error { _, err := c.Limits(ctx); return err },
		"CreateFolder": func() error { _, err := c.CreateFolder(ctx, 1); return err },
		"ListFolders":  func() error { _, err := c.ListFolders(ctx); return err },
		"GetFolder":    func() error { _, err := c.GetFolder(ctx, "f"); return err },
		"DeleteFolder": func() error { return c.DeleteFolder(ctx, "f") },
		"DeleteFile":   func() error { return c.DeleteFile(ctx, "f", "a") },
	}
	for name, call := range calls {
		err := call()
		var apiErr *APIError
		require.ErrorAs(t, err, &apiErr, name)
		assert.Equal(t, &APIError{Status: 401, Message: "invalid token"}, apiErr, name)
	}
}

func TestErrorFallsBackToStatus(t *testing.T) {
	c, _, _ := serve(t, 502, `<html>bad gateway</html>`)
	_, err := c.Me(context.Background())
	assert.EqualError(t, err, "server responded 502 Bad Gateway")

	c, _, _ = serve(t, 500, `{}`)
	_, err = c.Me(context.Background())
	assert.EqualError(t, err, "server responded 500 Internal Server Error")
}

func TestTransportErrors(t *testing.T) {
	_, err := New("://bad", staticTokens("k")).Me(context.Background())
	assert.Error(t, err)

	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	_, err = New(srv.URL, staticTokens("k")).Me(context.Background())
	assert.Error(t, err)

	c, _, _ := serve(t, 200, `not json`)
	_, err = c.Me(context.Background())
	assert.ErrorContains(t, err, "decode response from GET /api/v1/users/me")
	var syntax *json.SyntaxError
	assert.ErrorAs(t, err, &syntax)
}

// retryServer rejects the "old" token and accepts the "new" one, echoing
// the request body so a replayed upload can be checked.
func retryServer(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer new" {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":"invalid token"}`)
			return
		}
		fmt.Fprintf(w, `{"file":{"path":%q},"user":{"email":%q}}`, string(b), string(b))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestRetryAfterRefresh(t *testing.T) {
	ctx := context.Background()
	tokens := &rotatingTokens{}
	c := New(retryServer(t), tokens)
	_, err := c.Me(ctx)
	require.NoError(t, err)
	assert.True(t, tokens.refreshed)

	// The upload body is rewound and sent again.
	tokens = &rotatingTokens{}
	c = New(retryServer(t), tokens)
	r, err := c.PutFile(ctx, "rs", "a.txt", strings.NewReader("hello"), 5)
	require.NoError(t, err)
	assert.Equal(t, "hello", r.File.Path)

	// A JSON body is replayed too.
	tokens = &rotatingTokens{}
	c = New(retryServer(t), tokens)
	_, err = c.CreateFolder(ctx, 30)
	require.NoError(t, err)
}

func TestRetryFailures(t *testing.T) {
	ctx := context.Background()
	url := retryServer(t)

	_, err := New(url, &rotatingTokens{tokenErr: errors.New("not logged in")}).Me(ctx)
	assert.EqualError(t, err, "not logged in")

	_, err = New(url, &rotatingTokens{refreshErr: errors.New("session expired")}).Me(ctx)
	assert.EqualError(t, err, "session expired")

	// Still rejected after refreshing: the 401 reaches the caller.
	_, err = New(url, staticTokens("old")).Me(ctx)
	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, 401, apiErr.Status)

	_, err = New(url, &rotatingTokens{}).PutFile(ctx, "rs", "a", badSeeker{strings.NewReader("x")}, 1)
	assert.EqualError(t, err, "cannot seek")
}

type badSeeker struct{ io.Reader }

func (badSeeker) Seek(int64, int) (int64, error) { return 0, errors.New("cannot seek") }
