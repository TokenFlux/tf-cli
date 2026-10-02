# TUI / CLI 体验重构实施清单

状态：已实施（阶段 0–6 完成；阶段 7 的包内拆分未做，留作后续独立变更）。

实施偏差记录：

- 自动命名沿用 `suggestKeyName` 的现有输出（复合前缀会生成 `a+b` 形式，它不通过 `validKeyName`）；未新增对自动名的本地规则校验，与旧选择器行为一致。
- `status` 拆分为 `collectLocalStatus` 未做；实现为最小改动：`--check` 在 `runStatus` 内调用 `checkUsage`（原 `fetchUsage`）。
- `warnIdenticalSlots` 按计划保留；自动填充回落主模型时它仍可能触发，这是预期说明而非错误。
- `Config.CompletionsAsked` 字段保留兼容读取，登录不再写入。
- Windows ConPTY 测试（`console_windows_test.go`）已重写为 --with-key / 管道 / host 继承场景，需在有 Windows runner 的环境验证。

本文是基于 [`ux-baseline-and-ablation.md`](ux-baseline-and-ablation.md) 的执行计划。目标是减少默认路径中的决策次数，同时保留凭据安全、协议准入、进程隔离、透传和 JSON 输出契约。本文不改变 `tf` 的产品定位，不引入新的全局 profile，不把 `tf` 变成聊天 REPL 或代理。

## 1. 目标、非目标和完成定义

### 1.1 目标

第一阶段完成后，默认用户路径应当接近：

```text
tf login
  默认进入网页导入
  默认使用 DefaultHost
  自动生成本地 Key 名称
  终端保留一次写入确认

tf login --with-key
  直接读取 stdin 或隐藏输入

tf claude
  首次只选择主模型一次
  辅助模型槽自动填充
  后续启动不再进入槽位选择器
```

目标指标：

| 场景 | 当前基线 | 第一阶段目标 |
|---|---:|---:|
| 交互粘贴登录 | 1 个选择器 | 0 个选择器 |
| 默认网页导入 | 4 个选择器 | 1 个必要终端确认 |
| 显式网页导入并给定名称 | 1 个选择器 | 1 个必要终端确认 |
| 登录后的补全询问 | 1 个选择器 | 0 个选择器 |
| 首次 Claude 启动 | 3 个选择器 | 1 个主模型选择器 |
| 稳定态启动 | 0 个选择器 | 0 个选择器 |
| `tf status` | 每把 Key 1 次 `/v1/usage` | 默认 0 次，显式检查才请求 |

这里的“选择器”指 `ui.Select`/`ui.SelectWith` 的一次决策屏幕，不包含底层 harness 自己的 TUI，也不把网页上的页面操作计入 CLI 屏幕数。

### 1.2 非目标

本轮不做以下事情：

- 不修改 localhost web-import 协议 v1、字段、端口范围、HMAC 算法或 Origin 校验。
- 不移除网页导入的终端确认。
- 不取消不同 Key 覆盖确认、logout 确认或非交互安装保护。
- 不修改 `--` 透传规则、`tf --help <harness>` 规则或底层 `-h/--help` 归属。
- 不改变 `config.json`、`credentials.json` 的路径、权限、原子提交、快照冲突和事务恢复机制。
- 不实现 profile、项目级凭据、钥匙串、代理、MITM、遥测或新的网络缓存层。
- 不在本轮为了“职责清晰”进行大规模包重命名或公共 API 重写。
- 不把高级 `tf model --edit`、`tf keys --refresh`、`tf harness install` 删除；它们仍是显式高级入口。

### 1.3 完成定义

代码层面：

- 普通 `go test ./...`、`go vet ./...`、`go test -tags pty ./e2e/ -v` 全部通过。
- Linux、macOS 和 Windows 的现有登录、取消、终端恢复、Key 覆盖和透传测试仍通过。
- 新增测试能锁住每个默认流程只做一次决策的行为。
- `--json`、错误码、退出码、持久化和安全边界均有测试覆盖。
- 正常启动的缓存快路径仍然不联网；需要刷新时仍只在现有明确路径联网。

文档层面：

- README、web-import 集成契约、帮助输出和变更记录描述同一套行为。
- 归档文档不被误改成当前行为；当前设计文档明确哪些是现状、哪些是未来计划。
- 不保留旧的“默认会弹方式选择器/网关选择器/名称选择器/辅助槽位选择器”示例。

## 2. 先锁定的行为决策

### 2.1 登录方式

默认规则：

| 调用 | 行为 |
|---|---|
| `tf login` | 直接进入网页导入 |
| `tf login <name>` | 直接进入网页导入，导入后使用 `<name>` |
| `tf login --from-web` | 直接进入网页导入 |
| `tf login <name> --from-web` | 直接进入网页导入并固定本地名称 |
| `tf login --with-key` | 直接从 stdin 或隐藏输入读取 Key |
| `echo "$KEY" \| tf login` | 继续识别为管道输入，直接读取，不打开网页选择器 |
| `tf login --from-web --with-key` | 保持用法错误 |

`--with-key` 继续保留。它不是为了增加能力，而是为 SSH、CI、无浏览器环境和用户明确选择粘贴路径提供稳定入口。

不再使用 `loginMethodItems`。不要把 `--with-key` 改成 `--paste`，因为当前文档、补全和用户脚本已经使用 `--with-key`。

### 2.2 网关

默认规则：

- 没有 `--host` 时，若同名 Key 已保存 host，则沿用并归一化该地址；否则使用 `config.DefaultHost`。
- 所有来源的 host 都必须经过 `normalizeHost` 和 `webOrigin` 校验，不能用一个无关 Key 的 host 覆盖默认网关。
- 有 `--host` 时先 `normalizeHost`，再用 `webOrigin` 校验。
- 交互登录不再显示“默认网关/自定义网关”选择器。
- 自建网关路径使用 `tf login <name> --host https://router.example.com`。
- `--host` 的 host 仍不得包含用户信息、查询参数、片段，且只能是 HTTP(S)。

这里要明确取舍：默认网关选择器的删除减少一次决策，但不等于取消自建网关。自建网关属于少数派配置，应该由 flag 直接表达；保存到同名 Key 的 host 仍然属于该 Key 的持久元数据。

### 2.3 Key 名称

默认名称解析顺序：

1. 位置参数。
2. `--key` 兼容现有调用语义。
3. 没有显式名称时，使用 `suggestKeyName(ids, creds.Names())` 的自动名称。
4. 网页的 `key_name` 只保存为来源元数据 `Credential.KeyName`，不再成为默认本地名称候选。

自动名称规则暂不重写：

- 分组前缀单一时使用前缀。
- 多分组时使用排序后的复合前缀或 `multi`。
- 无法识别时使用 `key`。
- 与已存在名称冲突时添加 `-2`、`-3` 等后缀。

覆盖规则不变：

- 自动生成名称已被另一把不同 Key 占用时，不能静默覆盖。
- 交互环境进入覆盖确认；非交互环境返回错误并提示 `--force`。
- 同一把 Key 重复登录不需要覆盖确认。
- 显式名称依旧是用户的明确目标，但不自动授权破坏性覆盖；当前 `TestExplicitLoginNameDoesNotAuthorizeReplacement` 必须保留。
- `--force` 只用于本地名称冲突覆盖，不跳过 web-import 的终端写入确认。

网页来源元数据仍完整保存：`Origin`、`KeyName`、`GroupID`、`GroupName`，不能因为不再用网页名称做本地名称而丢掉。

### 2.4 辅助模型槽位

首次启动规则：

- 只对缺失的主槽 `default` 打开模型选择器。
- 主模型确定后，调用既有 `fill` 为所有辅助槽位自动选择模型。
- 必填辅助槽位必须有值；没有专用档位时回落到主模型。
- 辅助槽位必须来自同一把 Key，且通过 `compatibleSlotModels` 的协议约束。
- 自动填充后的槽位在正常首次配置中持久化；但 `-m`、`-k`、`-e`、`--host` 触发的一次性运行不得把临时结果写盘。
- `tf model <harness> --edit` 继续允许高级用户逐槽调整。
- 自动槽位记录在 `HarnessConfig.AutoSlots` 中。刷新模型目录后，自动值会重新计算；用户通过 `tf model --set` 或 `tf model --edit` 修改的槽位会清除自动标记，不被覆盖。
- `heavy` 优先选择带 `opus`、`pro`、`max` 等明确重型标记的模型；没有明确标记时按模型 ID 的数字版本选择最强候选，因此类似 `gpt-5.6-sol` 主模型配 `gpt-5.5` 会自动得到 `gpt-5.5` heavy 槽。

删除 `askSlots` 的默认调用，不删除 `fill`。这两个函数职责不同：`askSlots` 是交互决策，`fill` 是运行前补齐不变量。

`warnIdenticalSlots` 处理：

- 如果自动填充后所有槽位相同，仍可以保留警告，但只在用户显式编辑或确实存在多个档位时考虑是否显示。
- 第一阶段不删除该检查，先避免改变模型成本提示的语义。
- 如果自动回落到主模型是正常且预期行为，警告文案要降为日志级说明，不能让用户误以为启动失败。

### 2.5 Shell 补全

删除 `runLogin` 末尾的 `offerCompletions(c, cfg)` 调用，不删除 `completions` 命令、脚本生成、安装路径或 `CompletionsAsked` 的兼容读取。

持久化兼容决策：

- 第一阶段不删除 `Config.CompletionsAsked` 字段，旧配置可以继续读取。
- 第一阶段不迁移或重写 config schema。
- 后续版本确认不再需要字段后，再单独做 schema 清理和迁移说明。
- `tf completions <shell> --install` 是唯一安装入口，安装仍只写专用补全目录，不改 shell rc 文件。

这样做的原因是：删除字段属于存储迁移问题，不应和默认登录体验简化混在一个变更里。

### 2.6 `status`

将 `status` 分成两个明确模式：

```text
tf status          本地状态，不联网
 tf status --check  本地状态 + 远程额度/可达性检查 + 环境诊断
```

命令名不新增 `doctor`，因为当前设计已经把 doctor 并入 status；新增 `--check` 足以表达“现在做网络检查”。

默认 `tf status` 仍显示：

- 配置目录。
- 本地 Key 名称和掩码。
- 本地缓存模型数量。
- harness 是否安装、版本、持久绑定和主槽位。
- 本地配置问题，例如 dangling binding、没有主模型、缺失 harness。
- `checkEnvironment` 的本地冲突诊断；它只读取本地环境和配置，不发网络请求。

默认不做：

- `/v1/usage` 请求。
- 网络可达性验证。
- 模型列表刷新。
- 协议重新探测。

`tf status --check` 才做：

- 对每把已保存 Key 请求 `/v1/usage`。
- 保留现有并发和 6 秒总超时策略；不拆分 per-key deadline，避免本轮改变超时行为。
- 记录每把 Key 的检查错误，不因一把 Key 失败丢弃其他 Key 的成功结果。
- 继续运行同一套本地环境冲突诊断；它在默认 `status` 和 `--check` 都出现，避免诊断信息随 flag 消失。
- 不读取或发送 `TF_API_KEY` 运行时凭据；现有 `TestStatusDoesNotBroadcastEnvironmentCredential` 必须继续通过。

JSON 行为固定为：

- `statusOut` 增加始终输出的 `checked`：默认 `false`，`--check` 为 `true`。
- `usage` 继续使用 `omitempty`；默认模式省略该字段，检查模式只包含请求成功的 Key。
- 增加 `check_errors map[string]string`，只在至少一把 Key 检查失败时输出。错误摘要不得包含 Authorization、完整响应体、用户信息或 Key。
- 没有 Key 时 `--check` 仍输出 `checked: true`、空 usage、无 check_errors，不发请求。
- 远程检查失败不改变进程退出码：只要本地状态读取成功，命令返回 0；脚本通过 `checked` 和 `check_errors` 判断检查结果。
- 这个字段属于 JSON schema 变更，需在 `CHANGELOG.md` 的未发布部分说明，并为中英文人类输出保持同样的语义。

### 2.7 顶层命令帮助

第一阶段不删除任何命令，不把高级命令改成隐藏命令。帮助排序和摘要可以在第二阶段处理。

原因：删除默认交互步骤与命令面重排是两个不同风险；前者可以用当前 PTY 数据验证，后者会影响补全、文档和用户记忆。

第二阶段可考虑：

- 将 `claude/codex/opencode/pi/login/status` 放入主帮助的核心区域。
- 将 `keys/model/harness/config/completions/update/version` 作为管理命令区域展示。
- 不改变命令名、别名和补全返回值，除非另行做兼容决策。

## 3. 实施阶段

## 阶段 0：冻结基线与建立回归护栏

### 代码范围

不改生产代码。维护以下实验资产：

- `e2e/ux_baseline_test.go`
- `internal/cli/ux_baseline_test.go`
- `docs/research/ux-baseline-and-ablation.md`

### 任务

1. 保留当前基线表和 PTY 输出。
2. 将实验测试中的指标定义固定为 selector、control、submission，不再把耗时作为硬断言。
3. 为 `status` 基线保留假网关请求计数测试。
4. 将当前工作区已经存在的 harness 安装/Windows 改动视为外部变更，不在体验重构中顺手整理。
5. 创建一个独立分支或至少在实施前保存当前提交引用，便于逐阶段回滚。

### 验收

```text
go test ./...
go vet ./...
go test -tags pty ./e2e/ -v
git diff --check
```

## 阶段 1：删除登录方式选择器

### 代码修改

主要文件：`internal/cli/cmd_login.go`。

1. 删除或停止调用 `loginMethodItems`。
2. 在 `runLogin` 中按以下顺序解析：
   - 读取 `--from-web`、`--with-key`。
   - 两者同时出现时立即返回 `CodeUsage`。
   - 有 `--from-web` 时走 web import。
   - 有 `--with-key` 时走 `readKey`。
   - stdin 非终端时走 `readKey`。
   - 交互式 stdin 终端且没有明确 flag 时，默认走 web import。
3. 保持 `readKey` 使用控制终端隐藏输入，不把 Key 改放到 argv。
4. `tf login` 默认网页导入时，如果没有可用控制终端，必须返回现有 web-import 交互错误，并提示 `echo $KEY | tf login` 或 `tf login --with-key`。
5. 继续让 `--host` 在登录方式解析前生效，避免默认网页导入先做无意义的 host 选择。

### 测试修改

更新：

- `e2e/selector_test.go` 的 `TestInteractiveLoginCanChoosePaste`：改成验证 `tf login --with-key` 直接进入隐藏输入。
- `e2e/selector_test.go` 的 `TestWebImportRequiresTerminalConfirmationBeforeSaving`：删除“选择登录方式”步骤，直接等待网关/网页导入。
- `e2e/selector_test.go` 的 `TestPipedLoginSkipsMethodPicker`：保留并加强为不出现方式选择器。
- `e2e/ux_baseline_test.go`：新增/修改默认 `tf login` 进入 web-import，`--with-key` 进入 paste 的断言。
- `internal/cli/cli_test.go`：删除 `TestLoginMethodItemsFollowLocale`，或者将其替换为输入模式决策的纯函数测试。
- `internal/cli/web_import_test.go`：增加非交互默认登录失败、显式 `--from-web` 仍可识别的测试。
- Windows：更新 `internal/cli/console_windows_test.go`，去掉默认“Choose a login method”步骤；保留 `--with-key`、管道、取消和 host 场景。

### 失败与异常

- `--from-web --with-key`：错误码和文案不变。
- 浏览器打不开：仍打印 URL 并继续等待。
- 无 TTY 的 `tf login`：不尝试从 stdin 交互选择，也不静默启动 web import。
- `--json`/`--no-input`：不能进入选择器；按现有非交互规则返回错误。
- 没有控制终端且 stdin 是管道：按管道规则读取 Key。
- 没有控制终端且 stdin 不是可读管道：返回交互错误，并提示 `echo $KEY | tf login` 或 `tf login --with-key`；不得启动一个等待十分钟的网页导入监听。

### 验收

- `tf login` 不再出现“选择登录方式”。
- `tf login --with-key` 直接出现隐藏输入。
- 管道登录不读取控制终端来选择方式。
- Key 不出现在输出、argv 或测试日志中。

## 阶段 2：删除网关选择器

### 代码修改

主要文件：`internal/cli/cmd_login.go`。

1. 删除默认路径对 `selectLoginHost` 的调用。
2. `loginGatewayItems` 与 `selectLoginHost` 不再是默认路径依赖；在确认没有其他消费者后删除，或暂时保留为下一版本兼容测试迁移期间的未导出死代码后立即删除。
3. 新增纯函数 `loginHost(c, cfg, keyName)` 或等价逻辑，职责只有：
   - `--host` 优先。
   - 同名 Key 有已保存 host 且没有显式 host 时沿用它。
   - 否则使用 `config.DefaultHost`。
   - 统一 `normalizeHost`。
4. host 只在实际需要时校验：网页导入启动前必须通过 `webOrigin`，粘贴登录发起模型请求前必须是合法 URL。
5. 不允许“自建 host 已存在于任意 Key 就全局复用”。host 继承必须按目标名称限定，防止登录 `personal` 时意外连接 `work` 网关。

### 测试修改

- 将 `internal/cli/login_gateway_test.go` 的选择器项测试改为 host 优先级表格测试：显式 host、同名既有 host、默认 host、同名不存在。
- 增加 normalize 变体：裸域名、尾斜杠、`/v1`、非法 scheme、凭据、query、fragment。
- PTY 测试确认默认网页登录不显示“选择网关”。
- 粘贴登录与网页导入都确认最终 host 保存正确。
- Windows 登录测试删除 gateway selector 交互，保留显式 `--host` 和既有 host 继承场景。

### 失败与异常

- 无效 `--host`：在产生网络请求前返回 `CodeUsage`。
- 自建网关不可达：返回现有 `CodeNetwork`，不回退到默认网关。用户明确指定的 host 不能被悄悄替换。
- 既有同名 host 不可达：同样不回退到默认 host，避免 Key 保存到错误服务端。
- `--host` 不写入其他 Key 的配置。

### 验收

- 默认登录路径没有 gateway selector。
- 自建网关仍可用，且 host 与 Key 元数据一致。
- origin 校验、web-import host mismatch 和 URL 安全校验不变。

## 阶段 3：自动命名，移除默认名称选择器

### 代码修改

主要文件：`internal/cli/cmd_login.go`。

1. 保留 `suggestKeyName`，不在此阶段修改命名算法。
2. 将 `chooseImportedKeyName` 拆成两层：
   - `resolveImportedKeyName`：无显式本地名称时自动返回名称。
   - `confirmKeyReplacement`：只有名称冲突时负责确认。
3. `webName` 继续写入 `Credential.KeyName`，但不进入默认选择器。
4. 位置参数/显式 `--key` 仍优先于自动名称。
5. 自动名称冲突时：
   - 名称可以通过 `suggestKeyName` 避开时直接使用新后缀。
   - 如果是显式目标名称冲突，保留确认。
   - 不能为了减少选择器把不同 Key 静默覆盖。
6. 删除 `importedKeyNameItems` 和默认调用 `chooseImportedKeyName`；只有确认覆盖时仍可使用统一确认控件。

### 命名不变量

- 自动生成名称遵循 `suggestKeyName` 的既有规则。普通名称使用安全的模型前缀；复合 Key 的 `a+b` 形式是有意保留的内部自动名，不经过面向用户自定义名称的 `validKeyName` 校验。
- 不能与现有 Key 名重复，除非是同一把 Key 复用已有名称或用户确认覆盖。
- 生成名称只能从本次 `/v1/models` 的模型目录和已有名称得到，不读取网页未经校验的任意文本作为本地标识。
- 多分组 Key 必须保留复合前缀命名逻辑，不能退化为模型最多的单一分组。
- 名称只影响本地索引，不影响网关 Key 本身，不发送回网关。

### 测试修改

- 将 `TestImportedKeyNameItems` 改为 `TestImportedKeyNameAutoResolution`。
- 增加网页名称非法但自动名称合法时的测试：应成功保存，来源元数据保留网页名称。
- 增加自动名称重名时的后缀测试。
- 增加显式名称覆盖不同 Key 的默认取消、`--force` 通过测试。
- 更新 web-import PTY：导入成功后直接断言自动名称，不等待“选择本地 Key 名称”。
- JSON 登录输出断言本地 `name` 是自动名称，`key_name` 仍存在于 credentials 元数据/keys 展示。

### 失败与异常

- 模型目录为空：保持现有登录失败/网络错误，不生成任意名称。
- 网关返回重复或异常模型 ID：沿用当前目录处理和命名 fallback，不改变模型准入逻辑。
- 自动名称与显式名称冲突：区分“自动避让”和“显式覆盖”，不能混用。

### 验收

- 默认网页导入从 2 个后续选择降为 0 个名称选择。
- `tf keys` 仍能显示网页来源、网页名称和分组信息。
- credentials 文件仍是 `0600`，事务写入与恢复不变。

## 阶段 4：自动填充辅助槽位

### 代码修改

主要文件：`internal/cli/cmd_launch.go`。

1. 在 `resolveTarget` 中保留主模型选择、Key 选择、协议过滤和跨 Key 清理顺序。
2. 删除默认交互调用：

```go
if !oneShot && c.UI.Interactive(...) {
    askSlots(...)
}
```

3. 保留并提前/明确调用 `fill(h, slots, own)`，确保填充发生在：
   - 主模型和 Key 已经确定之后。
   - `compatibleSlotModels` 已经完成之后。
   - `oneShot` 持久化分支判断之前。
4. 自动填充后继续执行 `slotsComplete` 或等价验证。对于 Required 槽位，空值必须进入明确错误，不能让 harness 回落到内置默认模型。
5. `oneShot` 逻辑保持：临时 Key/模型/host/effort 的自动补槽只在内存中，不能修改 `cfg.Harness(h.Name)`。
6. 持久化首次启动结果时，保存主槽和自动填充的辅助槽位，以及选定 Key。保存失败只输出现有警告，不阻止已经准备好的本次启动；是否保留该策略不在本阶段改变。
7. 删除 `askSlots`、`suggestForSlot`、`slotSuggestionReason`，除非 `tf model --edit` 复用它们；当前代码中它们只服务启动路径，因此可删除。
8. `warnIdenticalSlots` 第一阶段保留。若它在自动填充场景大量产生噪音，再单独把它降为 debug/log 或增加“只有用户显式编辑时提示”的状态。

### 测试修改

- 更新 `e2e/cancel_test.go`：
  - 删除“在 fast 槽按 Ctrl-C”路径，改为确认首次启动不会出现辅助槽选择器。
  - 保留主模型选择器 Ctrl-C 返回 130。
  - 保留 `tf model --edit` 的 Ctrl-C 测试。
- 更新 `e2e/selector_test.go`：首次 Claude、Codex、opencode 启动验证只出现主模型选择。
- `internal/cli/keys_test.go`：保留 `TestFillPicksCheaperModelForFastSlot`、`TestSlotsComplete`、`TestModelsOfIsScopedToOneKey`、`TestAuxiliarySlotsMustSpeakMainProtocol`。
- 增加 `resolveTarget` 表格测试：每个 harness 的槽位最终都非空、同一 Key、符合主协议。
- 增加一次性模型覆盖不持久化辅助槽的测试，扩展现有 `TestTemporaryKeyDoesNotPersistBindingOrFilledSlots`。
- 更新 UX 基线期望：首次 Claude selectors 从 3 改为 1，稳定态仍为 0。
- 更新 Windows/ConPTY 首次登录或启动场景，避免等待已删除的辅助槽文案。

### 失败与异常

- 没有 fast/heavy/small 对应候选：按既有 `fill` 回落主模型，不报无关错误。
- 辅助候选跨协议或跨 Key：必须过滤掉，不允许为了填满槽位混用凭据。
- 主模型来源于 Key B、持久辅助槽来源于 Key A：清掉不兼容槽，再从 B 自动填充。
- 主模型不存在或缓存过期：仍进入主模型选择/错误恢复路径，不静默使用旧辅助槽。

### 验收

- 首次启动只做主模型决策。
- 注入计划中所有 Required 槽都有明确值。
- 后台任务模型仍不会静默回落到 harness 官方模型。
- `tf model --edit` 仍能完成精细调整。

## 阶段 5：移除登录后的补全询问

### 代码修改

主要文件：`internal/cli/cmd_login.go`、`internal/cli/cmd_completions.go`、`internal/config/config.go`。

1. 删除 `runLogin` 末尾的 `offerCompletions(c, cfg)`。
2. 保留 `offerCompletions` 一段时间只用于旧测试迁移？推荐直接删除函数，避免死代码；删除前确认没有其他调用方。
3. `CompletionsAsked` 字段暂时保留读取兼容，但不再由 login 写入。
4. 如果确认没有读取消费者，下一次独立 schema 清理再删除字段；不要在本次同时改 JSON 版本。
5. `tf completions` 的 `--install` 和直接输出行为保持不变。
6. 补全安装失败、路径选择、zsh/bash/fish 行为不变。

### 测试修改

- 删除或重写 `TestCompletionsAskedOnlyOnce`、`TestCompletionsNotOfferedNonInteractive`、`TestCompletionsAskedOnlyWhenSettled`，让它们测试显式 `completions --install` 而不是 login hook。
- `e2e/ux_baseline_test.go` 的补全询问测试改为断言登录成功后直接退出，且没有“是否安装”文本。
- 增加显式安装的成功、写入失败和 JSON 输出测试。
- Windows 登录测试不再设置 `CompletionsAsked = true` 作为绕过条件；若仍需要隔离旧行为，删掉该字段依赖。
- 补全候选测试继续确保命令注册表与补全一致。

### 失败与异常

- 登录成功不应因补全目录不可写而失败，因为登录不再尝试写补全。
- 用户想安装补全时，显式命令失败必须给出路径和底层错误。
- 不得偷偷写 `.bashrc`、`.zshrc` 或 shell 全局配置。

### 验收

- 登录路径没有补全 selector。
- 登录成功后只输出登录结果。
- `tf completions <shell> --install` 可独立完成安装。

## 阶段 6：拆分 `status` 本地模式和检查模式

### 代码修改

主要文件：`internal/cli/cmd_status.go`、可能新增 `internal/cli/status_check.go` 或 `internal/cli/status_local.go`。

推荐先做最小改动，不马上拆包：

1. 为 `status` 增加 `--check` 布尔 flag。
2. 将 `runStatus` 拆为：
   - `collectLocalStatus(c, st) statusOut`
   - `checkRemoteStatus(c, cfg, creds, *statusOut)`
   - `checkEnvironment(c)` 保持默认和 `--check` 都调用，因为它只读取本地状态。
3. 默认模式不调用 `checkUsage`，不运行任何网络请求。
4. `--check` 调用改造后的 `checkUsage`，返回 `(usage map[string]*gateway.Usage, errors map[string]string)`；保持所有 Key 并发和现有 6 秒总超时。
5. 给 `statusOut` 增加确定字段：

```go
type statusOut struct {
    // existing fields
    Checked     bool              `json:"checked"`
    CheckErrors map[string]string `json:"check_errors,omitempty"`
}
```

   `checked=false` 表示没有请求额度；`checked=true` 表示请求阶段已执行，即使某个 Key 请求失败。
6. `checkUsage` 错误摘要必须通过一个不含凭据的格式化函数生成；API 错误只保留状态/错误码或安全 message，网络错误只保留通用可行动文本。
7. JSON 输出必须仍是一个 envelope；warning/note 不能变成额外 stdout 行。`check_errors` 属于 data，终端 warning 只进统一 warnings 收集器。
8. 人类输出：默认不显示“额度未知”这一行；`--check` 请求失败时显示一条可行动的警告。
9. `status --check --no-input` 可以运行，因为它没有交互确认；`--json --check` 同样允许。
10. `status` 的 `--key` / `--host` 仍不改变 status 的作用域；它们暂时继续出现在全局帮助中以保持 parser 兼容，但 `runStatus` 不使用它们。单独建立后续 flag scope 任务，不在 status 拆分中改变全局解析。

### 测试修改

- `internal/cli/ux_baseline_test.go`：将当前测试改为：
  - 默认 `runStatus` 请求数为 0。
  - `--check` 请求数为 Key 数量。
  - `checked` JSON 字段分别为 false/true。
- `internal/cli/runtime_key_test.go`：保留环境变量 Key 不广播测试，改为显式 `--check` 调用。
- 增加多 Key、单 Key、一个网关返回 500 的测试；多 Key 部分成功时保留成功 usage。
- 增加 JSON 默认模式 `checked:false` 且不含 usage、检查模式 `checked:true` 且含成功 usage 或 `check_errors` 的断言。
- 增加人类输出测试，保证默认 status 仍能看见本地绑定/槽位和环境冲突；`--check` 失败时 warning 不进入 stdout JSON 之外的额外行。
- 超时场景保留为 CI/Windows 网络环境的补充验证，不改变当前 6 秒总超时策略。
- 明确 `harness.Detect` 属于本地进程探测，默认保留；本阶段不改变 Detect 语义。

### 失败与异常

- 默认状态读取配置失败：仍返回配置错误。
- `--check` 某把 Key 网络失败：不让其他 Key 的成功结果丢失；JSON 中记录检查结果或 warning。
- 所有 Key 网络失败：本地状态可读时仍返回 0，`checked=true`，每把失败 Key 写入 `check_errors` 和 warnings；status 作为诊断命令，不因瞬时网络故障变成脚本硬失败。
- 环境冲突检查在默认和 `--check` 都输出；它不得读取或发送 Key。

### 验收

- `tf status` 默认没有 `/v1/usage` 网络请求。
- `tf status --check` 明确发出检查请求。
- `--json` 消费者能区分未检查、检查成功和检查失败。
- 现有 `status` 文案和错误码不出现无解释的退化。

## 阶段 7：职责整理与小型重构

这一阶段只在阶段 1 至 6 稳定后执行。目的是降低后续维护成本，不借机改变用户行为。

### 7.1 `internal/cli` 内部边界

当前 `internal/cli` 混合命令注册、状态读取、远程刷新、候选收集、交互和输出。建议拆成内部语义单元：

```text
internal/cli/
  command_*.go       命令注册、参数和顶层错误转换
  login_flow.go      登录方式、host、名称、保存前后的流程
  launch_flow.go     Key/model/slot 解析和启动计划
  status_flow.go     本地状态与显式检查
  selection.go       candidate、模型候选、Key 所有者和过滤
  persistence.go     loadState、保存和冲突处理的 CLI 包装
  diagnostics.go     环境冲突、隐藏候选、可行动提示
```

不强制按文件名机械迁移。拆分标准是一个文件是否同时拥有三类责任：流程控制、网络/配置副作用、UI 文案。先把纯函数和网络函数分开，再移动交互流程。

### 7.2 `internal/ui` 内部边界

不引入第三方 TUI 框架。保留当前 raw TTY、Windows console、终端恢复和降级选择器。

可做的最小整理：

- `ui.UI` 保留人类/JSON 输出和本地化入口，避免全仓库大规模改调用。
- `Select`/`SelectWith` 继续是列表选择器。
- 新增或统一一个 `Confirm` 语义只在确认行为确实重复时做；不能把所有列表都替换成新的抽象。
- 保留 Esc 清过滤、Ctrl-C 中断、宽度计算、禁用项和无 TTY 编号降级。
- 不在本轮加入鼠标、多选、复杂快捷键、动画或全屏 wizard。

### 7.3 `internal/config` 只保留存储语义

`Config.CompletionsAsked` 暂时兼容读取；登录流程不再依赖它。

不要把自动命名、模型候选、远程检查状态写进 config，除非已有持久化设计明确要求。`config` 应继续负责 JSON、路径、权限、锁、快照和事务；业务流程留在 cli。

### 7.4 `internal/access`、`gateway`、`harness`

- `access` 继续保持纯准入判断，不加入交互。
- `gateway` 继续负责 HTTP、模型目录、usage 和协议探测，不负责选择器。
- `harness` 继续负责适配表、Detect、安装和注入计划，不知道 login/status 的 UI。

### 7.5 代码注释

删除与旧默认行为绑定的注释，例如“先选择登录方式”“只在首次配置该 harness 时问一次”。保留解释安全或协议不变量的注释。

优先写“为什么不能合并/跳过”的注释，不写逐行描述。

## 4. 测试矩阵

### 4.1 登录矩阵

| 场景 | 期望 |
|---|---|
| 交互 `tf login` | web import，无方式 selector |
| 交互 `tf login --with-key` | hidden prompt |
| stdin pipe | 直接读 Key |
| `--from-web` | web import |
| `--from-web --with-key` | usage error |
| `--no-input` | 不打开 selector、不等待 TTY |
| `--json` | 单 JSON envelope，不打开 selector |
| 默认 host | 使用 `DefaultHost` |
| 显式 host | 只使用显式 host |
| 同名 Key 既有 host | 沿用同名 host |
| 不同名 Key 有自建 host | 不沿用 |
| invalid host | 不发请求，usage error |
| browser open 失败 | 打印 URL，继续等待 |
| web import unverified | 警告后仍要求终端确认 |
| web import verified | 显示 verified，仍要求终端确认 |
| auto name | 保存自动名 |
| web name invalid | 不作为本地名，但保存元数据 |
| auto name collision | 生成后缀或进入冲突路径 |
| explicit overwrite | 默认取消，`--force` 通过 |

### 4.2 启动矩阵

| 场景 | 期望 |
|---|---|
| 无 Key | 交互路径进入 login；非交互给出 login hint |
| 无主模型 | 只显示主模型 selector |
| 辅助槽空 | 自动 fill，不显示 selector |
| 辅助槽无便宜档 | 回落主模型，Required 槽非空 |
| 主模型换 Key | 删除 foreign auxiliary slots，再自动 fill |
| 多 Key 同模型 | 尊重已有 owner/绑定优先级 |
| `-m` | one-shot，不写盘 |
| `-k` | one-shot，不写盘 |
| `-e` | one-shot，不写盘 |
| `--host` | one-shot，不写盘 |
| `--no-input` 无模型 | 直接错误，不 selector |
| Ctrl-C 主模型 | 130，不启动 harness |
| Esc 主模型 | 130，不输出错误红字 |
| configured slots complete | 0 selector，零网络快路径保持 |
| stale model | 清空相关槽并重新选择，不 panic |
| protocol mismatch | 解释隐藏模型和 Key 原因 |

### 4.3 Status 矩阵

| 场景 | `status` | `status --check` |
|---|---|---|
| 无 Key | 本地输出/现有 not logged in 语义 | 同上，不发请求 |
| 单 Key | 0 usage request | 1 usage request |
| 多 Key | 0 usage request | N usage requests |
| 网关超时 | 0 request or local-only | warning，不丢其他结果 |
| JSON | `checked=false` | `checked=true` |
| TF_API_KEY | 不向保存 host 广播 | 仍不向保存 host 广播 |
| environment conflict | 按最终决定的模式展示 | 按最终决定的模式展示 |

### 4.4 TUI/终端矩阵

保持现有测试并新增断言：

- macOS/Linux raw TTY。
- Linux 无 `/dev/tty` 的非交互降级。
- Windows `CONIN$`/`CONOUT$`。
- 终端已处于 raw 模式时 hidden input 能恢复 canonical。
- 窄终端模型截断可选且不改变完整 ID。
- 中文和组合字符过滤、退格正确。
- Ctrl-C 返回 130，Esc 取消不显示错误。
- JSON 模式 stdout 只有一个 envelope。

## 5. 输出、错误码和持久化契约

### 5.1 人类输出

登录成功输出仍包含：名称、网关、掩码 Key、模型数量、协议和可运行 harness。减少选择器不应删除这些结果信息。

自动命名后成功文案明确打印最终本地名，例如：

```text
✓ 已保存为 Key "gpt"
```

如果网页名称与本地自动名不同，只有在 `--verbose` 存在时才考虑展示差异；第一阶段不新增 verbose，避免增加命令面。来源元数据由 `tf keys` 查看。

状态输出默认不显示远程额度。`--check` 失败时说明“未检查成功”而不是显示 `0`。

### 5.2 JSON 输出

保持 envelope：

```json
{
  "ok": true,
  "command": "status",
  "data": {},
  "warnings": [],
  "notes": []
}
```

登录自动命名后的 `data.name` 是本地名称；来源网页名称仍在凭据数据和 `keys` 输出中。

`status` 的 JSON 数据固定新增：

```json
{
  "checked": false,
  "check_errors": {}
}
```

`usage` 继续使用 `omitempty`：默认模式省略 usage；`--check` 只写入成功的 Key。`checked` 表示是否执行过远程检查，`check_errors` 表示哪些 Key 检查失败。这样“尚未检查”“检查成功但额度为空”和“检查失败”不会混成同一种状态。

### 5.3 错误码

不新增错误码：

- host 无效继续 `CodeUsage`。
- 没 Key 继续 `CodeNotLoggedIn`。
- 不匹配继续 `CodeProtocolMismatch`。
- 取消继续 `CodeCancelled`，顶层返回 130。
- 覆盖拒绝继续 `CodeCancelled` 或现有用法错误，不能改成普通 internal error。

### 5.4 持久化

不改文件格式：

- `config.json` 仍为 0644。
- `credentials.json` 仍为 0600。
- `CompletionsAsked` 先保留读取兼容。
- 自动填充的槽位只有正常持久化启动写入；显式 one-shot 不写盘。
- 登录自动命名只改变 credentials/config 中的名称键，不改变 Key 内容和来源字段。
- 多文件保存仍使用 `SaveState` 和事务恢复，不拆成两个独立写入。

## 6. 文档和发布清单

### 6.1 README

更新 [README.md](../../README.md)：

1. 快速上手改成 `tf login` 默认直接打开网页导入，不再展示方式选择器。
2. 粘贴路径改成 `tf login --with-key`。
3. 删除“选择默认/自定义网关”的主流程描述；把自建网关放到紧随其后的单独小节。
4. 删除登录后的本地名称选择示例，说明默认自动命名、显式名称和覆盖规则。
5. 首次启动示例只展示主模型选择，不展示 `fast`/`heavy` 选择器。
6. 增加 `tf status --check`，说明默认 status 不联网。
7. 保留 web-import 集成文档链接，不在 README 复制协议字段。
8. 检查 README 中版本号和 harness 实测版本是否仍与 `docs/STATUS.md` 一致；不要在本次体验重构中顺手更新无关历史版本。

### 6.2 Web import 文档

更新 [docs/integrations/web-import.md](../integrations/web-import.md)：

- CLI 入口说明改为 `tf login` 默认 web import。
- 删除“先选择登录方式/再选择网关”的默认流程描述。
- 保留 `--from-web` 作为显式直达写法。
- 明确 `--host` 是自建网关入口。
- 保留网页导入终端确认、未验证会话警告、202 只表示终端接受的语义。
- 保留非交互环境拒绝 web import 的约束。

### 6.3 当前状态与设计文档

更新：

- `docs/STATUS.md`：当前命令行为、缺口和已完成项。
- `docs/PLAN.md`：增加本实施计划链接；不要把详细步骤复制进去。
- `docs/research/ux-baseline-and-ablation.md`：将“待实施”阶段更新为“已实施”，记录实际选择器数和任何偏差。
- `CHANGELOG.md`：在 `[未发布]` 增加改进和可能的破坏性变更。若保留 flag 兼容且命令仍能工作，通常归入“改进”；如果 `tf login` 默认从粘贴变成网页或反之导致脚本行为变化，要明确写出。

### 6.4 补全

更新所有 shell completion 测试和生成脚本涉及的帮助内容。新增或删除 flag 前必须确认：

- `commandNames()` 与注册表一致。
- login 的 `--with-key`、`--from-web`、`--force`、`--host` 仍能补全。
- status 的 `--check` 出现在补全候选。
- 透传边界后仍停止 tf 补全。

### 6.5 发布

实施完成后：

1. 先发布一个预发布版本或内部构建，验证已有配置读取和升级路径。
2. 用独立临时目录做 `tf login`、`tf status`、`tf claude` smoke test。
3. 验证 npm launcher、install.sh、PowerShell 安装产物的版本与帮助输出。
4. 生成 release notes，明确默认交互变化和恢复旧路径的写法。
5. 不在同一版本混入无关 Windows 安装或 harness 适配改动，除非当前工作区改动已单独验证并明确归属。

## 7. 文件级变更清单

### 第一阶段必改文件

- `internal/cli/cmd_login.go`
  - 删除默认 login method selector。
  - 删除默认 gateway selector。
  - 自动命名导入 Key。
  - 删除 login completion hook。
- `internal/cli/cmd_launch.go`
  - 删除默认 `askSlots` 调用。
  - 保留 `fill`、协议过滤、Key 归属、one-shot 不落盘。
- `internal/cli/cmd_status.go`
  - 增加 `--check`。
  - 默认不调用 `checkUsage`。
  - 增加 checked 语义。
- `internal/cli/cmd_completions.go`
  - 删除 login hook 后清理死函数和相关注释；保留显式 completions 命令。
- `internal/cli/cli.go` 或 `commands.go`
  - 仅在 status flag 注册或帮助需要时修改；不要在本轮重写 parser。
- `internal/config/config.go`
  - 预计无需结构性修改；若保留 `CompletionsAsked` 兼容，只更新注释。

### 测试文件

- `e2e/selector_test.go`
- `e2e/cancel_test.go`
- `e2e/ux_baseline_test.go`
- `internal/cli/cli_test.go`
- `internal/cli/login_gateway_test.go`
- `internal/cli/login_replace_test.go`
- `internal/cli/keys_test.go`
- `internal/cli/target_test.go`
- `internal/cli/runtime_key_test.go`
- `internal/cli/web_import_test.go`
- `internal/cli/console_windows_test.go`
- `internal/cli/ux_baseline_test.go`

### 文档文件

- `README.md`
- `docs/integrations/web-import.md`
- `docs/research/ux-baseline-and-ablation.md`
- `docs/STATUS.md`
- `docs/PLAN.md`
- `CHANGELOG.md`

## 8. 风险、回滚和停止条件

### 8.1 高风险点

1. **默认登录语义变化**：旧用户可能把 `tf login` 当作粘贴入口。保留 `--with-key`，并在 changelog 写明。
2. **自动命名误覆盖**：这是最高风险项。自动名冲突必须后缀或确认，任何静默覆盖都阻止合并。
3. **辅助槽位自动填充错误**：会导致主模型能用但后台任务失败。必须保留 Required 检查、协议过滤和同 Key 约束。
4. **status JSON 语义变化**：消费者可能依赖 `usage` 字段。增加 `checked` 并保留 envelope；必要时提供一个版本兼容策略。
5. **Windows 交互回归**：不能只改 Unix PTY。Windows ConPTY 测试必须同步更新并通过。
6. **工作区未提交改动干扰**：本次只处理体验相关文件，不覆盖安装/Windows 相关用户改动。

### 8.2 停止条件

遇到以下任一情况，停止继续合并该阶段并回到方案讨论：

- 自动命名无法在不静默覆盖的前提下保持登录成功率。
- 辅助槽位自动填充无法证明所有 Required 槽位和协议约束成立。
- `status --check` 无法让 JSON 消费者区分“未检查”和“检查失败”。
- Windows 与 Unix 的取消/隐藏输入语义无法保持一致。
- web-import 终端确认或会话验证出现任何弱化。
- 普通启动缓存快路径开始额外联网。
- 一次性 flag 开始写入持久配置。
- 任一现有安全测试、PTY 测试或文件权限测试无法恢复。

### 8.3 回滚方式

每个阶段独立提交，建议提交顺序：

```text
ux: remove login method choice
ux: remove gateway choice
ux: auto-name imported credentials
ux: auto-fill auxiliary slots
ux: remove completion prompt from login
ux: split local status from remote check
refactor: separate cli flow helpers
```

任何阶段出现回归时，只回滚该阶段提交，不回滚用户已有的安装/Windows 改动，也不删除实验记录。若阶段 1 至 5 已发布但阶段 6 失败，可以保留前五阶段，status 继续旧行为；这些变更之间没有必须同时上线的数据库迁移。

## 9. 最终验收命令

实现全部阶段后运行：

```bash
make check
make build
make pty
make cross
make npm-check
```

在没有发布凭据和网络条件时，至少运行：

```bash
go test ./...
go vet ./...
go test -tags pty ./e2e/ -v
go test -race ./...
git diff --check
sh -n install.sh uninstall.sh scripts/uninstall-test.sh
shellcheck install.sh uninstall.sh scripts/uninstall-test.sh
```

涉及 GitHub Actions 的改动再运行：

```bash
actionlint
```

### 人工 smoke checklist

1. 空配置目录运行 `tf login`：直接进入网页导入，不出现方式/网关选择器。
2. `tf login --with-key`：隐藏输入，成功后不出现补全选择器。
3. 网页导入：页面发送 Key 后终端仍必须确认；拒绝后监听继续；确认后再校验和写盘。
4. 自动命名：普通 Key 得到稳定名称；复合 Key 名称包含所有分组；重名不覆盖。
5. 空模型绑定运行 `tf claude`：只出现主模型选择器，辅助槽自动填充。
6. `tf model claude --edit`：高级槽位编辑仍可用。
7. `tf status`：离线也能快速返回，不访问 `/v1/usage`。
8. `tf status --check`：明确执行额度检查，失败时保留本地状态。
9. `TF_API_KEY`：启动可用，但 status 不把它广播给保存账户。
10. `--json`：每条命令 stdout 都是单个合法 JSON 文档。
11. `tf claude --help` 与 `tf --help claude`：仍分别属于 harness 和 tf。
12. Unix、Windows Terminal、Git Bash、无 TTY、SSH 场景分别验证取消、退出码和参数透传。
