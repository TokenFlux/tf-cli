# npm 分发与发布操作手册

本文档记录 `tf-cli` 的 npm 平台包分发架构、本地验证流程、历史首发 Bootstrap 记录、GitHub Actions OIDC Trusted Publishing 信任配置及常态发布流程。

> **当前状态**：`@tokenflux/tf` 及 5 个平台包已成功完成 `0.5.3-bootstrap.0` 预发布并完成 OIDC 绑定。当前正准备发布首个正式稳定版本 `v0.5.3`（发布 tag 尚未创建）。

---

## 一、包架构与包名清单

npm 分发包含 1 个公开主包和 5 个仅供内部引用的平台二进制包（均发布于 `@tokenflux` 组织下；未加 scope 的 `tf` 与 `tf-cli` 属于无关项目）：

| 包名 | 包含文件 | 平台约束 (`os` / `cpu`) |
| --- | --- | --- |
| `@tokenflux/tf` | `bin/tf.js`, `lib/launcher.js`, `platforms.json` | 通用（依赖 Node >=18 运行时） |
| `@tokenflux/tf-darwin-arm64` | `bin/tf` | `darwin` / `arm64` |
| `@tokenflux/tf-darwin-x64` | `bin/tf` | `darwin` / `x64` |
| `@tokenflux/tf-linux-arm64` | `bin/tf` | `linux` / `arm64` |
| `@tokenflux/tf-linux-x64` | `bin/tf` | `linux` / `x64` |
| `@tokenflux/tf-win32-x64` | `bin/tf.exe` | `win32` / `x64` |

- **主包机制**：主包声明所有平台包为精确版本匹配的 `optionalDependencies`。包管理器（npm、pnpm 等）在安装时根据当前系统的 `os` 与 `cpu` 仅下载对应的 1 个平台包。
- **无外部下载脚本**：不使用 `postinstall` 脚本从网络下载二进制，不受 `--ignore-scripts` 限制。安装时若带有 `--omit=optional` 或 `--no-optional`，JS 启动器会明确输出错误并提示重新安装。
- **轻量启动器**：JS 启动器直接定位平台二进制并通过 `spawn` 启动（不经过 shell），继承 stdio，转发终止信号（Unix 环境转发 `SIGINT`、`SIGTERM`、`SIGHUP`、`SIGQUIT`；Windows 环境转发 `SIGINT`、`SIGTERM`），并透传子进程退出码。

---

## 二、本地构建与验证

构建与校验脚本均采用 Node.js 原生 API 实现，零第三方依赖。

### 1. 运行单元与集成测试

```sh
make npm-test
```

测试覆盖平台映射、launcher 信号转发与退出码处理、package.json 打包规则等。

### 2. 本地交叉编译与打包

```sh
make npm-pack NPM_VERSION=0.5.3-test.0
```

该命令会编译 5 个平台的 Go 二进制至 `dist/npm-binaries`，随后执行 `scripts/build-npm-packages.mjs` 生成 6 个 `.tgz` 压缩包及 `SHA256SUMS`。

### 3. 本地安装与冒烟验证

```sh
make npm-check NPM_VERSION=0.5.3-test.0
```

该命令会创建临时目录，离线安装当前平台的压缩包，验证：
1. `npm` / `pnpm` 离线安装正常；
2. 软链或 shim 启动能正确输出版本信息；
3. 子进程异常退出码能被完整保留；
4. 终止信号能被正确捕获并转发给二进制。

---

## 三、历史首发 Bootstrap 记录（已完成）

npm 实行版本不可变机制（已发布的版本无法重新上传或覆盖）。由于 npm OIDC 要求包必须已存在于 registry，项目通过一次性手动 Bootstrap 完成了 6 个包的首次发布。

### 1. 组织与账号前置（已完成）

1. npm organization `tokenflux` 所有者账号完成 2FA（auth-and-writes）与邮箱验证；
2. 维护者账号本地临时通过 `npm login` 认证，未硬编码或存储长效 Token。

### 2. 首发预发布版本（已完成）

1. 本地打包 `0.5.3-bootstrap.0`（对应 commit `2b3c399`）；
2. 使用 `--tag bootstrap` 发布至 registry：
   ```sh
   node scripts/publish-npm-packages.mjs dist/npm --tag bootstrap
   ```
3. **npm registry 实际行为记录**：全新包发布首个自定义 tag 版本时，npm registry 仍会同时将其分配给 `latest`（与 npm 文档描述不符），且当 registry 仅有这一个版本时拒绝通过 `npm dist-tag rm` 移除 `latest`。此为临时现象，后续正式稳定版本 `0.5.3` 发布时会自动接管覆盖 `latest`。
4. **Registry 实测验证（已通过）**：
   - 清理缓存测试 `pnpx @tokenflux/tf@bootstrap --json version` 与 `npx @tokenflux/tf@bootstrap --json version`，均正确返回版本 `0.5.3-bootstrap.0` 与 commit `2b3c399`；
   - 隔离全局安装测试仅下载并安装了当前系统匹配的 `darwin-arm64` 平台包。

---

## 四、配置 GitHub Actions OIDC 信任（已完成）

在 6 个包完成首发后，维护者已完成 GitHub Actions Trusted Publishing 绑定：

```sh
npm trust github @tokenflux/tf --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-darwin-arm64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-darwin-x64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-linux-arm64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-linux-x64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-win32-x64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
```

`npm trust list` 已确认 6 个包均具备 `createPackage` 与 `createStagedPackage` 权限。工作流 `release.yml` 自身仅需运行标准的 `npm publish`。

---

## 五、常态化稳定版本发布流程

首发及信任绑定完成后，后续稳定版本（如 `v0.5.3`）走自动化 CI 流程。

### 1. 发布触发与执行阶段

推送版本 tag（如 `v0.5.3`）后，`.github/workflows/release.yml` 自动执行：

1. **check**：代码格式、测试、发版前校验；
2. **build**：交叉编译 5 组二进制产物；
3. **npm**：
   - 依赖 Node 24 与 GitHub OIDC 权限（`id-token: write`）；
   - 调用 `scripts/build-npm-packages.mjs` 构建 6 个 tarball；
   - 调用 `scripts/check-npm-packages.mjs` 进行离线安装校验；
   - 调用 `scripts/publish-npm-packages.mjs` **优先发布 5 个平台包，最后发布主包 `@tokenflux/tf`**，并附加 `--provenance`；
4. **release**：创建 GitHub Release 并上传归档与 `SHA256SUMS`。

### 2. 传播重试与防冲突机制

- **元数据传播重试**：当 npm 远端 package 文档尚未完成传播时，发布脚本通过 `dist-tags` 确认已发布版本，跳过不可变的已存在版本，并校验目标 tag 正确性；
- **重试幂等**：若流水线因网络中断，重新运行 GitHub Actions Job 时会自动跳过已发布的包，继续发布未完成的包。

### 3. 稳定发布后验证与凭据清理

在首个稳定版本（`v0.5.3`）发布并验证完成后，维护者在本机执行凭据清理：

```sh
# 验证稳定版
npx @tokenflux/tf@latest --json version

# 验证通过后退出本地 npm 登录
npm logout
```
