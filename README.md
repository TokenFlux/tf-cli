# tf

用 [TokenFlux](https://tokenflux.dev) 或自建的 TokenRouter 启动 Claude Code、Codex、opencode。
只注入环境变量与命令行参数，不改动 harness 自己的配置文件，可与 CC-Switch 这类配置管理器共存。

v0。已在 Claude Code 2.1、Codex 0.151、opencode 1.18 上实测。Windows 能编译，交互路径未验证。

## 安装

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tkr/main/install.sh | sh
```

装到 `~/.local/bin`，不需要 sudo。从源码构建：

```sh
git clone https://github.com/tokenflux/tkr && cd tkr && make build
```

## 快速开始

```sh
tf login     # 粘贴 API Key，输入不回显，当场校验
tf claude    # 选一个主模型，之后直接启动
```

harness 没装时 tf 列出可用的包管理器让你选。`--yes`、`--json`、无终端时只打印安装命令。

## 命令

| | |
| --- | --- |
| `tf claude` `tf codex` `tf opencode` | 启动 harness |
| `tf login [名称]` | 保存一把 Key |
| `tf keys` | 列出 Key，及各自能跑哪些 harness |
| `tf model [harness]` | 查看或编辑模型槽 |
| `tf config` | 打印配置文件路径 |
| `tf update` | 升级自身 |

启动时的一次性参数，都不写盘：

```sh
tf claude -m              # 换模型，进入选择界面
tf claude -e high         # 调思考强度
tf codex  -k work         # 指定用哪把 Key
tf claude -- --resume     # -- 之后的参数原样交给 harness
```

改默认值用 `tf model claude --set fast=claude-haiku-4-5-20251001`。

## Key 与模型

Key 的绑定属于 harness，不存在全局的「当前 Key」：

```
tf codex   → 用能跑 codex 的那把
tf claude  → 用能跑 claude 的那把
```

多把都符合时问一次并记住。启动前 tf 探测每把 Key 的分组允许哪些客户端协议，不消耗 token，
不可用的 Key 和模型不进候选。

`claude_code_only` 分组（如 Claude Max）按客户端指纹放行，只有 Claude Code 过得去，
它的模型只出现在 `tf claude` 里。

一把复合 Key 横跨多个分组，各分组能力不同，按分组前缀分别判断：

```
$ tf keys
work  sk-d61…5b1c
  Claude     claude
  GPT        codex opencode
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

## 配置与环境变量

`tf config` 打印路径。

- `config.json` 0644：Key 标签、host、harness 绑定与模型槽
- `credentials.json` 0600：只存密钥本身，权限过宽会被自动收紧
- `TKR_API_KEY` 优先于已保存的凭据，不写盘，供容器与 CI 使用
- `TKR_LANG` 取 `zh` 或 `en`，默认跟随系统 locale

凭据处理与安全边界见 [SECURITY.md](SECURITY.md)。

## 升级

```sh
tf update            # 校验 SHA256 后原子替换
tf update --check    # 只查有无新版
```

用包管理器装的不自替换，只打印对应的升级命令。

## 许可

Apache-2.0。Claude、Codex、opencode 是各自权利人的商标，tf 与它们无隶属关系。
