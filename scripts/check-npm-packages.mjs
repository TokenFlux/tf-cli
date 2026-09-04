#!/usr/bin/env node

import { spawn, spawnSync } from 'node:child_process'
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join, resolve } from 'node:path'

const [outputArg, ...flags] = process.argv.slice(2)
const requirePnpm = flags.includes('--require-pnpm')
if (!outputArg) fail('usage: check-npm-packages.mjs <output-root> [--require-pnpm]')

const outputRoot = resolve(outputArg)
const index = JSON.parse(readFileSync(join(outputRoot, 'packages.json'), 'utf8'))
const tarballsRoot = join(outputRoot, 'tarballs')
const platformKey = `${process.platform}-${process.arch}`
const main = index.packages.find((entry) => entry.name === '@tokenflux/tf')
const platform = index.packages.find((entry) => entry.platform === platformKey)
if (!main) fail('main package missing from package index')
if (!platform) fail(`no package available for test host ${platformKey}`)

const root = mkdtempSync(join(tmpdir(), 'tf-npm-install-'))
try {
  await checkInstall('npm', root, index, main, platform)
  if (commandExists('pnpm')) await checkInstall('pnpm', root, index, main, platform)
  else if (requirePnpm) fail('pnpm is required but was not found')
  else console.log('pnpm not found; skipped pnpm install check')
} finally {
  rmSync(root, { force: true, recursive: true })
}

async function checkInstall(manager, rootDir, packageIndex, mainEntry, platformEntry) {
  const project = join(rootDir, manager)
  mkdirSync(project, { recursive: true })

  const manifest = {
    name: `tf-npm-check-${manager}`,
    version: '0.0.0',
    private: true,
    dependencies: {
      [mainEntry.name]: `file:${join(tarballsRoot, mainEntry.tarball)}`,
    },
    optionalDependencies: Object.fromEntries(
      packageIndex.packages
        .filter((entry) => entry.platform)
        .map((entry) => [entry.name, `file:${join(tarballsRoot, entry.tarball)}`]),
    ),
  }
  writeFileSync(join(project, 'package.json'), `${JSON.stringify(manifest, null, 2)}\n`)

  const installArgs = manager === 'npm'
    ? ['install', '--ignore-scripts', '--offline', '--no-audit', '--no-fund']
    : ['install', '--ignore-scripts', '--offline', '--frozen-lockfile=false']
  run(manager, installArgs, { cwd: project }, `${manager} install`)

  const binaryName = process.platform === 'win32' ? 'tf.cmd' : 'tf'
  const shim = join(project, 'node_modules', '.bin', binaryName)
  const result = run(shim, ['--json', 'version'], { cwd: project }, `${manager} shim`)
  const payload = JSON.parse(result.stdout)
  if (payload?.data?.version !== packageIndex.version) {
    fail(`${manager} shim reported ${payload?.data?.version}; expected ${packageIndex.version}`)
  }

  const directBinary = join(
    project,
    'node_modules',
    '@tokenflux',
    platformEntry.name.slice('@tokenflux/'.length),
    process.platform === 'win32' ? 'bin/tf.exe' : 'bin/tf',
  )
  const direct = run(directBinary, ['--json', 'version'], { cwd: project }, `${manager} platform binary`)
  if (direct.stdout !== result.stdout) fail(`${manager} shim output differs from its platform binary`)

  const badArgs = ['command-that-does-not-exist']
  const shimFailure = spawnSync(shim, badArgs, { cwd: project, encoding: 'utf8' })
  const directFailure = spawnSync(directBinary, badArgs, { cwd: project, encoding: 'utf8' })
  if (shimFailure.status !== directFailure.status || shimFailure.status === 0) {
    fail(`${manager} shim did not preserve a failing exit code`)
  }

  if (process.platform !== 'win32') {
    writeFileSync(directBinary, '#!/usr/bin/env node\nprocess.stdout.write("ready\\n")\nsetInterval(() => {}, 1000)\n')
    chmodSync(directBinary, 0o755)
    await checkSignalRelay(shim, project, manager)
  }

  console.log(`${manager}: ${mainEntry.name}@${packageIndex.version} -> ${platformEntry.name} ok`)
}

async function checkSignalRelay(shim, cwd, manager) {
  const wrapper = spawn(shim, [], { cwd, stdio: ['ignore', 'pipe', 'pipe'] })
  await new Promise((resolveReady, reject) => {
    const timeout = setTimeout(() => reject(new Error(`${manager} shim child did not start`)), 5000)
    wrapper.stdout.once('data', (chunk) => {
      clearTimeout(timeout)
      if (!chunk.toString().includes('ready')) reject(new Error(`${manager} shim returned unexpected output`))
      else resolveReady()
    })
  })

  wrapper.kill('SIGTERM')
  const result = await new Promise((resolveExit, reject) => {
    const timeout = setTimeout(() => {
      wrapper.kill('SIGKILL')
      reject(new Error(`${manager} shim did not relay SIGTERM`))
    }, 5000)
    wrapper.once('exit', (code, signal) => {
      clearTimeout(timeout)
      resolveExit({ code, signal })
    })
  })
  if (result.signal !== 'SIGTERM') {
    fail(`${manager} shim exit was ${result.code}/${result.signal}; expected SIGTERM`)
  }
}

function commandExists(command) {
  const result = spawnSync(command, ['--version'], { encoding: 'utf8' })
  return result.status === 0
}

function run(command, args, options, action) {
  const result = spawnSync(command, args, {
    encoding: 'utf8',
    ...options,
  })
  if (result.status !== 0) {
    process.stderr.write(result.stderr || result.stdout)
    fail(`${action} failed`)
  }
  return result
}

function fail(message) {
  console.error(`error: ${message}`)
  process.exit(1)
}
