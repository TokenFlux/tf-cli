# tf Agent 使用说明

`tf` 是 TokenFlux / TokenRouter 的进程级 harness 启动器。面向 Agent、脚本和 CI 时，始终使用 `--no-tui --json`。

## 核心规则

- `--no-tui` 禁止选择器、确认和其它 TUI；`--no-input`、`--yes` 是兼容写法。
- `--json` 输出单个 JSON envelope；日志和警告不会污染 stdout。
- 不要把 API Key 写入日志、提示词或源码。一次性使用 `TF_API_KEY`，持久化使用 `tf login`。
- `tf` 只向子进程注入配置，不改 harness 的全局配置文件。

## 只读诊断

```sh
tf auth --no-tui --json
tf status --no-tui --json
tf status --check --no-tui --json
tf keys --no-tui --json
```

`tf auth` 解释当前实际凭据来源、名称、脱敏 Key、网关、模型数量和协议/harness 范围。默认不联网、不写盘。多把本地 Key 且没有 `TF_API_KEY` 时必须用 `--key NAME`，不会静默猜测。

`tf status` 默认离线；`--check` 才请求远程用量。部分 Key 检查失败时查看 `data.check_errors`，本地状态可读时命令仍成功。

## 启动

```sh
tf claude --no-tui -m MODEL -- --help
tf codex --no-tui -m MODEL -- exec PROMPT
TF_API_KEY=... tf codex --no-tui -m MODEL -- exec PROMPT
```

`--` 后的参数全部传给 harness。`TF_API_KEY` 只对当前进程生效，不写入磁盘。

## 登录

```sh
tf login
printf '%s' "$KEY" | tf login --with-key --no-tui
tf login work --host https://router.example.com --with-key --no-tui
```

网页导入永远需要真实终端确认，不能用 `--json` 或 `--no-tui` 绕过。

## 模型槽位

人类在交互终端运行 `tf model claude` 会进入槽位编辑向导。Agent 使用：

```sh
tf model claude --no-tui --json
tf model claude --set heavy=MODEL --no-tui --json
```

`tf keys --refresh` 用于更新模型候选。自动槽位可随模型目录更新；用 `--set` 或 `--edit` 手工设置后，该槽位不会被自动覆盖。

## JSON 与退出码

成功：`{"ok":true,"command":"...","data":...}`

失败：`{"ok":false,"command":"...","error":{"code":"...","message":"..."}}`

退出码：`0` 成功，`1` 命令执行错误（包括执行期间的 `TF_USAGE`），`2` 命令分发或参数解析错误，`130` 取消。harness 的退出码原样透传。远程 `status --check` 失败不会在本地状态可读时改变成功结果。

`--no-tui` 只关闭 tf 的交互；harness 的非交互参数需写在 `--` 后。harness 的输出保持原样，不包装成 tf 的 JSON envelope。
