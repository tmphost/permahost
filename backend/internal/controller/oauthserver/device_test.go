// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthserver

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	"github.com/tmphost/permahost/backend/internal/repository"
	mockrepo "github.com/tmphost/permahost/backend/internal/repository/mocks"
	"github.com/tmphost/permahost/backend/internal/service/quota"
	"github.com/tmphost/permahost/backend/internal/service/token"
	mocktoken "github.com/tmphost/permahost/backend/internal/service/token/mocks"
)

const _ip = "203.0.113.7"

// deviceEnv is a controller with a real token service and device code
// table, and a clock tests move.
type deviceEnv struct {
	ctrl    Controller
	users   *mockrepo.MockUserRepository
	devices repository.DeviceCodeRepository
	now     time.Time
}

func newDeviceEnv(t *testing.T) *deviceEnv {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.DeviceCode{}))
	e := &deviceEnv{
		users:   mockrepo.NewMockUserRepository(gomock.NewController(t)),
		devices: repository.NewDeviceCodeRepository(db),
		now:     _now,
	}
	clock := func() time.Time { return e.now }
	tokens, err := token.New(token.Config{Secret: "s", Now: clock})
	require.NoError(t, err)
	e.ctrl = New(Params{
		Token:       tokens,
		Users:       e.users,
		Devices:     e.devices,
		DeviceQuota: quota.New(quota.Params{Limit: DeviceAttempts, Window: DeviceWindow, Now: clock}),
		Issuer:      _issuer,
		Now:         clock,
	})
	return e
}

// withMockDevices builds a controller whose device codes and token
// service are mocks.
func withMockDevices(t *testing.T) (Controller, *mockrepo.MockDeviceCodeRepository, *mocktoken.MockService, *mockrepo.MockUserRepository) {
	t.Helper()
	mc := gomock.NewController(t)
	devices, tokens, users := mockrepo.NewMockDeviceCodeRepository(mc), mocktoken.NewMockService(mc), mockrepo.NewMockUserRepository(mc)
	ctrl := New(Params{
		Token:       tokens,
		Users:       users,
		Devices:     devices,
		DeviceQuota: quota.New(quota.Params{Limit: DeviceAttempts, Window: DeviceWindow}),
		Issuer:      _issuer,
		Now:         func() time.Time { return _now },
	})
	return ctrl, devices, tokens, users
}

func (e *deviceEnv) start(t *testing.T) *entity.AuthorizeDeviceResponse {
	t.Helper()
	rs, err := e.ctrl.AuthorizeDevice(context.Background(), &entity.AuthorizeDeviceRequest{ClientID: CLIClientID, IP: _ip})
	require.NoError(t, err)
	return rs
}

func (e *deviceEnv) poll(deviceCode string) (*entity.TokenResponse, error) {
	return e.ctrl.Token(context.Background(), &entity.TokenRequest{GrantType: GrantDeviceCode, DeviceCode: deviceCode, ClientID: CLIClientID})
}

func (e *deviceEnv) verify(t *testing.T, userCode string) *entity.DeviceConsent {
	t.Helper()
	e.users.EXPECT().FindUserByID(gomock.Any(), _userID).Return(_user, nil)
	rs, err := e.ctrl.VerifyDevice(context.Background(), &entity.VerifyDeviceRequest{UserID: _userID, UserCode: userCode, IP: _ip})
	require.NoError(t, err)
	return rs.Consent
}

func TestDevice_Approve(t *testing.T) {
	ctx := context.Background()
	e := newDeviceEnv(t)
	rs := e.start(t)

	assert.Regexp(t, `^[BCDFGHJKLMNPQRSTVWXZ]{4}-[BCDFGHJKLMNPQRSTVWXZ]{4}$`, rs.UserCode)
	assert.NotEmpty(t, rs.DeviceCode)
	assert.Equal(t, _issuer+"/auth/device", rs.VerificationURI)
	assert.Equal(t, _issuer+"/auth/device?user_code="+url.QueryEscape(rs.UserCode), rs.VerificationURIComplete)
	assert.Equal(t, DeviceCodeTTL, rs.ExpiresIn)
	assert.Equal(t, DeviceInterval, rs.Interval)

	// The table keeps the code's digest, never the code.
	stored, err := e.devices.FindDeviceCodeByHash(ctx, clientKey(rs.DeviceCode))
	require.NoError(t, err)
	assert.Equal(t, model.DeviceCodePending, stored.Status)
	assert.Equal(t, strings.ReplaceAll(rs.UserCode, "-", ""), stored.UserCode)
	assert.Equal(t, "permahost CLI", stored.ClientName)

	// Pending until the user answers; polling too fast slows it down.
	_, err = e.poll(rs.DeviceCode)
	assert.ErrorIs(t, err, ErrAuthorizationPending)
	_, err = e.poll(rs.DeviceCode)
	assert.ErrorIs(t, err, ErrSlowDown)
	e.now = e.now.Add(DeviceInterval)
	_, err = e.poll(rs.DeviceCode)
	assert.ErrorIs(t, err, ErrAuthorizationPending)

	// The user types the code the way people do.
	consent := e.verify(t, " "+strings.ToLower(strings.ReplaceAll(rs.UserCode, "-", " "))+" ")
	assert.Equal(t, &entity.DeviceConsent{ClientName: "permahost CLI", Verified: true, UserCode: rs.UserCode, Email: "a@b.c", Token: consent.Token}, consent)

	answer, err := e.ctrl.ConsentDevice(ctx, &entity.ConsentDeviceRequest{UserID: _userID, Token: consent.Token, Allow: true})
	require.NoError(t, err)
	assert.Equal(t, &entity.ConsentDeviceResponse{ClientName: "permahost CLI", Allowed: true}, answer)

	// Once answered, the code is no longer offered, and the answer stands.
	_, err = e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: rs.UserCode, IP: _ip})
	assert.ErrorIs(t, err, ErrInvalidUserCode)
	_, err = e.ctrl.ConsentDevice(ctx, &entity.ConsentDeviceRequest{UserID: _userID, Token: consent.Token})
	assert.ErrorIs(t, err, ErrInvalidConsent)

	e.users.EXPECT().FindUserByID(gomock.Any(), _userID).Return(_user, nil)
	tok, err := e.poll(rs.DeviceCode)
	require.NoError(t, err)
	assert.Equal(t, token.AccessTokenTTL, tok.ExpiresIn)

	// The tokens are the CLI's, like those of the browser flow, so they
	// refresh as the CLI.
	e.users.EXPECT().FindUserByID(gomock.Any(), _userID).Return(_user, nil)
	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: GrantRefreshToken, RefreshToken: tok.RefreshToken, ClientID: CLIClientID})
	require.NoError(t, err)

	// A code is redeemed once.
	_, err = e.poll(rs.DeviceCode)
	assert.ErrorIs(t, err, ErrInvalidGrant)
}

func TestDevice_Deny(t *testing.T) {
	ctx := context.Background()
	e := newDeviceEnv(t)
	rs := e.start(t)
	consent := e.verify(t, rs.UserCode)

	answer, err := e.ctrl.ConsentDevice(ctx, &entity.ConsentDeviceRequest{UserID: _userID, Token: consent.Token})
	require.NoError(t, err)
	assert.Equal(t, &entity.ConsentDeviceResponse{ClientName: "permahost CLI"}, answer)

	_, err = e.poll(rs.DeviceCode)
	assert.ErrorIs(t, err, ErrAccessDenied)
	_, err = e.poll(rs.DeviceCode)
	assert.ErrorIs(t, err, ErrInvalidGrant, "a denied code is spent")
}

func TestDevice_Expiry(t *testing.T) {
	ctx := context.Background()
	e := newDeviceEnv(t)
	rs := e.start(t)
	consent := e.verify(t, rs.UserCode)

	e.now = e.now.Add(DeviceCodeTTL)
	_, err := e.poll(rs.DeviceCode)
	assert.ErrorIs(t, err, ErrExpiredToken)
	_, err = e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: rs.UserCode, IP: _ip})
	assert.ErrorIs(t, err, ErrInvalidUserCode)
	_, err = e.ctrl.ConsentDevice(ctx, &entity.ConsentDeviceRequest{UserID: _userID, Token: consent.Token, Allow: true})
	assert.ErrorIs(t, err, ErrInvalidConsent)

	// The sweep deletes it.
	require.NoError(t, e.ctrl.SweepDeviceCodes(ctx))
	_, err = e.poll(rs.DeviceCode)
	assert.ErrorIs(t, err, ErrInvalidGrant)
}

func TestAuthorizeDevice_Errors(t *testing.T) {
	ctx := context.Background()
	e := newDeviceEnv(t)

	_, err := e.ctrl.AuthorizeDevice(ctx, &entity.AuthorizeDeviceRequest{IP: _ip})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = e.ctrl.AuthorizeDevice(ctx, &entity.AuthorizeDeviceRequest{ClientID: "someone-else", IP: _ip})
	assert.ErrorIs(t, err, ErrInvalidClient, "only the CLI")

	for range DeviceAttempts {
		e.start(t)
	}
	_, err = e.ctrl.AuthorizeDevice(ctx, &entity.AuthorizeDeviceRequest{ClientID: CLIClientID, IP: _ip})
	assert.ErrorIs(t, err, ErrTooManyAttempts)
	_, err = e.ctrl.AuthorizeDevice(ctx, &entity.AuthorizeDeviceRequest{ClientID: CLIClientID, IP: "198.51.100.1"})
	assert.NoError(t, err, "per IP address")

	ctrl, devices, _, _ := withMockDevices(t)
	devices.EXPECT().CreateDeviceCode(gomock.Any(), gomock.Any()).Return(errBoom)
	_, err = ctrl.AuthorizeDevice(ctx, &entity.AuthorizeDeviceRequest{ClientID: CLIClientID, IP: _ip})
	assert.ErrorIs(t, err, errBoom)
}

func TestVerifyDevice_Errors(t *testing.T) {
	ctx := context.Background()
	e := newDeviceEnv(t)
	rs := e.start(t)

	_, err := e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserCode: rs.UserCode, IP: _ip})
	assert.ErrorIs(t, err, ErrLoginRequired)
	_, err = e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: "BCD", IP: _ip})
	assert.ErrorIs(t, err, ErrInvalidUserCode)
	_, err = e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: "BCDF-GHJK", IP: _ip})
	assert.ErrorIs(t, err, ErrInvalidUserCode)

	// A deleted account signs in again.
	e.users.EXPECT().FindUserByID(gomock.Any(), _userID).Return(model.User{}, repository.ErrNotFound)
	_, err = e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: rs.UserCode, IP: _ip})
	assert.ErrorIs(t, err, ErrLoginRequired)
	e.users.EXPECT().FindUserByID(gomock.Any(), _userID).Return(model.User{}, errBoom)
	_, err = e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: rs.UserCode, IP: _ip})
	assert.ErrorIs(t, err, errBoom)

	// Codes cannot be guessed: every code typed counts.
	for range DeviceAttempts - 4 {
		_, err = e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: "x", IP: _ip})
		assert.ErrorIs(t, err, ErrInvalidUserCode)
	}
	_, err = e.ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: rs.UserCode, IP: _ip})
	assert.ErrorIs(t, err, ErrTooManyAttempts)

	ctrl, devices, tokens, users := withMockDevices(t)
	devices.EXPECT().FindPendingDeviceCode(gomock.Any(), "BCDFGHJK", _now).Return(model.DeviceCode{}, errBoom)
	_, err = ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: "BCDF-GHJK", IP: _ip})
	assert.ErrorIs(t, err, errBoom)
	devices.EXPECT().FindPendingDeviceCode(gomock.Any(), "BCDFGHJK", _now).Return(model.DeviceCode{ClientID: "c"}, nil)
	users.EXPECT().FindUserByID(gomock.Any(), _userID).Return(_user, nil)
	tokens.EXPECT().CreateDeviceConsentToken(gomock.Any()).Return("", errBoom)
	_, err = ctrl.VerifyDevice(ctx, &entity.VerifyDeviceRequest{UserID: _userID, UserCode: "BCDF-GHJK", IP: _ip})
	assert.ErrorIs(t, err, errBoom)
}

func TestConsentDevice_Errors(t *testing.T) {
	ctx := context.Background()
	e := newDeviceEnv(t)
	rs := e.start(t)
	consent := e.verify(t, rs.UserCode)

	for name, rq := range map[string]*entity.ConsentDeviceRequest{
		"forged":          {UserID: _userID, Token: "forged"},
		"signed out":      {Token: consent.Token},
		"another account": {UserID: _userID + 1, Token: consent.Token},
	} {
		_, err := e.ctrl.ConsentDevice(ctx, rq)
		assert.ErrorIs(t, err, ErrInvalidConsent, name)
	}

	// A browser consent is not a device consent.
	tokens, err := token.New(token.Config{Secret: "s", Now: func() time.Time { return _now }})
	require.NoError(t, err)
	browser, err := tokens.CreateConsentToken(token.OAuthGrant{UserID: _userB62, ClientID: "c"})
	require.NoError(t, err)
	_, err = e.ctrl.ConsentDevice(ctx, &entity.ConsentDeviceRequest{UserID: _userID, Token: browser, Allow: true})
	assert.ErrorIs(t, err, ErrInvalidConsent)

	ctrl, devices, mockTokens, _ := withMockDevices(t)
	mockTokens.EXPECT().ValidateDeviceConsentToken("t").Return(token.OAuthGrant{UserID: _userB62, DeviceCode: "h"}, nil)
	devices.EXPECT().AnswerDeviceCode(gomock.Any(), "h", _userID, model.DeviceCodeApproved, _now).Return(errBoom)
	_, err = ctrl.ConsentDevice(ctx, &entity.ConsentDeviceRequest{UserID: _userID, Token: "t", Allow: true})
	assert.ErrorIs(t, err, errBoom)
}

func TestTokenDevice_Errors(t *testing.T) {
	ctx := context.Background()
	e := newDeviceEnv(t)
	rs := e.start(t)

	_, err := e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: GrantDeviceCode, ClientID: CLIClientID})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: GrantDeviceCode, DeviceCode: rs.DeviceCode})
	assert.ErrorIs(t, err, ErrInvalidRequest)
	_, err = e.ctrl.Token(ctx, &entity.TokenRequest{GrantType: GrantDeviceCode, DeviceCode: rs.DeviceCode, ClientID: "unknown"})
	assert.ErrorIs(t, err, ErrInvalidClient)
	_, err = e.poll("not-a-code")
	assert.ErrorIs(t, err, ErrInvalidGrant)

	ctrl, devices, _, users := withMockDevices(t)
	poll := func() error {
		_, err := ctrl.Token(ctx, &entity.TokenRequest{GrantType: GrantDeviceCode, DeviceCode: "dc", ClientID: CLIClientID})
		return err
	}
	hash, cli := clientKey("dc"), clientKey(CLIClientID)
	live := _now.Add(time.Minute)

	devices.EXPECT().FindDeviceCodeByHash(gomock.Any(), hash).Return(model.DeviceCode{}, errBoom)
	assert.ErrorIs(t, poll(), errBoom)

	// Another client's code.
	devices.EXPECT().FindDeviceCodeByHash(gomock.Any(), hash).Return(model.DeviceCode{ClientID: "other", ExpiresAt: live}, nil)
	assert.ErrorIs(t, poll(), ErrInvalidGrant)

	devices.EXPECT().FindDeviceCodeByHash(gomock.Any(), hash).Return(model.DeviceCode{ClientID: cli, Status: model.DeviceCodePending, ExpiresAt: live}, nil)
	devices.EXPECT().PollDeviceCode(gomock.Any(), hash, _now).Return(errBoom)
	assert.ErrorIs(t, poll(), errBoom)

	// Two polls racing for one approval: only one gets tokens.
	approved := model.DeviceCode{ClientID: cli, UserID: _userID, Status: model.DeviceCodeApproved, ExpiresAt: live}
	devices.EXPECT().FindDeviceCodeByHash(gomock.Any(), hash).Return(approved, nil)
	devices.EXPECT().DeleteDeviceCode(gomock.Any(), hash).Return(repository.ErrNotFound)
	assert.ErrorIs(t, poll(), ErrInvalidGrant)
	devices.EXPECT().FindDeviceCodeByHash(gomock.Any(), hash).Return(approved, nil)
	devices.EXPECT().DeleteDeviceCode(gomock.Any(), hash).Return(errBoom)
	assert.ErrorIs(t, poll(), errBoom)

	// The account was deleted since.
	devices.EXPECT().FindDeviceCodeByHash(gomock.Any(), hash).Return(approved, nil)
	devices.EXPECT().DeleteDeviceCode(gomock.Any(), hash).Return(nil)
	users.EXPECT().FindUserByID(gomock.Any(), _userID).Return(model.User{}, repository.ErrNotFound)
	assert.ErrorIs(t, poll(), ErrInvalidGrant)
}

func TestNewUserCode(t *testing.T) {
	t.Cleanup(func() { randRead = randRead0 })
	// Bytes of 240 and up are skipped, so every letter is as likely.
	bytes := []byte{255, 240, 0, 19, 20, 239, 1, 2, 3, 4}
	randRead = func(b []byte) (int, error) {
		b[0], bytes = bytes[0], bytes[1:]
		return 1, nil
	}
	assert.Equal(t, "BZBZCDFG", newUserCode())
}

func TestUserCodeFormat(t *testing.T) {
	assert.Equal(t, "WDJB-MJHT", formatUserCode("WDJBMJHT"))
	assert.Equal(t, "WDJBMJHT", normalizeUserCode(" wdjb-mjht "))
	assert.Equal(t, "WDJBMJHT", normalizeUserCode("WDJB MJHT"))
}

var randRead0 = randRead
