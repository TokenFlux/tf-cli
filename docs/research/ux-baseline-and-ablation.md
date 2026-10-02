# TUI / CLI 重构前基线与消融记录

状态：已实施。上表为重构前实测基线；实施后的对应断言见 `e2e/ux_baseline_test.go` 与 `internal/cli/ux_baseline_test.go`（默认 `tf login` 直达网页导入、`--with-key` 直达隐藏输入、首次启动 1 个主模型选择器、`tf status` 默认 0 次网络请求）。

这份记录回答两个问题：当前默认路径让用户做了多少次决策，以及删除某个决策后是否会丢失安全或功能约束。测试只使用现有命令和 flag，没有向正式程序加入实验开关。

## 实验方法

真实交互由 `go test -tags pty ./e2e/ -run 'TestUXBaseline' -v` 驱动。测试使用假网关、假 harness 和隔离配置目录，不访问真实账户，也不把 Key 发到外部服务。

指标含义：

- `selectors`：进入一次方向键选择器的次数。一个选择器代表一次需要用户作出列表决策的屏幕。
- `controls`：方向键、回车、取消等离散控制动作。它不统计用户在 harness 自己的界面中的操作。
- `submissions`：完成一次文本输入的次数，例如 Key 或自定义网关地址。
- `elapsed_ms`：从启动测试进程到 harness 假进程退出的时间。PTY、编译和机器负载会影响这个数，只用于同一环境内的粗略比较，不作为产品性能承诺。

选择器计数不把终端重绘算作多次，也不把网页浏览器进程启动作为 CLI 决策。网页导入协议本身由已有的 `TestWebImportRequiresTerminalConfirmationBeforeSaving` 覆盖；新增基线只测 CLI 侧交互成本。

## 重构前版本实测基线

以下是一次本机运行的记录。稳定结论使用选择器和输入次数，耗时仅作为参考。

| 路径 | 命令形态 | selectors | controls | submissions | 观察 |
|---|---|---:|---:|---:|---|
| 交互粘贴登录 | `tf login paste --host <host>` | 1 | 2 | 1 | 先选“网页/粘贴”，再隐藏输入 Key |
| 显式粘贴登录 | `tf login paste --with-key --host <host>` | 0 | 0 | 1 | 方式选择完全由 flag 表达 |
| 默认网页导入 | `tf login` | 4 | 5 | 1 | 登录方式、网关、导入确认、本地名称 |
| 显式网页导入 | `tf login named --from-web --host <host>` | 1 | 1 | 0 | 只剩终端确认；浏览器页面仍是必要外部步骤 |
| 登录后的补全询问 | `tf login paste --with-key --host <host>` | 1 | 2 | 1 | Key 保存成功后又询问是否安装 shell 补全 |
| 首次 Claude 启动 | `tf claude` | 3 | 3 | 0 | 主模型、`fast`、`heavy` 各一屏 |
| 稳定态 Claude 启动 | `tf claude` | 0 | 0 | 0 | 已有完整绑定时直接启动 |
| 一次性指定模型 | `tf claude -m <model> -- ...` | 0 | 0 | 0 | 当前已有的免选择路径 |

重构前运行还验证了：

- 首次启动选择主模型后，辅助槽位仍会逐个出现。
- Esc 在辅助槽位上使用推荐值并继续启动，Ctrl-C 会中止启动并返回 130。
- 网页导入先在终端确认，再返回 HTTP 202，之后才做网关校验和写盘。
- 显式名称能删除导入后的“本地 Key 名称”选择，但不能删除覆盖已有 Key 时的安全确认。

## 消融结果

### A1：删除登录方式选择器

**现状对照**：`tf login` 有 1 个选择器；`tf login --with-key` 有 0 个。

**判定：建议删除默认路径中的选择器。**

方式已经有稳定、可文档化的表达：

```text
tf login             默认网页导入
tf login --with-key  从 stdin 或隐藏输入读取 Key
```

管道输入本身已经能表达“粘贴 Key”，所以不应再打开方式选择器。网页导入的终端确认不属于本项消融，仍需保留。

### A2：删除网关选择器

**现状对照**：默认网页导入的自定义网关路径有 1 个选择器加 1 个文本输入；显式 `--host` 后两者都消失。

**判定：建议默认使用编译时网关，`--host` 作为自建网关入口。**

默认网关是稳定配置，交互式二选一只是在把一个参数是否显式覆盖变成一次额外决策。已有自建网关的账号可以沿用 host；首次使用自建网关时，`--host` 比选择“自定义”再输入地址短，也更适合文档和脚本。

需要保留的行为：

- host 仍要做 URL 校验和 origin 校验。
- 网页导入的请求 host 必须与本次 CLI 会话一致。
- 非交互环境不能凭空猜自建网关。

### A3：删除导入后的 Key 名称选择

**现状对照**：默认网页导入有 1 个本地名称选择器；显式本地名称后为 0 个。

**判定：建议自动命名，只有冲突或非法名称时才询问。**

当前已有 `suggestKeyName`，它能根据模型分组生成名称并避开已占用名称。默认路径应直接采用这个结果；网页提供的 `key_name` 保留为来源元数据，不应成为默认决策屏幕。

仍需询问或拒绝的情况：

- 本地命令明确指定的名称将覆盖另一把不同 Key。
- 网页名称不符合本地命名规则。
- `--force` 只跳过明确允许的覆盖确认，不能跳过网页导入终端确认。

### A4：删除辅助槽位选择

**现状对照**：首次 Claude 启动有 3 个选择器，其中 2 个只用于辅助槽位；保存后稳定态没有选择器。

**判定：建议删除首次启动的辅助槽位选择器，保留自动填充。**

`fill` 已经按模型档位为 `fast`、`small`、`heavy` 等槽位选择更便宜或更强的候选，没有候选时回退到主模型。`askSlots` 的选择是高级调优，不应阻塞第一次启动。

推荐行为：

- 首次启动只在没有主模型时询问一次。
- 辅助槽位由 `fill` 自动填充并保存。
- `tf model <harness> --edit` 继续提供完整的槽位编辑器。
- 如果用户显式使用 `--model`，所有辅助槽位继续只对本次运行生效，不写入持久配置。

这是本轮收益最大的消融：Claude 首次启动预计从 3 屏降到 1 屏；Codex 和 opencode 也分别少 1 屏。

### A5：删除登录后的补全询问

**重构前现状**：登录成功后，`offerCompletions` 会再打开 1 个选择器，并写入补全目录。PTY 基线 `TestUXBaselineLoginDoesNotOfferCompletions` 记录了这条历史路径。

**实施结果**：已从登录默认流程移除；补全只通过显式 `tf completions <shell> --install` 安装。

**判定：建议从登录默认流程移除，改为显式安装。**

补全不是保存 Key 的必要条件，也不会影响当前命令。登录成功后继续询问会把凭据操作和 shell 配置写入混在一起，失败时还会让用户不清楚登录是否完成。

推荐保留的入口：

```text
tf completions zsh --install
tf completions bash --install
tf completions fish --install
```

删除询问时必须保留 `completions` 命令以及现有的“不修改 `.bashrc` / `.zshrc`”边界。

### A6：`status` 默认网络请求

**重构前现状**：`tf status` 对每把本地 Key 并发请求 `/v1/usage`，同时做 harness 检测和环境诊断。`TestUXBaselineStatusFetchesUsage` 用假网关确认：一把 Key 会发出一次 `/v1/usage` 请求。

**实施结果**：`tf status` 默认不联网；`tf status --check` 才请求额度，结果通过 `checked`、`usage` 和 `check_errors` 区分。

**判定：建议拆成快速本地状态与显式远程检查。**

这项没有在本轮 PTY 基线里伪造耗时，因为网络超时和网关响应会污染交互数据；代码路径明确显示每把 Key 最多等待 6 秒的共享 context。默认状态命令若只是回答“本地配置是什么”，不应隐式联网。

建议形态：

```text
tf status          本地配置、Key、harness 和绑定
 tf status --check  显式检查额度、环境冲突和可达性
```

这项已补充 fake gateway 计数测试，覆盖默认模式零请求、显式检查、失败保留本地状态和多 Key 部分成功；超时场景仍应在 Windows/CI 网络环境中补做一次端到端验证。

### A7：透传边界

**现状**：harness 命令遇到第一个未知参数或位置参数后全部透传；`--` 强制透传；命令后的 `--help` 属于 harness，tf 帮助使用 `tf --help <harness>`。

**判定：暂不消融。**

这是 tf 与底层 harness 共存的核心契约。它确实有认知成本，但改成全量解析或再加一层子命令都会增加冲突。当前应继续通过帮助、补全和 PTY 测试降低误解，不在第一轮重构中改变语义。

### A8：Esc 语义

**现状**：有过滤词时第一次 Esc 清除过滤，没有过滤词时取消；辅助槽位的 Esc 使用推荐值并继续。

**判定：保留当前选择器语义，重新考虑辅助槽位后再评估。**

第一次 Esc 清除过滤是可恢复操作，现有 PTY 测试证明它能避免用户因拼错一个字退出整个选择器。辅助槽位的特殊 Esc 语义会随着 A4 删除 `askSlots` 一并消失，不需要单独重写选择器。

## 不应消融的安全边界

以下步骤虽然增加交互，但不能仅为了减少按键而删除：

- 网页导入收到 Key 后的终端确认。
- 覆盖不同 Key 前的确认；非交互环境必须显式 `--force`。
- `tf logout` 的删除确认；非交互环境必须显式 `--force`。
- 非交互环境禁止静默安装 harness。
- Key 不进入 argv、shell history 或普通输出。
- `--json` 显式开启，stdout 保持单个结构化文档。
- `--key`、`--host`、`--model`、`--effort` 仍只影响本次启动，不写入持久配置，除非用户明确运行 `tf model`。

## 推荐实施顺序

第一批只做能由现有实现直接证明的默认路径简化：

1. `tf login` 默认网页导入，`--with-key` 默认粘贴，移除登录方式选择器。
2. 默认网关直接使用，`--host` 处理自建网关，移除网关选择器。
3. 登录成功自动采用生成的本地名称，冲突时才询问。
4. 首次启动只询问主模型，自动填充辅助槽位。
5. 移除登录后的补全询问，补全改为显式安装。

第二批单独设计并测试：

1. `tf status` 拆分本地读取与远程检查。
2. 重新整理 `internal/cli` 中的流程、持久化和输出边界。
3. 在默认命令帮助中压低高级管理命令的视觉权重，而不是删除兼容入口。

## 实验产物

- 真实 PTY 基线：[`e2e/ux_baseline_test.go`](../../e2e/ux_baseline_test.go)
- 既有网页导入协议与安全边界测试：[`e2e/selector_test.go`](../../e2e/selector_test.go)
- 模型槽和一次性覆盖测试：[`internal/cli/keys_test.go`](../../internal/cli/keys_test.go)
- 当前 CLI 透传契约测试：[`internal/cli/cli_test.go`](../../internal/cli/cli_test.go)
- `status` 隐式网络请求基线：[`internal/cli/ux_baseline_test.go`](../../internal/cli/ux_baseline_test.go)
