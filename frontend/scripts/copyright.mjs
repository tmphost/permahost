// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Fail the build when a source file has lost its copyright notice.
 *
 * Run in CI on every pull request. The rules live in `copyright.js`, where they
 * are tested; this is the part that talks to git and the disk.
 *
 *   node frontend/scripts/copyright.mjs          # check, and name what is missing
 *   node frontend/scripts/copyright.mjs --fix    # add it where it is absent
 */
import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { check, report } from './copyright.js'

/** The repository, not this package: the notice belongs on every file in it. */
const REPO_ROOT = fileURLToPath(new URL('../../', import.meta.url))

const tracked = execFileSync('git', ['ls-files'], { cwd: REPO_ROOT, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024 })
  .split('\n')
  .filter(Boolean)

const fix = process.argv.includes('--fix')
const { files, missing } = check(tracked, {
  fix,
  read: (path) => readFileSync(join(REPO_ROOT, path), 'utf8'),
  write: (path, contents) => writeFileSync(join(REPO_ROOT, path), contents),
})

if (missing.length > 0) {
  console.error(report(missing))
  process.exit(1)
}
console.log(`✓ ${files.length} source files carry the copyright notice`)
