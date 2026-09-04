# npm 分发与发布操作手册

本文档记录 `tf-cli` 的 npm 平台包分发架构、本地验证流程、首发 Bootstrap 与 GitHub Actions OIDC Trusted Publishing 信任配置。

> **当前状态**：npm 包分发代码与本地测试已全部就绪，但 npm 首发 Bootstrap、OIDC 信任绑定与 v0.5.3 正式发布尚未执行。首个正式 npm 版本为 `v0.5.3`。

---

## 一、包架构与包名清单

npm 分发包含 1 个公开主包和 5 个仅供内部引用的平台二进制包（均发布于 `@tokenflux` 组织下）：

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

## 三、前置准备与首发 Bootstrap

npm 实行版本不可变机制（已发布的版本无法重新上传或覆盖）。发布流程分为一次性的 Bootstrap 流程与后续的自动化 CI 发布。

### 前置条件

1. npm organization `tokenflux` 已存在，所有者账号开启了 2FA（auth-and-writes）；
2. 发布者账号已通过 npm 邮箱验证（外部前置条件）；
3. 本机已通过 `npm login` 登录拥有该组织发布权限的维护者账号。**严禁在终端、配置文件或聊天记录中硬编码或复制长效 npm Token**。

### Bootstrap 步骤（预发布 0.5.3-bootstrap.0）

由于 npm OIDC 必须对已存在的包配置信任关系，首次需要手动上传 6 个包的完整可运行预发布版本：

1. **打包 Bootstrap 版本**：
   ```sh
   make npm-pack NPM_VERSION=0.5.3-bootstrap.0
   ```

2. **发布至非默认 dist-tag**：
   ```sh
   # 必须使用非默认 tag bootstrap，防止用户意外安装该预发布版本；后续正式发版 v0.5.3 将默认写入 latest
   node scripts/publish-npm-packages.mjs dist/npm --tag bootstrap
   ```

3. **核实所有 6 个包均已成功发布**：
   ```sh
   npm view @tokenflux/tf versions --json
   npm view @tokenflux/tf-darwin-arm64 versions --json
   npm view @tokenflux/tf-darwin-x64 versions --json
   npm view @tokenflux/tf-linux-arm64 versions --json
   npm view @tokenflux/tf-linux-x64 versions --json
   npm view @tokenflux/tf-win32-x64 versions --json
   ```

---

## 四、配置 GitHub Actions OIDC 信任

在所有 6 个包完成首发后，维护者在本地对每个包配置 GitHub Actions Trusted Publishing：

```sh
npm trust github @tokenflux/tf --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-darwin-arm64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-darwin-x64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-linux-arm64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-linux-x64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
npm trust github @tokenflux/tf-win32-x64 --repo TokenFlux/tf-cli --file release.yml --allow-publish
```

配置完成后，GitHub Actions 中的 `.github/workflows/release.yml` 即可在发版流程中通过短期 OIDC Token (`id-token: write`) 自动发布并生成 `--provenance` 签名，无需在 GitHub 仓库中配置任何静态 npm Token 或 Secret。

> **安全清理**：完成信任配置后，在本地终端运行 `npm logout` 退出临时登录会话，确保本机不留存活跃的发布凭据，也不使用任何长效发布 Token。

---

## 五、正式版本发布与重试机制

### 发布流程

推送版本 tag（如 `v0.5.3`）后，`.github/workflows/release.yml` 自动执行以下阶段：

1. **check**：代码格式、测试、发版前校验；
2. **build**：交叉编译 5 组二进制产物；
3. **npm**：
   - 依赖 Node 24 与 OIDC 权限；
   - 调用 `scripts/build-npm-packages.mjs` 构建 6 个 tarball；
   - 调用 `scripts/check-npm-packages.mjs` 进行离线安装校验；
   - 调用 `scripts/publish-npm-packages.mjs` **优先发布 5 个平台包，最后发布主包 `@tokenflux/tf`**，并附加 `--provenance`；
4. **release**：创建 GitHub Release 并上传归档与 `SHA256SUMS`。

### 部分发布失败后的重试与恢复

若发布流水线在部分平台包发布后中断（例如网络超时）：
- `scripts/publish-npm-packages.mjs` 内置防重复发布逻辑：发布前会检查 npm 远端是否存在对应 `package@version`；若已存在则直接输出跳过信息；
- 维护者只需在 GitHub Actions 界面重新运行失败的 Job，发布脚本会自动跳过已发布的包，继续发布未完成的包。
