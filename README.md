# tkr

用 [TokenFlux](https://tokenflux.dev) 或自建的 TokenRouter 启动 Claude Code、Codex、opencode。

```sh
tkr claude
tkr codex
tkr opencode
```

harness 读的还是它自己的配置文件，tkr 只在启动那一刻把网关地址和模型换掉。

## 只做进程内注入

tkr 传给子进程的只有环境变量、命令行参数和进程私有的临时文件。

- 不修改 `~/.claude/settings.json`、`~/.codex/config.toml` 或任何其他配置文件
- 不安装常驻进程，不启动本地代理
- 不改写 User-Agent。部分分组按客户端指纹放行，经过代理就会被拒
- 不上报遥测
- 退出后机器状态与运行前一致

tkr 与 CC-Switch 这类配置管理器可以同时使用：那类工具管持久配置，tkr 管单次启动。

## 安装

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tkr/main/install.sh | sh
```

安装到 `~/.local/bin`，不需要 sudo。也可以从源码构建：

```sh
git clone https://github.com/tokenflux/tkr && cd tkr && make build
```

## 第一次运行

```sh
tkr login     # 粘贴 API Key，输入不回显，当场校验
tkr claude    # 选一个主模型，之后直接启动
```

harness 未安装时会询问，并列出可用的包管理器。非交互环境（`--yes`、`--json`、无终端）
只打印安装命令，不会自行安装。

## 日常使用

```sh
tkr keys                          # 有哪些 Key，各自可用于哪些 harness
tkr model                         # 查看全部 harness 的模型槽
tkr model claude                  # 交互编辑：选槽位，再选模型
tkr model claude --set fast=claude-haiku-4-5-20251001
tkr claude -m                     # 本次换个模型，进入选择界面
tkr claude -e high                # 本次调整思考强度
tkr codex -k work                 # 本次使用哪把 Key
tkr claude -- --resume            # -- 之后的参数原样交给 harness
```

flag 管这一次，`tkr model` 管以后。`-m`、`-e`、`-k` 只影响本次运行，不写入磁盘。

升级：

```sh
tkr update            # 校验 SHA256 后原子替换
tkr update --check    # 只查看有无新版
```

用包管理器安装的不会自替换，只给出对应的升级命令：自替换的结果会在下次
`npm i -g` 时被换回旧版。

## 多把 Key 与分组

绑定属于 harness，没有全局的「当前 Key」：

```
tkr codex   → 用可运行 codex 的那把
tkr claude  → 用可运行 claude 的那把
```

有多把符合条件时询问一次并记住。启动前 tkr 会探测每把 Key 的分组允许哪些客户端协议，
这一步不消耗 token，不可用的 Key 和模型不会进入候选。

harness 支持哪几种协议就按哪几种判断。opencode 内置 openai 与 anthropic 两个 provider，
只开放 `anthropic_messages` 的分组它同样可用，注入配方随之切换。

`claude_code_only` 分组（如 Claude Max）按客户端指纹放行，只有 Claude Code 本身能通过。
这类分组的模型只出现在 `tkr claude` 的候选里，其他 harness 下会说明隐藏了多少个模型和原因。

一把复合 Key 横跨多个分组，各分组能力不同，tkr 按分组前缀分别判断：

```
$ tkr keys
work  sk-d61…5b1c
  Claude     claude
  GPT        codex opencode
```

## 文件与环境变量

```sh
tkr config
```

- `config.json` 0644：Key 标签、host、harness 绑定与模型槽
- `credentials.json` 0600：只存密钥本身，权限过宽会被自动收紧

`TKR_API_KEY` 优先于已保存的凭据且不写入磁盘，供容器与 CI 使用。
`TKR_LANG=zh` 或 `en` 覆盖界面语言，默认跟随系统 locale。

## 状态

v0。Claude Code 2.1、Codex 0.151、opencode 1.18 均已实测：可正常启动并完成真实对话，
退出码透传正确，用户配置文件未被修改。

Windows 可以编译，交互路径未经验证。

## 许可

Apache-2.0。Claude、Codex、opencode 是各自权利人的商标，tkr 与它们无隶属关系。
