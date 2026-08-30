# tf 现状

更新于 2026-08-29。实施路线见 [`PLAN.md`](PLAN.md)。

支撑文档：

- `research/ori.md` — ori 逆向调研、整体设计、分发方案
- `research/tokenflux-api-probe.md` — 生产环境实测记录
- `design/import-from-web.md` — login 最终方案
- `design/cli-auth-discussion.md` — 与后端沟通的事实与议题

---

## 一、进度

| 里程碑 | 状态 | 说明 |
| --- | --- | --- |
| M0 骨架 | 完成 | CLI 框架、config/credentials、权限自修复、错误码、双语文案、`--json` |
| M1 目录 | 未开始 | `catalog` + `tf models` / `tf groups` |
| M2 认证 | 大部完成 | `login`（stdin/隐藏输入、当场校验、冲突不静默覆盖、自订名）、`logout`、`keys`。缺 `tf status`（余额、限速窗口） |
| M3 启动 | 机制完成 | fork+wait、信号转发、退出码穿透、注入配方。三项实测未做 |
| M4 模型 | 完成 | ID 解析（分组前缀 + 强度后缀）、族折叠、方向键 TUI、按 harness 分开的模型槽、一屏确认 |
| M5 预检 | 部分完成 | 协议探测（零 token）、按分组前缀的准入记录、Key 与模型两级筛选。缺 `doctor` |
| M6 harness | 1/3 实测 | opencode 实测通过；claude/codex 配方已写但未验证 |
| M7 分发 | 未开始 | goreleaser、npm 平台包、install.sh、Apache-2.0、SECURITY.md |

M1 走公开的 `/api/v1/marketplace/models`。倍率、定价、可用率、模态都在这份数据里，
它同时是「同模型多分组该选哪个」和「过滤掉图像/专用模型」的唯一数据来源。

M5 的 `doctor` 需覆盖：CC-Switch 残留、harness 自存凭据、代理风险、废弃端点。

### 阻塞项

claude 与 codex 未安装，M3 的三项实测（`claude_code_only` 指纹识别、`HTTPS_PROXY` 影响、
`ENABLE_TOOL_SEARCH`）无法进行。这是全项目最高风险项：若 tf 的 exec 路径破坏 UA+TLS 指纹，
Claude Max（倍率 20）这条主线就不成立。

### 计划外的修正（已完成）

- **废除全局 current profile**：绑定改为属于 harness，按能力自动选 Key。
  见 `design/no-global-mode.md`。原 PLAN 中 v0.5 的「完整 profile 机制、`tf use`」作废。
- **协议准入按分组前缀记录**：复合 Key 一把横跨多个分组，同一把 Key 的不同模型可调端点不同。
- **模型列表合并为一处真相**（config.json），删掉会串味的独立缓存。

---

## 二、还缺什么

### A. 待实测 / 待实现

| 缺口 | 影响 | 优先级 |
|---|---|---|
| `claude_code_only` 分组靠什么识别 Claude Code（UA？header？），tf 注入的 header 会不会破坏识别 | Claude Max 倍率 20，是最有价值的分组，识别不过就用不了 | P0 |
| 三个 harness 的端到端实测 | 适配表的正确性 | P0 |
| opencode 的 AI SDK provider 实际走 responses 还是 chat | 候选列表顺序 | P1 |
| `ENABLE_TOOL_SEARCH` 在 Anthropic 分组是否生效 | 省近一半系统提示词 token | P1 |
| 复合 Key 的 `composite_groups` 实际结构与前缀形态 | 前缀自动补全 | P1 |
| fast 通道（`fast_mode_policy` + `fast_*` 价格）怎么在 CLI 表达 | 功能完整性 | P2 |

### B. 要问后端的

按优先级排列。第一条最值得先提：后端改动成本极低，而它能让预检从「探测」升级为「查表」，
省掉整个零成本探测子系统。

| 问题 | 为什么重要 | 影响 |
|---|---|---|
| 能否在公开的 `/api/v1/marketplace/models` 里加上 `allowed_client_protocols` | 未登录即可做完整预检，不必依赖探测 | M5 |
| `claude_code_only` 的判定依据 | 同 A 表第一行，一问可能就省掉一轮实测 | M3 |
| 会话粘性/归因的确切 header 名与语义（是不是 `X-Session-Id`） | 决定能不能白捡账号粘性 + 用量归因 | M3 |
| `max_reasoning_effort` / `reasoning_effort_mappings` 的语义（当前为空） | effort 是三层映射，要给分组层留位置 | M4 |
| Key 要不要加 `source` 标记 | 让用户在 `/keys` 页认出并单独撤销 CLI 发的 Key | v0.5 |
| fallback 分组链能否对客户端可见 | 影响预检措辞与 doctor 诊断 | M5 |

### C. 产品决策

已全部拍板，见 `design/product-decisions.md`、`design/open-decisions.md`、
`design/model-selection.md`：

- host/profile：Profile 模型 + 项目级只存 profile 名；v0 先做单 profile + `--host`
- 默认模型：必须注入；三层策略；模型槽按 harness 分开存
- telemetry：默认关，版本信息只走 tf 自身请求的 UA
- License：Apache-2.0 开源
- 命令名 `tf`（npm 可用）
- 进程模型 fork+wait、存储形态、文案语言、`--json` 触发、缓存 TTL、harness 未装的交互、
  用量摘要详细显示、Windows 实验性，均已定

---

## 三、已经定了的

### 产品定位

1. **tf 是启动器，不是配置管理器。** 只做进程内注入（env + CLI flag + 进程私有临时文件），
   退出不留痕，不改用户的 `~/.claude/settings.json`、`~/.codex/config.toml`。
2. 与 CC-Switch 分工共存：它管持久化配置，tf 管临时启动。两者会在同一进程里冲突，
   所以 `doctor` 是必需品。
3. 不做：自己的聊天 REPL、自己的 agent loop、常驻守护进程。

### 技术选型

4. Go 单二进制（复用 TokenRouter 的 goreleaser 链路），约 10–15MB。
5. 分发：npm 主包（JS shim）+ 按 `os`/`cpu`/`libc` 切分的 optionalDependencies 平台包，
   无 postinstall 下载；并行提供 install.sh / brew / scoop。`pnpx tf` 可用。
6. npm 名字 `tf` 可用（`tokenflux` 也可用，`tokenrouter` 已被占）。

### 认证

7. v0：`tf login --with-key`（粘贴/stdin），零后端改动，同时是 SSH/容器场景的永久兜底。
8. v0.5：网页「导入 tf」按钮 → localhost 回环 HTTP，只动前端。不用 `tf://` scheme：
   curl/npx 装的二进制注册不了，且 Key 会经 argv 泄露。
9. 安全性集中在终端侧带 Origin 的预览确认，不做配对码。非交互用 `--yes`。
10. PKCE / 授权服务器推迟到要开放给第三方工具时再谈。

### 适配与预检

11. harness 首批：claude / codex / opencode，其次 hermes。
12. codex 是单协议 harness：官方已移除 `wire_api="chat"`，只剩 `responses`，没有降级路径。
13. 协议是集合不是格式：`anthropic_messages` / `openai_responses` /
    `openai_chat_completions` / `gemini_generate_content`，可任意组合，空集合也合法。
14. 预检只能证伪不能证真（账号 endpoint capability 更窄，还有 fallback 分组链）。
    通过时静默放行，不给正面承诺；预检自身失败时降级放行。
15. 零成本协议探测已验证可行：协议准入发生在 body 解析之前，空 body 请求 → 400 表示准入通过，
    403 + `does not allow ... requests` 表示不准入，不消耗 token。
16. 错误按 message 分类，不能只看状态码：两种 403（协议不准入 / 模型不在分组，
    后者响应里自带 available models 列表）、两种 401（`INVALID_API_KEY` / `INVALID_TOKEN`）。
17. 模型 ID 变换收口成一个纯函数（复合 Key 前缀 → 服务端 model_mapping → harness provider 前缀）。
18. 数据可见性分层已实测清楚：匿名可拿目录/价格/容量/可用率；Key 只能加读 `/v1/models`；
    协议准入必须 JWT。
19. `tf models` / `tf groups` 未登录即可用，`pnpx tf models` 开箱即用。
