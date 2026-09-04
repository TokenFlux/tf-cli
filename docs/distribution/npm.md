# npm 分发与发布操作手册

本文档记录 `tf-cli` 的 npm 平台包分发架构、本地验证流程、历史首发 Bootstrap 记录、GitHub Actions OIDC Trusted Publishing 信任配置及常态发布流程。

> **当前状态**：v0.6.0 Release 与 npm 分发已完成发布验证。六个 npm 包均为 `latest=0.6.0` 且保留历史首发 `bootstrap=0.5.3-bootstrap.0`。全部 6 个包均附带 SLSA provenance 元数据。工作流记录见 [Workflow run 33860769841](https://github.com/TokenFlux/tf-cli/actions/runs/33860769841)，发布见 [Release v0.6.0](https://github.com/TokenFlux/tf-cli/releases/tag/v0.6.0)。

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

另行实测：当前 main 分支以 `0.5.3-bootstrap.0` 构建并在 npm `node_modules` 路径中运行，能正确识别稳定版 `0.5.3` 为更新版本，自更新拦截返回 `TF_USAGE` 并提示 `npm install -g @tokenflux/tf@latest` 且不执行自替换。该 SemVer 修复在 `v0.5.3` tag 之后合入 main 分支，纳入 `v0.6.0`。

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

首发及信任绑定完成后，后续稳定版本走自动化 CI 流程。

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

### 2. 失败重试与幂等恢复机制

发布流程具备幂等与传播容忍设计，job 失败时可直接在 GitHub Actions 重新运行：

1. **不可变版本跳过**：已存在的版本无法覆盖，重跑时脚本会自动跳过已发布的 tarball。
2. **E404 传播降级检测**：若新版本的 package document 尚未完成传播而返回 E404，脚本会回退读取 dist-tags 列表确认版本是否存在，避免重试时误判未发布而重复上传。
3. **目标 Tag 读取退避重试**：发布完成后校验目标 dist-tag 时，脚本使用 `0/1/2/4/8/15/30` 秒有界退避重试等待 registry 传播一致。该机制于 `v0.5.3` tag 发布后合入 `main` 分支，纳入 `v0.6.0`。

### 3. v0.5.3 发布实测与历史记录

1. **工作流首发与重跑**：`v0.5.3` 首发执行时，因 `@tokenflux/tf-linux-x64` 发布后立即读取到旧 tag（`latest` 仍指向预发布版本）导致首次 job 失败；触发 failed-job rerun 后，发布脚本自动跳过已存在的 6 个不可变版本，顺利通过 tag 校验并创建 GitHub Release。
2. **实测校验结果**：
   - 清理缓存测试 `npx @tokenflux/tf@latest --json version` 与 `pnpx @tokenflux/tf@latest --json version`，均正确返回版本 `0.5.3` 与 commit `59ef0c8`；
   - 隔离全局安装测试中，从 `bootstrap` 升级至 `latest` 成功，且仅下载匹配当前系统的 `darwin-arm64` 平台包；
   - `npm audit signatures` 验证通过（2 个已安装包 registry 签名及 2 个 attestation 校验通过）；
   - 全部 6 个包在 npm 上均具备 SLSA provenance v1 元数据；
   - GitHub Release 中的 5 个归档产物及 `SHA256SUMS` 校验通过。
3. **凭据与生态记录**：
   - 维护者本地已执行登出，`npm whoami` 校验失败，`~/.npmrc` 中无 npmjs 认证 token；
   - Go proxy 传播已完成，全新查询与无缓存请求下 `@latest` 均已正确解析并返回 `v0.5.3`（tag 指向 `59ef0c8`）。

### 4. v0.6.0 发布实测与验证记录

1. **工作流执行与传播恢复**：`v0.6.0` 触发 release 工作流时，首次 workflow 的 `linux-x64` publish 返回成功后，package document / tag 传播耗时超过了原有约 60 秒窗口，导致首个 npm job 报错失败。随后约 65 秒远端版本可见，触发 failed-job 重跑后发布脚本幂等跳过已存在版本并顺利通过 tag 校验，最终成功创建 GitHub Release（工作流：[Run 33860769841](https://github.com/TokenFlux/tf-cli/actions/runs/33860769841)，Release：[v0.6.0](https://github.com/TokenFlux/tf-cli/releases/tag/v0.6.0)）。
2. **实测校验结果**：
   - 全部 6 个 npm 包的 `latest` tag 均已指向 `0.6.0`，且均附带 SLSA provenance 元数据；
   - 隔离环境下的 npm 安装（包含主包与平台二进制包）通过 `npm audit signatures` 校验（registry 签名与 attestation 均通过）；
   - 清理缓存测试 `npx @tokenflux/tf@latest --json version` 与 `pnpx @tokenflux/tf@latest --json version`，均正确输出 `0.6.0` 与 commit `47d06b6`；
   - 官方 install.sh 安装出 0.6.0/47d06b6；
   - Go proxy 传播完成，Go module `@latest` 已解析到 `v0.6.0`；但 `go install github.com/tokenflux/tf-cli/cmd/tf@latest` 产物因无 release ldflags 注入，当前二进制版本输出显示为 `dev/unknown`（分发限制/待修项）；
   - GitHub Release 中的 5 个平台归档资产 `SHA256SUMS` 和内嵌二进制版本核对一致。
