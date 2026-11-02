// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	mockrepo "github.com/tmphost/tmphost/backend/internal/repository/mocks"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	mockidgen "github.com/tmphost/tmphost/backend/internal/service/idgen/mocks"
	"github.com/tmphost/tmphost/backend/internal/service/oauth"
	mockoauth "github.com/tmphost/tmphost/backend/internal/service/oauth/mocks"
	mocktoken "github.com/tmphost/tmphost/backend/internal/service/token/mocks"
)

var (
	_now    = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type env struct {
	ctrl   Controller
	google *mockoauth.MockProvider
	github *mockoauth.MockProvider
	token  *mocktoken.MockService
	users  *mockrepo.MockUserRepository
	idgen  *mockidgen.MockService
}

func newEnv(t *testing.T) *env {
	t.Helper()
	mc := gomock.NewController(t)
	e := &env{
		google: mockoauth.NewMockProvider(mc),
		github: mockoauth.NewMockProvider(mc),
		token:  mocktoken.NewMockService(mc),
		users:  mockrepo.NewMockUserRepository(mc),
		idgen:  mockidgen.NewMockService(mc),
	}
	e.google.EXPECT().Name().Return(oauth.ProviderGoogle).AnyTimes()
	e.github.EXPECT().Name().Return(oauth.ProviderGitHub).AnyTimes()
	e.google.EXPECT().Label().Return("Google").AnyTimes()
	e.github.EXPECT().Label().Return("GitHub").AnyTimes()
	e.google.EXPECT().Icon().Return("").AnyTimes()
	e.github.EXPECT().Icon().Return("<svg/>").AnyTimes()
	e.ctrl = New(Params{
		Providers: []oauth.Provider{e.google, e.github},
		Token:     e.token,
		Users:     e.users,
		IDGen:     e.idgen,
		Grant:     func() int { return 250 },
		BasePath:  "/app/",
		BaseURL:   "https://tmphost.io/app/",
		Now:       func() time.Time { return _now },
	})
	return e
}

func TestNew_DefaultClock(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.WithinDuration(t, time.Now(), c.now(), time.Second)
	assert.Equal(t, 250, c.grant())
}

func TestGetAuthConfig(t *testing.T) {
	e := newEnv(t)
	rs := e.ctrl.GetAuthConfig(context.Background())
	assert.Equal(t, []entity.AuthProvider{
		{ID: "google", Name: "Google"},
		{ID: "github", Name: "GitHub", Icon: "<svg/>"},
	}, rs.Providers)
}

func TestStartLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown provider", func(t *testing.T) {
		e := newEnv(t)
		_, err := e.ctrl.StartLogin(ctx, &entity.StartLoginRequest{Provider: "x"})
		assert.ErrorIs(t, err, ErrUnknownProvider)
	})

	t.Run("state error", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().CreateStateToken("google", "/").Return("", errBoom)
		_, err := e.ctrl.StartLogin(ctx, &entity.StartLoginRequest{Provider: "google"})
		assert.ErrorIs(t, err, errBoom)
	})

	t.Run("provider unavailable", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().CreateStateToken("github", "/").Return("st", nil)
		e.github.EXPECT().AuthURL(gomock.Any(), "st").Return("", errBoom)
		_, err := e.ctrl.StartLogin(ctx, &entity.StartLoginRequest{Provider: "github"})
		assert.ErrorIs(t, err, ErrProviderUnavailable)
	})

	t.Run("ok sanitizes redirect", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().CreateStateToken("github", "/").Return("st", nil)
		e.github.EXPECT().AuthURL(gomock.Any(), "st").Return("https://github.com/login?state=st", nil)
		rs, err := e.ctrl.StartLogin(ctx, &entity.StartLoginRequest{Provider: "github", RedirectURL: "//evil.com"})
		require.NoError(t, err)
		assert.Equal(t, &entity.StartLoginResponse{AuthURL: "https://github.com/login?state=st", State: "st"}, rs)
	})
}

func validLogin() *entity.CompleteLoginRequest {
	return &entity.CompleteLoginRequest{Provider: "google", Code: "code", State: "st", StateCookie: "st"}
}

func TestCompleteLogin_Rejections(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown provider", func(t *testing.T) {
		e := newEnv(t)
		rq := validLogin()
		rq.Provider = "x"
		_, err := e.ctrl.CompleteLogin(ctx, rq)
		assert.ErrorIs(t, err, ErrUnknownProvider)
	})

	t.Run("missing state", func(t *testing.T) {
		e := newEnv(t)
		_, err := e.ctrl.CompleteLogin(ctx, &entity.CompleteLoginRequest{Provider: "google"})
		assert.ErrorIs(t, err, ErrInvalidState)
	})

	t.Run("state cookie mismatch", func(t *testing.T) {
		e := newEnv(t)
		rq := validLogin()
		rq.StateCookie = "other"
		_, err := e.ctrl.CompleteLogin(ctx, rq)
		assert.ErrorIs(t, err, ErrInvalidState)
	})

	t.Run("state signature invalid", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().ValidateStateToken("st", "google").Return("", errBoom)
		_, err := e.ctrl.CompleteLogin(ctx, validLogin())
		assert.ErrorIs(t, err, ErrInvalidState)
	})

	t.Run("unverified email", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().ValidateStateToken("st", "google").Return("/", nil)
		e.google.EXPECT().Exchange(ctx, "code").Return(nil, oauth.ErrNoVerifiedEmail)
		_, err := e.ctrl.CompleteLogin(ctx, validLogin())
		assert.ErrorIs(t, err, ErrEmailNotVerified)
	})

	t.Run("exchange fails", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().ValidateStateToken("st", "google").Return("/", nil)
		e.google.EXPECT().Exchange(ctx, "code").Return(nil, errBoom)
		_, err := e.ctrl.CompleteLogin(ctx, validLogin())
		assert.ErrorIs(t, err, ErrProviderExchange)
	})
}

func expectExchange(e *env, ctx context.Context) {
	e.token.EXPECT().ValidateStateToken("st", "google").Return("/settings", nil)
	e.google.EXPECT().Exchange(ctx, "code").Return(&oauth.Identity{
		Subject: "g1", Email: " New@Example.com ", Name: "New", Picture: "pic",
	}, nil)
}

func TestCompleteLogin_NewUserGetsMonthlyCredits(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	expectExchange(e, ctx)
	e.users.EXPECT().FindUserByEmail(ctx, "new@example.com").Return(model.User{}, repository.ErrNotFound)
	e.idgen.EXPECT().NextID().Return(int64(1000))
	e.users.EXPECT().CreateUser(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, u model.User) (model.User, error) {
		assert.Equal(t, model.User{
			ID: 1000, Email: "new@example.com", Name: "New", Picture: "pic", Provider: "google",
			Credits: 250, CreditsRenewAt: time.Date(2026, 11, 17, 0, 0, 0, 0, time.UTC),
		}, u)
		return u, nil
	})
	e.token.EXPECT().CreateSessionToken(monoflake.ID(1000).String()).Return("session", nil)

	rs, err := e.ctrl.CompleteLogin(ctx, validLogin())
	require.NoError(t, err)
	assert.Equal(t, "session", rs.SessionToken)
	assert.Equal(t, "/app/settings", rs.RedirectURL)
	assert.Equal(t, 250, rs.User.Credits)
	assert.Equal(t, 250, rs.User.CreditsLimit)
}

func TestCompleteLogin_CreateFails(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	expectExchange(e, ctx)
	e.users.EXPECT().FindUserByEmail(ctx, "new@example.com").Return(model.User{}, repository.ErrNotFound)
	e.idgen.EXPECT().NextID().Return(int64(1000))
	e.users.EXPECT().CreateUser(ctx, gomock.Any()).Return(model.User{}, errBoom)
	_, err := e.ctrl.CompleteLogin(ctx, validLogin())
	assert.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_FindFails(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	expectExchange(e, ctx)
	e.users.EXPECT().FindUserByEmail(ctx, "new@example.com").Return(model.User{}, errBoom)
	_, err := e.ctrl.CompleteLogin(ctx, validLogin())
	assert.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_ExistingUserUnchanged(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	expectExchange(e, ctx)
	existing := model.User{ID: 5, Email: "new@example.com", Name: "Kept", Picture: "kept", Credits: 3, CreditsRenewAt: _now.Add(time.Hour)}
	e.users.EXPECT().FindUserByEmail(ctx, "new@example.com").Return(existing, nil)
	e.token.EXPECT().CreateSessionToken(monoflake.ID(5).String()).Return("session", nil)

	rs, err := e.ctrl.CompleteLogin(ctx, validLogin())
	require.NoError(t, err)
	assert.Equal(t, "Kept", rs.User.Name)
	assert.Equal(t, 3, rs.User.Credits, "a second provider must not grant extra credits")
}

func TestCompleteLogin_ExistingUserBackfilled(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	expectExchange(e, ctx)
	existing := model.User{ID: 5, Email: "new@example.com", Credits: 7, CreditsRenewAt: _now.Add(time.Hour)}
	e.users.EXPECT().FindUserByEmail(ctx, "new@example.com").Return(existing, nil)
	e.users.EXPECT().UpdateUser(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, u model.User) (model.User, error) {
		assert.Equal(t, "New", u.Name)
		assert.Equal(t, "pic", u.Picture)
		return u, nil
	})
	e.token.EXPECT().CreateSessionToken(monoflake.ID(5).String()).Return("session", nil)

	rs, err := e.ctrl.CompleteLogin(ctx, validLogin())
	require.NoError(t, err)
	assert.Equal(t, 7, rs.User.Credits, "signing in never touches the balance")
}

func TestCompleteLogin_UpdateFails(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	expectExchange(e, ctx)
	e.users.EXPECT().FindUserByEmail(ctx, "new@example.com").Return(model.User{ID: 5, CreditsRenewAt: _now.Add(time.Hour)}, nil)
	e.users.EXPECT().UpdateUser(ctx, gomock.Any()).Return(model.User{}, errBoom)
	_, err := e.ctrl.CompleteLogin(ctx, validLogin())
	assert.ErrorIs(t, err, errBoom)
}

func TestCompleteLogin_SessionFails(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	expectExchange(e, ctx)
	e.users.EXPECT().FindUserByEmail(ctx, "new@example.com").Return(model.User{ID: 5, Name: "n", Picture: "p", CreditsRenewAt: _now.Add(time.Hour)}, nil)
	e.token.EXPECT().CreateSessionToken(gomock.Any()).Return("", errBoom)
	_, err := e.ctrl.CompleteLogin(ctx, validLogin())
	assert.ErrorIs(t, err, errBoom)
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()

	t.Run("empty", func(t *testing.T) {
		e := newEnv(t)
		_, err := e.ctrl.Authenticate(ctx, &entity.AuthenticateRequest{})
		assert.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("invalid", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().ValidateSessionToken("tok").Return("", errBoom)
		_, err := e.ctrl.Authenticate(ctx, &entity.AuthenticateRequest{SessionToken: "tok"})
		assert.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("bad subject", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().ValidateSessionToken("tok").Return("", nil)
		_, err := e.ctrl.Authenticate(ctx, &entity.AuthenticateRequest{SessionToken: "tok"})
		assert.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("ok", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().ValidateSessionToken("tok").Return(monoflake.ID(77).String(), nil)
		rs, err := e.ctrl.Authenticate(ctx, &entity.AuthenticateRequest{SessionToken: "tok"})
		require.NoError(t, err)
		assert.Equal(t, int64(77), rs.UserID)
	})
}

func TestAuthenticateAnonymous(t *testing.T) {
	ctx := context.Background()
	want := idgen.AnonymousUserID("203.0.113.7")

	for name, rq := range map[string]entity.AuthenticateAnonymousRequest{
		"origin":          {IP: "203.0.113.7", Origin: "https://tmphost.io"},
		"origin any case": {IP: "203.0.113.7", Origin: "https://TMPHOST.io"},
		"referer":         {IP: "203.0.113.7", Referer: "https://tmphost.io/app/files?x=1"},
		"origin wins":     {IP: "203.0.113.7", Origin: "https://tmphost.io", Referer: "https://evil.example/"},
	} {
		t.Run(name, func(t *testing.T) {
			rs, err := newEnv(t).ctrl.AuthenticateAnonymous(ctx, &rq)
			require.NoError(t, err)
			assert.Equal(t, &entity.AuthenticateResponse{UserID: want, Anonymous: true}, rs)
		})
	}

	for name, rq := range map[string]entity.AuthenticateAnonymousRequest{
		"no origin":       {IP: "203.0.113.7"},
		"other origin":    {IP: "203.0.113.7", Origin: "https://evil.example"},
		"other scheme":    {IP: "203.0.113.7", Origin: "http://tmphost.io"},
		"subdomain":       {IP: "203.0.113.7", Origin: "https://f00000000001.tmphost.io"},
		"origin is wrong": {IP: "203.0.113.7", Origin: "https://evil.example", Referer: "https://tmphost.io/"},
		"other referer":   {IP: "203.0.113.7", Referer: "https://evil.example/https://tmphost.io"},
		"bad referer":     {IP: "203.0.113.7", Referer: "::"},
		"relative":        {IP: "203.0.113.7", Referer: "/files"},
		"bad ip":          {IP: "nope", Origin: "https://tmphost.io"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := newEnv(t).ctrl.AuthenticateAnonymous(ctx, &rq)
			assert.ErrorIs(t, err, ErrUnauthorized)
		})
	}

	t.Run("no base url", func(t *testing.T) {
		_, err := New(Params{}).AuthenticateAnonymous(ctx, &entity.AuthenticateAnonymousRequest{IP: "203.0.113.7"})
		assert.ErrorIs(t, err, ErrUnauthorized)
	})
}

func TestAuthenticate_AccessToken(t *testing.T) {
	ctx := context.Background()

	t.Run("wins over the session", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().ValidateAccessToken("at").Return(monoflake.ID(9).String(), nil)
		rs, err := e.ctrl.Authenticate(ctx, &entity.AuthenticateRequest{AccessToken: "at", SessionToken: "ignored"})
		require.NoError(t, err)
		assert.Equal(t, int64(9), rs.UserID)
	})

	t.Run("invalid", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().ValidateAccessToken("at").Return("", errBoom)
		_, err := e.ctrl.Authenticate(ctx, &entity.AuthenticateRequest{AccessToken: "at"})
		assert.ErrorIs(t, err, ErrUnauthorized)
	})
}

func TestGetCurrentUser(t *testing.T) {
	ctx := context.Background()
	rq := &entity.GetCurrentUserRequest{UserID: 5}

	t.Run("deleted user", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(ctx, int64(5)).Return(model.User{}, repository.ErrNotFound)
		_, err := e.ctrl.GetCurrentUser(ctx, rq)
		assert.ErrorIs(t, err, ErrUnauthorized)
	})

	t.Run("db error", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(ctx, int64(5)).Return(model.User{}, errBoom)
		_, err := e.ctrl.GetCurrentUser(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})

	t.Run("within period", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(ctx, int64(5)).Return(model.User{ID: 5, Credits: 10, CreditsRenewAt: _now.Add(time.Hour)}, nil)
		rs, err := e.ctrl.GetCurrentUser(ctx, rq)
		require.NoError(t, err)
		assert.Equal(t, 10, rs.User.Credits)
	})
}

func TestSanitizeRedirect(t *testing.T) {
	cases := map[string]string{
		"":                   "/",
		"/":                  "/",
		"/settings?tab=1":    "/settings?tab=1",
		"//evil.com":         "/",
		"/\\evil.com":        "/",
		"https://evil.com/x": "/",
		"relative":           "/",
	}
	for in, want := range cases {
		assert.Equal(t, want, sanitizeRedirect(in), in)
	}
}
