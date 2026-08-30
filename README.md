# tf

[![Release](https://img.shields.io/github/v/release/tokenflux/tkr)](https://github.com/tokenflux/tkr/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

用 [TokenFlux](https://tokenflux.dev) 或自建的 TokenRouter 启动 Claude Code、Codex、opencode。

```sh
tf claude
```

v0。三个 harness 均已端到端实测：Claude Code 2.1.251、Codex 0.151.0、opencode 1.18.20。
Windows 能编译，交互路径未验证。

## 安装

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tkr/main/install.sh | sh
```

仓库名 tkr，装出来的二进制叫 `tf`。安装位置是 `~/.local/bin`，不需要 sudo，
`TF_INSTALL_DIR` 可以改。也可以从源码构建：

```sh
git clone https://github.com/tokenflux/tkr && cd tkr && make build
```

`tf update` 升级，校验 SHA256 后原子替换；`tf update --check` 只查有无新版。
用包管理器装的不自替换，只打印对应的升级命令。
卸载：`rm ~/.local/bin/tf`，再删掉 `tf config` 打印的配置目录（默认 `~/.tf`）。

## 快速开始

```console
$ tf login
粘贴 API Key（不回显）：
✓ 已保存为 Key "work"
  网关     https://tokenflux.dev
  Key      sk-d61…5b1c
  模型     14 claude-opus-5, claude-sonnet-5, gpt-5.6-sol, …
  协议     anthropic_messages / openai_responses
  可用于   claude codex opencode

$ tf claude
为 claude 选择主模型
❯ claude-opus-5  Claude/
  claude-sonnet-5  Claude/
  claude-haiku-4-5  Claude/

╭───────────────────────────────╮
│ ✻ Welcome to Claude Code      │
╰───────────────────────────────╯
```

模型选过一次就记住，之后 `tf claude` 直接启动。选择器里直接输入可以过滤，esc 先清过滤、
再按才退出。harness 没装时 tf 会列出可用的包管理器让你选；`--no-input`、`--json`、
无终端时只打印安装命令。

启动时可以临时改，都不写盘：

```sh
tf claude -m              # 换模型，进入选择界面
tf codex  -e high         # 调思考强度（claude 没有这个开关，用模型变体代替）
tf codex  -k work         # 指定用哪把 Key
tf claude -- --resume     # -- 之后的参数原样交给 harness
```

改默认值用 `tf model claude --set fast=claude-haiku-4-5-20251001`。

## 工作方式

tf 解析出 Key、分组和每个模型槽，再按各 harness 自己的方式传进去，全部是进程级的：

| harness | 注入 | 实测 |
| --- | --- | --- |
| Claude Code | `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_DEFAULT_*_MODEL`，并清空 `ANTHROPIC_API_KEY` 与 bedrock / vertex 变量 | 2.1.251 通过 |
| Codex | `-c model_provider=…`、`-c model_providers.*.base_url=…`、`env_key`，不设环境变量 | 0.151.0 通过 |
| opencode | `OPENCODE_CONFIG_CONTENT` 内联 JSON，`model` 与 `small_model` 一起注入 | 1.18.20 通过 |

`~/.claude/settings.json`、`~/.codex/config.toml` 不被读写，可与 CC-Switch 这类配置管理器共存。

每个 harness 的后台任务用的是独立模型槽，漏配就会静默失败，所以 tf 把槽位一起管起来：

```console
$ tf model claude
claude
  default   claude-sonnet-5           — 主对话
  fast      claude-haiku-4-5-20251001 — 后台任务：标题、文件摘要
  heavy     未配置                    — /model 切到最强档时
```

槽位由 harness 决定：codex 是 `default` 与 `review`，opencode 是 `default` 与 `small`，
两个都必填，缺 `small` 时 opencode 会回落到内置的 `gpt-5.4-nano`，该模型通常不在分组里。

## 模型为什么会不见

`claude_code_only` 分组（如 Claude Max）按客户端指纹放行，只接受 Claude Code，
它的模型不会出现在 `tf codex` 和 `tf opencode` 的候选里。

其余情况是启动前的协议探测把不可用的 Key 和模型过滤掉了。探测不消耗 token。

## 多把 Key

绑定属于 harness，不存在全局的“当前 Key”。多把都符合时询问一次并记住。
一把复合 Key 横跨多个分组、各分组能力不同，按分组前缀分别判断：

```console
$ tf keys
work  sk-d61…5b1c
  Claude/    claude
  GPT/       codex opencode
personal  sk-9f2…a047
  ChatGPT/   codex opencode
```

## 自建 TokenRouter

编译期写死网关地址，团队成员照常 `tf login`：

```sh
make build HOST=https://router.acme.com
```

用官方二进制则在登录时给地址，它随这把 Key 保存：

```sh
tf login work --host https://router.acme.com
```

优先级：`--host` > Key 保存的 host > 编译期默认值。

## 配置

`tf config` 打印配置目录，默认 `~/.tf`，设了 `XDG_CONFIG_HOME` 时是 `$XDG_CONFIG_HOME/tf`。

- `config.json`：Key 标签、host、harness 绑定与模型槽，可分享
- `credentials.json`：只存密钥本身
- `TF_API_KEY` 优先于已保存的凭据，不写盘，供容器与 CI 使用
- `TF_LANG` 取 `zh` 或 `en`，默认跟随系统 locale

文件权限与凭据处理见 [SECURITY.md](SECURITY.md)。

## 反馈

用法问题和 bug 请提 [issue](https://github.com/tokenflux/tkr/issues)。安全问题见 SECURITY.md，不要开公开 issue。

## 许可

Apache-2.0。Claude、Codex、opencode 是各自权利人的商标，tf 与它们无隶属关系。
