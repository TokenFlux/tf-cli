# tkr 实施计划

所有设计决策的落地路线。设计依据见 `design/` 与 `research/`。

---

## 已锁定的关键决策速查

| | 决定 |
|---|---|
| 定位 | 启动器，进程内注入，退出不留痕，不改用户配置文件 |
| 语言 / 分发 | Go 单二进制；npm 平台包（optionalDeps + JS shim）+ install.sh + brew/scoop |
| 进程模型 | **fork + wait**（信号转发自己写，换取确定性清理与用量摘要）|
| 绝对禁止 | 本地代理 / MITM / 覆盖 harness UA（会破坏 `claude_code_only` 的 UA+TLS 指纹识别）|
| 认证 | v0 `--with-key`；v0.5 网页「导入 tkr」+ localhost 回环 + 带 Origin 的预览确认 |
| 参数 | tkr 只认自己的一小组 flag，遇到第一个陌生参数起全部透传；`--` 无条件透传；`--model` 由 tkr 吃掉 |
| 存储 | `config.json`(0644) / `credentials.json`(0600) 分开；支持 XDG；v0 不用钥匙串 |
| 文案 | 跟随 locale + `TKR_LANG`；**错误码保持英文常量** |
| 输出 | 显式 `--json`；非 TTY 时日志转 stderr、去色，但不自动改格式 |
| 缓存 | 目录 24h / 探测 1h；刷新超时 2s 用旧数据；离线放行并说明 |
| 模型 | 必须注入；三层策略（默认直用 / 首次选择器 / 非交互启发式）；`-m` 空值进选择器；槽按 harness 分开存 |
| harness 未装 | 交互式二选一（退出 / 由 tkr 装），非交互默认拒绝，绝不 sudo |
| 用量摘要 | **详细显示** |
| Telemetry | **默认关**，版本信息只走 tkr 自身请求的 UA |
| License | **Apache-2.0**，开源 |
| Windows | v0 出二进制，标注实验性 |

---

## 代码结构

```
cmd/tkr/main.go
internal/
  cli/        命令定义、参数透传规则、locale
  config/     profile、凭据、XDG、文件权限
  catalog/    marketplace 目录 + /v1/models + 缓存
  gateway/    HTTP 客户端、错误分类、tkr 自身 UA
  model/      模型 ID 解析（纯函数，收口三重变换）
  harness/    适配表 + 各 harness 注入器
  precheck/   协议准入判定 + 零成本探测
  launch/     fork+wait、信号转发、临时文件、用量摘要
  tui/        模型选择器（与 models 命令共用渲染）
  doctor/     诊断规则
  ui/         输出层、--json、错误码与文案
```

---

## 里程碑

### M0 骨架
CLI 框架、config/credentials 读写与权限、错误码与双语文案脚手架、`--json` 约定、非 TTY 行为。

**验收**：`tkr --help` / `tkr version --json` 可用；配置文件权限正确。

### M1 目录（第一个可发布的东西）
`catalog` + `gateway` + `tkr models` / `tkr groups`，走公开的 `/api/v1/marketplace/models`。

**验收**：**完全未登录**能列出分组与模型，含定价、倍率、可用率、并发；缓存与 `--refresh` 生效；离线用旧缓存并提示。
**注意**：文案要区分「市场上有（12 个）」和「你能用」，marketplace 是上架子集。

### M2 认证
`tkr login --with-key`（stdin / 隐藏输入）、`tkr status`、host 归一化、用 `/api/v1/settings/public` 校验 host。

**验收**：Key 落盘 0600；`status` 显示余额、限速窗口剩余、当前 profile/host/分组；错误的 host 当场报错。

### M3 启动（核心）
`harness` 适配表 + **先只做 claude** + `launch`（fork+wait、信号转发、临时 settings 文件、退出清理、详细用量摘要）。

**验收**：`tkr claude` 能起真实 Claude Code 并正常对话；Ctrl+C 语义正确；退出码透传；临时文件确定性清理；退出后打印详细用量。

**本阶段必须完成的实测**：
1. **`claude_code_only` 分组（Claude Max）能否正常识别** —— 最高优先级，验证 UA+TLS 指纹在 tkr 启动路径下不受影响。
2. 环境里存在 `HTTPS_PROXY` 时是否破坏识别 → 转化为 doctor 规则。
3. `ENABLE_TOOL_SEARCH` 在 Anthropic 分组是否生效。

### M4 模型解析与选择器
`model` 纯函数（复合 Key 前缀 / model_mapping / harness provider 前缀）+ `tui` 选择器 + claude 三档一屏确认 + 模型槽持久化。

**验收**：三层策略行为正确（默认直用 / 首次选择器 / 非交互启发式）；`-m` 空值进选择器；纯函数单测覆盖三重变换；档位不足时给出明确说明。

### M5 预检与 doctor
协议集合判定、`claude_code_only` 维度、零成本探测（无 JWT 时）、错误分类映射、`tkr doctor`。

**验收**：协议不匹配在本地拦下且给出可执行修复；预检失败时降级放行；两种 403 / 两种 401 分类正确；doctor 能查出 CC-Switch 残留、harness 自存凭据、废弃端点、代理风险。

### M6 codex 与 opencode
按适配表加两行 + 各自的冲突检测。

**验收**：`tkr codex` / `tkr opencode` 跑通且不写用户配置文件；codex 走 `-c` 覆盖 + `env_key`；opencode 优先 responses、必要时回退 chat；检测到 harness 自存凭据时警告。

### M7 分发
goreleaser 交叉编译、npm 主包 + 平台包、install.sh + SHA256SUMS、brew/scoop、Apache-2.0 与 README 商标声明、SECURITY.md。

**验收**：`pnpx tkr models` 可用（无 postinstall 下载）；`curl | bash` 可用；`npm publish --provenance` 通过；识别安装来源后 `tkr update` 行为正确。

### v0.5
网页「导入 tkr」按钮 + localhost 回环 + Origin 预览确认；完整 profile 机制（多 profile、`tkr use`、项目级 `.tkr/config.json` 只存 profile 名）。

### v1（视情况）
`tkr run --`、hermes 及更多 harness、钥匙串后端、`--json` 全量覆盖。

---

## 并行推进：要问后端的事

不阻塞 v0，但越早有答案越省事。

| 事项 | 影响的里程碑 | 优先级 |
|---|---|---|
| **公开端点 `/api/v1/marketplace/models` 能否加 `allowed_client_protocols`** | M5 —— 若能加，**整个零成本探测子系统可以不做** | **最高** |
| 会话粘性 / 用量归因的确切 header 名 | M3 | 高 |
| Key 加 `source` 标记以便识别与撤销 | v0.5 | 中 |
| `max_reasoning_effort` / `reasoning_effort_mappings` 语义 | M4 | 中 |
| fallback 分组链能否对客户端可见 | M5 | 低 |

---

## 风险

| 风险 | 应对 |
|---|---|
| **`claude_code_only` 识别在 tkr 路径下失效** | M3 最先验证；一旦失效需重新设计注入方式 |
| harness 迭代导致注入 flag 失效 | 记录已验证版本范围，不匹配只警告；不做版本嗅探分支 |
| 分组配置被管理员改动导致缓存过期 | 探测结果 TTL 短（1h）；403 时自动失效缓存并重试一次 |
| npm 平台包体积与发布复杂度 | 用 optionalDependencies，无 postinstall；CI 里一次性把六个平台包发完 |
