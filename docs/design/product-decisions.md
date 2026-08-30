# 产品决策

前置：`claude_code_only` 的识别机制已确认为 **UA + TLS 指纹，直连时自动识别**。

## 0. 由此产生的硬约束

tf 只设置环境变量后 `exec` 真正的 harness 二进制，**自己不在请求路径上**。所以 claude 发出的请求，UA 和 TLS 指纹都是它自己的，识别天然成立。由此得到三条硬规则：

1. **tf 永远不做本地代理 / 不 MITM 流量。** 任何"起一个本地 HTTP 代理来注入 header"的方案都会改变 TLS 指纹，直接废掉 `claude_code_only` 分组（Claude Max，倍率 20）。ori 有个 `vault-tunnel` 的 CONNECT 代理，**这个不能学**。
2. **绝不覆盖 harness 的 User-Agent。** tf 自己发的请求（models / groups 查询）才带 `tf/x.y` UA；注入给 harness 的环境里不含任何 UA 覆盖。
3. **`HTTPS_PROXY` 是风险项。** 用户环境里的代理会改变 TLS 指纹 → `doctor` 必须检查：如果目标分组是 `claude_code_only` 且环境里有代理变量，明确警告"这会导致识别失败"。这是一条非常具体、别人想不到的诊断规则。

另外，预检文案要直说：非 claude harness **无法**使用 `claude_code_only` 分组，这是设计如此，不是配置问题。

---

## 1. 自托管实例的 host / profile 形态

### 问题

tf 要同时服务两类目标：TokenFlux（托管，单一权威 host）和 TokenRouter（自托管，任意 host，用户可能有多个：公司网关、个人实例、测试实例）。需要一个模型来回答"我现在指向哪、用哪把 Key"。

### 三种形态

**形态一：单一全局配置**（一个 host + 一个 key）
最简单，切换靠重新 login。缺点：多实例用户反复 login，且无法在两个项目里用不同网关。

**形态二：Profile（推荐）**
命名 profile = `{host, key, 默认模型槽}`，存 `~/.tf/config.json`，一个 `current` 指针。

```
tf login                                  → 写 default profile（指向 tokenflux.dev）
tf login --profile work --host https://gw.corp.internal
tf use work                               → 切换当前
tf --profile work claude                  → 单次覆盖
TKR_PROFILE=work tf claude                → 环境变量覆盖
```

优先级：`--profile` flag > `TKR_PROFILE` > 项目级配置 > 全局 current。

**形态三：项目级绑定**（配合形态二）
仓库里放 `.tf/config.json`，**只存 profile 名，不存 Key**：

```json
{ "profile": "work" }
```

价值：公司项目自动走公司网关，团队共享这个约定，进 git 也无所谓。
安全：**明确禁止内联 Key**。ori 支持把凭据写进 repo 本地的 `.ori/credentials.json`，这个不学：误提交的代价太大，而收益只是省一次 `tf use`。

### 几个实际细节

- **Host 归一化**：用户会输入 `gw.corp.internal`、`https://gw.corp.internal/`、`https://gw.corp.internal/v1` 三种写法。必须统一存成 base，再按协议自己拼（Anthropic 用根路径、OpenAI 用 `/v1`）。这正是 TokenDocs 里用户最常填反的地方，CLI 应该让它变成不可能出错。
- **Host 校验**：`GET /api/v1/settings/public` 是公开端点（实测 200），login 时打一下就能确认"这确实是一个 TokenRouter 实例"，还能拿到站点开关。填错域名当场就报，不用等到第一次请求。
- **自托管的现实**：内网 http、自签证书都会遇到。需要 `--insecure` 逃生阀，但默认拒绝，且每次启动都提示。

### 建议

方向定 Profile，但 **v0 只实现 `default` 单 profile + `--host` 覆盖**，完整 profile 机制放 v0.5。避免在还没有真实多实例用户时过早复杂化。

---

## 2. 默认分组 / 默认模型

### 为什么这件事非做不可

harness 自带的默认模型是**官方模型 id**（`claude-sonnet-4-5` 之类）。网关分组里的 id 不一定同名（实测这个站是 `gpt-5.6-sol`、`gpt-5.4` 这种）。如果 tf 不注入默认模型，harness 会用自带默认发请求，直接撞上：

```
403 The current group does not support the requested model "claude-sonnet-4-5".
    Available models: ...
```

**所以默认模型必须注入，不能依赖 harness 自己的默认。** 这不是体验优化，是能不能启动的问题。

### claude 的特殊性：要三个槽不是一个

Claude Code 有 haiku / sonnet / opus 三档，`/model` 命令会在档位间切换，各档走不同的环境变量（`ANTHROPIC_DEFAULT_*_MODEL`）。只注入一个模型的话，用户一按 `/model` 就坏。

所以 profile 里应该存**模型槽**而不是单个模型：

```json
"models": { "default": "...", "fast": "...", "heavy": "..." }
```

codex / opencode 只用 `default`（opencode 还有 `small_model` 可选）。

### 怎么填这三个槽

1. **自动**：login 后用 marketplace 公开数据（模型列表 + 定价 + 模态）按启发式填：最贵的 → heavy，最便宜的 → fast，`sort_order` 最靠前 → default。
2. **交互确认**：首次启动时把自动选的结果展示一次，让用户确认或改（一次性，之后记住）。
3. **随时改**：`tf config set model.fast <id>`，或 `tf models --set`。
4. **兜底**：分组只有一个模型时（如 ChatGPT Image），三个槽都填它。

### 分组的默认

- 普通 Key：分组已经绑死在 Key 上，没得选，只需要模型。
- 复合 Key：分组由模型前缀决定，所以**默认模型同时决定了默认分组**，一并解决。

---

## 3. Telemetry：建议默认关（与 ori 相反）

ori 默认开启匿名遥测，`ORI_TELEMETRY=0` 关闭。tf 反过来，理由：

**（1）用户群体的敏感度不同。** ori 背后是 OpenRouter 的品牌和隐私政策；tf 的用户里很大比例是中国用户 + 通过第三方网关用模型，对"CLI 往外发数据"的容忍度低得多。而这个工具本身就在处理 API Key，任何非必要的外发都会被放大解读。

**（2）信任成本不对称。** 默认开启换来的是产品分析数据；代价是一旦有人发现"这个 CLI 会上报"，哪怕完全匿名，在这个用户群里也是灾难性的口碑事件。收益有限，风险无上限。

**（3）最关键：我们本来就不需要单独的遥测通道。** 用户的请求本来就要经过我们自己的网关，服务端能看到 UA、模型、分组、用量。CLI 版本信息可以直接写进**自己发的那部分请求**的 UA：

```
tf/0.1.0 (darwin/arm64)     ← 只用于 tf 自己的 models/groups 查询
```

这是零额外隐私成本的遥测，没有新增任何一次原本不存在的网络请求。

**注意与第 0 节的冲突**：UA 只能加在 **tf 自己发的请求**上。注入给 harness 的环境**绝不能改 UA**，否则破坏 `claude_code_only` 的识别。这两条必须一起遵守。

**例外**：崩溃报告做成显式主动上报（`tf feedback` 生成一份可读的诊断包，用户自己决定发不发），而不是自动。

---

## 4. 开源与 License

### 是否受 TokenRouter 的 LGPL 传染

不受。TokenRouter 是 LGPL-3.0-or-later，但 tf 是**独立的 CLI**，不链接也不衍生自它的代码，只通过 HTTP 调用。License 可以自由选。

### 选项

**（1）开源 MIT / Apache-2.0（推荐）**

- **可审计性直接解决信任问题。** 这是一个会拿到你 API Key、还会往你 shell 环境里注入变量的工具。"你可以自己看代码"比任何隐私声明都有说服力，也和第 3 节的 telemetry 决策互相加强。
- **包管理器收录的前提**：homebrew-core、AUR、nixpkgs、scoop 基本都要求开源。
- **适配表是数据驱动的，社区 PR 门槛极低**：加一个 harness 就是加一行配置。这是最可能收到有效外部贡献的部分。
- **npm provenance**：`npm publish --provenance` 需要公开仓库 + CI 构建，能给出可验证的供应链证明。对一个会接触 API Key 的 npm 包，这是很强的信任信号。

**（2）闭源发二进制（ori 的做法）**
OpenRouter 能这么做是靠品牌背书，我们没有。而且 tf 的核心价值在网关侧，CLI 闭源保护不了任何有价值的东西，只是徒增用户疑虑。

**（3）BSL / Elastic 这类限制商用协议**
对一个启动器属于过度设计，还会挡住包管理器收录。不考虑。

### 具体建议：Apache-2.0

优于 MIT 的两点：带**明确的专利授权条款**；商标条款更清晰，这对一个名字里带 harness 商标引用的项目有实际意义。ori 的二进制也是按 Apache-2.0 发布的。

### 开源前要处理的事

- 仓库里**不能有任何凭据**，默认端点之外的 host 也不要硬编码。
- 适配表里引用 Claude Code / Codex / opencode 属于**指称性使用**，但 README 要有一句明确声明：不隶属于 Anthropic / OpenAI / SST，也未获其背书。
- 需要 `SECURITY.md`（这类工具会收到凭据处理相关的报告）。
