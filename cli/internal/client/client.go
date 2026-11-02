// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package client is a typed client for the permahost REST API (/api/v1).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// User is the authenticated account.
type User struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	Name           string    `json:"name"`
	Picture        string    `json:"picture"`
	Provider       string    `json:"provider"`
	Credits        int       `json:"credits"`
	CreditsLimit   int       `json:"creditsLimit"`
	CreditsRenewAt time.Time `json:"creditsRenewAt"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Limits are the server's upload limit and pricing.
type Limits struct {
	MaxUploadBytes int64   `json:"maxUploadBytes"`
	Pricing        Pricing `json:"pricing"`
}

// Pricing is what hosting costs, in credits per month.
type Pricing struct {
	// StorageCredits is charged for each started StorageBlockBytes the
	// account stores.
	StorageBlockBytes int64 `json:"storageBlockBytes"`
	StorageCredits    int   `json:"storageCredits"`
	FolderCredits     int   `json:"folderCredits"`
	DomainCredits     int   `json:"domainCredits"`
	// DomainSetupCredits is charged once, when a domain is verified.
	DomainSetupCredits int `json:"domainSetupCredits"`
}

// Usage is what the account hosts and what that costs a month.
type Usage struct {
	Folders        int     `json:"folders"`
	StorageBytes   int64   `json:"storageBytes"`
	StorageBlocks  int     `json:"storageBlocks"`
	Domains        int     `json:"domains"`
	MonthlyCredits int     `json:"monthlyCredits"`
	Pricing        Pricing `json:"pricing"`
}

// Folder is a permanent public folder.
type Folder struct {
	ID           string    `json:"id"`
	CreatedAt    time.Time `json:"createdAt"`
	SizeBytes    int64     `json:"sizeBytes"`
	URL          string    `json:"url"`
	Title        string    `json:"title"`
	DefaultTitle string    `json:"defaultTitle"`
}

// CreateFolderResult is the response to creating a folder.
type CreateFolderResult struct {
	Folder       Folder `json:"folder"`
	CreditsSpent int    `json:"creditsSpent"`
}

// Domain is a custom domain pointed at a folder.
type Domain struct {
	ID       string `json:"id"`
	FolderID string `json:"folderId"`
	Hostname string `json:"hostname"`
	// Target is what the hostname's CNAME record must name.
	Target string `json:"target"`
	// Status is "pending" until the CNAME is verified, then "active".
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	VerifiedAt time.Time `json:"verifiedAt,omitzero"`
	CheckedAt  time.Time `json:"checkedAt,omitzero"`
	LastError  string    `json:"lastError,omitempty"`
}

// File is a file inside a folder.
type File struct {
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updatedAt"`
	URL       string    `json:"url"`
}

// FolderDetail is a folder together with its files.
type FolderDetail struct {
	Folder Folder `json:"folder"`
	Files  []File `json:"files"`
}

// PutFileResult is the response to a file upload.
type PutFileResult struct {
	File         File `json:"file"`
	CreditsSpent int  `json:"creditsSpent"`
}

// APIError is a non-2xx response; Message is the server's "error" field.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return e.Message }

// TokenSource supplies OAuth access tokens.
type TokenSource interface {
	// Token returns a usable access token, refreshing it when it is about
	// to expire.
	Token(ctx context.Context) (string, error)
	// Refresh returns a new access token after the server rejected the
	// current one.
	Refresh(ctx context.Context) (string, error)
}

// Client talks to one permahost server as one signed-in user.
type Client struct {
	baseURL string
	tokens  TokenSource
	http    *http.Client
}

// New returns a client for server (e.g. "https://permahost.io").
func New(server string, tokens TokenSource) *Client {
	// No client timeout: large uploads on slow links legitimately take long,
	// and callers bound requests with their context instead.
	return &Client{baseURL: strings.TrimRight(server, "/") + "/api/v1", tokens: tokens, http: &http.Client{}}
}

// Me returns the authenticated user.
func (c *Client) Me(ctx context.Context) (*User, error) {
	var out struct {
		User User `json:"user"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/users/me", nil, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

// Limits returns the server's upload and billing limits.
func (c *Client) Limits(ctx context.Context) (*Limits, error) {
	var out Limits
	if err := c.doJSON(ctx, http.MethodGet, "/limits", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Usage returns what the account hosts and what that costs a month.
func (c *Client) Usage(ctx context.Context) (*Usage, error) {
	var out struct {
		Usage Usage `json:"usage"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/users/me/usage", nil, &out); err != nil {
		return nil, err
	}
	return &out.Usage, nil
}

// CreateFolder creates an empty folder, which is charged for the month.
func (c *Client) CreateFolder(ctx context.Context) (*CreateFolderResult, error) {
	var out CreateFolderResult
	if err := c.doJSON(ctx, http.MethodPost, "/folders", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListFolders returns the folders, newest first.
func (c *Client) ListFolders(ctx context.Context) ([]Folder, error) {
	var out struct {
		Folders []Folder `json:"folders"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/folders", nil, &out); err != nil {
		return nil, err
	}
	return out.Folders, nil
}

// GetFolder returns a folder and its files.
func (c *Client) GetFolder(ctx context.Context, id string) (*FolderDetail, error) {
	var out FolderDetail
	if err := c.doJSON(ctx, http.MethodGet, "/folders/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteFolder deletes a folder and everything in it.
func (c *Client) DeleteFolder(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/folders/"+url.PathEscape(id), nil, nil)
}

// PutFile uploads size bytes from body to filePath (slash separated) in the
// folder, replacing any file already there. body is rewound if the request
// has to be retried with a refreshed token.
func (c *Client) PutFile(ctx context.Context, folderID, filePath string, body io.ReadSeeker, size int64) (*PutFileResult, error) {
	// NopCloser keeps the transport from closing the caller's file, which
	// a retry still needs.
	req, err := c.newRequest(ctx, http.MethodPut, filesPath(folderID, filePath), io.NopCloser(body))
	if err != nil {
		return nil, err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		return io.NopCloser(body), nil
	}
	// An *os.File body would otherwise be sent chunked, which defeats the
	// server's up-front size check.
	req.ContentLength = size
	req.Header.Set("Content-Type", "application/octet-stream")
	var out PutFileResult
	if err := c.send(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetFile streams a file's bytes; the caller must close the returned body.
func (c *Client) GetFile(ctx context.Context, folderID, filePath string) (io.ReadCloser, error) {
	req, err := c.newRequest(ctx, http.MethodGet, filesPath(folderID, filePath), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// DeleteFile deletes one file from a folder.
func (c *Client) DeleteFile(ctx context.Context, folderID, filePath string) error {
	return c.doJSON(ctx, http.MethodDelete, filesPath(folderID, filePath), nil, nil)
}

// ListDomains returns a folder's custom domains.
func (c *Client) ListDomains(ctx context.Context, folderID string) ([]Domain, error) {
	var out struct {
		Domains []Domain `json:"domains"`
	}
	if err := c.doJSON(ctx, http.MethodGet, domainsPath(folderID), nil, &out); err != nil {
		return nil, err
	}
	return out.Domains, nil
}

// AddDomain adds hostname to a folder as a pending custom domain; it is
// served once its CNAME record names Target and it is verified.
func (c *Client) AddDomain(ctx context.Context, folderID, hostname string) (*Domain, error) {
	in := struct {
		Hostname string `json:"hostname"`
	}{hostname}
	var out struct {
		Domain Domain `json:"domain"`
	}
	if err := c.doJSON(ctx, http.MethodPost, domainsPath(folderID), in, &out); err != nil {
		return nil, err
	}
	return &out.Domain, nil
}

// VerifyDomain checks a domain's CNAME record now and activates the domain,
// which is charged, when it points at the folder.
func (c *Client) VerifyDomain(ctx context.Context, folderID, domainID string) (*Domain, error) {
	var out struct {
		Domain Domain `json:"domain"`
	}
	if err := c.doJSON(ctx, http.MethodPost, domainsPath(folderID)+"/"+url.PathEscape(domainID)+"/verify", nil, &out); err != nil {
		return nil, err
	}
	return &out.Domain, nil
}

// DeleteDomain removes a custom domain from a folder.
func (c *Client) DeleteDomain(ctx context.Context, folderID, domainID string) error {
	return c.doJSON(ctx, http.MethodDelete, domainsPath(folderID)+"/"+url.PathEscape(domainID), nil, nil)
}

func domainsPath(folderID string) string {
	return "/folders/" + url.PathEscape(folderID) + "/domains"
}

// filesPath escapes each segment on its own so that "/" keeps separating
// directories while spaces, "#" and "?" stay part of the name.
func filesPath(folderID, filePath string) string {
	segs := strings.Split(filePath, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return "/folders/" + url.PathEscape(folderID) + "/files/" + strings.Join(segs, "/")
}

// doJSON sends in (when non-nil) as a JSON body and decodes the response
// into out (when non-nil).
func (c *Client) doJSON(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		// The request types are plain structs, which always marshal.
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	req, err := c.newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.send(req, out)
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// do sends req with an access token and turns a non-2xx response into an
// *APIError, closing its body; on success the caller owns the body. A 401
// is retried once with a refreshed token, since the token may have been
// revoked or the local clock may be off.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.attempt(req, c.tokens.Token)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		retry := req.Clone(req.Context())
		if req.GetBody != nil {
			if retry.Body, err = req.GetBody(); err != nil {
				return nil, err
			}
		}
		if resp, err = c.attempt(retry, c.tokens.Refresh); err != nil {
			return nil, err
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}
	return resp, nil
}

func (c *Client) attempt(req *http.Request, token func(context.Context) (string, error)) (*http.Response, error) {
	t, err := token(req.Context())
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+t)
	return c.http.Do(req)
}

func (c *Client) send(req *http.Request, out any) error {
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response from %s %s: %w", req.Method, req.URL.Path, err)
	}
	return nil
}

// decodeError prefers the server's {"error": "..."} message and falls back
// to the status line, e.g. when a proxy in front of the server answered.
func decodeError(resp *http.Response) error {
	var body struct {
		Error string `json:"error"`
	}
	if json.NewDecoder(resp.Body).Decode(&body) != nil || body.Error == "" {
		body.Error = "server responded " + resp.Status
	}
	return &APIError{Status: resp.StatusCode, Message: body.Error}
}
