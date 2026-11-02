// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'
import { recordName, registeredDomain } from '../src/utils/domains'

describe('domains', () => {
  it('recordName is the part left of the registered domain', () => {
    expect(recordName('www.example.com')).toBe('www')
    expect(recordName('a.b.example.com')).toBe('a.b')
    expect(recordName('example.com')).toBe('@')
    expect(recordName('www.example.co.uk')).toBe('www')
    expect(recordName('example.co.uk')).toBe('@')
    expect(recordName('www.co.com')).toBe('www')
    expect(recordName('docs.example.io')).toBe('docs')
  })

  it('registeredDomain is where the record is added', () => {
    expect(registeredDomain('www.example.com')).toBe('example.com')
    expect(registeredDomain('a.b.example.co.uk')).toBe('example.co.uk')
    expect(registeredDomain('example.com')).toBe('example.com')
  })
})
