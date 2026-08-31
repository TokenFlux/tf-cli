# login 之后：下一批功能的想法

初稿，不是定案。

---

## 0. 一条贯穿始终的产品定位

**tf 不改用户的配置文件，只做「每次启动时的进程内注入」。**

这是和 CC-Switch 的根本分工：CC-Switch 是配置管理器，把供应商写进 `~/.claude/settings.json`、`~/.codex/config.toml` 并持久化；tf 是启动器，env + CLI flag + 进程私有临时文件，**退出后什么都不留**。

它带来两个直接结论：

- 用户可以同时用 CC-Switch（日常）和 tf（临时切分组/切模型跑一次），互不打架。
- 但两者**会在同一进程里冲突**（CC-Switch 写的配置可能压过注入值），所以 `doctor` 是必需品而不是加分项。

顺带一个几乎零成本的能力：`tf run -- <任意命令>`，把同一套注入喂给任意进程（`tf run -- python eval.py`、`tf run -- curl ...`）。适配表已经有了，多这一个子命令等于白送。

---

## 1. 优先级排序

| 优先 | 功能 | 理由 |
|---|---|---|
| P0 | `claude` / `codex` / `opencode` 三个 harness | 覆盖 TokenDocs 里绝大部分教程流量 |
| P0 | 分组协议预检 | **差异化价值所在**，把线上 403 提前到本地 |
| P1 | `doctor` | 支持成本大头；CC-Switch 残留、旧端点、已存凭据 |
| P1 | `models` / `status` | 依赖的数据在预检时已经拉了，顺手做 |
| P2 | `run --` / `hermes` / 更多 harness | 适配表已在，加行即可 |
| P2 | `--json` 全量覆盖 | 有了就能被别的 agent 调用 |

**不做**：自己的聊天 REPL、自己的 agent loop。ori 有 `ori code` 那一整套 runtime，那是 OpenRouter 想做 harness 本身，我们没有理由跟。

---

## 2. 核心抽象：一张数据驱动的 harness 适配表

一个 harness 就是一行数据，逻辑全共用。字段：

```
kind            claude | codex | opencode | hermes | ...
bin             可执行名 + 探测路径 + 安装命令 + 文档 URL
protocol        anthropic | openai-chat | openai-responses | gemini
                ↑ 同时决定 base URL 形态和「Key 分组必须支持哪种格式」
baseURL(host)   anthropic → <host>          openai → <host>/v1
inject          env（分 locked / overridable）、CLI args、配置文件三选多
clear           必须清空的竞争变量
modelId(id)     模型 ID 变换（provider 前缀、复合 Key 前缀）
effort          7 档 → 本家表达；不支持则明确报错
conflicts       会压过注入值的已存凭据位置
headers         能否注入自定义 header（决定能不能带 session id）
```

### 三行具体的（依据 TokenDocs 现有教程 + ori 逆向）

**claude**（protocol: anthropic）
```
locked:      ANTHROPIC_BASE_URL=<host>
             ANTHROPIC_AUTH_TOKEN=<key>
             ANTHROPIC_API_KEY=（清空，否则两个凭据打架）
overridable: CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1（教程已有）
clear:       ANTHROPIC_{BEDROCK,VERTEX,FOUNDRY,GOOGLE_CLOUD,AWS}_BASE_URL
             CLAUDE_CODE_USE_{BEDROCK,VERTEX,GATEWAY,FOUNDRY,MANTLE}
             CLAUDE_CODE_OAUTH_TOKEN、ANTHROPIC_UNIX_SOCKET
model:       ANTHROPIC_MODEL / ANTHROPIC_DEFAULT_{HAIKU,SONNET,OPUS}_MODEL
headers:     ANTHROPIC_CUSTOM_HEADERS
settings:    写进程私有的 <cwd>/.tf/claude-<pid>.json 用 --settings 传入，
             用户自己传了 --settings/--setting-sources 时让路，24h 前的残留自动清理
```
待验证：`ENABLE_TOOL_SEARCH` 在 TokenFlux 的 Anthropic 分组下是否生效（上游是真 Anthropic 账号，理论上可行；能省近一半系统提示词 token，值得实测）。

**codex**（protocol: `openai_responses`，可降级到 `openai_chat_completions`；降级由分组能力自动决定，见第 3 节）
```
全部走 -c 覆盖，不碰用户的 config.toml：
  -c model_provider=tokenflux
  -c model_providers.tokenflux.name='TokenFlux'
  -c model_providers.tokenflux.base_url='<host>/v1'
  -c model_providers.tokenflux.wire_api='responses' | 'chat'
     ↑ 分组的 allowed_client_protocols 含 openai_responses 就用 responses，
       否则含 openai_chat_completions 就降级 chat，两者都没有则本地拦下
  -c model_providers.tokenflux.env_http_headers.X-Session-Id='TF_SESSION_ID'
  凭据两条路：env_key='TOKENFLUX_API_KEY'（简单）
            或 auth.command='sh' auth.args=['-c','echo $TOKENFLUX_API_KEY']（ori 的做法，更通用）
  [-m <model>] [-c model_reasoning_effort=<effort>]
```
注意教程目前教用户写 `auth.json` + `requires_openai_auth=true`，那是持久化方案；tf 用 `-c` 就完全不落盘。`wire_api` 必须能按分组降级：TokenRouter 文档明确 `preserve_client_protocol` 下 Chat 请求只走 `/v1/chat/completions`，Anthropic 格式分组接 responses 是另一条转换路径，要实测。

**opencode**（protocol: openai-chat）
```
OPENCODE_CONFIG_CONTENT = {"provider":{"tokenflux":{...baseURL:"<host>/v1", apiKey, headers}}}
（用户已设该变量则不覆盖，只警告）
model: tokenflux/<id> 或沿用教程的内置 openai provider
effort: --variant
conflicts: ~/.local/share/opencode/auth.json 里的同名 provider key 会压过注入值 → 警告
```

### 顺带白送的能力：会话粘性

三个 harness 都能注入自定义 header。每次启动生成一个 session id 打进 `X-Session-Id`，就同时得到：
- TokenRouter 侧的**账号粘性**（同一会话稳定命中同一上游账号，减少 context 抖动）
- 用量归因（`tf history` 能把一次启动和网关侧用量对上）

这是 ori 也做了的事，但对 TokenRouter 的价值更大，因为粘性调度本来就是它的核心能力之一。

---

## 3. 分组协议预检

**问题**：Key 绑错分组，线上返回 403 `This group does not allow ... requests`。用户在终端看到的是 harness 转述后的模糊报错，得去翻文档才知道是分组选错了。这是**纯粹的信息缺失**，而信息在本地就拿得到。

### 3.1 准确的能力模型：协议是一个集合，不是「一种格式」

查了 `docs/interfaces/http_api.md#分组客户端协议` 和 `dto.Group`，实际模型是：

- Group 有 `platform`（上游平台）和 **`allowed_client_protocols`（客户端文本协议集合）**，两者**独立准入**。
- 集合取值固定这四个，且返回顺序固定：
  `anthropic_messages` / `openai_responses` / `openai_chat_completions` / `gemini_generate_content`
- **可以只支持其中一种，也可以支持两三种**；创建时省略用平台默认，切换平台时只保留两平台的交集。
- **空数组是合法的**：所有平台都接受显式空集合，CLI 必须能优雅处理「这个分组不支持任何文本协议」。
- `allow_messages_dispatch` 是**弃用字段**，值由新集合派生。CLI 只读 `allowed_client_protocols`。
- 好消息：这个字段在**公开 Group DTO** 里就有，`GET /api/v1/groups/available` 直接能拿到，不需要管理员权限。

所以适配表里 harness 那一栏不是「一种协议」，而是**一个带优先级的候选列表**：

| harness | 首选 | 可降级到 | 都没有时 |
|---|---|---|---|
| claude | `anthropic_messages` | 无 | 本地拦下 |
| codex | `openai_responses` | **无**（官方已移除 `wire_api="chat"`）| 本地拦下 |
| opencode | `openai_chat_completions` | `openai_responses`（待确认其 AI SDK provider 走哪条）| 本地拦下 |
| hermes | `openai_chat_completions` | — | 本地拦下 |

**关键推论：`openai_chat_completions` 和 `openai_responses` 不能笼统当成「OpenAI 格式」。** 一个只开 `openai_chat_completions` 的分组，opencode / hermes / Cherry Studio 都能跑，**但 `tf codex` 跑不了**。这正是预检最该抓的一类错配：分组名字写着「OpenAI格式」，用户很自然会以为 codex 能用。

### 3.2 复合 Key：检查粒度是「模型解析后的最终分组」

文档写得很明确：准入用的是**认证后最终选中的分组**；复合 Key 要先解析正文才知道打到哪个子分组。所以预检不能按 Key 做，得按 `(模型 → 最终分组)` 做：

- 普通 Key：一个分组，直接比。
- 复合 Key：先按模型前缀解析出目标分组，再比该分组的集合。同一把 Key 里，`claude-*` 走的分组可能支持 `anthropic_messages`，`gpt-*` 走的分组只支持 `openai_chat_completions`：**同一把 Key 对 `tf claude` 可用、对 `tf codex` 也可用，但换个模型就不一定**。这个组合关系必须在 `tf models` 里显式展示出来。

### 3.3 预检只能证伪，不能证真（措辞要诚实）

文档明说：文本协议开关**不扩展** Live / WebSocket / Embedding / 图片 / 视频能力，**也不会绕过账号 endpoint capability 等更窄的限制**。

所以预检的定位是「**能确定失败的，提前失败**」，不是「通过了就一定能跑」。这决定了两件事：

- 通过时**不要给任何「配置正确」的正面承诺**，静默放行即可。
- 失败时的文案要说清是**分组协议准入**这一层不匹配，避免用户在真正原因是账号能力时被误导。

### 3.4 具体做法

1. login 时把分组信息一并落盘（`GET /api/v1/groups/available`、`GET /api/v1/keys`），带 TTL 缓存。
2. 启动前比对：harness 的候选协议列表 ∩ 目标分组的 `allowed_client_protocols`。
   - 交集非空 → 取优先级最高的那个，**并据此决定注入参数**（codex 的 `wire_api`、base URL 形态）。
   - 交集为空 → 本地拦下。
3. 拦下时给出可执行的修复路径：

```
tf claude 需要 anthropic_messages 协议。
当前 Key「macbook」→ 分组「DeepSeek（OpenAI格式）」只开放：
  openai_responses, openai_chat_completions

可选：
  · 换分组：tf use-group "Claude（Anthropic格式）"
  · 换 harness：这个分组可以跑 tf codex / tf opencode
  · 在 https://tokenflux.dev/keys 新建一把绑定 Anthropic 分组的 Key
  · 开启复合 Key，一把 Key 绑多个分组，用模型前缀切换
```

第二条「换 harness」是反向建议：既然本地已经知道这个分组开放了哪些协议，就顺手告诉用户**手上这把 Key 能跑什么**，而不只是说它不能跑什么。

4. 新增 `tf groups`：直接把矩阵摊开给用户看，一屏解决所有困惑。

```
分组                         anth_msg  oai_resp  oai_chat  gemini   可用 harness
Claude（Anthropic格式）         ✓         ·         ·        ·      claude
DeepSeek（OpenAI格式）          ·         ✓         ✓        ·      codex opencode hermes
Gemini                          ·         ·         ·        ✓      （暂无适配）
```

5. 复合 Key 的**模型前缀自动补全**：用户写 `--model deepseek-v3`，tf 知道这把 Key 是复合的、该模型属于哪个分组，自动补成 `<prefix>/deepseek-v3`。这是另一个高频踩坑点。
6. `--model` 和缓存的 `/v1/models` 对一遍，模型名打错在本地就报，并给出最接近的候选。

预检要能一键跳过（`--no-precheck`），缓存过期或网络不通时**降级为放行 + 提示**，绝不因为预检本身失败而挡住启动。

---

## 4. 模型 ID 有三重变换，必须在一个地方收口

同一个「模型」在四个层面有四种写法，这是最容易写乱的部分：

```
用户输入        deepseek-v3
复合 Key 前缀   ds/deepseek-v3          ← 按 Key 的分组绑定决定
Key 的 model_mapping                    ← 服务端重定向，本地只需知道它存在（诊断用）
harness 前缀    tokenflux/ds/deepseek-v3（opencode）/ 原样（claude、codex）
```

建议做成一个纯函数 `resolveModel(userInput, key, harness) -> {sent, display, warnings}`，所有 harness 共用，单测覆盖。`tf models` 直接把四种写法都列出来，用户一眼看清。

---

## 5. doctor 要检测什么

按「实际会导致启动后行为不对」排序：

1. **CC-Switch 写入的配置**（官方推荐它，所以最常见）：`~/.claude/settings.json` 里的 env、`~/.codex/config.toml` 里的 `model_provider`。
2. **harness 自己存的凭据**优先级高于注入值：opencode `auth.json`、codex `auth.json`、hermes `~/.hermes/.env`。
3. **废弃端点** `token.memoh.net` 出现在任何配置里 → 直接给迁移指引。
4. **环境里残留的竞争变量**：`ANTHROPIC_API_KEY`、`OPENAI_BASE_URL`、代理变量（`HTTPS_PROXY` 指向失效代理是排障常客）。
5. **连通性**：对 `<host>/v1/models` 发一个请求，区分「Cloudflare 质询 / DNS / 代理 / 401 / 402 余额不足」，每种给不同的下一步。

输出保持 ori 的分级（conflict / warning / note），`--json` 可被其它 agent 消费。

---

## 6. status / models

数据都在 login 和预检时拉过了，成本很低：

- `tf status`：余额、订阅、今日消耗、当前 profile/host/分组/Key 名。
- `tf models`：`/v1/models` + 分组可用性 + 四种模型写法 + 价格（`/api/v1/groups/rates`）。
- 两者都默认人类可读、管道时输出 JSON。

---

## 7. 开工前建议先实测的项

这几条会实质影响适配表，不实测就是猜：

1. **`ENABLE_TOOL_SEARCH` 在 TokenFlux 的 Anthropic 分组能不能用**（省 token，收益大）。
2. **`allowed_client_protocols` 通过、但账号 endpoint capability 更窄**时，网关实际返回什么错误。这决定 `doctor` 怎么把这类失败和「分组协议不匹配」区分开。
3. **`X-Session-Id` 打到网关后，粘性调度和用量归因是否真的按预期生效**（这条要问后端确认字段名，可能不是 `X-Session-Id`）。

（原来的「codex wire_api 兼容边界」不用测了，读 `allowed_client_protocols` 即可确定。）

三条都可以做成 `tf harness test` 的契约用例，以后回归也用它。

## 给后端的诉求（按价值排序）

1. **`/v1/responses` 的保活改用 SSE 注释行。** 现在伪造
   `response.output_text.delta`（`item_id: "SSE-Keep-Alive"`），
   AI SDK 会因为该 id 从未宣告而抛错，opencode 约 1/4 的请求整轮失败。
   改成 `: keepalive` 即可，那是协议为此保留的形式。

2. **`/v1/models` 给出分组。** 现在连 `owned_by` 都没有，
   客户端只能靠模型集合反推分组，且 Grok 三档模型完全相同、推不出来。

3. **`/api/v1/marketplace/models` 加 `allowed_client_protocols`。**
   现在 tf 只能靠发探针、读拒绝文案来反推，文案一改就失灵。
   `claude_code_only` 同理，那本该是一个字段。
