# tf-cli

[![Release](https://img.shields.io/github/v/release/tokenflux/tf-cli)](https://github.com/tokenflux/tf-cli/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

`tf-cli` 项目提供命令 `tf`，用于启动和管理 AI 编程客户端（Claude Code、Codex、opencode、Pi）的网关凭据。它将 [TokenFlux](https://tokenflux.dev) 或自建 TokenRouter 网关的密钥、模型路由及槽位配置以纯进程级方式注入各客户端，不读写各工具的全局配置文件，支持多 Key 隔离与多模型槽协同。

## 特性

- **非侵入式注入**：仅在子进程生命周期内通过环境变量与 CLI 参数注入配置，不修改 `~/.claude/settings.json`、`~/.codex/config.toml` 或 `~/.pi`；`~/.claude/settings.json` 的 `env` 可覆盖进程注入，`tf` 会在启动前检查并告警。
- **全模型槽位管理**：自动配置主对话模型及后台任务模型（如 Claude 的 `fast` 摘要槽、Codex 的 `review` 审查槽、opencode 的 `small` 模型槽、Pi 的 `default` 主对话槽），避免因缺省配置导致静默调用失败。
- **协议探测与路由过滤**：登录及需要模型候选或校验时刷新模型列表，通过零 token 探测过滤不兼容模型。绑定与槽位完整的启动和补全直接读取本地缓存。
- **多凭据隔离**：按 harness 独立绑定 Key，支持单凭据跨分组前缀路由，无需切换全局环境。
- **环境诊断与冲突预警**：内置状态检查与配置冲突检测，实时识别本地覆盖环境变量及配额耗尽状态。

## 安装

### 预编译二进制（推荐）

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/install.sh | sh
```

默认安装至 `~/.local/bin/tf`（可通过环境变量 `TF_INSTALL_DIR` 自定义安装路径），无需管理员权限。

### npm（自 v0.5.3 起）

npm 官方包名为 `@tokenflux/tf`（未加 scope 的 `tf` 与 `tf-cli` 属于无关项目）。主包通过 optionalDependencies 分发平台对应的 Go 二进制，无 postinstall 外部下载脚本：

```sh
# 全局安装
npm install -g @tokenflux/tf
tf login

# 或单次执行
npx @tokenflux/tf status
# 或
pnpx @tokenflux/tf status
```

安装时请勿添加 `--omit=optional` 或 `--no-optional`。通过 npm 全局安装时，升级请运行 `npm install -g @tokenflux/tf@latest`（`tf update` 检测到 `node_modules` 路径时会提示该命令，不会自行覆盖包管理器文件）。

### 源码安装与编译

通过 `go install` 直接安装：

```sh
go install github.com/tokenflux/tf-cli/cmd/tf@latest
```

或克隆源码仓库本地编译：

```sh
git clone https://github.com/tokenflux/tf-cli.git
cd tf-cli
make build
```

### 更新

- **检查更新**：`tf update --check`
- **自动升级**：`tf update`（独立脚本安装支持自动校验并替换；npm 安装请运行 `npm install -g @tokenflux/tf@latest`）

### 卸载

- **npm 安装**：
  ```sh
  npm uninstall -g @tokenflux/tf
  ```
- **install.sh 安装**：
  ```sh
  # 默认仅删除 tf 二进制，保留当前配置与凭据
  curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.sh | sh

  # 同时删除 ~/.tf 或 XDG 对应的配置、凭据与缓存
  curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.sh | sh -s -- --purge
  ```
  若安装时指定了自定义目录，请在管道右侧传入环境变量（例如 `curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.sh | TF_INSTALL_DIR=/原安装目录 sh`）。
- **源码 / go install 安装**：
  删除安装时设置的 GOBIN 中的 tf；若 GOBIN 为空，则位置为 `$(go env GOPATH)/bin`。若需清理配置与凭据，再按需删除配置目录（默认 `~/.tf`，或 `tf config` 显示的路径）。

> **操作系统支持**：支持 Linux 与 macOS 全交互特性。Windows 当前仅支持非交互模式（支持通过管道传入 Key 及使用 `--set` 预设参数）。

## 快速上手

### 1. 登录并绑定网关

`tf login` 默认高亮“从网页导入”。确认后 CLI 会建立本机监听并自动打开 Keys 页面：

```console
$ tf login
选择登录方式
❯ 从网页导入    自动打开 Keys 页面
  粘贴 API Key  终端隐藏输入
等待网页导入
  监听     http://127.0.0.1:43110
  来源     https://tokenflux.dev
  打开     https://tokenflux.dev/keys#tfcli=1.43110.<session-secret>
  10 分钟内没有请求会自动退出
```

需要粘贴时，在选择器中选择第二项，或使用 `tf login --with-key`。

如使用私有化部署的 TokenRouter 网关：

```sh
tf login work --host https://router.example.com
```

也可以用 flag 直接进入网页导入：

```sh
tf login work --from-web
```

网页和终端确认页会对终端链接显示“已验证当前 tf 会话”；直接打开页面仍可导入，但两端都会显示未验证会话警告。网页请求到达后，终端会展示 Origin、网关、分组和脱敏 Key，只有手动确认后才会继续。

未在命令中指定名称时，CLI 校验 Key 后会让用户选择按可用模型自动命名、采用网页 Key 名称或自订名称。自动命名会避开已有名称；显式运行 `tf login work` 则使用 `work`；该名称已有不同 Key 时会先确认覆盖，默认取消。非交互覆盖必须添加 `--force`。完整前端协议见 [`docs/integrations/web-import.md`](docs/integrations/web-import.md)。

### 2. 启动客户端

直接指定目标 harness 启动。首次运行若未配置模型，将启动交互式选择器；配置后将自动持久化偏好：

```console
$ tf claude
为 claude 选择主模型
❯ claude-opus-5  Claude/
  claude-sonnet-5  Claude/
  claude-haiku-4-5  Claude/

╭───────────────────────────────╮
│ ✻ Welcome to Claude Code      │
╰───────────────────────────────╯
```

若本地未安装对应 harness，`tf` 将提供系统包管理器安装选项；在 `--no-input`、`--json` 或无 TTY 环境下将输出标准安装命令。

## 常用命令与参数

### 运行时临时参数

运行参数仅对当前进程生效，不影响已保存的默认配置：

```sh
tf claude -m              # 唤起模型交互选择器
tf codex  -e high         # 调整推理思考强度（reasoning effort）
tf pi     -e high         # 调整推理思考强度（reasoning effort）
tf codex  -k work         # 显式指定本次使用的 Key 别名
tf claude -- --resume     # 双破折号 -- 后的所有参数将直接透传给底层工具
tf opencode --help        # harness 命令后的 -h/--help 直接交给底层工具
tf --help opencode        # 查看 tf 包装器对该 harness 的帮助
```

### 模型与槽位管理

查看当前 harness 的槽位分配：

```console
$ tf model claude
claude
  default   claude-sonnet-5           — 主对话
  fast      claude-haiku-4-5-20251001 — 后台任务：标题、文件摘要
  heavy     未配置                    — /model 切换高算力档位
```

修改指定槽位的默认模型：

```sh
tf model claude --set fast=claude-haiku-4-5-20251001
```

各 harness 支持的槽位定义：
- **Claude Code**：`default`（主会话）、`fast`（摘要与标题生成）、`heavy`（高算力模式）
- **Codex**：`default`（主会话）、`review`（代码审查）
- **opencode**：`default`（主模型）、`small`（轻量任务模型；未配置时将触发回退警告）
- **Pi**：`default`（主会话）

### 凭据与多 Key 查看

查看本地保存的凭据及其适用客户端范围：

```console
$ tf keys
work  sk-d61…5b1c
  Claude/    claude
  GPT/       codex opencode pi
personal  sk-9f2…a047
  ChatGPT/   codex opencode pi
```

`tf keys --refresh` 先拉每把 Key 的 `/v1/models` 再按最新前缀探协议；启动筛选从缓存找不到可用 Key 时同样自动刷新；这些刷新路径网络失败时沿用已有缓存。

### 运行状态与诊断

通过 `tf status` 检查当前生效的凭据、各客户端模型映射、用量配额及环境冲突：

```console
$ tf status
/Users/user/.tf

default  sk-d7d…a4fe  13 个模型
  额度  0/10 推理积分  额度已耗尽，请求将被拒绝
  今日  8 次请求，170933 tokens

  claude    default  claude-sonnet-5
  codex     default  gpt-5.6-terra
  opencode  —        claude-sonnet-5
  pi        default  gpt-5.6-terra
警告：~/.codex/auth.json 本地已存在凭据；直接运行 codex（不经由 tf）将使用该凭据
```

## 技术实现与注入机制

`tf` 作为宿主进程与 CLI 客户端之间的隔离层，采用进程级入参及环境变量注入：

| 客户端 | 验证版本 | 注入方式 | 环境清理与保护 |
| --- | --- | --- | --- |
| **Claude Code** | 2.1.251 | 注入 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_DEFAULT_*_MODEL` | 清理继承的 `ANTHROPIC_API_KEY` 及 AWS Bedrock / GCP Vertex 相关环境变量 |
| **Codex** | 0.151.0 | 注入命令行参数 `-c model_provider=…`、`-c model_providers.*.base_url=…`、`env_key` | 不注入额外环境变量，避免污染 Shell 上下文 |
| **opencode** | 1.18.20 | 注入 `OPENCODE_CONFIG_CONTENT` 内联 JSON（包含 `model` 与 `small_model`） | 覆盖进程局部配置，不落盘修改全局配置文件 |
| **Pi** | 0.84.4 / 0.85.0 | 通过 `--extension` 加载进程私有临时 JS 扩展注册随机 provider，仅在专用环境变量传递凭据，注入限定 `--model <provider>/<model>` | 临时扩展文件权限 `0600` 且退出后自动删除；Key 不进入 argv 或落盘配置，不改动 `~/.pi` |

## 配置与存储结构

执行 `tf config` 可输出当前配置目录位置（默认 `~/.tf`，遵循 XDG 规范时为 `$XDG_CONFIG_HOME/tf`）：

| 文件 / 环境变量 | 权限 | 说明 |
| --- | --- | --- |
| `config.json` | `0644` | 存储 Key 别名、网关地址、harness 映射与模型槽配置（无敏感信息，可同步） |
| `credentials.json` | `0600` | 明文存储 API Key，仅当前用户可读写 |
| `TF_API_KEY` | - | 仅本次启动使用，不覆盖已保存的 Key；默认连接默认网关，自建网关需指定 `--host`；显式 `-k` 优先使用本地 Key |
| `TF_LANG` | - | 强制指定 CLI 显示语言（可选 `zh` 或 `en`，默认跟随系统 Locale） |

CI/CD 与容器无需先登录：通过环境注入 `TF_API_KEY` 后，运行 `tf codex --no-input -m=gpt-5.6-terra -- exec "your prompt"`。自建网关添加 `--host https://router.example.com`。运行时 Key、模型目录与槽位均不落盘；`tf status` 和 `tf keys` 仍只查询本地保存的账户。

详细安全模型与凭据存储机制请参考 [SECURITY.md](SECURITY.md)。

## 问题反馈

- 遇到问题或功能建议，请提交 [GitHub Issue](https://github.com/tokenflux/tf-cli/issues)。
- 涉及安全漏洞请参阅 [SECURITY.md](SECURITY.md) 的披露流程进行反馈。

## 许可协议

本项目采用 [Apache-2.0](LICENSE) 许可证。相关名称与商标归各自权利人所有，本项目与其无商业从属关系。
