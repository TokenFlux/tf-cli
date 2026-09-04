# @tokenflux/tf

`@tokenflux/tf` is the npm package for the `tf` CLI, providing process-level credential and model slot management for AI coding tools including Claude Code, Codex, opencode, and Pi.

Unscoped `tf` and `tf-cli` on npm belong to unrelated projects. Always use `@tokenflux/tf`.

## Install and Run

Global installation:

```sh
npm install -g @tokenflux/tf
tf login
```

One-off execution:

```sh
npx @tokenflux/tf status
# or
pnpx @tokenflux/tf status
```

## How It Works

`@tokenflux/tf` installs a lightweight Node.js launcher (requiring Node >=18) that delegates execution directly to a platform-specific Go binary via `optionalDependencies`.

- No postinstall download scripts and no runtime JavaScript dependencies.
- The launcher runs the binary directly without an intermediate shell, inherits stdio, forwards termination signals, and preserves exit status.
- Do not install with `--omit=optional` or `--no-optional`.

## Documentation

For login workflows, model slots, named keys, update handling, and platform support, see the [TokenFlux/tf-cli repository](https://github.com/TokenFlux/tf-cli#readme).
