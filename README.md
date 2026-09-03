# tf-cli

[![Release](https://img.shields.io/github/v/release/tokenflux/tf-cli)](https://github.com/tokenflux/tf-cli/releases)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)

`tf-cli` 项目提供命令 `tf`，用于启动和管理 AI 编程客户端（Claude Code、Codex、opencode）的网关凭据。它将 [TokenFlux](https://tokenflux.dev) 或自建 TokenRouter 网关的密钥、模型路由及槽位配置以纯进程级方式注入各客户端，不读写各工具的全局配置文件，支持多 Key 隔离与多模型槽协同。

## 特性

- **非侵入式注入**：仅在子进程生命周期内通过环境变量与 CLI 参数注入配置，不修改 `~/.claude/settings.json` 或 `~/.codex/config.toml`，可与本地其他配置管理器无缝共存。
- **全模型槽位管理**：自动配置主对话模型及后台任务模型（如 Claude 的 `fast` 摘要槽、Codex 的 `review` 审查槽、opencode 的 `small` 模型槽），避免因缺省配置导致静默调用失败。
- **协议探测与路由过滤**：启动时自动执行无消耗探测，过滤协议不兼容的模型；自动识别 `claude_code_only`（如 Claude Max）等客户端限定分组。
- **多凭据隔离**：按 harness 独立绑定 Key，支持单凭据跨分组前缀路由，无需切换全局环境。
- **环境诊断与冲突预警**：内置状态检查与配置冲突检测，实时识别本地覆盖环境变量及配额耗尽状态。

## 安装

### 预编译二进制（推荐）

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/install.sh | sh
```

默认安装至 `~/.local/bin/tf`（可通过环境变量 `TF_INSTALL_DIR` 自定义安装路径），无需管理员权限。

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

### 更新与卸载

- **检查更新**：`tf update --check`
- **自动升级**：`tf update`（下载最新 Release 并校验 SHA256 后执行原子替换）
- **卸载**：删除配置将永久清除本机保存的 Key。默认安装和默认配置路径可执行：
  ```sh
  rm ~/.local/bin/tf
  rm -rf ~/.tf
  ```
  若安装时设置了 `TF_INSTALL_DIR`，二进制路径为 `$TF_INSTALL_DIR/tf`。若使用 XDG 路径，请先运行 `tf config` 确认配置与缓存目录，再删除对应目录。

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

未在命令中指定名称时，CLI 校验 Key 后会让用户选择按可用模型自动命名、采用网页 Key 名称或自订名称。自动命名会避开已有名称；显式运行 `tf login work` 则直接使用 `work`。完整前端协议见 [`docs/integrations/web-import.md`](docs/integrations/web-import.md)。

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
tf codex  -k work         # 显式指定本次使用的 Key 别名
tf claude -- --resume     # 双破折号 -- 后的所有参数将直接透传给底层工具
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

### 凭据与多 Key 查看

查看本地保存的凭据及其适用客户端范围：

```console
$ tf keys
work  sk-d61…5b1c
  Claude/    claude
  GPT/       codex opencode
personal  sk-9f2…a047
  ChatGPT/   codex opencode
```

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
警告：~/.codex/auth.json 本地已存在凭据；直接运行 codex（不经由 tf）将使用该凭据
```

## 技术实现与注入机制

`tf` 作为宿主进程与 CLI 客户端之间的隔离层，采用进程级入参及环境变量注入：

| 客户端 | 验证版本 | 注入方式 | 环境清理与保护 |
| --- | --- | --- | --- |
| **Claude Code** | 2.1.251 | 注入 `ANTHROPIC_BASE_URL`、`ANTHROPIC_AUTH_TOKEN`、`ANTHROPIC_DEFAULT_*_MODEL` | 清理继承的 `ANTHROPIC_API_KEY` 及 AWS Bedrock / GCP Vertex 相关环境变量 |
| **Codex** | 0.151.0 | 注入命令行参数 `-c model_provider=…`、`-c model_providers.*.base_url=…`、`env_key` | 不注入额外环境变量，避免污染 Shell 上下文 |
| **opencode** | 1.18.20 | 注入 `OPENCODE_CONFIG_CONTENT` 内联 JSON（包含 `model` 与 `small_model`） | 覆盖进程局部配置，不落盘修改全局配置文件 |

## 配置与存储结构

执行 `tf config` 可输出当前配置目录位置（默认 `~/.tf`，遵循 XDG 规范时为 `$XDG_CONFIG_HOME/tf`）：

| 文件 / 环境变量 | 权限 | 说明 |
| --- | --- | --- |
| `config.json` | `0644` | 存储 Key 别名、网关地址、harness 映射与模型槽配置（无敏感信息，可同步） |
| `credentials.json` | `0600` | 明文存储 API Key，仅当前用户可读写 |
| `TF_API_KEY` | - | 运行时环境变量，优先级高于本地配置文件，适合 CI/CD 与容器环境 |
| `TF_LANG` | - | 强制指定 CLI 显示语言（可选 `zh` 或 `en`，默认跟随系统 Locale） |

详细安全模型与凭据存储机制请参考 [SECURITY.md](SECURITY.md)。

## 问题反馈

- 遇到问题或功能建议，请提交 [GitHub Issue](https://github.com/tokenflux/tf-cli/issues)。
- 涉及安全漏洞请参阅 [SECURITY.md](SECURITY.md) 的披露流程进行反馈。

## 许可协议

本项目采用 [Apache-2.0](LICENSE) 许可证。Claude、Codex、opencode 分别为各自实体的注册商标，本项目与其无商业从属关系。
