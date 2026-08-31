# tf 现状

更新于 2026-08-31，对应 v0.4.0 之后的 main。实施路线见 [`PLAN.md`](PLAN.md)。

支撑文档：

- `research/ori.md` — ori 逆向调研、整体设计、分发方案
- `research/tokenflux-api-probe.md` — 网关实测记录
- `research/harness-probe.md` — 三个 harness 的注入配方与实测
- `design/` — 各项决策的来龙去脉

---

## 一、当前形态

命令：`version` `status` `config` `login` `logout` `keys` `update` `harness`
`model` `completions` + `claude` `codex` `opencode`。

全局 flag：`--help/-h` `--json` `--key/-k` `--host` `--no-input`（旧名 `--yes`）。

约 7100 行代码，2800 行测试，113 个测试，四个已发布版本，零第三方依赖。

## 二、里程碑

| 里程碑 | 状态 | 说明 |
| --- | --- | --- |
| M0 骨架 | 完成 | CLI 框架、config/credentials、权限自修复、错误码、双语、`--json` |
| M1 目录 | 放弃 | 见下 |
| M2 认证 | 完成 | `login`、`logout`、`keys`、`status`（含额度） |
| M3 启动 | 完成 | fork+wait、信号转发、退出码穿透、终端复位。三个 harness 均已实测 |
| M4 模型 | 完成 | ID 解析、族折叠、方向键选择器、按 harness 分开的模型槽 |
| M5 预检 | 完成 | 零 token 协议探测、按分组前缀的准入记录、隐藏原因说明 |
| M6 harness | 完成 | claude 2.1.251、codex 0.151.0、opencode 1.18.20 均真实对话通过 |
| M7 分发 | 大部完成 | GitHub Actions + install.sh + `tf update`，五平台产物。npm 包未做 |

**M1 目录（`tf models` / `tf groups`）已放弃。** 原计划走公开的
`/api/v1/marketplace/models` 做未登录查询，`internal/catalog` 写了又删。
理由是它回答的问题（「市面上有什么」）属于网页控制台，而 tf 是启动器 ——
用户在终端里要的是「我这把 Key 现在能跑什么」，那个由 `tf keys` 与
`tf status` 回答，数据来自 `/v1/models`，不需要匿名目录。

**M5 的 `doctor` 并入了 `tf status`。** 状态与问题是同一屏：正常时只显示状态，
有问题时追加警告。单独一个 `doctor` 会让人不知道该敲哪个。

## 三、v0 之后发生的事

按时间：

- **v0.1.0** 首发。命令名 `tkr`
- **v0.2.0** 改名 `tf`。二进制、配置目录（`~/.tf`）、环境变量（`TF_*`）、
  错误码、产物名全部跟随。仓库与模块路径保持 `tokenflux/tkr`
- **v0.3.0** 选择器按键修复、shell 补全能用了、全局 flag 可写在子命令前、
  JSON 模式保留警告、`logout --all` 确认
- **v0.4.0** 十二条界面缺陷修复，含两处破坏性变更（移除 `-y`、
  `tf model` 需 `--edit` 才进编辑器）
- **未发布**：`tf status`、启动前的注入冲突检查、pty 端到端测试

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

### settings.json 会赢过注入

把 `~/.claude/settings.json` 的 `env.ANTHROPIC_BASE_URL` 设成
`http://127.0.0.1:9`，`tf claude` 连的是那个死地址而不是网关。

这是 tf 唯一会说谎的场合：横幅写着模型名，请求发去了别处。
启动前会检查并警告，只报与本次注入真正相撞的键。

CC-Switch 这类工具正是往那里写东西的 —— 所以 README 里「与配置管理器
共存」这句话是有条件的。

### opencode 的两个必备条件

① 模型必须显式声明（`provider.<p>.models.<id> = {"name": <id>}`），
否则报 `Model not found`。② 两个 provider 的 baseURL **都要带 `/v1`** ——
与 Claude Code 相反，`@ai-sdk/anthropic` 只补 `/messages`。写成根地址会
404 且被静默吞掉：退出码 0、无输出、无报错。

### 网关的 SSE 保活 bug

`/v1/responses` 用伪造的 `response.output_text.delta` 做保活，`item_id`
为 `SSE-Keep-Alive` 且从未经 `output_item.added` 宣告。AI SDK 抛
`text part SSE-Keep-Alive not found`，opencode 约四分之一的请求整轮失败。
应改用 SSE 注释行 `: keepalive`。

tf 不在请求路径上，为此建代理会违背定位 —— 只能等后端改。

## 五、还缺什么

### A. 工程

| 缺口 | 影响 |
|---|---|
| `internal/cli` 3771 行占一半代码，逻辑与 I/O 缠在一起 | 覆盖率卡在 23.9%，每加一个功能更难分 |
| `gateway` 覆盖 3%，而它承担最微妙的判断 | 手上有大量实测应答可做固件测试，没做 |
| ~~终端四条防线只在 macOS 验证过~~ | 已在 Linux 6.8 上全部跑通，`scripts/linux-check.sh` 可重跑 |
| npm 平台包未做 | `pnpx tf` 不可用 |
| 没有 CHANGELOG，没有英文 README | |

### B. Windows

只能非交互跑。交互栈整个建在 `/dev/tty` 与 `stty` 上，Windows 要走
`CONIN$` 与 `SetConsoleMode` 重写。报错已经会说实话（「这个平台还没有
交互界面」而不是「非交互」）。

没有 Windows 机器验证之前不动手：交付没测过的交互实现比诚实的报错更糟。

### C. 要后端做的三件事

按价值排。第一条最实在，今天还在发作。

| 诉求 | 为什么 |
|---|---|
| `/v1/responses` 保活帧改用 SSE 注释行 | opencode 四次挂一次，见上 |
| `/v1/models` 返回分组信息 | 现在连 `owned_by` 都没有。客户端只能靠模型集合反推分组，而 Grok 三个档位的模型完全相同，推不出来 |
| marketplace 补 `allowed_client_protocols` 与 `claude_code_only` | 补上之后可以删掉客户端整套探测逻辑，连带解决探测成本与「网关文案一改就失灵」的脆弱 |

## 六、不会变的几条

1. **tf 是启动器，不是配置管理器。** 只做进程内注入，退出不留痕，
   不改用户的 `~/.claude/settings.json`、`~/.codex/config.toml`
2. **不做本地代理、不做 MITM、不覆盖 harness 的 User-Agent**
3. **没有隐藏的全局可变状态。** 绑定属于 harness，没有「当前 profile」
4. **不在客户端建推断子系统去补上游数据缺口**
5. **零第三方依赖**，CI 卡死这条线（`go.sum` 必须为空）
6. **flag 管这一次，`tf model` 管以后。** `-m` / `-e` / `-k` 绝不写盘
7. **非交互环境绝不静默安装、绝不静默覆盖凭据、不弹选择器**
8. 不做：自己的聊天 REPL、自己的 agent loop、常驻守护进程
