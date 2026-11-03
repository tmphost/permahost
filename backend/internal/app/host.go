// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"

	folderctrl "github.com/tmphost/permahost/backend/internal/controller/folder"
	oauthctrl "github.com/tmphost/permahost/backend/internal/controller/oauthserver"
	setupctrl "github.com/tmphost/permahost/backend/internal/controller/setup"
	"github.com/tmphost/permahost/backend/internal/handler/api"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
	"github.com/tmphost/permahost/backend/internal/service/idgen"
)

// LocalSiteID is the fiber.Ctx local a dynamic subdomain's site ID is
// stored under.
const LocalSiteID = "siteID"

type (
	// hosts tells the app host, the dynamic subdomains and the verified
	// custom domains apart from hosts the server does not answer for.
	hosts struct {
		app string
		// sites is the parent of the dynamic subdomains: file.<sites
		// domain>.
		sites string
		// mcp serves only the MCP endpoint and its OAuth metadata.
		mcp string
		// domain reports whether a host is a verified custom domain.
		domain func(host string) bool
		// open skips the host check: the app URL names this machine or an
		// IP, as in local development.
		open bool
	}

	hostKind int
)

const (
	hostUnknown hostKind = iota
	hostApp
	hostSite
	hostDomain
	hostMCP
)

// _mcpHostPaths are the paths the MCP host answers: the endpoint, the
// OAuth metadata that MCP clients discover on the endpoint's host, and the
// MCP host issuer's registration and token endpoints. Its authorization
// endpoint is on the app host, where users sign in.
var _mcpHostPaths = []string{
	MCPPath,
	api.ResourceMetadataPath,
	api.ResourceMetadataPath + MCPPath,
	api.ServerMetadataPath,
	oauthctrl.RegisterPath,
	oauthctrl.TokenPath,
}

func newHosts(appURL, sitesDomain, mcpDomain string, domain func(host string) bool) hosts {
	app := strings.ToLower(setupctrl.AppHost(appURL))
	return hosts{
		app:    app,
		sites:  folderctrl.FolderDomain(strings.ToLower(sitesDomain)),
		mcp:    strings.ToLower(mcpDomain),
		domain: domain,
		open:   isLocal(app) || net.ParseIP(app) != nil,
	}
}

// classify returns what host (a Host header, port allowed) is, and the site
// ID for a dynamic subdomain.
func (h hosts) classify(host string) (hostKind, string) {
	name := strings.ToLower(host)
	if n, _, err := net.SplitHostPort(name); err == nil {
		name = n
	}
	if h.mcp != "" && name == h.mcp {
		return hostMCP, ""
	}
	if h.sites != "" {
		if label, ok := strings.CutSuffix(name, "."+h.sites); ok && idgen.ParseFolderID(label) != 0 {
			return hostSite, label
		}
	}
	// This machine always reaches the app, e.g. for health checks.
	if name == h.app || isLocal(name) {
		return hostApp, ""
	}
	// Custom domains are looked up before the open check, so they work
	// while the app is reached by IP.
	if h.domain(name) {
		return hostDomain, ""
	}
	if h.open {
		return hostApp, ""
	}
	return hostUnknown, ""
}

// isSite reports whether host is a dynamic subdomain. Any well-formed one
// qualifies for a certificate, so a folder's host works from its first
// request.
func (h hosts) isSite(host string) bool {
	kind, _ := h.classify(host)
	return kind == hostSite
}

func isLocal(name string) bool {
	return name == "localhost" || name == "127.0.0.1" || name == "::1"
}

// isMCP reports whether host (a Host header, port allowed) is the MCP host.
func (h hosts) isMCP(host string) bool {
	kind, _ := h.classify(host)
	return kind == hostMCP
}

// isDomain reports whether host is a verified custom domain.
func (h hosts) isDomain(host string) bool {
	kind, _ := h.classify(host)
	return kind == hostDomain
}

// hostRouter lets app host requests through, and MCP host requests for the
// MCP endpoint, answers dynamic subdomains no folder was served on, serves
// custom domains with domain, and refuses every other host.
func hostRouter(h hosts, domain fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		kind, siteID := h.classify(string(c.Request().Host()))
		switch kind {
		case hostApp:
			return c.Next()
		case hostMCP:
			if slices.Contains(_mcpHostPaths, c.Path()) {
				return c.Next()
			}
			return sendJSONError(c, fiber.StatusNotFound, "not found")
		case hostSite:
			c.Locals(LocalSiteID, siteID)
			// Live folders were answered before this check, when
			// subdomains are on and the public domain is the sites
			// domain; anything left names no folder.
			return sendJSONError(c, fiber.StatusNotFound, "site not found")
		case hostDomain:
			return domain(c)
		default:
			return sendJSONError(c, fiber.StatusNotFound, "host not allowed")
		}
	}
}

func sendJSONError(c fiber.Ctx, status int, msg string) error {
	c.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	return c.Status(status).Send(mapper.FromErrorToHTTPResponse(msg))
}

// httpsRedirect is the plain http server while TLS is on: it sends
// requests for known hosts to https on port.
func httpsRedirect(h hosts, port int) *fiber.App {
	f := fiber.New()
	f.Use(func(c fiber.Ctx) error {
		host := string(c.Request().Host())
		if kind, _ := h.classify(host); kind == hostUnknown {
			return sendJSONError(c, fiber.StatusNotFound, "host not allowed")
		}
		if n, _, err := net.SplitHostPort(host); err == nil {
			host = n
		}
		if port != 443 {
			host = net.JoinHostPort(host, strconv.Itoa(port))
		}
		return c.Redirect().Status(fiber.StatusMovedPermanently).To("https://" + host + string(c.Request().RequestURI()))
	})
	return f
}
