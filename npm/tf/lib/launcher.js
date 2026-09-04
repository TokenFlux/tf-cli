'use strict'

const { spawn } = require('node:child_process')
const platforms = require('../platforms.json')

function targetFor(platform, arch) {
  return platforms[`${platform}-${arch}`] || null
}

function binaryFor(target, resolve = require.resolve) {
  return resolve(`${target.package}/${target.binary}`)
}

function supportedPlatforms() {
  return Object.keys(platforms).sort().join(', ')
}

function main(options = {}) {
  const runtime = options.process || process
  const spawnProcess = options.spawn || spawn
  const resolve = options.resolve || require.resolve
  const platform = options.platform || runtime.platform
  const arch = options.arch || runtime.arch
  const args = options.args || runtime.argv.slice(2)
  const target = targetFor(platform, arch)

  if (!target) {
    runtime.stderr.write(
      `TF CLI does not provide a binary for ${platform}/${arch}.\n` +
      `Supported platforms: ${supportedPlatforms()}.\n`,
    )
    runtime.exitCode = 1
    return null
  }

  let binary
  try {
    binary = binaryFor(target, resolve)
  } catch {
    runtime.stderr.write(
      `The optional package ${target.package} is missing.\n` +
      'Reinstall @tokenflux/tf without --omit=optional or --no-optional.\n',
    )
    runtime.exitCode = 1
    return null
  }

  const child = spawnProcess(binary, args, {
    shell: false,
    stdio: 'inherit',
    windowsHide: false,
  })
  const relaySignals = platform === 'win32'
    ? ['SIGINT', 'SIGTERM']
    : ['SIGINT', 'SIGTERM', 'SIGHUP', 'SIGQUIT']
  const relays = new Map()
  let settled = false

  for (const signal of relaySignals) {
    const relay = () => {
      try {
        child.kill(signal)
      } catch {
        // The child may already have exited between delivery and forwarding.
      }
    }
    relays.set(signal, relay)
    runtime.once(signal, relay)
  }

  const removeRelays = () => {
    for (const [signal, relay] of relays) runtime.removeListener(signal, relay)
  }

  child.once('error', (error) => {
    if (settled) return
    settled = true
    removeRelays()
    runtime.stderr.write(`Unable to start TF CLI: ${error.message}\n`)
    runtime.exitCode = 1
  })

  child.once('exit', (code, signal) => {
    if (settled) return
    settled = true
    removeRelays()

    if (signal) {
      try {
        runtime.kill(runtime.pid, signal)
      } catch {
        runtime.exitCode = 1
      }
      return
    }
    runtime.exitCode = code == null ? 1 : code
  })

  return child
}

module.exports = {
  binaryFor,
  main,
  supportedPlatforms,
  targetFor,
}
