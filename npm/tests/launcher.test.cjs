'use strict'

const assert = require('node:assert/strict')
const { EventEmitter } = require('node:events')
const test = require('node:test')

const {
  binaryFor,
  main,
  supportedPlatforms,
  targetFor,
} = require('../tf/lib/launcher')

class FakeRuntime extends EventEmitter {
  constructor() {
    super()
    this.platform = 'darwin'
    this.arch = 'arm64'
    this.argv = ['node', 'tf.js', 'status', '--json']
    this.pid = 42
    this.exitCode = undefined
    this.stderr = {
      output: '',
      write: (value) => {
        this.stderr.output += value
      },
    }
    this.kills = []
  }

  kill(pid, signal) {
    this.kills.push([pid, signal])
  }
}

class FakeChild extends EventEmitter {
  constructor() {
    super()
    this.kills = []
  }

  kill(signal) {
    this.kills.push(signal)
    return true
  }
}

test('maps every supported Node platform to an npm package', () => {
  assert.deepEqual(targetFor('darwin', 'arm64'), {
    package: '@tokenflux/tf-darwin-arm64',
    binary: 'bin/tf',
    goos: 'darwin',
    goarch: 'arm64',
  })
  assert.equal(targetFor('freebsd', 'x64'), null)
  assert.equal(
    supportedPlatforms(),
    'darwin-arm64, darwin-x64, linux-arm64, linux-x64, win32-x64',
  )
})

test('resolves the binary from the selected optional package', () => {
  const target = targetFor('win32', 'x64')
  let requested
  const resolved = binaryFor(target, (specifier) => {
    requested = specifier
    return 'C:\\tf.exe'
  })

  assert.equal(requested, '@tokenflux/tf-win32-x64/bin/tf.exe')
  assert.equal(resolved, 'C:\\tf.exe')
})

test('reports unsupported platforms without spawning', () => {
  const runtime = new FakeRuntime()
  const child = main({
    process: runtime,
    platform: 'freebsd',
    arch: 'x64',
    spawn: () => assert.fail('spawn should not run'),
  })

  assert.equal(child, null)
  assert.equal(runtime.exitCode, 1)
  assert.match(runtime.stderr.output, /does not provide a binary for freebsd\/x64/)
})

test('explains when optional dependencies were omitted', () => {
  const runtime = new FakeRuntime()
  main({
    process: runtime,
    resolve: () => {
      throw new Error('missing')
    },
    spawn: () => assert.fail('spawn should not run'),
  })

  assert.equal(runtime.exitCode, 1)
  assert.match(runtime.stderr.output, /@tokenflux\/tf-darwin-arm64 is missing/)
  assert.match(runtime.stderr.output, /--omit=optional/)
})

test('inherits stdio and propagates the child exit code', () => {
  const runtime = new FakeRuntime()
  const child = new FakeChild()
  let invocation

  main({
    process: runtime,
    resolve: () => '/bin/tf',
    spawn: (file, args, options) => {
      invocation = { file, args, options }
      return child
    },
  })
  child.emit('exit', 23, null)

  assert.deepEqual(invocation, {
    file: '/bin/tf',
    args: ['status', '--json'],
    options: { shell: false, stdio: 'inherit', windowsHide: false },
  })
  assert.equal(runtime.exitCode, 23)
  assert.equal(runtime.listenerCount('SIGINT'), 0)
})

test('forwards termination and preserves signal exit semantics', () => {
  const runtime = new FakeRuntime()
  const child = new FakeChild()

  main({
    process: runtime,
    resolve: () => '/bin/tf',
    spawn: () => child,
  })
  runtime.emit('SIGTERM')
  child.emit('exit', null, 'SIGTERM')

  assert.deepEqual(child.kills, ['SIGTERM'])
  assert.deepEqual(runtime.kills, [[42, 'SIGTERM']])
  assert.equal(runtime.listenerCount('SIGTERM'), 0)
})

test('reports spawn failures and removes signal listeners', () => {
  const runtime = new FakeRuntime()
  const child = new FakeChild()

  main({
    process: runtime,
    resolve: () => '/missing/tf',
    spawn: () => child,
  })
  child.emit('error', new Error('ENOENT'))

  assert.equal(runtime.exitCode, 1)
  assert.match(runtime.stderr.output, /Unable to start TF CLI: ENOENT/)
  assert.equal(runtime.listenerCount('SIGINT'), 0)
})
