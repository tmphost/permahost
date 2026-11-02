// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthserver

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mustafaturan/monoflake"

	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	"github.com/tmphost/permahost/backend/internal/repository"
	"github.com/tmphost/permahost/backend/internal/service/token"
)

const (
	// DeviceCodeTTL is how long a user has to approve a device.
	DeviceCodeTTL = 10 * time.Minute
	// DeviceInterval is how often a device may poll the token endpoint.
	DeviceInterval = 5 * time.Second
	// DeviceAttempts is how many device codes an IP address may ask for,
	// and how many user codes it may type, per DeviceWindow each.
	DeviceAttempts = 20
	// DeviceWindow is the DeviceAttempts window.
	DeviceWindow = 10 * time.Minute
	// DeviceSweepSchedule is when expired device authorizations are
	// deleted.
	DeviceSweepSchedule = "*/10 * * * *"

	// _userCodeAlphabet has no vowels, so a code never spells a word, and
	// no letters that look alike (RFC 8628 §6.1).
	_userCodeAlphabet = "BCDFGHJKLMNPQRSTVWXZ"
	_userCodeLen      = 8
)

func (c *controller) AuthorizeDevice(ctx context.Context, rq *entity.AuthorizeDeviceRequest) (*entity.AuthorizeDeviceResponse, error) {
	if rq.ClientID == "" {
		return nil, fmt.Errorf("%w: client_id is required", ErrInvalidRequest)
	}
	// Only the CLI may: a registered client names itself, and a user who
	// types a code has no redirect URI to check it by.
	if rq.ClientID != CLIClientID {
		return nil, ErrInvalidClient
	}
	if !c.deviceQuota.Take("code:"+rq.IP, 1) {
		return nil, ErrTooManyAttempts
	}
	deviceCode, userCode := rand.Text(), newUserCode()
	now := c.now().UTC()
	err := c.devices.CreateDeviceCode(ctx, model.DeviceCode{
		DeviceCodeHash: clientKey(deviceCode),
		UserCode:       userCode,
		ClientID:       clientKey(rq.ClientID),
		ClientName:     _cliClient.Name,
		Status:         model.DeviceCodePending,
		ExpiresAt:      now.Add(DeviceCodeTTL),
		CreatedAt:      now,
	})
	if err != nil {
		return nil, fmt.Errorf("create device code: %w", err)
	}
	shown := formatUserCode(userCode)
	return &entity.AuthorizeDeviceResponse{
		DeviceCode:              deviceCode,
		UserCode:                shown,
		VerificationURI:         c.issuer + DevicePath,
		VerificationURIComplete: c.issuer + DevicePath + "?user_code=" + url.QueryEscape(shown),
		ExpiresIn:               DeviceCodeTTL,
		Interval:                DeviceInterval,
	}, nil
}

func (c *controller) VerifyDevice(ctx context.Context, rq *entity.VerifyDeviceRequest) (*entity.VerifyDeviceResponse, error) {
	if rq.UserID == 0 {
		return nil, ErrLoginRequired
	}
	// Every code typed counts, so codes cannot be guessed.
	if !c.deviceQuota.Take("guess:"+rq.IP, 1) {
		return nil, ErrTooManyAttempts
	}
	userCode := normalizeUserCode(rq.UserCode)
	if len(userCode) != _userCodeLen {
		return nil, ErrInvalidUserCode
	}
	d, err := c.devices.FindPendingDeviceCode(ctx, userCode, c.now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrInvalidUserCode
	}
	if err != nil {
		return nil, fmt.Errorf("find device code: %w", err)
	}
	user, err := c.users.FindUserByID(ctx, rq.UserID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrLoginRequired
	}
	if err != nil {
		return nil, fmt.Errorf("find user: %w", err)
	}
	consent, err := c.token.CreateDeviceConsentToken(token.OAuthGrant{
		UserID:     monoflake.ID(user.ID).String(),
		ClientID:   d.ClientID,
		DeviceCode: d.DeviceCodeHash,
	})
	if err != nil {
		return nil, fmt.Errorf("create device consent token: %w", err)
	}
	return &entity.VerifyDeviceResponse{Consent: &entity.DeviceConsent{
		ClientName: d.ClientName,
		Verified:   d.ClientID == clientKey(CLIClientID),
		UserCode:   formatUserCode(userCode),
		Email:      user.Email,
		Token:      consent,
	}}, nil
}

func (c *controller) ConsentDevice(ctx context.Context, rq *entity.ConsentDeviceRequest) (*entity.ConsentDeviceResponse, error) {
	grant, err := c.token.ValidateDeviceConsentToken(rq.Token)
	// The answer must come from the user who was asked.
	if err != nil || rq.UserID == 0 || grant.UserID != monoflake.ID(rq.UserID).String() {
		return nil, ErrInvalidConsent
	}
	status := model.DeviceCodeDenied
	if rq.Allow {
		status = model.DeviceCodeApproved
	}
	err = c.devices.AnswerDeviceCode(ctx, grant.DeviceCode, rq.UserID, status, c.now().UTC())
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrInvalidConsent
	}
	if err != nil {
		return nil, fmt.Errorf("answer device code: %w", err)
	}
	// Only the CLI may use the device flow.
	return &entity.ConsentDeviceResponse{ClientName: _cliClient.Name, Allowed: rq.Allow}, nil
}

func (c *controller) SweepDeviceCodes(ctx context.Context) error {
	return c.devices.DeleteExpiredDeviceCodes(ctx, c.now().UTC())
}

// exchangeDeviceCode answers a device's poll (RFC 8628 §3.5).
func (c *controller) exchangeDeviceCode(ctx context.Context, rq *entity.TokenRequest) (*entity.TokenResponse, error) {
	if rq.DeviceCode == "" || rq.ClientID == "" {
		return nil, fmt.Errorf("%w: device_code and client_id are required", ErrInvalidRequest)
	}
	if _, err := c.client(rq.ClientID); err != nil {
		return nil, err
	}
	hash := clientKey(rq.DeviceCode)
	d, err := c.devices.FindDeviceCodeByHash(ctx, hash)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, fmt.Errorf("%w: device_code is invalid", ErrInvalidGrant)
	}
	if err != nil {
		return nil, fmt.Errorf("find device code: %w", err)
	}
	if d.ClientID != clientKey(rq.ClientID) {
		return nil, fmt.Errorf("%w: device_code was issued to another client", ErrInvalidGrant)
	}
	now := c.now().UTC()
	if !now.Before(d.ExpiresAt) {
		return nil, ErrExpiredToken
	}
	switch d.Status {
	case model.DeviceCodeApproved:
		// Deleting first redeems the code exactly once.
		err := c.devices.DeleteDeviceCode(ctx, hash)
		if errors.Is(err, repository.ErrNotFound) {
			return nil, fmt.Errorf("%w: device_code was already used", ErrInvalidGrant)
		}
		if err != nil {
			return nil, fmt.Errorf("delete device code: %w", err)
		}
		return c.issue(ctx, token.OAuthGrant{UserID: monoflake.ID(d.UserID).String(), ClientID: d.ClientID})
	case model.DeviceCodeDenied:
		// The code is spent either way; one left behind expires.
		_ = c.devices.DeleteDeviceCode(ctx, hash)
		return nil, ErrAccessDenied
	}
	if err := c.devices.PollDeviceCode(ctx, hash, now); err != nil {
		return nil, fmt.Errorf("poll device code: %w", err)
	}
	if now.Sub(d.PolledAt) < DeviceInterval {
		return nil, ErrSlowDown
	}
	return nil, ErrAuthorizationPending
}

// randRead is swapped in tests to steer the random bytes.
var randRead = rand.Read

// newUserCode returns _userCodeLen random letters of _userCodeAlphabet.
func newUserCode() string {
	const n = byte(len(_userCodeAlphabet))
	// The largest multiple of n that fits a byte; bytes above it would
	// make the first letters likelier.
	const limit = 256 / int(n) * int(n)
	code := make([]byte, 0, _userCodeLen)
	buf := make([]byte, 1)
	for len(code) < _userCodeLen {
		_, _ = randRead(buf) // Never fails; it crashes the program instead.
		if int(buf[0]) < limit {
			code = append(code, _userCodeAlphabet[buf[0]%n])
		}
	}
	return string(code)
}

// formatUserCode splits a code in two halves to read it out: WDJB-MJHT.
func formatUserCode(code string) string {
	return code[:_userCodeLen/2] + "-" + code[_userCodeLen/2:]
}

// normalizeUserCode forgives what people type: lower case, dashes and
// spaces.
func normalizeUserCode(code string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == ' ' {
			return -1
		}
		return r
	}, strings.ToUpper(strings.TrimSpace(code)))
}
