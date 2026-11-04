// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/gofiber/fiber/v3"
)

// ContentETag names fixed content by its hash. It is weak, so it still
// matches once the response is compressed.
func ContentETag(b []byte) string {
	sum := sha256.Sum256(b)
	return `W/"` + hex.EncodeToString(sum[:8]) + `"`
}

// SendWithETag sends body tagged etag, or 304 Not Modified when the
// browser already holds it. Set Cache-Control first: a 304 carries it too.
func SendWithETag(c fiber.Ctx, etag string, body []byte) error {
	c.Set(fiber.HeaderETag, etag)
	if c.Fresh() {
		return c.SendStatus(fiber.StatusNotModified)
	}
	return c.Send(body)
}
