#!/usr/bin/env node

import { createHash } from 'node:crypto'
import { spawnSync } from 'node:child_process'
import {
  chmodSync,
  copyFileSync,
  cpSync,
  mkdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs'
import { dirname, join, parse, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const repoRoot = resolve(scriptDir, '..')
const [version, binaryRootArg, outputRootArg] = process.argv.slice(2)

if (!version || !binaryRootArg || !outputRootArg) {
  fail('usage: build-npm-packages.mjs <version> <binary-root> <output-root>')
}
if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/.test(version)) {
  fail(`invalid npm version: ${version}`)
}

const binaryRoot = resolve(binaryRootArg)
const outputRoot = resolve(outputRootArg)
for (const forbidden of [parse(outputRoot).root, repoRoot, process.cwd()]) {
  if (outputRoot === forbidden) fail(`refusing unsafe output path: ${outputRoot}`)
}

const mainSource = join(repoRoot, 'npm', 'tf')
const platformSource = join(repoRoot, 'npm', 'platform')
const licenseSource = join(repoRoot, 'LICENSE')
const platforms = JSON.parse(readFileSync(join(mainSource, 'platforms.json'), 'utf8'))
const expectedPlatforms = ['darwin-arm64', 'darwin-x64', 'linux-arm64', 'linux-x64', 'win32-x64']
if (JSON.stringify(Object.keys(platforms).sort()) !== JSON.stringify(expectedPlatforms)) {
  fail(`platform list must be exactly: ${expectedPlatforms.join(', ')}`)
}
const packagesRoot = join(outputRoot, 'packages')
const tarballsRoot = join(outputRoot, 'tarballs')
const generated = []

rmSync(outputRoot, { force: true, recursive: true })
mkdirSync(packagesRoot, { recursive: true })
mkdirSync(tarballsRoot, { recursive: true })

for (const [platformKey, target] of Object.entries(platforms)) {
  validateTarget(platformKey, target)
  const [os, cpu] = platformKey.split('-')
  const packageDir = join(packagesRoot, target.package.slice('@tokenflux/'.length))
  const binarySource = join(
    binaryRoot,
    `npm-${target.goos}-${target.goarch}`,
    target.binary.endsWith('.exe') ? 'tf.exe' : 'tf',
  )
  const binaryTarget = join(packageDir, target.binary)

  if (!statSync(binarySource).isFile()) fail(`missing binary: ${binarySource}`)
  mkdirSync(dirname(binaryTarget), { recursive: true })
  copyFileSync(binarySource, binaryTarget)
  chmodSync(binaryTarget, 0o755)
  copyFileSync(licenseSource, join(packageDir, 'LICENSE'))
  copyFileSync(join(platformSource, 'README.md'), join(packageDir, 'README.md'))

  writeJSON(join(packageDir, 'package.json'), {
    name: target.package,
    version,
    description: `TF CLI binary for ${os}/${cpu}`,
    license: 'Apache-2.0',
    repository: {
      type: 'git',
      url: 'git+https://github.com/TokenFlux/tf-cli.git',
      directory: 'npm/platform',
    },
    homepage: 'https://github.com/TokenFlux/tf-cli#readme',
    bugs: { url: 'https://github.com/TokenFlux/tf-cli/issues' },
    os: [os],
    cpu: [cpu],
    files: ['bin', 'README.md', 'LICENSE'],
    publishConfig: {
      access: 'public',
      registry: 'https://registry.npmjs.org/',
    },
  })
  generated.push({ name: target.package, platform: platformKey, directory: packageDir })
}

const mainDir = join(packagesRoot, 'tf')
cpSync(mainSource, mainDir, { recursive: true })
copyFileSync(licenseSource, join(mainDir, 'LICENSE'))
chmodSync(join(mainDir, 'bin', 'tf.js'), 0o755)

const mainManifestPath = join(mainDir, 'package.json')
const mainManifest = JSON.parse(readFileSync(mainManifestPath, 'utf8'))
mainManifest.version = version
delete mainManifest.private
mainManifest.optionalDependencies = Object.fromEntries(
  Object.values(platforms)
    .map((target) => target.package)
    .sort()
    .map((name) => [name, version]),
)
writeJSON(mainManifestPath, mainManifest)
generated.push({ name: mainManifest.name, platform: null, directory: mainDir })

const packed = generated.map((entry) => pack(entry))
const checksums = packed
  .map((entry) => `${sha256(entry.tarballPath)}  ${entry.tarball}`)
  .join('\n')
writeFileSync(join(tarballsRoot, 'SHA256SUMS'), `${checksums}\n`)
writeJSON(join(outputRoot, 'packages.json'), {
  version,
  packages: packed.map(({ tarballPath: _, ...entry }) => entry),
})

for (const entry of packed) {
  console.log(`${entry.name}@${version} -> ${entry.tarball}`)
}

function validateTarget(key, target) {
  if (target.package !== `@tokenflux/tf-${key}`) fail(`invalid platform package: ${target.package}`)
  const expectedBinary = key.startsWith('win32-') ? 'bin/tf.exe' : 'bin/tf'
  if (target.binary !== expectedBinary) fail(`invalid binary path: ${target.binary}`)
  if (!/^(darwin|linux|windows)$/.test(target.goos)) fail(`invalid GOOS: ${target.goos}`)
  if (!/^(arm64|amd64)$/.test(target.goarch)) fail(`invalid GOARCH: ${target.goarch}`)
}

function pack(entry) {
  const npm = process.platform === 'win32' ? 'npm.cmd' : 'npm'
  const result = spawnSync(npm, ['pack', '--json', '--pack-destination', tarballsRoot], {
    cwd: entry.directory,
    encoding: 'utf8',
  })
  if (result.status !== 0) {
    process.stderr.write(result.stderr || result.stdout)
    fail(`npm pack failed for ${entry.name}`)
  }

  let details
  try {
    ;[details] = JSON.parse(result.stdout)
  } catch {
    fail(`npm pack returned invalid JSON for ${entry.name}`)
  }
  if (details.name !== entry.name || details.version !== version || !details.filename) {
    fail(`npm pack metadata mismatch for ${entry.name}`)
  }

  const files = details.files.map((file) => file.path).sort()
  const expected = entry.platform
    ? ['LICENSE', 'README.md', entry.platform.startsWith('win32') ? 'bin/tf.exe' : 'bin/tf', 'package.json']
    : ['LICENSE', 'README.md', 'bin/tf.js', 'lib/launcher.js', 'package.json', 'platforms.json']
  if (JSON.stringify(files) !== JSON.stringify(expected.sort())) {
    fail(`unexpected files in ${entry.name}: ${files.join(', ')}`)
  }

  return {
    name: entry.name,
    platform: entry.platform,
    tarball: details.filename,
    tarballPath: join(tarballsRoot, details.filename),
  }
}

function sha256(file) {
  return createHash('sha256').update(readFileSync(file)).digest('hex')
}

function writeJSON(file, value) {
  writeFileSync(file, `${JSON.stringify(value, null, 2)}\n`)
}

function fail(message) {
  console.error(`error: ${message}`)
  process.exit(1)
}
