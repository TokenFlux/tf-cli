# 分组识别：Key 不自报，只能反推

> **归档。** 本文记录的是已放弃的模型集合指纹方案。当前实现不猜分组，
> 而是按 Key + 模型前缀记录协议探测结果；后端补 `owned_by` 仍是正确诉求。
> 现行状态见 [`../STATUS.md`](../STATUS.md)。

## 事实

`/v1/models` 用 API Key 能读，但**完全不含分组信息**，连 OpenAI 标准的
`owned_by` 字段都没有，而那本是放分组名的天然位置：

```json
{"id": "gpt-5.4", "type": "model", "display_name": "gpt-5.4", "created_at": "..."}
```

`/api/v1/groups/available`、`/api/v1/keys` 能给出分组，但需要用户 JWT。
API Key 拿不到。

于是「这把 Key 属于哪个分组」在客户端只能**推断**，不能查询。

## 为什么非知道不可

分组决定倍率，而倍率差异巨大：

| 同一批模型 | 分组 | 倍率 |
| --- | --- | --- |
| claude-opus-5 等 | Claude Max | ×20 |
| 同上（少一个 fable） | Claude Kiro | ×5 |
| grok-4.5 / 4.6 | Grok (Heavy) | ×3.6 |
| 同上 | Grok (Super Grok) | ×3.3 |
| 同上 | Grok (Free) | ×0.8 |

不知道分组就不能显示价格。而**显示错的价格比不显示更糟**。

## 推断算法

数据来自公开免鉴权的 `/api/v1/marketplace/models`（13 个上架分组，含每组模型列表）。

```
1. 精确匹配：Key 的模型集合 == 某分组的模型集合
2. 否则子集匹配：Key 的模型集合 ⊆ 分组的模型集合
3. 用探测到的协议筛掉平台不符的候选
4. 唯一 → 推定（inferred）
   多个 → 问用户一次，记住（confirmed）
   零个 → 未知（分组未上架 marketplace，或自托管 TokenRouter）
```

复合 Key 逐前缀做同样的事：每个前缀是一个独立分组。

## 实测的可分性（13 个分组）

- 模型集合唯一的：10 个 → 精确匹配即可定
- `DeepSeek（OpenAI格式）` vs `DeepSeek（Anthropic格式）`：模型相同、倍率相同（都 ×50），
  但 platform 不同 → **协议探测可分**
- `Grok (Heavy / Super Grok / Free)`：模型相同、平台相同，只有倍率不同（3.6 / 3.3 / 0.8）
  → **任何探测都分不了，只能问用户**

`Claude Max` 与 `Claude Kiro` 仅差一个 `claude-fable-5`。能分，但余量只有一个模型：
若 Key 被限制了模型范围，就会退化成歧义类。

## 诚实规则

- 推定与确认必须在界面上**区分**：推定的价格标注为推定，不能冒充事实
- 歧义时**绝不静默取第一个**，倍率可能差 4.5 倍
- 分组不在 marketplace（自托管、未上架）时显示「未知」，不猜
- 推断结果可被用户覆盖，并持久化为 confirmed

## 正确的后端诉求（优先级最高）

**把分组放进 `/v1/models` 的 `owned_by` 字段。**

- 是 OpenAI 标准字段，响应形状不变，客户端零成本
- 不新增端点、不改鉴权
- 一个字段就能让上面整套推断子系统作废

次优先：`/api/v1/marketplace/models` 增加 `allowed_client_protocols`
（可省掉零成本探测），以及一个 API Key 可读的 `whoami`。
