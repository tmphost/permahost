// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceGrant is the device code grant type (RFC 8628).
const DeviceGrant = "urn:ietf:params:oauth:grant-type:device_code"

// slowDownStep is how much longer to wait after a slow_down (RFC 8628
// §3.5).
const slowDownStep = 5 * time.Second

// sleep waits d, or until ctx is done; swapped in tests to skip the wait.
var sleep = func(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DeviceCode is the server's answer to a device authorization request.
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int64  `json:"expires_in"`
	Interval                int64  `json:"interval"`
}

// RequestDeviceCode starts a device authorization.
func (c *Client) RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Server+"/oauth2/device/code", strings.NewReader(url.Values{"client_id": {ClientID}}.Encode()))
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
			e = &Error{Code: "server_error", Description: "device authorization endpoint responded " + resp.Status}
		}
		return nil, e
	}
	var dc DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, fmt.Errorf("decode device authorization response: %w", err)
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return nil, errors.New("device authorization endpoint returned no code")
	}
	return &dc, nil
}

// PollDevice asks once for the tokens of a device code. Until the user
// answers, it fails with the *Error authorization_pending or slow_down.
func (c *Client) PollDevice(ctx context.Context, deviceCode string) (*Token, error) {
	return c.token(ctx, url.Values{
		"grant_type":  {DeviceGrant},
		"device_code": {deviceCode},
		"client_id":   {ClientID},
	})
}

// DeviceLogin runs the device flow for a machine without a browser, such
// as over SSH: it shows a URL and a code to enter on any other device, and
// polls until the user approves there.
func (c *Client) DeviceLogin(ctx context.Context, o LoginOptions) (*Token, error) {
	dc, err := c.RequestDeviceCode(ctx)
	if err != nil {
		return nil, fmt.Errorf("start device sign-in: %w", err)
	}
	fmt.Fprintf(o.Out, "To sign in, open this page on any device:\n\n  %s\n\nand enter the code:\n\n  %s\n\n", dc.VerificationURI, dc.UserCode)
	if dc.VerificationURIComplete != "" {
		fmt.Fprintf(o.Out, "Or open this link, which fills in the code:\n\n  %s\n\n", dc.VerificationURIComplete)
	}
	fmt.Fprintln(o.Out, "Waiting for you to approve...")

	timeout := o.Timeout
	if expires := time.Duration(dc.ExpiresIn) * time.Second; expires > 0 && expires < timeout {
		timeout = expires
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	interval := max(time.Duration(dc.Interval)*time.Second, time.Second)
	for {
		if err := sleep(ctx, interval); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("timed out after %s waiting for the sign-in to be approved", timeout)
			}
			return nil, errors.New("login canceled")
		}
		tok, err := c.PollDevice(ctx, dc.DeviceCode)
		var oe *Error
		switch {
		case err == nil:
			return tok, nil
		case !errors.As(err, &oe):
			return nil, err
		case oe.Code == "authorization_pending":
		case oe.Code == "slow_down":
			interval += slowDownStep
		case oe.Code == "access_denied":
			return nil, errors.New("sign-in was denied")
		case oe.Code == "expired_token":
			return nil, errors.New("the code expired; run permahost login again")
		default:
			return nil, fmt.Errorf("sign-in failed: %w", err)
		}
	}
}
