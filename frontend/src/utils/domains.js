// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Second-level labels registries sell names under, as in example.co.uk.
const SECOND_LEVEL = new Set(['ac', 'co', 'com', 'edu', 'gov', 'net', 'org'])

// recordName is what goes in a DNS provider's Name (or Host) field for
// hostname: the labels left of the registered domain, or '@' for the
// registered domain itself. www.example.com -> www, a.b.example.co.uk -> a.b.
export function recordName(hostname) {
  const labels = hostname.split('.')
  let domainLabels = 2
  if (labels.length > 2 && labels.at(-1).length === 2 && SECOND_LEVEL.has(labels.at(-2))) domainLabels = 3
  return labels.slice(0, -domainLabels).join('.') || '@'
}

// registeredDomain is the domain the record is added under.
export function registeredDomain(hostname) {
  const name = recordName(hostname)
  return name === '@' ? hostname : hostname.slice(name.length + 1)
}
