#!/usr/bin/env node

import { createHash } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import { existsSync, readFileSync } from 'node:fs'
import { resolve, join } from 'node:path'

const args = process.argv.slice(2)
const outputArg = args.shift()
let tag = 'latest'
let provenance = false
let dryRun = false

while (args.length > 0) {
  const arg = args.shift()
  if (arg === '--tag') tag = args.shift()
  else if (arg === '--provenance') provenance = true
  else if (arg === '--dry-run') dryRun = true
  else fail(`unknown argument: ${arg}`)
}

if (!outputArg) fail('usage: publish-npm-packages.mjs <output-root> [--tag <tag>] [--provenance] [--dry-run]')
if (!tag || !/^[a-z][a-z0-9._-]*$/.test(tag)) fail(`invalid dist-tag: ${tag}`)

const outputRoot = resolve(outputArg)
const tarballsRoot = join(outputRoot, 'tarballs')
const index = JSON.parse(readFileSync(join(outputRoot, 'packages.json'), 'utf8'))
const checksums = parseChecksums(readFileSync(join(tarballsRoot, 'SHA256SUMS'), 'utf8'))

if (index.version.includes('-') && tag === 'latest') {
  fail(`refusing to publish prerelease ${index.version} under the latest tag`)
}
if (!Array.isArray(index.packages) || index.packages.length !== 6) {
  fail('expected five platform packages and one main package')
}
if (index.packages.at(-1)?.name !== '@tokenflux/tf') {
  fail('the main package must be published last')
}

for (const entry of index.packages) {
  const tarball = join(tarballsRoot, entry.tarball)
  if (!existsSync(tarball)) fail(`missing tarball: ${entry.tarball}`)
  if (sha256(tarball) !== checksums.get(entry.tarball)) {
    fail(`checksum mismatch: ${entry.tarball}`)
  }

  const spec = `${entry.name}@${index.version}`
  if (!dryRun && packageExists(spec)) {
    console.log(`${spec} already exists; skipping`)
    continue
  }

  const publishArgs = ['publish', tarball, '--access', 'public', '--tag', tag]
  if (provenance) publishArgs.push('--provenance')
  if (dryRun) publishArgs.push('--dry-run')
  runNpm(publishArgs, `publishing ${spec}`)
}

function packageExists(spec) {
  const result = npm(['view', spec, 'version', '--json'])
  if (result.status === 0) return true
  const output = `${result.stdout}\n${result.stderr}`
  if (output.includes('E404')) return false
  process.stderr.write(result.stderr || result.stdout)
  fail(`cannot check whether ${spec} exists`)
}

function runNpm(npmArgs, action) {
  const result = npm(npmArgs, { stdio: 'inherit' })
  if (result.status !== 0) fail(`npm failed while ${action}`)
}

function npm(npmArgs, options = {}) {
  const command = process.platform === 'win32' ? 'npm.cmd' : 'npm'
  return spawnSync(command, npmArgs, {
    encoding: 'utf8',
    ...options,
  })
}

function parseChecksums(text) {
  const values = new Map()
  for (const line of text.trim().split('\n')) {
    const match = /^([a-f0-9]{64})  ([^/]+\.tgz)$/.exec(line)
    if (!match) fail(`invalid checksum line: ${line}`)
    values.set(match[2], match[1])
  }
  return values
}

function sha256(file) {
  return createHash('sha256').update(readFileSync(file)).digest('hex')
}

function fail(message) {
  console.error(`error: ${message}`)
  process.exit(1)
}
