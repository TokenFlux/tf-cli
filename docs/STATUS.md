# tf-cli 现状

对应 main 分支（v0.7.0）。实施路线见 [`PLAN.md`](PLAN.md)。

支撑文档：

- `research/ori.md` — ori 逆向调研、整体设计、分发方案
- `research/tokenflux-api-probe.md` — 网关实测记录
- `research/harness-probe.md` — 各 harness 的注入配方与实测
- `design/` — 当前设计决策及其依据
- `archive/` — 已放弃或被替代的早期方案，仅供追溯
- `integrations/web-import.md` — 网页前端接入 `tf login --from-web` 的协议契约
- `distribution/npm.md` — npm 平台包分发架构、验证与发布操作手册

---

## 一、当前形态

命令：`version` `status` `config` `login` `logout` `keys` `update` `harness`
`model` `completions` + `claude` `codex` `opencode` `pi`。`tf login` 默认高亮网页导入，确认后打开 Keys 页面，也可选择粘贴；`--from-web` 可直接进入网页导入。

全局 flag：`--help/-h` `--json` `--key/-k` `--host` `--no-input`（旧名 `--yes`）。对 `claude`/`codex`/`opencode`/`pi` 透传命令，写在 harness 名后的 `-h`/`--help` 交给底层工具，`tf --help <harness>` 用于查看 tf 包装帮助。`login` 另有 `--with-key`、`--from-web`、`--force`。

约 8,400 行生产代码（7,846 Go + 586 npm）；约 4,800 行测试（4,519 Go + 305 Node）；162 个 Go 测试函数；8 个 Node 用例，十个已发布版本，零第三方 Go 依赖及运行时 JS 依赖。

## 二、里程碑

| 里程碑 | 状态 | 说明 |
| --- | --- | --- |
| M0 骨架 | 完成 | CLI 框架、config/credentials、权限自修复、错误码、双语、`--json` |
| M1 目录 | 放弃 | 见下 |
| M2 认证 | 完成 | `login`、`login --from-web`、`logout`、`keys`、`status`（含额度） |
| M3 启动 | 完成 | fork+wait、信号转发、退出码穿透、终端复位。各 harness 均已实测 |
| M4 模型 | 完成 | 模型 ID 与强度后缀解析、补全顺序、方向键选择器、按 harness 分开的模型槽 |
| M5 预检 | 完成 | 零 token 协议探测、按分组前缀的准入记录、隐藏原因说明 |
| M6 harness | 完成 | claude 2.1.251、codex 0.151.0、opencode 1.18.20 均真实对话通过；pi 0.84.4 与 0.85.0 均已验证（真实 TokenFlux Responses 对话通过，且本地假网关跑通三协议流式请求） |
| M7 分发 | 完成 | GitHub Actions + install.sh + `tf update`（5 组平台二进制）；npm 平台包已正式发布（1 个主包 + 5 个平台二进制包，详见 [`distribution/npm.md`](distribution/npm.md)） |

**M1 目录（`tf models` / `tf groups`）已放弃。** 原计划走公开的
`/api/v1/marketplace/models` 做未登录查询，`internal/catalog` 模块已移除。
查询公开全量目录的职责属于 Web 控制台，而 `tf` 作为启动器聚焦于展示当前凭据可用的模型与协议，
该数据直接来源于 `/v1/models`。

**M5 的 `doctor` 并入了 `tf status`。** 状态查询与环境诊断整合展示：正常运行时输出核心状态，
检测到冲突时输出警告提示。

## 三、v0 之后发生的事

按时间：

- **v0.1.0** 首发。命令名 `tkr`
- **v0.2.0** 命令改名为 `tf`。二进制、配置目录（`~/.tf`）、环境变量（`TF_*`）、
  错误码、产物名全部跟随；当时仓库与模块路径仍为 `tokenflux/tkr`
- **v0.3.0** 选择器按键修复、shell 补全能用了、全局 flag 可写在子命令前、
  JSON 模式保留警告、`logout --all` 确认
- **v0.4.0** 十二条界面缺陷修复，含两处破坏性变更（移除 `-y`、
  `tf model` 需 `--edit` 才进编辑器）
- **v0.5.0** 新增 `tf status`、额度显示和启动前冲突检查；补上 PTY、网关固件与 Linux 实机测试
- **v0.5.1** 重写 README，统一中文 CLI 文案，规范发布说明
- **v0.5.2** 落地网页 Key 导入与可选会话证明、归档早期设计稿，并新增前端接入文档；
  GitHub 仓库改名为 `TokenFlux/tf-cli`，Go module 改为 `github.com/tokenflux/tf-cli`，命令名仍为 `tf`
- **v0.5.3** 支持 npm 平台包分发（`@tokenflux/tf` 及 5 个平台二进制包）并完成正式发布。详见 [`distribution/npm.md`](distribution/npm.md)
- **v0.6.0** 修正 harness 透传 `-h`/`--help` 语义、重构模型目录先于协议探测的自愈刷新顺序、修复预发布版本 SemVer 比较与 npm 升级拦截、增加 npm dist-tags 校验有界退避重试，并完成全项目结构与冗余字段消融
- **v0.7.0** 新增 Pi coding agent harness（覆盖三协议、纯进程环境与动态临时 Extension 注入、不修改 `~/.pi` 持久配置）与保守独立卸载脚本 `uninstall.sh`（支持 `--purge`）

仓库改名后，旧 GitHub URL 会自动重定向，现有二进制的自更新和 `install.sh` 均已实测可用。
`go install github.com/tokenflux/tf-cli/cmd/tf@latest` 原生支持；v0.6.0 发布后复核 Go proxy 的 @latest 传播。

## 四、实测得到的事实

这些是靠发请求、跑进程得来的，不是推的。

### 网关应答形状

| 场景 | 形状 |
|---|---|
| 模型不在分组（chat/messages） | 403 `The current group does not support the requested model` |
| 模型不在分组（responses） | 404 `model_not_found` |
| 模型不在分组（responses，Kiro 分组） | 503 `No available accounts: ... requested model` |
| `claude_code_only`（messages） | 403 `this group only allows Claude Code clients` |
| `claude_code_only`（chat/completions） | 403 `This group is restricted to Claude Code clients` |
| Key 无效 | 401 `INVALID_API_KEY` |
| 额度用尽 | 429，`/v1/usage` 里 `status: quota_exhausted` |

判据放文案不放状态码：同一件事出现过 403 / 404 / 503 三种码。

### `claude_code_only` 拦的是客户端指纹

实测：真实模型 + 真实请求体，走 `/v1/messages` 仍然 403。它认的是
UA 加 TLS 指纹，不是协议。tf 不伪装，所以这类分组在 tf 下确实用不了 ——
但**能藏就必须能解释**，隐藏候选时会说明原因。

这条曾是全项目最高风险项（Claude Max 倍率 20）。结论是这条路走不通，
风险已经兑现，不再是未知。

### settings.json 覆盖注入优先级

若 `~/.claude/settings.json` 中配置了 `env.ANTHROPIC_BASE_URL`，其优先级高于外部环境变量。
启动前会主动检测并输出警告，明确指出冲突的配置项。

CC-Switch 等外部工具常修改该文件，启动时需注意该层覆盖关系。

### opencode 的两个必备条件

1. 模型必须显式声明（`provider.<p>.models.<id> = {"name": <id>}`），否则报 `Model not found`。
2. 两个 provider 的 baseURL 均需包含 `/v1`（`@ai-sdk/anthropic` 默认追加 `/messages` 而非 `/v1`）。若写成根地址将返回 404 且被静默忽略。

### 网关的 SSE 保活机制

`/v1/responses` 使用未声明的 `SSE-Keep-Alive` 作为 `item_id` 发送文本增量包进行保活。AI SDK 会抛出 `text part SSE-Keep-Alive not found` 错误，导致部分请求异常中断。后端宜调整为标准 SSE 注释行（`: keepalive`）实现保活。

### npm 发布与分发实测

1. **发布与来源证明**：主包及 5 个平台二进制包均由 GitHub Actions 经 OIDC 发布并附带 SLSA provenance（六个 npm 包均为 `latest=0.6.0` 且保留首发 `bootstrap=0.5.3-bootstrap.0`；GitHub Release 与工作流运行见 [Release v0.6.0](https://github.com/TokenFlux/tf-cli/releases/tag/v0.6.0) 与 [Workflow run 33860769841](https://github.com/TokenFlux/tf-cli/actions/runs/33860769841)）。隔离 npm 安装的两个实际包通过 registry signature 与 attestation audit 校验。
2. **发布重试与幂等性**：首次 workflow 的 `linux-x64` publish 返回成功后 package document / tag 传播超过原有约 60 秒窗口，导致 npm job 失败；额外约 65 秒后可见，failed-job 幂等重跑跳过已存在版本并成功创建 Release。
3. **安装与产物验证**：干净环境下 `npx @tokenflux/tf@latest` 与 `pnpx @tokenflux/tf@latest` 均运行出 version 0.6.0、commit 47d06b6；官方 install.sh 安装出 0.6.0/47d06b6；Go proxy/module `@latest=v0.6.0` 传播解析正常（注：`go install ...@latest` 生成的二进制因无 release ldflags 显示为 `dev/unknown`，列为分发限制待修项）；官方五个资产 SHA256SUMS 和内嵌版本核对一致。主包安装路径下继续受自更新保护并提示 npm 升级命令。完整流程与机制见 [`distribution/npm.md`](distribution/npm.md)。

## 五、还缺什么

### A. 工程

| 缺口 | 影响 |
|---|---|
| `go install ...@latest` 可安装，但二进制显示 `dev/unknown` | 版本展示与更新判断缺少 release 元数据，后续应从 Go build info 回退读取模块版本 |
| `internal/cli` 仍有约 4000 行，命令 I/O 尚未完全拆分 | `access`、`completions` 与网页导入已接入，剩余部分覆盖率仍需继续提升 |
| ~~`gateway` 覆盖 3%，而它承担最微妙的判断~~ | 已用真实应答建立固件测试，覆盖率提升至 38.3% |
| ~~终端四条防线只在 macOS 验证过~~ | 已在 Linux 6.8 上全部跑通，`scripts/linux-check.sh` 可重跑 |
| ~~npm 平台包分发~~ | 1 个主包 + 5 个平台二进制包已发布并通过完整验证，详见 [`distribution/npm.md`](distribution/npm.md) |
| 英文 README 未做 | `README.en.md` 待补 |

### B. Windows

只能非交互跑。交互栈整个建在 `/dev/tty` 与 `stty` 上，Windows 要走
`CONIN$` 与 `SetConsoleMode` 重写。报错已经会说实话（「这个平台还没有
交互界面」而不是「非交互」）。

没有 Windows 机器验证之前不动手：交付没测过的交互实现比诚实的报错更糟。

### C. 后端协同改进点

按优先级排列：

| 诉求 | 说明 |
|---|---|
| `/v1/responses` 保活帧改用标准 SSE 注释行 | 避免客户端 AI SDK 抛出解析异常 |
| `/v1/models` 返回所属分组信息 | 便于客户端直接识别模型分组与归属 |
| marketplace 提供 `allowed_client_protocols` 与 `claude_code_only` 字段 | 完善元数据，减少客户端前置探测的成本 |

## 六、不会变的几条

1. **tf 是启动器，不是 harness 配置改写器。** 对 harness 的注入只存在于子进程，退出后不改写
   `~/.claude/settings.json`、`~/.codex/config.toml`、`~/.pi`；tf 自己的 Key、绑定和模型槽仍会按用户操作持久化。
2. **不做本地代理、不做 MITM、不覆盖 harness 的 User-Agent**
3. **没有隐藏的全局可变状态。** 绑定属于 harness，没有「当前 profile」
4. **不在客户端建推断子系统去补上游数据缺口**
5. **零第三方依赖**，CI 卡死这条线（`go.sum` 必须为空）
6. **flag 管这一次，`tf model` 管以后。** `-m` / `-e` / `-k` 绝不写盘
7. **非交互环境绝不静默安装、绝不静默覆盖凭据、不弹选择器**
8. 不做：自己的聊天 REPL、自己的 agent loop、常驻守护进程
