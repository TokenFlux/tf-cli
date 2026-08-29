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
git clone https://github.com/tokenflux/tkr && cd tkr && make build
# 二进制在 ./bin/tkr
```

预编译包与 npm 分发在做。

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
tkr model claude                  # 看模型槽
tkr model claude --set fast=claude-haiku-4-5-20251001
tkr claude -m                     # 本次换个模型（进选择器，不写盘）
tkr claude -e high                # 本次调思考强度
tkr codex -k work                 # 本次用哪把 Key
tkr claude -- --resume            # -- 之后原样透传给 harness
```

### 多把 Key

绑定属于 harness，**没有全局「当前 Key」这种模式**：

```
tkr codex   → 自动用能跑 codex 的那把
tkr claude  → 自动用能跑 claude 的那把
```

有多把合格时问一次并记住，之后不再问。tkr 会在启动前探测每把 Key 的分组
允许哪些协议（零 token 成本），跑不了的 Key 和模型根本不会出现在候选里。

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
