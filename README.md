# tkr

Launch the AI coding harnesses you already use — Claude Code, Codex, opencode —
against [TokenFlux](https://tokenflux.dev) or a self-hosted TokenRouter.

```
tkr claude      # 用你的 TokenFlux Key 起 Claude Code
tkr codex
tkr opencode
```

## tkr 是启动器，不是配置管理器

tkr 只做**进程内注入**：环境变量 + 命令行参数 + 进程私有的临时文件。

- **不改**你的 `~/.claude/settings.json`、`~/.codex/config.toml`
- **不装**常驻守护进程，**不开**本地代理
- 退出后机器上和运行前一样

因此它与 CC-Switch 这类配置管理器**可以共存**：那类工具管持久配置，tkr 管临时启动。

## 装

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tkr/main/install.sh | sh
```

装到 `~/.local/bin`，不需要 sudo。或者自己编：

```sh
git clone https://github.com/tokenflux/tkr && cd tkr && make build
```

升级：

```sh
tkr update            # 独立二进制会自替换（校验 SHA256 后原子替换）
tkr update --check    # 只看有没有新版
```

用包管理器装的，`tkr update` 不会自替换 —— 它会告诉你该用哪条命令，
因为自替换的结果会在下次 `npm i -g` 时被悄悄换回旧版。

## 用

```sh
tkr login                 # 粘贴 API Key（隐藏输入），当场校验
tkr claude                # 首次会让你选主模型，之后直接起
```

harness 没装时会问你要不要装，并列出可用的包管理器。非交互环境（`--yes`、
`--json`、无终端）**绝不静默安装**，只打印命令让你自己决定。

### 常用

```sh
tkr keys                          # 有哪些 Key，各自能跑什么
tkr model                         # 看全部 harness 的模型槽
tkr model claude                  # 交互编辑（选槽 → 选模型）
tkr model claude --set fast=claude-haiku-4-5-20251001
tkr claude -m                     # 本次换个模型（进选择器）
tkr claude -e high                # 本次调思考强度
tkr codex -k work                 # 本次用哪把 Key
tkr claude -- --resume            # -- 之后原样透传给 harness
```

**flag 管这一次，`tkr model` 管以后。** `-m`、`-e`、`-k` 都只影响本次运行，
绝不写盘；要固化就用 `tkr model <harness>`。

### 多把 Key

绑定属于 harness，**没有全局「当前 Key」这种模式**：

```
tkr codex   → 自动用能跑 codex 的那把
tkr claude  → 自动用能跑 claude 的那把
```

有多把合格时问一次并记住，之后不再问。tkr 会在启动前探测每把 Key 的分组
允许哪些协议（零 token 成本），跑不了的 Key 和模型根本不会出现在候选里。

harness 会几种协议就按几种算：opencode 内置 openai 与 anthropic 两个
provider，所以它在只开 anthropic_messages 的分组上照样能跑，注入配方
会跟着换。

有一类分组任何 harness 都替代不了 Claude Code：`claude_code_only`
（如 Claude Max）按客户端指纹放行，只有 Claude Code 本身过得去。
这类分组的模型只会出现在 `tkr claude` 的候选里，别处会明确告诉你
藏了多少个、为什么藏 —— tkr 不伪装成别的客户端。

复合 Key 一把横跨多个分组，各分组能力不同，tkr 按分组前缀分别判断：

```
$ tkr keys
work  sk-d61…5b1c
  Claude     claude
  GPT        codex opencode
```

## 不做的事

- **不改 User-Agent、不做本地代理 / MITM。** 部分分组靠客户端指纹识别
  （如 Claude Max 的 `claude_code_only`），代理会破坏它。
- **不自动切换你的全局配置。** 每次启动只影响那一个子进程。
- **不上报遥测。** 默认关，且没有开关可开 —— v0 根本没写。

## 配置在哪

```sh
tkr config
```

- `config.json` 0644 — Key 标签、host、harness 绑定与模型槽
- `credentials.json` 0600 — 只有密钥本身；权限过宽会被自动收紧

`TKR_API_KEY` 优先于落盘凭据且不写盘，供容器与 CI 使用。

## 状态

v0，能跑。已实测：Claude Code 2.1、Codex 0.151、opencode 1.18 均可正常起
并完成真实对话，退出码透传正确，用户配置文件未被改动。

Windows 尚未验证。

## 许可

Apache-2.0。Claude、Codex、opencode 是各自权利人的商标，tkr 与它们无隶属关系。
