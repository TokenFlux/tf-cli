import assert from 'node:assert/strict'
import { spawnSync } from 'node:child_process'
import {
  chmodSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs'
import { tmpdir } from 'node:os'
import { delimiter, dirname, join, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..')
const buildScript = join(repoRoot, 'scripts', 'build-npm-packages.mjs')
const publishScript = join(repoRoot, 'scripts', 'publish-npm-packages.mjs')
const platforms = JSON.parse(readFileSync(join(repoRoot, 'npm', 'tf', 'platforms.json'), 'utf8'))
const version = '1.2.3-test.0'

function runNode(args, options = {}) {
  return spawnSync(process.execPath, args, {
    cwd: repoRoot,
    encoding: 'utf8',
    ...options,
  })
}

test('builds five platform packages and the main shim package', (t) => {
  const root = mkdtempSync(join(tmpdir(), 'tf-npm-packages-'))
  const binaries = join(root, 'binaries')
  const output = join(root, 'output')
  t.after(() => {
    chmodSync(root, 0o755)
    rmSync(root, { force: true, recursive: true })
  })

  for (const target of Object.values(platforms)) {
    const directory = join(binaries, `npm-${target.goos}-${target.goarch}`)
    const filename = target.binary.endsWith('.exe') ? 'tf.exe' : 'tf'
    mkdirSync(directory, { recursive: true })
    writeFileSync(join(directory, filename), target.goos === 'windows' ? 'MZ' : '#!/bin/sh\nexit 0\n')
  }

  const built = runNode([buildScript, version, binaries, output])
  assert.equal(built.status, 0, built.stderr || built.stdout)

  const index = JSON.parse(readFileSync(join(output, 'packages.json'), 'utf8'))
  assert.equal(index.version, version)
  assert.equal(index.packages.length, 6)
  assert.equal(index.packages.at(-1).name, '@tokenflux/tf')
  assert.equal(readdirSync(join(output, 'tarballs')).filter((name) => name.endsWith('.tgz')).length, 6)
  assert.equal(readFileSync(join(output, 'tarballs', 'SHA256SUMS'), 'utf8').trim().split('\n').length, 6)

  const main = JSON.parse(readFileSync(join(output, 'packages', 'tf', 'package.json'), 'utf8'))
  assert.equal(main.version, version)
  assert.equal(main.private, undefined)
  assert.deepEqual(Object.keys(main.optionalDependencies).sort(), [
    '@tokenflux/tf-darwin-arm64',
    '@tokenflux/tf-darwin-x64',
    '@tokenflux/tf-linux-arm64',
    '@tokenflux/tf-linux-x64',
    '@tokenflux/tf-win32-x64',
  ])
  assert.ok(statSync(join(output, 'packages', 'tf', 'bin', 'tf.js')).mode & 0o111)

  for (const [platformKey, target] of Object.entries(platforms)) {
    const packageDir = join(output, 'packages', target.package.slice('@tokenflux/'.length))
    const manifest = JSON.parse(readFileSync(join(packageDir, 'package.json'), 'utf8'))
    const [os, cpu] = platformKey.split('-')
    assert.equal(manifest.version, version)
    assert.deepEqual(manifest.os, [os])
    assert.deepEqual(manifest.cpu, [cpu])
    assert.ok(statSync(join(packageDir, target.binary)).mode & 0o111)
  }

  const unsafeLatest = runNode([publishScript, output, '--dry-run'])
  assert.notEqual(unsafeLatest.status, 0)
  assert.match(unsafeLatest.stderr, /refusing to publish prerelease/)

  const dryRun = runNode([publishScript, output, '--tag', 'bootstrap', '--dry-run'])
  assert.equal(dryRun.status, 0, dryRun.stderr || dryRun.stdout)

  const fakeBin = join(root, 'fake-bin')
  const fakeState = join(root, 'registry.json')
  mkdirSync(fakeBin)
  writeFileSync(fakeState, JSON.stringify({
    calls: [],
    packages: Object.fromEntries(index.packages.map((entry) => [entry.name, {
      bootstrap: version,
      latest: version,
    }])),
  }))
  const fakeNpm = join(fakeBin, 'npm')
  writeFileSync(fakeNpm, `#!/usr/bin/env node
const fs = require('node:fs')
const stateFile = process.env.FAKE_NPM_STATE
const state = JSON.parse(fs.readFileSync(stateFile, 'utf8'))
const args = process.argv.slice(2)
state.calls.push(args)
if (args[0] === 'view') {
  fs.writeFileSync(stateFile, JSON.stringify(state))
  console.error('E404')
  process.exit(1)
}
if (args[0] === 'dist-tag' && args[1] === 'ls') {
  state.tagReads = state.tagReads || {}
  state.tagReads[args[2]] = (state.tagReads[args[2]] || 0) + 1
  const tags = { ...(state.packages[args[2]] || {}) }
  if (args[2] === '@tokenflux/tf' && state.tagReads[args[2]] === 2) {
    tags.bootstrap = '1.2.3-test.stale'
  }
  fs.writeFileSync(stateFile, JSON.stringify(state))
  for (const [tag, value] of Object.entries(tags)) console.log(tag + ': ' + value)
  process.exit(0)
}
if (args[0] === 'dist-tag' && args[1] === 'rm') {
  delete state.packages[args[2]][args[3]]
  fs.writeFileSync(stateFile, JSON.stringify(state))
  process.exit(0)
}
fs.writeFileSync(stateFile, JSON.stringify(state))
process.exit(args[0] === 'publish' ? 90 : 91)
`)
  chmodSync(fakeNpm, 0o755)

  const recovered = runNode([publishScript, output, '--tag', 'bootstrap'], {
    env: {
      ...process.env,
      FAKE_NPM_STATE: fakeState,
      PATH: `${fakeBin}${delimiter}${process.env.PATH}`,
    },
  })
  assert.equal(recovered.status, 0, recovered.stderr || recovered.stdout)
  const state = JSON.parse(readFileSync(fakeState, 'utf8'))
  assert.equal(state.calls.filter((args) => args[0] === 'publish').length, 0)
  assert.equal(state.calls.filter((args) => args[0] === 'dist-tag' && args[1] === 'rm').length, 0)
  assert.match(recovered.stderr, /waiting for @tokenflux\/tf tag bootstrap to reach 1\.2\.3-test\.0/)
  assert.match(recovered.stderr, /latest temporarily points to @tokenflux\/tf@1\.2\.3-test\.0/)
  assert.equal(state.tagReads['@tokenflux/tf'], 3)
  for (const tags of Object.values(state.packages)) {
    assert.deepEqual(tags, { bootstrap: version, latest: version })
  }
})
