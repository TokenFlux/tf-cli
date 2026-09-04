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
  if (!dryRun && packageExists(entry.name, index.version)) {
    console.log(`${spec} already exists; skipping`)
    continue
  }

  const publishArgs = ['publish', tarball, '--access', 'public', '--tag', tag]
  if (provenance) publishArgs.push('--provenance')
  if (dryRun) publishArgs.push('--dry-run')
  runNpm(publishArgs, `publishing ${spec}`)
}

if (!dryRun) {
  const verifiedTags = new Map()
  for (const entry of index.packages) {
    verifiedTags.set(entry.name, waitForDistTag(entry.name, tag, index.version))
  }

  // npm currently adds latest while creating a package even when the first
  // publish uses another tag, and rejects removing it while it is the only
  // version. Surface that temporary state until the first stable release.
  if (index.version.includes('-') && tag !== 'latest') {
    for (const entry of index.packages) {
      if (verifiedTags.get(entry.name).latest === index.version) {
        console.warn(`warning: latest temporarily points to ${entry.name}@${index.version}`)
      }
    }
  }
}

function waitForDistTag(name, tag, version) {
  const retryDelaysMs = [0, 1_000, 2_000, 4_000, 8_000, 15_000, 30_000]
  let tags = {}
  for (const delayMs of retryDelaysMs) {
    if (delayMs > 0) sleep(delayMs)
    tags = distTags(name)
    if (tags[tag] === version) return tags
    if (delayMs !== retryDelaysMs.at(-1)) {
      console.warn(`waiting for ${name} tag ${tag} to reach ${version}`)
    }
  }
  fail(`${name} tag ${tag} is ${tags[tag] || 'missing'}; expected ${version}`)
}

function sleep(milliseconds) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, milliseconds)
}

function packageExists(name, version) {
  const spec = `${name}@${version}`
  const result = npm(['view', spec, 'version', '--json'])
  if (result.status === 0) return true
  const output = `${result.stdout}\n${result.stderr}`
  if (!output.includes('E404')) {
    process.stderr.write(result.stderr || result.stdout)
    fail(`cannot check whether ${spec} exists`)
  }

  // A new package's dist-tags endpoint can become readable before its package
  // document. This fallback keeps an immediate retry from republishing a version.
  return Object.values(distTags(name, true)).includes(version)
}

function distTags(name, allowMissing = false) {
  const result = npm(['dist-tag', 'ls', name, '--color=false'])
  if (result.status !== 0) {
    const output = `${result.stdout}\n${result.stderr}`
    if (allowMissing && output.includes('E404')) return {}
    process.stderr.write(result.stderr || result.stdout)
    fail(`cannot read dist-tags for ${name}`)
  }

  const tags = {}
  for (const line of result.stdout.trim().split('\n')) {
    if (!line) continue
    const separator = line.indexOf(': ')
    if (separator < 1) fail(`invalid dist-tag output for ${name}: ${line}`)
    tags[line.slice(0, separator)] = line.slice(separator + 2)
  }
  return tags
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
