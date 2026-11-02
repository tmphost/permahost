// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const deviceJSON = `{"device_code":"dc","user_code":"WDJB-MJHT","verification_uri":"https://t.io/auth/device",
"verification_uri_complete":"https://t.io/auth/device?user_code=WDJB-MJHT","expires_in":600,"interval":5}`

// noSleep records the waits instead of waiting, and fails with err once
// fail waits were made, if err is set.
func noSleep(t *testing.T, waits *[]time.Duration, fail int, err error) {
	t.Helper()
	orig := sleep
	t.Cleanup(func() { sleep = orig })
	sleep = func(_ context.Context, d time.Duration) error {
		if err != nil && len(*waits) == fail {
			return err
		}
		*waits = append(*waits, d)
		return nil
	}
}

// deviceServer answers the device endpoint with deviceStatus and
// deviceBody, and token polls with the next of polls, each a status and a
// body.
func deviceServer(t *testing.T, deviceStatus int, deviceBody string, polls ...[2]any) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, ClientID, r.PostForm.Get("client_id"))
		switch r.URL.Path {
		case "/oauth2/device/code":
			w.WriteHeader(deviceStatus)
			_, _ = io.WriteString(w, deviceBody)
		case "/oauth2/token":
			assert.Equal(t, url.Values{"grant_type": {DeviceGrant}, "device_code": {"dc"}, "client_id": {ClientID}}, r.PostForm)
			require.NotEmpty(t, polls, "no more polls expected")
			p := polls[0]
			polls = polls[1:]
			w.WriteHeader(p[0].(int))
			_, _ = io.WriteString(w, p[1].(string))
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{Server: srv.URL, HTTP: srv.Client(), Now: func() time.Time { return now }}
}

func TestDeviceLogin(t *testing.T) {
	var waits []time.Duration
	noSleep(t, &waits, 0, nil)
	c := deviceServer(t, 200, deviceJSON,
		[2]any{400, `{"error":"authorization_pending"}`},
		[2]any{400, `{"error":"slow_down"}`},
		[2]any{400, `{"error":"authorization_pending"}`},
		[2]any{200, tokenJSON},
	)
	var out bytes.Buffer
	tok, err := c.DeviceLogin(context.Background(), LoginOptions{Out: &out, Timeout: time.Hour})
	require.NoError(t, err)
	assert.Equal(t, &Token{AccessToken: "at", RefreshToken: "rt", Expiry: now.Add(time.Hour)}, tok)
	assert.Equal(t, []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second}, waits, "slow_down adds 5s")
	assert.Equal(t, `To sign in, open this page on any device:

  https://t.io/auth/device

and enter the code:

  WDJB-MJHT

Or open this link, which fills in the code:

  https://t.io/auth/device?user_code=WDJB-MJHT

Waiting for you to approve...
`, out.String())
}

func TestDeviceLogin_answers(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":"access_denied"}`:                         "sign-in was denied",
		`{"error":"expired_token"}`:                         "the code expired; run permahost login again",
		`{"error":"invalid_grant","error_description":"x"}`: "sign-in failed: invalid_grant: x",
	} {
		var waits []time.Duration
		noSleep(t, &waits, 0, nil)
		c := deviceServer(t, 200, deviceJSON, [2]any{400, body})
		_, err := c.DeviceLogin(context.Background(), LoginOptions{Out: io.Discard, Timeout: time.Hour})
		assert.EqualError(t, err, want)
	}

	// A poll that fails on the way is returned as it is.
	var waits []time.Duration
	noSleep(t, &waits, 0, nil)
	c := deviceServer(t, 200, deviceJSON, [2]any{200, `nope`})
	_, err := c.DeviceLogin(context.Background(), LoginOptions{Out: io.Discard, Timeout: time.Hour})
	assert.ErrorContains(t, err, "decode token response")
}

func TestDeviceLogin_waits(t *testing.T) {
	// The wait ends at the code's expiry when that comes first.
	var waits []time.Duration
	noSleep(t, &waits, 1, context.DeadlineExceeded)
	c := deviceServer(t, 200, deviceJSON, [2]any{400, `{"error":"authorization_pending"}`})
	_, err := c.DeviceLogin(context.Background(), LoginOptions{Out: io.Discard, Timeout: time.Hour})
	assert.EqualError(t, err, "timed out after 10m0s waiting for the sign-in to be approved")

	waits = nil
	noSleep(t, &waits, 0, context.Canceled)
	c = deviceServer(t, 200, deviceJSON)
	_, err = c.DeviceLogin(context.Background(), LoginOptions{Out: io.Discard, Timeout: time.Minute})
	assert.EqualError(t, err, "login canceled")

	// Without an interval or expiry, it polls every second until the
	// timeout.
	waits = nil
	noSleep(t, &waits, 1, context.DeadlineExceeded)
	c = deviceServer(t, 200, `{"device_code":"dc","user_code":"U","verification_uri":"https://t.io/d"}`, [2]any{400, `{"error":"authorization_pending"}`})
	var out bytes.Buffer
	_, err = c.DeviceLogin(context.Background(), LoginOptions{Out: &out, Timeout: time.Minute})
	assert.EqualError(t, err, "timed out after 1m0s waiting for the sign-in to be approved")
	assert.Equal(t, []time.Duration{time.Second}, waits)
	assert.NotContains(t, out.String(), "Or open this link")
}

func TestSleep(t *testing.T) {
	assert.NoError(t, sleep(context.Background(), time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, sleep(ctx, time.Hour), context.Canceled)
}

func TestRequestDeviceCode_errors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"error":"invalid_client","error_description":"only the permahost CLI may use the device flow"}`, "invalid_client: only the permahost CLI may use the device flow"},
		{404, `<html>`, "server_error: device authorization endpoint responded 404 Not Found"},
		{200, `nope`, "decode device authorization response"},
		{200, `{"device_code":"dc"}`, "device authorization endpoint returned no code"},
	} {
		_, err := deviceServer(t, tc.status, tc.body).RequestDeviceCode(ctx)
		assert.ErrorContains(t, err, tc.want)
	}

	// DeviceLogin says which step failed.
	_, err := deviceServer(t, 404, `<html>`).DeviceLogin(ctx, LoginOptions{Out: io.Discard, Timeout: time.Minute})
	assert.EqualError(t, err, "start device sign-in: server_error: device authorization endpoint responded 404 Not Found")

	_, err = (&Client{Server: "://bad", HTTP: http.DefaultClient}).RequestDeviceCode(ctx)
	assert.Error(t, err)
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	_, err = (&Client{Server: srv.URL, HTTP: http.DefaultClient}).RequestDeviceCode(ctx)
	assert.Error(t, err)
}
