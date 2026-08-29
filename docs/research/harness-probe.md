# harness 注入配方实测

针对本机实际安装的 harness，逐条验证适配表。测试用 Key 属于 ChatGPT 分组
（模型：gpt-5.4 / gpt-5.5 / gpt-5.6-sol / gpt-5.6-terra / codex-auto-review）。

| harness | 版本 | 状态 |
|---|---|---|
| opencode | 1.18.20 | ✅ 已验证 |
| claude | 未安装 | ⏳ 待验证（含最高风险项 `claude_code_only`）|
| codex | 未安装 | ⏳ 待验证 |

---

## opencode ✅

### 配方（已验证可用）

用 `OPENCODE_CONFIG_CONTENT` 覆盖内置 `openai` provider 的 `baseURL` 与 `apiKey`，
**不写任何配置文件**：

```json
{
  "model": "openai/<主模型>",
  "small_model": "openai/<小模型>",
  "provider": {
    "openai": {
      "options": { "baseURL": "<host>/v1", "apiKey": "<key>" }
    }
  }
}
```

模型 ID 前缀是 `openai/`（内置 provider 名），不是分组名。

### 结论

- **走 `/v1/responses`**，本地探针确认。对应协议准入 `openai_responses`。
  UA：`opencode/1.18.20 (darwin) ai-sdk/provider-utils/4.0.38 runtime/bun/1.3.14`
- `OPENCODE_CONFIG_CONTENT` 注入生效，项目目录**没有**被写入配置文件。
  （opencode 会自建 `.opencode/` 存会话，是它自身行为，与注入无关。）
- 机器上已存在 `~/.local/share/opencode/auth.json` 且含 `openai` provider，
  实测**注入的 `options.apiKey` 优先**，请求成功。冲突不致命，但 doctor 仍应提示。

### ⚠️ 必须同时注入 small_model，否则静默失败

opencode 每轮会用一个「小模型」跑标题生成 agent。不注入 `small_model` 时，
它使用内置默认 `gpt-5.4-nano`，该模型不在分组里：

```
model=gpt-5.4-nano  small=true  agent=title
  → 404 Model "gpt-5.4-nano" is not supported by any configured account in this group
```

**这个失败是静默的**：主对话正常返回，进程退出码 0，用户只会觉得「标题没生成」。
注入 `small_model` 后新增日志里该错误归零。

三条推论：

1. 模型槽设计必须覆盖**所有** harness 会用到的槽位，不只是主模型。
2. 预检要校验全部槽位，只验主模型会漏掉这一类。
3. TokenDocs 的 opencode 教程里 `small_model: openai/gpt-5.6-luna` 在本测试 Key 的分组中同样不存在
   —— 文档给的是固定值，而分组可用模型因人而异。tkr 从分组实际模型列表里选，正好解决这个问题。

---

## 错误形状：同一语义，两种外形

「模型在这个分组里不可用」在不同协议入口下返回**不同的状态码和文案**：

| 入口 | 状态码 | 文案 | 带可用模型列表 |
|---|---|---|---|
| `/v1/chat/completions` | **403** | `The current group does not support the requested model "X". Available models: A, B, C` | ✅ |
| `/v1/responses` | **404** | `Model "X" is not supported by any configured account in this group` | ❌ |

已用「完全不存在的模型」与「目录里有但分组没有的模型」交叉验证，
**差异来自入口而非模型**。

对 M5 错误分类器的要求：

- 不能只按状态码分类，必须按 `(入口, 状态码, 文案)` 三元组。
- 命中 responses 的 404 时，tkr 应自行调 `/v1/models` 补出可用模型列表，
  把一条没法行动的错误变成可行动的。

至此已知的网关错误形状：

| 含义 | 形状 |
|---|---|
| 协议不准入 | 403 `This group does not allow <Protocol> requests`（gemini 入口实测，空 body 即返回）|
| 模型不可用（chat） | 403 `... does not support the requested model ...` + 可用列表 |
| 模型不可用（responses） | 404 `... is not supported by any configured account ...` |
| Key 无效 | 401 `INVALID_API_KEY` |
| JWT 无效 | 401 `INVALID_TOKEN` |
| 请求体不合法 | 400 `model is required` 等（说明协议准入已通过）|

---

## 待验证

需要先安装对应 harness：

- **claude** —— 最高风险项：`claude_code_only` 分组（Claude Max，倍率 20）
  在 tkr 的 exec 路径下能否正常通过 UA + TLS 指纹识别；
  以及 `HTTPS_PROXY` 存在时是否破坏识别。
- **codex** —— `-c` 覆盖 + `env_key` 是否足以完全避免落盘；
  `wire_api` 只剩 `responses` 后与 `openai_responses` 准入的对应关系。
