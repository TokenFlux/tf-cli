# 不要全局模式：绑定属于 harness

## 病灶

`tkr use` 引入了一个全局可变模式：一个看不见的「当前 profile」，静默决定之后
每一条命令用哪把 Key。

这违反 tkr 自己的产品理念。我们当初正是因为「不做持久化模式切换」才和 CC-Switch
划清界限（见 product-decisions.md），结果又把同一个模型搬进了 profile 层。

实际后果已经发生：用户 login 后存进 `gpt`，当前 profile 仍是 `default`，
下一条 `tkr codex` 继续用旧 Key，列出一堆 claude 模型且毫无提示。

修掉那次遗漏不解决问题。**只要存在隐藏的全局模式，就永远存在「我以为在 A 实际在 B」。**

## 更根本的一点

这个选择本来不该由用户做。

用户的心智是「codex 用我的 gpt key，claude 用我的 Claude Max key」——
绑定关系天然属于 harness，不属于某个全局状态。

而且**哪把 Key 能跑 codex，tkr 是知道的**：协议准入 + 模型列表都拿得到。
既然知道，就不该让用户去管。

`tkr codex` 列出 claude 模型，根因不是 profile 选错，是**那把 Key 压根不该出现在
codex 的候选里**。

## 新模型

凭据不再叫 profile，就叫 Key，有一个标签。绑定关系放进 harness：

```json
{
  "harnesses": {
    "codex":    { "key": "gpt",    "slots": { "default": "gpt-5.6-sol" } },
    "opencode": { "key": "gpt",    "slots": { ... } },
    "claude":   { "key": "max",    "slots": { ... } }
  }
}
```

没有 `current`，因此没有「当前模式」可言。

## 启动时如何定 Key

```
候选 = 所有 Key ∩ 支持该 harness 所需协议
  0 个 → 报错，说明每把 Key 差在哪，并给出可行动的下一步
  1 个 → 直接用，不提问
  n 个 → 首次询问一次，记进该 harness 的绑定；之后不再问
```

`-k/--key <标签>` 本次覆盖，不写盘。绑定失效（Key 被删）时重新走上面的流程。

关键性质：**用户不可能「用错 Key」**，因为不能用的 Key 根本不会出现在候选里。
这类 bug 从「需要小心避免」变成「结构上不可能」。

## 能力从哪来

`allowed_client_protocols` 只有 JWT 能读（见 tokenflux-api-probe.md），
但协议准入可以零成本探测：

| 请求 | 结果 | 含义 |
| --- | --- | --- |
| 空 body 打各协议入口 | `400 model is required` | 该协议准入通过 |
| 同上 | `403 does not allow ... requests` | 该协议不准入 |

准入检查发生在读 body 之前，所以**不消耗任何 token**。login 时顺带探 3 次，
结果连同模型列表一起存进凭据记录。启动时只读本地，零网络。

## 边界

- 探测结果**只能证伪**：账号级 endpoint capability 可能更窄，通过不代表一定能跑。
  因此候选为空时才拦，候选非空时静默放行，绝不给「配置正确」的承诺。
- 能力会变（用户改分组），所以带 TTL，并在启动失败时主动失效重探。
- `tkr keys` 展示每把 Key 的协议矩阵与可用 harness，替代 `tkr use` 的查看职能。

## 处置

- 删除 `tkr use`。它的「切换」职能没了，「查看」职能归 `tkr keys`。
- `--profile` 更名 `--key`，语义从「切换模式」变成「本次用哪把」。
- `config.current` 迁移为各 harness 的初始绑定。
