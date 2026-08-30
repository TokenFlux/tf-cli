# TokenFlux 生产环境实测记录

2026-08-29，对 `https://tokenflux.dev` 的实测。用于校正 tf 的预检与适配设计。
（测试用的 API Key 与 JWT 不记录在本文件中。）

---

## 1. 三种身份各能读到什么

| 端点 | 匿名 | API Key | JWT | 内容 |
|---|---|---|---|---|
| `GET /api/v1/marketplace/models` | **200** | 200 | 200 | **分组目录 + 模型 + 完整定价 + 实时容量 + 可用率** |
| `GET /api/v1/settings/public` | 200 | — | — | 站点开关 |
| `GET /v1/models` | 401 | **200** | — | 该 Key 可见的模型列表 |
| `GET /api/v1/groups/available` | — | 401 `INVALID_TOKEN` | **200** | **含 `allowed_client_protocols`** |
| `GET /api/v1/keys` | — | 401 `INVALID_TOKEN` | **200** | Key + 嵌套的完整 group 对象 |
| `GET /api/v1/user/profile` | — | 401 `INVALID_TOKEN` | 200 | 用户信息 |
| `/api/v1/marketplace/groups`、`/api/v1/groups`、`/api/v1/public/groups` | 404 | | | 不存在 |

### 两条关键结论

**（1）公开端点能拿到目录、价格、容量、可用率，但拿不到协议。**

`marketplace/models` 里 `protocol` / `allowed_client_protocols` / `claude_code_only` 的出现次数均为 **0**。

**（2）API Key 无法自省自己的分组能力。** 只能读 `/v1/models`。

推论：

- `tf models` / `tf groups` **完全未登录就能工作**——`pnpx tf models` 开箱即用，首次体验很好。
- 但**协议准入仍只能靠 JWT 或主动探测**（§3）。
- v0.5 的「导入 tf」按钮价值因此被大幅抬高：页面持有 JWT，能把 Key **连同完整分组元数据（含协议集合）**一起推给 CLI。它不再只是「省一次粘贴」，而是**唯一能一次性拿齐预检数据的路径**。

### 公开 marketplace 端点的内容（意外丰富）

结构：`data[]` = 12 个上架分组，每个带 `models[]`。

```
分组层  id, name, platform, display_brand, rate_multiplier,
         official_price_ratio（相对官方价的比例，ChatGPT = 0.0358）,
         official_price_rmb_equivalent, data_sharing_enabled, model_count,
         capacity  { concurrency_used/max, sessions_used/max, rpm_used/max },
         availability { window_days:3, availability_rate, last_status,
                        last_checked_at, days[] 逐小时成功率 }
模型层  id, display_name, input_modalities, output_modalities,
         pricing { pricing_mode, price_status,
                   context_intervals[] 分段上下文定价，每段含
                     input/output/cache_read 与 fast_* 通道单价 }
```

直接可用的产品机会：

- **比价**：同一个模型在多个分组里时，`tf models <id>` 直接列出哪个分组更便宜（分段价 + 倍率）。
- **健康度**：`tf groups` 显示「Claude Max 当前并发 15/310、3 天可用率 99.2%」——零成本、网页上也不一眼可见。
- **离线模型→分组映射**：复合 Key 前缀补全、模型名纠错、「这个模型在哪些分组有」都能本地算。

⚠️ **marketplace 不等于可用集合**：公开 12 个 vs JWT 下 `groups/available` 14 个（少了「百炼（Anthropic格式）」和「Grok (Super Grok)」）。marketplace 是**上架展示子集**，CLI 不能拿它当权威可用列表，文案上要区分「市场上有」和「你能用」。

---

## 2. 分组能力：生产返回的真实矩阵

`GET /api/v1/groups/available?scope=personal`（JWT）确认返回 `allowed_client_protocols`。个人可见 14 个分组，节选：

| 分组 | platform | 协议集合 | 倍率 | claude_code_only |
|---|---|---|---|---|
| ChatGPT | openai | anth_msg + oai_resp + oai_chat | 4 | |
| ChatGPT Image | openai | oai_resp + oai_chat | 1 | |
| **Claude Max** | anthropic | anth_msg + oai_resp + oai_chat | 20 | **true** |
| Claude Antigravity | anthropic | anth_msg + oai_resp + oai_chat | 10 | |
| Claude Kiro | anthropic | anth_msg + oai_resp + oai_chat | 5 | |
| **Google** | **anthropic** | anth_msg + oai_resp + oai_chat | 8 | |
| Google Image | gemini | **仅 gemini** | 1 | |
| DeepSeek（OpenAI格式） | openai | oai_resp + oai_chat | 50 | |
| **DeepSeek（Anthropic格式）** | anthropic | **仅 anth_msg** | 50 | |
| 百炼（OpenAI格式） | openai | oai_resp + oai_chat | 50 | |
| 百炼（Anthropic格式） | anthropic | **仅 anth_msg** | 50 | |
| Grok (Heavy / Super / Free) | grok | anth_msg + oai_resp + oai_chat | 3.6 / 3.3 / 0.8 | |

### 直接结论

1. **协议集合的差异是真实存在的、且分布很广**，不是理论情况：
   - 只有 `anthropic_messages` 的分组（DeepSeek/百炼 Anthropic 格式）→ `tf codex`、`tf opencode`、`tf hermes` **全部不可用**
   - 没有 `anthropic_messages` 的分组（DeepSeek/百炼 OpenAI 格式、ChatGPT Image）→ `tf claude` **不可用**
   - `Google Image` 只有 `gemini_generate_content` → **目前所有 harness 都不可用**
2. **分组名字完全不能用来推断能力**：叫「Google」的分组，`platform` 是 **anthropic**。这一条本身就足以证明「本地预检」比「让用户看文档猜」有价值。
3. **倍率差异极大**（0.8 ~ 50），`tf groups` / `tf models` 展示倍率是高价值信息。

### 意料之外的字段（原设计完全没覆盖）

- **`claude_code_only: true`** —— 真实存在（Claude Max，倍率 20）。分组可以限定只给 Claude Code 用。
  - 预检必须加这一维：非 claude harness 选到这类分组要本地拦下。
  - 反过来这是 tf 的卖点：`tf claude` 是用上这类高价值分组的正规方式。
  - 待确认：网关靠什么识别 Claude Code（UA？特定 header？）——tf 启动的是真 `claude` 二进制，理论上天然满足，但要实测确认注入的 header 不会破坏识别。
- **`max_reasoning_effort` / `reasoning_effort_mappings`** —— 字段已存在（当前值为空）。说明 effort 是**三层**而不是两层：
  ```
  用户输入 → harness 能力表 → 分组上限/映射 → 最终请求
  ```
  设计要给第三层留位置。
- **`fallback_group_id` / `fallback_group_id_on_invalid_request` / `unavailable_fallback_group_id`** —— 分组可配置故障转移到别的分组。**实际执行的分组可能不是选中的那个**，这是「预检只能证伪、不能证真」的又一条硬证据。
- `require_oauth_only`、`require_privacy_set`、`data_sharing_enabled`、`session_isolation_enabled`、`allow_live`、`allow_image_generation`、`rpm_limit`、各类价格字段。

---

## 3. 零成本协议探测（v0 只有 Key 时的方案）

协议准入检查发生在**读取请求正文之前**，所以可以用一个必然失败的请求探测能力，**不产生任何 token 消费**：

| 请求 | 结果 | 含义 |
|---|---|---|
| `POST /v1/chat/completions` body `{}` | 400 `model is required` | 协议**准入通过** |
| `POST /v1/responses` body `{}` | 400 `model is required` | 协议**准入通过** |
| `POST /v1/messages` body `{}` | 400 `model is required` | 协议**准入通过** |
| `POST /v1beta/models/x:generateContent` body `{}` | **403** `This group does not allow Gemini GenerateContent requests` | 协议**不准入** |

Gemini 那条用空 body 也能拿到 403，证明准入确实在 body 解析之前完成。

**探测规则**：`403 + "does not allow ... requests"` → 不准入；其它任何状态码 → 准入通过。

复合 Key 需要先解析正文才能定分组，所以探测时要带上真实模型名（仍然可以不带 `messages`，一样零成本）。

---

## 4. 错误分类（doctor 与预检的映射依据）

实测拿到的确切形状，**必须按 message 分类，不能只看状态码**：

| 场景 | 状态 | 响应 |
|---|---|---|
| 协议不准入 | 403 | `{"error":{"code":403,"message":"This group does not allow Gemini GenerateContent requests","status":"PERMISSION_DENIED"}}` |
| 模型不在分组 | 403 | `The current group does not support the requested model "xxx". Available models: gpt-5.4, gpt-5.5, ...` |
| API Key 无效 | 401 | `{"code":"INVALID_API_KEY","message":"Invalid API key"}` |
| JWT 无效/用 Key 调管理接口 | 401 | `{"code":"INVALID_TOKEN","message":"Invalid token"}` |
| 请求体缺字段 | 400 | `{"error":{"message":"model is required","type":"invalid_request_error"}}` |

两个高价值细节：

- **两种 403 语义完全不同**（协议层 vs 模型层），CLI 必须分开报，否则用户会去改错东西。
- **「模型不在分组」的 403 直接带 available models 列表** → CLI 可以解析出来，立刻给出「你可以用这些模型」的建议，甚至做拼写纠正。
- 两个 401 错误码不同（`INVALID_API_KEY` vs `INVALID_TOKEN`），可用于区分「Key 错了」和「Key 用错了地方」。

---

## 5. 协议连通性实测（某 OpenAI 平台分组）

同一把 Key（分组三种协议全开）：

| 端点 | 结果 |
|---|---|
| `POST /v1/responses` | 200 |
| `POST /v1/chat/completions` | 200 |
| `POST /v1/messages` | 200，返回标准 Anthropic 形状（`content[].text`、`stop_reason`、`usage.cache_*`）|

所以 **opencode 走 responses 没问题**（只要分组开了 `openai_responses`），保留 chat 作为回退即可。

另外两个观察：

- `/v1/responses` 的返回里带 `instructions`，内容是 Codex 的系统提示词 → 上游是 Codex 订阅型账号，网关做了注入。
- 给 `/v1/messages` 发只带 `model` 的请求，报错却是 OpenAI 风格的 `Missing required parameter: 'input'` → 请求被归一化成上游形状后才校验，**上游协议形状会泄漏到错误信息里**。doctor 要能识别这类「答非所问」的报错，否则用户会被带偏。

---

## 6. Key DTO 里对 tf 有用的字段

`GET /api/v1/keys`（JWT）返回，本账号 20 把 Key：

```
key（明文）, name, group_id, is_composite, composite_groups, model_mapping,
group（嵌套的完整分组对象，含 allowed_client_protocols）,
scope, team_id, status, billing_mode, preferred_subscription_id,
quota, quota_used, rate_limit_{5h,1d,7d}, usage_{5h,1d,7d}, window_{5h,1d,7d}_start,
current_concurrency, expires_at, fallback_to_default_group_when_unavailable,
data_sharing_confirmed_group_id, data_sharing_notice_version, ip_whitelist, ip_blacklist
```

**关键**：Key 对象里**嵌套了完整的 group 对象**。所以「导入 tf」按钮只要推送这一个对象，CLI 就拿到了预检所需的全部信息，不需要再拼第二个接口。

`usage_{5h,1d,7d}` + `window_*_start` + `current_concurrency` 让 `tf status` 能显示「限速窗口还剩多少、什么时候重置」，这是网页上也不容易一眼看到的信息。
