# tkr 现状：定了什么，还缺什么

> **实施计划见 [`PLAN.md`](PLAN.md)**。本文件只管「定了什么 / 还缺什么」。

更新于 2026-08-29。支撑文档：
- `research/ori.md` — ori 逆向调研 + 整体设计 + 分发方案
- `research/tokenflux-api-probe.md` — 生产环境实测记录
- `design/import-from-web.md` — login 最终方案
- `design/cli-auth-discussion.md` — 与后端沟通的事实与议题

---

## 一、已经定了的

### 产品定位
1. **tkr 是启动器，不是配置管理器。** 只做进程内注入（env + CLI flag + 进程私有临时文件），退出不留痕，不改用户的 `~/.claude/settings.json`、`~/.codex/config.toml`。
2. 与 CC-Switch **分工共存**：它管持久化配置，tkr 管临时启动。因此 `doctor` 是必需品——两者会在同一进程里冲突。
3. **不做**：自己的聊天 REPL、自己的 agent loop、常驻守护进程。

### 技术选型
4. **Go 单二进制**（复用 TokenRouter 的 goreleaser 链路），~10–15MB。
5. **分发**：npm 主包（JS shim）+ 按 `os`/`cpu`/`libc` 切分的 optionalDependencies 平台包，**无 postinstall 下载**；并行提供 install.sh / brew / scoop。`pnpx tkr` 可用。
6. **npm 名字 `tkr` 可用**（`tokenflux` 也可用；`tokenrouter` 已被占）。

### 认证
7. **v0：`tkr login --with-key`**（粘贴/stdin），零后端改动，同时是 SSH/容器场景的永久兜底。
8. **v0.5：网页「导入 tkr」按钮 → localhost 回环 HTTP**，只动前端。不用 `tkr://` scheme（curl/npx 装的二进制注册不了，且 Key 会经 argv 泄露）。
9. 安全性集中在**终端侧带 Origin 的预览确认**，不做配对码。非交互用 `--yes`。
10. PKCE / 授权服务器推迟到「要开放给第三方工具」时再谈。

### 适配与预检
11. **harness 首批**：claude / codex / opencode，其次 hermes。
12. **codex 是单协议 harness**：官方已移除 `wire_api="chat"`，只剩 `responses`，**没有降级路径**。
13. **协议是集合不是格式**：`anthropic_messages` / `openai_responses` / `openai_chat_completions` / `gemini_generate_content`，可任意组合，空集合也合法。
14. **预检只能证伪不能证真**（账号 endpoint capability 更窄、还有 fallback 分组链）。通过时静默放行，不给正面承诺；预检自身失败时降级放行。
15. **零成本协议探测已验证可行**：协议准入发生在 body 解析之前，空 body 请求 → 400 = 准入通过，403 + `does not allow ... requests` = 不准入，不消耗 token。
16. **错误按 message 分类，不能只看状态码**：两种 403（协议不准入 / 模型不在分组，后者响应里自带 available models 列表）、两种 401（`INVALID_API_KEY` / `INVALID_TOKEN`）。
17. **模型 ID 变换收口成一个纯函数**（复合 Key 前缀 → 服务端 model_mapping → harness provider 前缀）。
18. **数据可见性分层已实测清楚**：匿名可拿目录/价格/容量/可用率；Key 只能加读 `/v1/models`；协议准入必须 JWT。
19. `tkr models` / `tkr groups` **未登录即可用**，`pnpx tkr models` 开箱即用。

---

## 二、还缺什么

### A. 我能自己解决的（实测 / 实现）

| 缺口 | 影响 | 优先级 |
|---|---|---|
| **`claude_code_only` 分组靠什么识别 Claude Code**（UA？header？）tkr 注入的 header 会不会破坏识别 | **高**——Claude Max 倍率 20，是最有价值的分组，识别不过就用不了 | P0 |
| 三个 harness 端到端实际跑通 | 适配表的正确性 | P0 |
| opencode 的 AI SDK provider 实际走 responses 还是 chat | 候选列表顺序 | P1 |
| `ENABLE_TOOL_SEARCH` 在 Anthropic 分组是否生效 | 省近一半系统提示词 token | P1 |
| 复合 Key 的 `composite_groups` 实际结构与前缀形态 | 前缀自动补全 | P1 |
| fast 通道（`fast_mode_policy` + `fast_*` 价格）怎么在 CLI 表达 | 功能完整性 | P2 |

### B. 要问后端的

| 问题 | 为什么重要 |
|---|---|
| **能否在公开的 `/api/v1/marketplace/models` 里加上 `allowed_client_protocols`** | **成本极低、收益极大**：v0 就能不登录做完整预检，不必依赖探测。**建议优先提这条** |
| `claude_code_only` 的判定依据 | 同 A 表第一行，一问可能就省掉一轮实测 |
| 会话粘性/归因的确切 header 名与语义（是不是 `X-Session-Id`） | 决定能不能白捡账号粘性 + 用量归因 |
| Key 要不要加 `source` 标记 | 让用户在 `/keys` 页认出并单独撤销 CLI 发的 Key |
| `max_reasoning_effort` / `reasoning_effort_mappings` 的语义（当前为空） | effort 是三层映射，要给分组层留位置 |
| fallback 分组链能否对客户端可见 | 影响预检措辞与 doctor 诊断 |

### C. 产品决策 —— 已全部拍板

见 `design/product-decisions.md`、`design/open-decisions.md`、`design/model-selection.md`：

- host/profile：Profile 模型 + 项目级只存 profile 名；v0 先做单 profile + `--host`
- 默认模型：必须注入；三层策略；模型槽按 harness 分开存
- telemetry：**默认关**，版本信息只走 tkr 自身请求的 UA
- License：**Apache-2.0 开源**
- 命令名 `tkr`（npm 可用）
- 进程模型 fork+wait、存储形态、文案语言、`--json` 触发、缓存 TTL、harness 未装的交互、用量摘要详细显示、Windows 实验性 —— 均已定

---

## 三、依赖关系

```
v0（--with-key + 三个 harness + 基础预检）
  └── 不被任何后端问题阻塞，可以立刻开工
      唯一变数：若后端愿意在公开端点加 allowed_client_protocols，
                v0 的预检就从「探测」升级为「查表」，更准更快

v0.5（导入按钮）
  └── 依赖前端改动，不依赖后端接口

v1（更多 harness / run -- / 完整 JSON 输出）
  └── 依赖 v0 的适配表结构稳定
```

**结论：v0 现在就能开工。** 建议同时把 B 表第一条（公开端点加协议字段）作为最优先的沟通事项抛给后端——它可能让 v0 的预检直接完整，省掉整个探测子系统。
