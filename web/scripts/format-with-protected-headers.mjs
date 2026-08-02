/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { spawnSync } from 'node:child_process'
import {
  mkdirSync,
  mkdtempSync,
  readdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, relative } from 'node:path'

const mode = process.argv[2]

if (mode !== '--check' && mode !== '--write') {
  console.error(
    'Usage: node scripts/format-with-protected-headers.mjs --check|--write'
  )
  process.exit(2)
}

const root = process.cwd()
const excludedDirs = new Set([
  '.git',
  '.tanstack',
  'build',
  'coverage',
  'dist',
  'node_modules',
])
const headerExtensions = new Set([
  '.cjs',
  '.cts',
  '.js',
  '.jsx',
  '.mjs',
  '.mts',
  '.ts',
  '.tsx',
])
const protectedHeaderPattern =
  /^\/\*\r?\nCopyright \(C\)[\s\S]*?QuantumNous[\s\S]*?\*\/(?:\r?\n)+/

function extensionOf(path) {
  const index = path.lastIndexOf('.')
  return index === -1 ? '' : path.slice(index)
}

function walk(dir, files = []) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (!excludedDirs.has(entry.name)) {
        walk(join(dir, entry.name), files)
      }
      continue
    }

    if (entry.isFile()) {
      files.push(join(dir, entry.name))
    }
  }

  return files
}

function snapshotFiles(files) {
  const snapshot = new Map()
  for (const file of files) {
    snapshot.set(file, readFileSync(file))
  }
  return snapshot
}

function writeFileWithRetry(file, content) {
  let lastError

  for (let attempt = 0; attempt < 20; attempt += 1) {
    try {
      writeFileSync(file, content)
      return
    } catch (error) {
      lastError = error
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 100)
    }
  }

  throw lastError
}

function stripProtectedHeaders(files) {
  const headers = new Map()

  for (const file of files) {
    if (!headerExtensions.has(extensionOf(file))) {
      continue
    }

    const content = readFileSync(file, 'utf8')
    const match = content.match(protectedHeaderPattern)
    if (!match) {
      continue
    }

    headers.set(file, match[0])
    writeFileWithRetry(file, content.slice(match[0].length))
  }

  return headers
}

function restoreProtectedHeaders(headers) {
  for (const [file, header] of headers) {
    const content = readFileSync(file, 'utf8').replace(/^\n+/, '')
    if (!content.startsWith(header)) {
      writeFileWithRetry(file, header + content)
    }
  }
}

function prepareCheckWorkspace(files, checkRoot) {
  const headers = new Map()

  for (const file of files) {
    const target = join(checkRoot, relative(root, file))
    mkdirSync(dirname(target), { recursive: true })
    let content = readFileSync(file)

    if (headerExtensions.has(extensionOf(file))) {
      const text = content.toString('utf8')
      const match = text.match(protectedHeaderPattern)
      if (match) {
        headers.set(file, match[0])
        content = Buffer.from(text.slice(match[0].length))
      }
    }

    writeFileSync(target, content)
  }

  return headers
}

function listChangedFiles(before, files, headers, checkRoot) {
  const changed = []

  for (const file of files) {
    const previous = before.get(file)
    let current = readFileSync(join(checkRoot, relative(root, file)))
    const header = headers.get(file)
    if (header) {
      current = Buffer.concat([Buffer.from(header), current])
    }
    if (!previous || !previous.equals(current)) {
      changed.push(relative(root, file))
    }
  }

  return changed
}

const files = walk(root).filter(
  (file) => statSync(file).size < 10 * 1024 * 1024
)
let exitCode = 0

if (mode === '--check') {
  const before = snapshotFiles(files)
  const checkRoot = mkdtempSync(join(tmpdir(), 'new-api-format-check-'))

  try {
    const headers = prepareCheckWorkspace(files, checkRoot)
    const result = spawnSync(
      'oxfmt',
      ['-c', '.oxfmtrc.json', '--ignore-path', '.gitignore', '--write', '.'],
      {
        cwd: checkRoot,
        stdio: 'inherit',
      }
    )
    exitCode = result.status ?? 1

    if (exitCode === 0) {
      const changed = listChangedFiles(before, files, headers, checkRoot)
      if (changed.length > 0) {
        console.error('Format issues found in protected-header-safe check:')
        for (const file of changed) {
          console.error(file)
        }
        exitCode = 1
      }
    }
  } finally {
    rmSync(checkRoot, { recursive: true, force: true })
  }
} else {
  let headers = new Map()
  try {
    headers = stripProtectedHeaders(files)
    const result = spawnSync(
      'oxfmt',
      ['-c', '.oxfmtrc.json', '--ignore-path', '.gitignore', '--write', '.'],
      {
        cwd: root,
        stdio: 'inherit',
      }
    )
    exitCode = result.status ?? 1
  } finally {
    restoreProtectedHeaders(headers)
  }
}

process.exit(exitCode)
