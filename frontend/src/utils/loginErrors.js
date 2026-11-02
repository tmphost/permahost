// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Messages for the codes the server puts in /login?error=<code>.
const MESSAGES = {
  cancelled: 'Sign-in was cancelled.',
  unknown_provider: 'That sign-in provider is not available.',
  invalid_state: 'Your sign-in session expired. Please try again.',
  email_not_verified: 'Your account needs a verified email address to sign in.',
  provider_error: 'The sign-in provider rejected the request. Please try again.',
  server_error: 'Something went wrong on our side. Please try again.',
}

export function loginErrorMessage(code) {
  if (!code) return ''
  return MESSAGES[code] || MESSAGES.server_error
}
