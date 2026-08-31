# Ori 调研 + tf 设计草案

调研对象：`ori`（OpenRouter 官方 CLI，2026-08 发布）
目标：为 TokenFlux（托管）与 TokenRouter（自托管）做一个同类工具 `tf`。

---

## 一、Ori 是什么

**不是聊天 CLI，而是 harness 启动器**：把用户已安装的 Claude Code / Codex / opencode 等 agent，用一套经过调优的环境变量与配置启动，指向 OpenRouter。

官方博客：https://openrouter.ai/blog/announcements/ori-harness
分发仓库：https://github.com/OpenRouterLabs/ori-releases （只有产物，源码在私有的 `OpenRouterIncubator/ori`）

核心论点（值得照抄的产品定位）：用户接网关时要么写 ad-hoc 脚本，要么在环境变量里考古；不同 harness 需要的变量数量差异巨大，Claude Code 尤其多，而且**变量取值要随模型而变**（例如 `ENABLE_TOOL_SEARCH` 只对 Anthropic 模型该开）。这张「每个 harness × 每个模型的最优配置矩阵」就是产品价值。

### 分发方式

- 单文件独立可执行（Bun `--compile`，约 80 MB，内部是 Effect-TS 写的 TypeScript，binary 里能直接 `strings` 出压缩后的 JS）。
- `curl -fsSL https://openrouter.ai/labs/ori/install.sh | bash` → 默认装到 `~/.local/bin`，`ORI_INSTALL_DIR` 可覆盖。
- 产物矩阵：`ori-{darwin,linux}-{arm64,x64}` + linux musl 变体 + `ori-windows-x64.exe`；`SHA256SUMS` 校验（未签名）。
- 双通道：stable 走 GitHub `releases/latest`，alpha 走默认分支上的 `version-alpha` 文件（因为 CDN 代理只服务 non-prerelease）。`ori update --alpha/--stable/--check/--force`。
- release tag 形如 `cli-0.12.0-68f9a36`，release body 里注明 `Built from <私有 repo>@<sha>`，manifest.json 记录 builtAt/builtFrom。
- 安装脚本自带 telemetry（安装阶段/成功失败上报），`ORI_TELEMETRY=0` 关闭；运行期 telemetry 端点 `POST /api/v1/ori/telemetry`。

### 命令面（v0.12.0）

```
login / auth                      凭据获取与诊断
claude codex grok opencode hermes omp kilo prime-agent dsh   启动各 harness
harness list|test                 已注册/可启动 harness、契约测试
harness-doctor codex              离线只读地找出会打架的环境变量与配置
mcp list|test                     检查 mcp.json 里的 server 能否连上、暴露了哪些工具
history                           本机 agent 会话列表
workspace reset                   归档并重建全局 workspace
eval                              跑 *.eval.ts agent evals
vault-tunnel                      本地 CONNECT 代理，容器里跑 agent 时真实密钥不落地
changelog / update / version
```

全局约定（很值得抄）：

- `--json/--agent` 与 `--human/--tty`。**管道/重定向时默认输出 JSON**，stdout 恰好一个 JSON 文档 `{ok, command, data}`，所有日志走 stderr。
- `--wizard` 让任意命令进交互向导；`--completions bash|zsh|fish|sh`。
- `--model` 与 `--reasoning-effort`（统一 7 档：`max xhigh high medium low minimal none`）是所有 harness 命令的公共 flag，其余参数原样透传给底层二进制。

### 认证

- OAuth PKCE：`https://openrouter.ai/auth?callback_url=<http://127.0.0.1:PORT>&code_challenge=<S256>&code_challenge_method=S256&key_label=ori`，本地起随机端口回调，再 `POST /api/v1/auth/keys` 换 API Key，`GET /api/v1/key` 自省。`--no-browser` 打印 URL（SSH 友好），`--callback-port`，`--with-key` 从 stdin 读 key 全非交互。
- 凭据落盘：全局 `~/.ori/credentials.json`，工作区 `<repo>/.ori/credentials.json`（`--local`），另有兼容路径 `~/.openrouter/credentials.json`。`ori auth` 报告"当前目录解析到哪个凭据、来自哪里"，解析不到就非零退出。
- 解析优先级：`OPENROUTER_API_KEY` env → 存储凭据（workspace-local 优先于 global）→ 项目 dotenv（花钱前要确认，非交互直接拒绝）。

### 每个 harness 具体干了什么（从 binary 里还原）

**Claude Code**（最复杂，两套 profile）：
```
ANTHROPIC_BASE_URL / ANTHROPIC_AUTH_TOKEN(空) / ANTHROPIC_API_KEY / OPENROUTER_API_KEY
CLAUDE_CODE_SKIP_FAST_MODE_ORG_CHECK=1
ENABLE_TOOL_SEARCH=<按模型能力 true/false>
CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=<0 | 1>
CLAUDE_CODE_SIMPLE_SYSTEM_PROMPT=1（可被用户覆盖）
# 走 per-model 能力时额外：DISABLE_TELEMETRY=1、DISABLE_GROWTHBOOK=0、
#   CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1、CLAUDE_CODE_MAX_CONTEXT_TOKENS=<上限>
# 否则回退到显式默认模型：ANTHROPIC_DEFAULT_{HAIKU,SONNET,OPUS,FABLE}_MODEL、CLAUDE_CODE_AUTO_MODE_MODEL
ANTHROPIC_CUSTOM_HEADERS 里注入 X-Session-Id
```
另外：把一整张竞争变量表**显式清空**（`ANTHROPIC_*_BASE_URL`、`CLAUDE_CODE_USE_{BEDROCK,VERTEX,GATEWAY,...}`、`CLAUDE_CODE_OAUTH_TOKEN` …）；读用户 `~/.claude/settings.json` 的 env 做"可覆盖项"的合并；把 apiKeyHelper + env 写进一个进程私有的 `<cwd>/.ori/claude-launch-settings-<pid>.json` 再用 `--settings` 传入（并清理 24h 前的残留），只有用户自己传了 `--settings/--setting-sources` 时才让路；还会原子改写 `.claude.json` 缓存模型列表；给 secure storage 单独指目录 `~/.ori/claude-secure-storage`。变量分 `overridable / non-overridable` 两类，这个建模很干净。

**Codex**（不设环境变量，全部用 `-c` 覆盖）：
```
-c model_provider=openrouter
-c model_providers.openrouter.base_url='https://openrouter.ai/api/v1'
-c model_providers.openrouter.wire_api='responses'
-c model_providers.openrouter.env_http_headers.X-Session-Id='ORI_OPENROUTER_SESSION_ID'
-c model_providers.openrouter.auth.command='sh'
-c model_providers.openrouter.auth.args=['-c','echo $OPENROUTER_API_KEY']
[-m <model>] [-c model_reasoning_effort=<effort>]
```
`harness-doctor codex` 会扫 `/etc/codex/{config,managed_config,requirements}.toml`、逐级向上的 `.codex/config.toml`、代理变量与 `OPENAI_*/CODEX_*` 变量，报冲突。

**opencode**：`OPENROUTER_API_KEY` + `OPENCODE_CONFIG_CONTENT`（内联 JSON 注入 `X-Session-Id` header，用户已设则不覆盖），模型自动补 `openrouter/` 前缀，effort → `--variant`。**并且检测 `~/.local/share/opencode/auth.json` 里已存的 openrouter key 与注入 key 不一致时警告**（存储凭据优先级更高，会导致认证失败）。

**其余 harness**：grok 改 `GROK_MODELS_BASE_URL/GROK_MODELS_LIST_URL/GROK_XAI_API_BASE_URL/XAI_API_KEY` 并关掉其 telemetry；hermes 只有 key，明确报错说自己不支持 effort 翻译；omp 走 `--provider openrouter`/`--model openrouter/x` + `--thinking`；kilo 读多层配置定模型、检测 `KILO_AUTH_CONTENT` 冲突；prime-agent 生成一个 `~/.ori/prime-agent/openrouter-auth.ts` 扩展并清空 **30+ 个别家 provider 的 API key 变量**；dsh 写 `~/.dsh/.credentials.yaml` 并装插件。

模式很统一，抄的时候按这个抽象：
```
launch(harness) = 解析 key → 解析 model/effort（按 harness 能力表翻译）
                → 清竞争变量 → 注入本家变量/CLI flag/临时配置文件
                → 检测"已存凭据会压过注入值"的冲突并警告 → exec 透传剩余 argv
```
effort 能力表（不支持的档位直接报错并列出可用档）：
| harness | 支持 |
|---|---|
| claude | low medium high xhigh max |
| codex | minimal low medium high xhigh |
| opencode / kilo | minimal low medium high max |
| omp / prime-agent | 全 7 档（none→off）|
| hermes | 无（明确拒绝）|

### 值得学的取舍

1. 不重造 agent，只做配置层，接入成本低，护城河在配置矩阵。
2. 配置**随模型变**，而不是一套静态 env。
3. 主动做冲突诊断（doctor / 凭据冲突警告），这是网关类产品最大的支持成本来源。
4. 机器可读输出是一等公民（管道即 JSON）。
5. 独立二进制 + 校验和 + 双通道更新，用户零运行时依赖。
6. 源码闭源但产物 Apache-2.0，分发仓库明确写「这里没有代码可读」。

---

## 二、tf 设计草案

### 定位

`tf` = TokenFlux / TokenRouter 的 harness 启动器 + 账号自省工具。相对 ori 的差异点：

- **双目标**：托管的 TokenFlux（`https://tokenflux.dev`）与用户自托管的 TokenRouter 实例，同一套命令用 profile 切换。
- **TokenRouter 有完整的用户 API**（`/api/v1/auth/login`、`/api/v1/keys`、`/api/v1/groups/available`、`/api/v1/user/*`），所以 `tf login` 可以**直接登录并自动创建/复用 API Key**，比 ori 的 OAuth 换 key 更进一步。
- **分组（group）概念是 TokenFlux 独有的坑**：分组按协议格式区分（OpenAI 格式 vs Anthropic 格式），选错返回 403 `This group does not allow ... requests`；复合 Key 还要求模型 ID 带前缀。这些 ori 都没有，正好是 `tf` 的核心价值：**启动前就校验"这个 harness 需要的协议 × 这个 Key 的分组能力"，把 403 提前到本地**。

### 端点事实（来自 TokenDocs / TokenRouter docs）

| 协议 | Base URL（TokenFlux） | 用途 |
|---|---|---|
| OpenAI | `https://tokenflux.dev/v1` | opencode、codex、Cherry Studio… |
| Anthropic | `https://tokenflux.dev` | Claude Code |
| Gemini | `https://tokenflux.dev` | `/v1beta/models/<id>:generateContent` |

认证头：`Authorization: Bearer`、`x-api-key`、`x-goog-api-key`（Gemini 另支持 `?key=`）。自托管 TokenRouter 把域名换成实例地址即可，路径族一致。

### 命令面（v0）

```
tf login [--host <url>] [--with-key] [--local] [--no-browser]
tf auth                      # 当前目录解析到哪个凭据、余额、分组能力、协议支持
tf profile list|use|add      # tokenflux / 自托管实例切换
tf claude | codex | opencode | hermes | cherry(?)   # 启动，透传 argv
tf models [--group g] [--format openai|anthropic]   # 拉 /v1/models，标注分组可用性
tf doctor [harness]          # 冲突变量、CC-Switch 残留配置、旧端点 token.memoh.net 检测
tf status                    # 余额/订阅/今日用量（/api/v1/user/*）
tf update / version
```

全局 flag 与 ori 对齐：`--model`、`--reasoning-effort`、`--json`（管道即 JSON，单文档 `{ok,command,data}`）、`--human`、`--completions`。

### 关键实现点

1. **凭据存储**：`~/.tf/credentials.json`（多 profile：host + key + label），工作区 `.tf/credentials.json` 优先；env `TOKENFLUX_API_KEY` / `TF_API_KEY` 最高。文件 0600。
2. **harness 适配表**：一张 `harness × {协议格式, 环境变量, CLI flag, 配置文件写法, effort 映射}` 的数据表，逻辑与数据分离，新增 harness 只加一行。
   - claude → `ANTHROPIC_BASE_URL=<host>`、`ANTHROPIC_AUTH_TOKEN=<key>`、清空 `ANTHROPIC_API_KEY` 与全部 bedrock/vertex/gateway 变量、`--settings` 临时文件。
   - codex → 全 `-c` 覆盖 `model_providers.tokenflux.base_url='<host>/v1'`，`wire_api` 按分组能力选 `chat`/`responses`（TokenRouter 文档明确 `preserve_client_protocol` 下 Chat 只走 `/v1/chat/completions`）。
   - opencode → `OPENCODE_CONFIG_CONTENT` 内联自定义 provider（OpenAI 兼容 + baseURL），检测 `~/.local/share/opencode/auth.json` 冲突。
3. **冲突诊断**：CC-Switch 是官方推荐的配置管理器，它写的配置会和注入值打架 → `tf doctor` 必须识别 CC-Switch/`~/.claude/settings.json`/`.codex/config.toml`/旧 `token.memoh.net` 端点。
4. **分组预检**：启动前用 key 调 `/api/v1/groups/available` 或直接读 `/v1/models`，若 harness 需要 Anthropic 协议而 Key 绑的是 OpenAI 格式分组，本地直接给出可读错误 + 修复指引，而不是让用户去猜 403。复合 Key 自动补模型前缀。
5. **分发**：TokenRouter 已是 Go + goreleaser（`Dockerfile.goreleaser`、`Makefile`），**建议 tf 也用 Go**，复用发布链路，产出跨平台单文件 + `SHA256SUMS` + `install.sh`，走独立的 `tf-releases`（或直接 TokenRouter repo 的 release）。若追求与 harness 生态一致的 JS 配置注入能力，Bun 单文件也可行，但 Go 与现有工程更省事。
6. **telemetry**：默认关或明确 opt-in（国内用户对此敏感），别照抄 ori 的默认开启。

### 建议的落地顺序

1. `login` + `auth` + 凭据解析（含 workspace-local）
2. `claude` + `codex` + `opencode` 三个 harness（覆盖 TokenDocs 里 90% 的教程）
3. `--json` 输出约定 + `doctor`
4. `models` / `status` / 分组预检
5. 打包分发与 `update`

### 待确认

- 是否需要 `vault-tunnel` 式的容器场景支持（后续再说）。
- 名字：`tf` 与 TokenRouter 同名，若要同时服务 TokenFlux，考虑 `tf` / `tflux` 作为别名。

---

## 三、认证：绕开 Cloudflare 质询

> **已决策（2026-08-29）**：本节的 PKCE 浏览器授权流**不采用**。
> 结论见 [`../design/import-from-web.md`](../design/import-from-web.md)：
> - **v0** 只做 `tf login --with-key`（粘贴 Key，零后端改动，同时是 SSH/容器场景的永久兜底）
> - **v0.5** CLI 起 127.0.0.1 本地服务，网页 keys 页加「导入 tf」按钮推送 Key；只动前端，安全性靠终端侧带 Origin 的预览确认
> - PKCE / 授权服务器留到「要开放给第三方工具」时再谈，届时价值是开放能力而非省事
>
> 下面的内容保留，作为 ori 做法的调研记录与将来做授权服务器时的参考。

**结论：CLI 永远不碰密码、不直接调 `/api/v1/auth/login`。** 用户名密码登录路径挂着 Cloudflare 质询 / Turnstile / 腾讯天御，CLI 里的 HTTP client 天生过不去，也不该试图过去（绕质询本身就是错的方向）。把交互挪进浏览器：浏览器有 cookie、能过质询、已经登录。

### 浏览器授权流（照抄 ori 的 PKCE，但授权页是自家前端）

```
tf login
  1. 生成 code_verifier(32B base64url) + code_challenge = S256(verifier)
  2. 本地监听 127.0.0.1:<随机端口>
  3. 打开 https://tokenflux.dev/cli/auth
         ?callback_url=http://127.0.0.1:PORT/callback
         &code_challenge=...&code_challenge_method=S256&label=tf
  4. 前端新页面（未登录先走正常登录流程，质询在浏览器里过）：
     显示「授权 tf 访问你的账号」+ 分组下拉 + Key 名称
     用户确认 → 用现有用户 JWT 调 POST /api/v1/keys 建 Key
                → POST /api/v1/auth/cli/grant {code_challenge, key_id}
                  服务端存一次性 code → key 映射（TTL 60s）
     → 302 到 http://127.0.0.1:PORT/callback?code=...
  5. CLI 用 code + code_verifier 调 POST /api/v1/auth/cli/exchange
     → 拿到明文 Key，写入 ~/.tf/credentials.json (0600)
  6. 浏览器页面显示「可以关掉这个标签页了」
```

要点：

- 后端只需两个新接口：`POST /api/v1/auth/cli/grant`（用户 JWT）与 `POST /api/v1/auth/cli/exchange`（公开、一次性、校验 `S256(verifier)==challenge`、60s 过期、绑定创建时的 IP 段可选）。前端只需一个 `/cli/auth` 路由（现有 router 已有大量 `/auth/*` callback 页，风格一致）。
- **`exchange` 这条路径必须在 Cloudflare 上和网关 `/v1/*` 一样豁免 bot fight / managed challenge**，否则又被拦。最稳的是挂在已经必须放行的 API 网关同一规则下，或用独立子域 `cli.tokenflux.dev`。
- CLI 的 UA 要显式设成 `tf/<ver> (+https://tokenflux.dev)`，别用 Go 默认 UA（`Go-http-client/2.0` 是 WAF 的重点关照对象）。
- **质询兜底诊断**：任何请求收到 403/503 且响应含 `cf-mitigated` / `Just a moment` / `cf-chl` 特征时，不要报「网络错误」，直接告诉用户「被 Cloudflare 质询拦下，请改用 `tf login --with-key` 或在浏览器里完成授权」。这是 ori 没有、但你这边必然高频的错误分支。
- **无浏览器环境**（SSH / 容器 / WSL 无 GUI）：`--no-browser` 打印 URL 让用户在别的设备打开；页面在拿不到本地回调时退化成显示一段一次性 code，用户粘回终端（device-code 变体）。
- **v0 最小可用**：先只做 `tf login --with-key`（stdin 管道或隐藏输入，从 https://tokenflux.dev/keys 复制），完全不改后端就能发第一版；浏览器流作为 v0.2，改动只在自家前后端，风险可控。
- 自托管 TokenRouter 同一套流程，`--host https://my.gateway` 即可，因为前后端是同一份代码。

---

## 四、分发：Go 二进制 + npm 平台包（`pnpx tf` 照样能用）

用 Go 不等于放弃 npx/pnpx。业界标准做法（esbuild、biome、swc、turbo 都是这个结构）：**主包是纯 JS shim，二进制放在按平台切分的 optionalDependencies 里**。

```
tf                          (主包，~5KB)
├─ bin/tf.js                shim：resolve 平台包里的二进制并 exec，透传 argv/stdio/退出码
└─ optionalDependencies:
   @tokenflux/tkr-darwin-arm64   { "os": ["darwin"], "cpu": ["arm64"] }
   @tokenflux/tkr-darwin-x64
   @tokenflux/tkr-linux-x64      { "os": ["linux"], "cpu": ["x64"], "libc": ["glibc"] }
   @tokenflux/tkr-linux-x64-musl { ..., "libc": ["musl"] }
   @tokenflux/tkr-linux-arm64
   @tokenflux/tkr-win32-x64
```

包管理器按 `os`/`cpu`/`libc` 字段只装匹配的那一个，于是：

- `pnpx tf login` / `npx tf claude` 开箱即用，只下 ~10MB。
- **没有 postinstall 下载脚本**，不受 `--ignore-scripts`、企业内网、离线 registry 镜像影响，这点比「postinstall 里 curl 二进制」的方案稳得多。
- Go 二进制约 10–15MB，装进 npm 完全合理；对比 ori 的 Bun `--compile` 产物 80MB，**Go 在这条路上反而是优势**。

shim 注意事项：`execFileSync(bin, process.argv.slice(2), { stdio: 'inherit' })` 保持 TTY（agent 是全屏 TUI，必须继承）；Windows 补 `.exe`；透传退出码与信号；平台包缺失时给出可读报错（提示 `--no-optional` 或不支持的平台）。

发布链路：goreleaser 已在仓库里（`.goreleaser.yaml`），交叉编译产物直接复用；再加一个 release 后的小脚本，把每个 `dist/` 产物塞进平台包目录并 `npm publish --provenance`。同时保留：

- `curl -fsSL https://tokenflux.dev/install.sh | bash`（照抄 ori：探测平台 → 下载 → `SHA256SUMS` 校验 → 装 `~/.local/bin`）
- Homebrew tap / Scoop（goreleaser 内建 `brews`、`scoops`）
- `tf update`（自更新，stable/alpha 双通道可选）

### 与「纯 TS/Bun 实现」的取舍

| 方案 | npx/pnpx | 体积 | 与 TokenRouter 工程栈 | 启动 |
|---|---|---|---|---|
| Go + npm 平台包 | ✅ | ~10MB | 一致（Go + goreleaser 已有） | 最快 |
| Bun `--compile` 单文件 | 勉强（80MB 进 npm 不现实） | 80MB | 新增 Bun 工具链 | 快 |
| 纯 TS 跑在 node 上 | ✅ 原生 | 最小 | 新增 Node 依赖，要求用户装 node | 慢，且依赖用户 node 版本 |

**推荐 Go + npm 平台包**：既留住 `pnpx tf`，又没丢单文件、无运行时依赖、和后端同栈的好处。
