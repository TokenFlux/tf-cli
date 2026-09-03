# 不要全局模式：绑定属于 harness

## 病灶

`tf use` 引入了一个全局可变模式：一个看不见的「当前 profile」，静默决定之后
每一条命令用哪把 Key。

这违反 tf 自己的产品理念。我们当初正是因为「不做持久化模式切换」才和 CC-Switch
划清界限（见历史提案 [`../archive/product-decisions.md`](../archive/product-decisions.md)），结果又把同一个模型搬进了 profile 层。

实际后果已经发生：用户 login 后存进 `gpt`，当前 profile 仍是 `default`，
下一条 `tf codex` 继续用旧 Key，列出一堆 claude 模型且毫无提示。

修掉那次遗漏不解决问题。**只要存在隐藏的全局模式，就永远存在「我以为在 A 实际在 B」。**

## 更根本的一点

这个选择本来不该由用户做。

用户的心智是「codex 用我的 gpt key，claude 用我的 Claude Max key」；
绑定关系天然属于 harness，不属于某个全局状态。

而且**哪把 Key 能跑 codex，tf 是知道的**：协议准入 + 模型列表都拿得到。
既然知道，就不该让用户去管。

`tf codex` 列出 claude 模型，根因不是 profile 选错，是**那把 Key 压根不该出现在
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

| 探针结果 | 含义 |
| --- | --- |
| `400` 参数校验，或明确的 model miss（如 `requested model` / `model_not_found`） | 已越过协议与分组准入，记为通过 |
| `403 ...does not allow ... requests` | 该协议不准入 |
| `403 ...Claude Code client...` | 分组锁定真实 Claude Code 客户端，单独记录 |
| `429`、网络错误或未知响应 | 本次无结论，保留已有结果 |

准入检查发生在模型调度之前，所以**不消耗任何 token**。login 时按分组前缀探测三个协议入口，
结果连同模型列表一起存进 `config.json` 的 `KeyMeta`。正常启动只读本地；遇到失败时才触发重探。

## 边界

- 探测结果**只能证伪**：账号级 endpoint capability 可能更窄，通过不代表一定能跑。
  因此候选为空时才拦，候选非空时静默放行，绝不给「配置正确」的承诺。
- 能力会变（用户改分组）。早期曾计划使用 TTL，现行实现不设 TTL，而是在启动失败时主动重探。
- `tf keys` 展示每把 Key 的协议矩阵与可用 harness，替代 `tf use` 的查看职能。

## 处置

- 删除 `tf use`。它的「切换」职能没了，「查看」职能归 `tf keys`。
- `--profile` 更名 `--key`，语义从「切换模式」变成「本次用哪把」。
- `config.current` 迁移为各 harness 的初始绑定。

## 修正：能力是**分组级**，不是 Key 级

初版把协议准入当成整把 Key 的属性，这对复合 Key 是错的。

复合 Key 一把横跨最多 20 个分组，每个分组有各自的 `allowed_client_protocols`。
同一把 Key 里 `GPT/gpt-5.6-sol` 能跑 codex，`Claude/claude-opus-5` 只能跑 claude：
**同一个 Key 的不同模型，可调的端点不同**。

因此：

- `KeyMeta.Protocols` 是 `map[分组前缀][]协议`，普通 Key 用空串作唯一键
- `Supports`（任一分组允许）用来筛 **Key 候选**
- `SupportsIn`（指定分组允许）用来筛 **模型候选**
- 探测必须逐前缀做：空 body 对复合 Key 无效，网关会先要 `COMPOSITE_KEY_MODEL_PREFIX_REQUIRED`。
  改为发 `{"model":"<前缀>/__tf_probe__"}`：前缀决定分组，模型不存在则请求
  在调度前就被拒，同样零 token。
- 403 有两种含义，必须按文案区分：`does not allow ... requests` 是协议不准入，
  其余 403 只是模型不在分组里。只看状态码会把「模型选错」误判成「协议不通」。

## 顺带合并的真相源

模型列表原本存两份：config.json 的 `keys.<name>.models` 和一份独立缓存文件。
两份数据必然打架，而且缓存要按 Key 分开存、分错就串味（已经发生过一次）。

现在只保留 config.json 里那一份。补全直接读它，并按该 harness 的绑定与协议过滤；
补全不该提示一个选了就会 403 的模型。

## 复查：删掉的东西

一轮自审后删除的（合计约 600 行）：

| 删除项 | 为什么它不该存在 |
| --- | --- |
| `internal/catalog` 分组推断 | 靠模型集合指纹反推分组。Claude Max 与 Kiro 只差一个模型，TokenFlux 给任一分组加一个模型，识别就静默失效。用易碎子系统换「显示倍率」这个锦上添花，不值。**正确解法是后端在 `/v1/models` 的 `owned_by` 里给出分组**。 |
| `KeyMeta.UnmarshalJSON` 兼容层 | 为自己一小时前发明的格式写向后兼容。那个格式只活过 40 分钟，只存在于测试目录。 |
| `Config.migrate` | 未发布软件的配置迁移。没有用户。 |
| `internal/config/cache.go` | 模型列表合并进 config 后整个文件无人调用。 |
| `gateway.Reachable` / `model.IsComposite` / `Family.HasVariants` | 「顺手先写上」的导出函数，零调用者。 |
| `CodeConfigPerm` / `CodeProfileNotFound` | 零使用的错误码。 |
| `tf config show` | 与 `tf keys` 有 80% 重叠。同一份信息两处维护，必然走样。`config` 只回答「东西在哪」。 |

## 复查：修正的设计缺陷

**探测结果过期会让 Key 凭空消失。** 用户在网页上改了分组绑定后，
缓存的「不支持」使一把现在可用的 Key 不再出现在候选里，且没有任何解释。

修法是在**失败路径上**重探，而不是设 TTL：顺利时零开销，出事时自愈。

**复合 Key 的能力展示曾是谎报。** `tf keys` 用「任一分组支持」来回答
「这把 Key 能跑什么」，于是一把 GPT+Claude 的复合 Key 会显示能跑全部三个
harness，而实际上 `GPT/*` 跑不了 claude。现在按分组前缀分行展示。

## 教训

连续三次的模式是一样的：**发现一个数据缺口 → 在客户端建一个推断子系统去补**。
协议不知道就探测，分组不知道就指纹匹配。每个子系统都会过期、都要维护、
都会在上游数据变化时静默失效。

判据应当是：**这个缺口该由谁补？** 能靠上游一个字段解决的，
不要在客户端建一套推断。客户端只做上游给不了的事。
