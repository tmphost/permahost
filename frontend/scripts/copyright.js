// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The copyright notice every source file carries, and the rules about it.
 *
 * Text in a git repository cannot be made immutable: anyone can delete a line.
 * What the check guarantees is that removing or rewording the notice never
 * reaches `main` unnoticed, because CI runs it on every pull request and names
 * each file whose notice is missing or altered.
 *
 * The notice is compared exactly (after trimming), so it cannot drift into
 * several spellings. The runner beside this file (`copyright.mjs`) does the git
 * and disk work; the rules live here, where they are tested.
 */

/** The notice itself. The only place it is written down. */
export const NOTICE_LINES = Object.freeze([
  'Copyright 2026 Contextual, Inc. https://tmphost.io',
  'This notice may not be modified or removed.',
  'SPDX-License-Identifier: AGPL-3.0-only',
])

const slashes = (lines) => lines.map((line) => `// ${line}`).join('\n')
const hashes = (lines) => lines.map((line) => `# ${line}`).join('\n')
const markup = (lines) => ['<!--', ...lines.map((line) => `  ${line}`), '-->'].join('\n')

/**
 * How each kind of file says it. One comment block, so it reads as one notice.
 * Keyed by extension, or by the whole name for files that have none.
 */
export const COMMENT = Object.freeze({
  '.go': slashes,
  '.js': slashes,
  '.mjs': slashes,
  '.vue': markup,
  '.html': markup,
  '.css': (lines) => ['/*', ...lines.map((line) => ` * ${line}`), ' */'].join('\n'),
  '.sh': hashes,
  '.ps1': hashes,
  '.yml': hashes,
  '.yaml': hashes,
  Dockerfile: hashes,
  Makefile: hashes,
})

/** Files keyed by name rather than extension. */
const NAMED = new Set(['Dockerfile', 'Makefile'])

/** Dependencies and build output: not ours, and regenerated anyway. */
const NOT_OURS = /(^|\/)(node_modules|dist|coverage)\//

/**
 * Go's marker for generated code. Mocks carry it and are rewritten by
 * `make mocks`, which would drop a notice and fail the mocks-in-sync check.
 */
const GENERATED = /^\/\/ Code generated .* DO NOT EDIT\.$/m

/** A first line that must stay first: a shebang, or an HTML doctype. */
const PROLOGUE = /^(#!|<!doctype )/i

export function extensionOf(path) {
  const name = String(path ?? '')
  const dot = name.lastIndexOf('.')
  const slash = name.lastIndexOf('/')
  return dot > slash ? name.slice(dot) : ''
}

/** The key into COMMENT: the name for Dockerfile and Makefile, else the extension. */
export function kindOf(path) {
  const name = String(path ?? '')
  const base = name.slice(name.lastIndexOf('/') + 1)
  return NAMED.has(base) ? base : extensionOf(name)
}

/** Whether a path is one of ours to mark. */
export function isOurs(path) {
  if (NOT_OURS.test(String(path ?? ''))) return false
  return Object.hasOwn(COMMENT, kindOf(path))
}

/** The exact block this file should carry, or '' for a file that carries none. */
export function noticeFor(path) {
  const comment = COMMENT[kindOf(path)]
  return comment ? comment(NOTICE_LINES) : ''
}

/**
 * Whether a file carries its notice, or needs none.
 *
 * Looked for in the first few lines rather than only the first, so a file may
 * open with something that has to stay on top, such as a shebang.
 */
export function carriesNotice(contents, path, within = 8) {
  const wanted = noticeFor(path)
  const text = String(contents ?? '')
  if (!wanted || GENERATED.test(text)) return true

  const head = text
    .split('\n')
    .slice(0, within)
    .map((line) => line.trim())
  const block = wanted.split('\n').map((line) => line.trim())

  return head.some((_, at) => block.every((line, n) => head[at + n] === line))
}

/**
 * The file with its notice added, or unchanged if it already has one.
 *
 * A blank line follows the notice. In Go a comment directly above `package` is
 * the package documentation, so running into it would change what godoc says.
 * A shebang or doctype stays on the first line, above the notice.
 */
export function withNotice(contents, path) {
  const text = String(contents ?? '')
  if (carriesNotice(text, path)) return text
  const notice = noticeFor(path)
  if (!PROLOGUE.test(text)) return `${notice}\n\n${text}`
  const [first, ...rest] = text.split('\n')
  return [first, notice, '', ...rest].join('\n')
}

/**
 * Checks (or with `fix`, repairs) every file in `paths` that is ours.
 *
 * `read` and `write` are injected so the rules can be tested without a disk.
 * Returns the files checked and those still missing the notice.
 */
export function check(paths, { read, write, fix = false }) {
  const files = paths.filter(isOurs)
  const missing = []
  for (const path of files) {
    const contents = read(path)
    if (carriesNotice(contents, path)) continue
    if (fix) write(path, withNotice(contents, path))
    else missing.push(path)
  }
  return { files, missing }
}

/** The failure message: the files and the exact notice they should carry. */
export function report(missing, limit = 40) {
  const lines = [`✗ ${missing.length} file${missing.length === 1 ? '' : 's'} without the copyright notice:`, '']
  for (const path of missing.slice(0, limit)) lines.push(`    ${path}`)
  if (missing.length > limit) lines.push(`    … and ${missing.length - limit} more`)
  lines.push('', 'Expected, exactly:', '', ...NOTICE_LINES.map((line) => `    ${line}`))
  lines.push('', 'Run: node frontend/scripts/copyright.mjs --fix')
  return lines.join('\n')
}
