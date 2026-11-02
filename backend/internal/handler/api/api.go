// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package api is the HTTP (Fiber) transport for the REST API.
package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"

	adminctrl "github.com/tmphost/permahost/backend/internal/controller/admin"
	authctrl "github.com/tmphost/permahost/backend/internal/controller/auth"
	"github.com/tmphost/permahost/backend/internal/controller/credit"
	domainctrl "github.com/tmphost/permahost/backend/internal/controller/domain"
	folderctrl "github.com/tmphost/permahost/backend/internal/controller/folder"
	oauthctrl "github.com/tmphost/permahost/backend/internal/controller/oauthserver"
	setupctrl "github.com/tmphost/permahost/backend/internal/controller/setup"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
)

// RoutePrefix is where the API is mounted.
const RoutePrefix = "/api/v1"

// AuthPrefix is where signing in and out is mounted, outside the API:
// /auth/config, /auth/<provider>/login, the provider's
// /auth/<provider>/callback and /auth/logout.
const AuthPrefix = "/auth"

// Root sign-in attempts allowed per client IP per window, to slow down
// guessing the root access token.
const (
	_rootLoginMax    = 10
	_rootLoginWindow = time.Minute
)

// Params are the API's dependencies.
type Params struct {
	Router fiber.Router
	// Hosts checks the Host header of every request that is not for a
	// folder host. It may be nil.
	Hosts   fiber.Handler
	Auth    authctrl.Controller
	Setup   setupctrl.Controller
	Folders folderctrl.Controller
	Domains domainctrl.Controller
	Credits credit.Controller
	OAuth   oauthctrl.Controller
	Admin   adminctrl.Controller
	// FileSettings returns the current file settings, which choose how
	// folders are served.
	FileSettings func() entity.FileSettings
	// BasePath is the path prefix the app is served under ("" or "/x").
	BasePath string
	// CookieSecure marks cookies Secure; enable it behind HTTPS.
	CookieSecure bool
}

// New registers every API route on p.Router.
func New(p Params) {
	basePath := strings.TrimRight(p.BasePath, "/")
	auth := &Auth{
		controller:   p.Auth,
		basePath:     basePath,
		cookieSecure: p.CookieSecure,
	}
	user := &User{controller: p.Auth, credits: p.Credits}
	setup := &Setup{controller: p.Setup, basePath: basePath, cookieSecure: p.CookieSecure}
	folder := &Folder{controller: p.Folders}
	domain := &Domain{controller: p.Domains}
	public := &Public{controller: p.Folders, settings: p.FileSettings}
	oauth := &OAuth{controller: p.OAuth, basePath: basePath}
	admin := &Admin{controller: p.Admin}

	// Folder hosts are answered before any route, so no app route is ever
	// reachable on a host that serves user content.
	p.Router.Use(public.Serve)
	if p.Hosts != nil {
		p.Router.Use(p.Hosts)
	}
	p.Router.All(folderctrl.PublicPathPrefix+":user/:folder/*", public.ServePath)

	// MCP clients may run in a browser; these need no cookies.
	allowAny := cors.New()
	for _, path := range []string{ServerMetadataPath, ResourceMetadataPath, oauthctrl.RegisterPath, oauthctrl.TokenPath} {
		p.Router.Use(path, allowAny)
	}
	p.Router.Get(ServerMetadataPath, oauth.ServerMetadata)
	p.Router.Get(ResourceMetadataPath, oauth.ResourceMetadata)
	p.Router.Get(ResourceMetadataPath+"/*", oauth.ResourceMetadata)
	p.Router.Post(oauthctrl.RegisterPath, oauth.Register)
	p.Router.Get(oauthctrl.AuthorizePath, OptionalAuthenticate(p.Auth), oauth.Authorize)
	p.Router.Post(oauthctrl.AuthorizePath, OptionalAuthenticate(p.Auth), oauth.Consent)
	p.Router.Post(oauthctrl.TokenPath, oauth.Token)
	p.Router.Post(oauthctrl.DeviceCodePath, oauth.DeviceCode)
	p.Router.Get(oauthctrl.DevicePath, OptionalAuthenticate(p.Auth), oauth.Device)
	p.Router.Post(oauthctrl.DevicePath, OptionalAuthenticate(p.Auth), oauth.DeviceConsent)

	p.Router.Get(AuthPrefix+"/config", auth.Config)
	p.Router.Get(AuthPrefix+"/:provider/login", auth.Login)
	p.Router.Get(AuthPrefix+"/:provider/callback", auth.Callback)
	p.Router.Post(AuthPrefix+"/logout", auth.Logout)

	r := p.Router.Group(RoutePrefix)

	authn := Authenticate(p.Auth)
	r.Get("/users/me", authn, user.Get)
	r.Get("/users/me/usage", authn, user.Usage)

	// Visitors of the web app may upload without signing in, with lower
	// limits.
	anyone := AuthenticateOrAnonymous(p.Auth)
	r.Get("/limits", anyone, folder.Limits)
	r.Post("/folders", anyone, folder.Create)
	r.Get("/folders", authn, folder.List)
	r.Get("/folders/:id", authn, folder.Get)
	r.Patch("/folders/:id", authn, folder.Update)
	r.Delete("/folders/:id", authn, folder.Delete)
	r.Put("/folders/:id/files/*", anyone, folder.PutFile)
	r.Get("/folders/:id/files/*", authn, folder.GetFile)
	r.Delete("/folders/:id/files/*", authn, folder.DeleteFile)
	r.Get("/folders/:id/domains", authn, domain.List)
	r.Post("/folders/:id/domains", authn, domain.Create)
	r.Post("/folders/:id/domains/:domain/verify", authn, domain.Verify)
	r.Delete("/folders/:id/domains/:domain", authn, domain.Delete)

	r.Post("/setup/login", limiter.New(limiter.Config{
		Max:          _rootLoginMax,
		Expiration:   _rootLoginWindow,
		LimitReached: func(c fiber.Ctx) error { return sendError(c, fiber.StatusTooManyRequests, "too many attempts") },
	}), setup.Login)
	r.Post("/setup/logout", setup.Logout)
	r.Get("/setup/settings", AuthenticateRoot(p.Setup), setup.Get)
	r.Put("/setup/settings", AuthenticateRoot(p.Setup), setup.Update)
	r.Get("/setup/users", AuthenticateRoot(p.Setup), admin.ListUsers)
	r.Get("/setup/users/:id/credits", AuthenticateRoot(p.Setup), admin.ListCreditAdjustments)
	r.Post("/setup/users/:id/credits", AuthenticateRoot(p.Setup), admin.AdjustCredits)
}

func sendJSON(c fiber.Ctx, status int, payload []byte) error {
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Status(status).Send(payload)
}

func sendError(c fiber.Ctx, status int, msg string) error {
	return sendJSON(c, status, mapper.FromErrorToHTTPResponse(msg))
}
