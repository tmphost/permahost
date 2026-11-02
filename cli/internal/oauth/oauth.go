// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package oauth signs the CLI in to a tmphost server with OAuth 2.1: the
// authorization code flow with PKCE and a loopback redirect, plus refresh.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ClientID is the CLI's built-in public client: it has no secret and needs
// no registration, and the server lets it redirect to any loopback port.
const ClientID = "00000000001"

// listen is swapped in tests to make the loopback listener fail.
var listen = net.Listen

// Token is a set of tokens from the token endpoint.
type Token struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Error is an OAuth error response, such as invalid_grant.
type Error struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *Error) Error() string {
	if e.Description == "" {
		return e.Code
	}
	return e.Code + ": " + e.Description
}

// Client talks to one server's OAuth endpoints.
type Client struct {
	Server string
	HTTP   *http.Client
	Now    func() time.Time
}

// Exchange trades an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code, redirectURI, verifier string) (*Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {ClientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	})
}

// Refresh trades a refresh token for new tokens. The server rotates refresh
// tokens, so the old one must be replaced with the returned one.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {ClientID},
	})
}

func (c *Client) token(ctx context.Context, form url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Server+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		e := &Error{}
		if json.NewDecoder(resp.Body).Decode(e) != nil || e.Code == "" {
			e = &Error{Code: "server_error", Description: "token endpoint responded " + resp.Status}
		}
		return nil, e
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if body.AccessToken == "" || !strings.EqualFold(body.TokenType, "bearer") {
		return nil, errors.New("token endpoint returned no bearer access token")
	}
	return &Token{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		Expiry:       c.Now().Add(time.Duration(body.ExpiresIn) * time.Second),
	}, nil
}

// LoginOptions configure the interactive login.
type LoginOptions struct {
	// Open shows the authorization URL to the user, normally in a browser.
	Open func(url string) error
	// Out receives the URL to copy if the browser does not open.
	Out io.Writer
	// Timeout bounds the wait for the user to finish in the browser.
	Timeout time.Duration
}

// callbackPage is what the browser shows once the CLI has the code.
const callbackPage = `<!doctype html><meta charset="utf-8"><title>tmphost</title>
<body style="font-family:system-ui,sans-serif;text-align:center;margin-top:20vh">
<h1>%s</h1><p>%s</p></body>`

// Login runs the authorization code flow: it listens on a loopback port,
// sends the user to the server's authorize page and exchanges the code the
// browser brings back.
func (c *Client) Login(ctx context.Context, o LoginOptions) (*Token, error) {
	ln, err := listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for the login callback: %w", err)
	}
	redirectURI := fmt.Sprintf("http://%s/callback", ln.Addr())
	verifier, state := randomString(), randomString()

	codes := make(chan result, 1)
	srv := &http.Server{Handler: callbackHandler(state, codes), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		// Shutdown, not Close, so the browser still gets its page.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	authURL := c.AuthorizeURL(redirectURI, Challenge(verifier), state)
	fmt.Fprintf(o.Out, "Opening the browser to sign in. If it does not open, visit:\n\n  %s\n\n", authURL)
	if err := o.Open(authURL); err != nil {
		fmt.Fprintf(o.Out, "Could not open the browser: %v\n", err)
	}

	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	select {
	case r := <-codes:
		if r.err != nil {
			return nil, r.err
		}
		return c.Exchange(ctx, r.code, redirectURI, verifier)
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("timed out after %s waiting for the browser sign-in", o.Timeout)
		}
		return nil, errors.New("login canceled")
	}
}

// AuthorizeURL is the server page where the user approves the CLI.
func (c *Client) AuthorizeURL(redirectURI, challenge, state string) string {
	return c.Server + "/oauth2/authorize?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {ClientID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}.Encode()
}

type result struct {
	code string
	err  error
}

// callbackHandler answers the browser's redirect and hands the outcome to
// Login. Only the first outcome counts; later hits just get the page.
func callbackHandler(state string, out chan<- result) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var res result
		switch {
		// A mismatched state means the redirect did not come from our
		// request (login CSRF), so its code must not be used.
		case q.Get("state") != state:
			res.err = errors.New("login callback had the wrong state; try again")
		case q.Get("error") != "":
			res.err = fmt.Errorf("sign-in failed: %w", &Error{Code: q.Get("error"), Description: q.Get("error_description")})
		case q.Get("code") == "":
			res.err = errors.New("login callback had no code")
		default:
			res.code = q.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if res.err != nil {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, callbackPage, "Sign-in failed", "Return to the terminal for details.")
		} else {
			fmt.Fprintf(w, callbackPage, "Signed in to tmphost", "You can close this tab.")
		}
		select {
		case out <- res:
		default:
		}
	})
	return mux
}

// randomString returns 256 random bits, base64url encoded (43 characters):
// a valid PKCE verifier and an unguessable state.
func randomString() string {
	b := make([]byte, 32)
	// crypto/rand.Read never fails; it crashes the program if the OS
	// cannot supply randomness.
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Challenge is the S256 PKCE challenge for a verifier.
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
