// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// providerSlug is a custom provider's route name, derived from its name the
// same way the server does: lowercase, runs of anything but a-z0-9 become a
// single dash, and dashes are trimmed from the ends.
export function providerSlug(name) {
  return (name || '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

// svgDataUri renders SVG markup as an <img> source, so scripts inside it
// never run.
export function svgDataUri(svg) {
  return `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`
}
