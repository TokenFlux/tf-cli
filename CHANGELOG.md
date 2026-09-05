# 变更记录

本文件记录每个版本对**使用者**的影响。内部重构只在会改变行为时才写。

格式参照 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。1.0 之前，
次版本号（0.**x**.0）可以带破坏性变更，补丁号（0.x.**y**）不会。

分节固定用这五个，没有内容就不写：

| 分节 | 放什么 |
| --- | --- |
| 破坏性变更 | 升级后原来的命令不再照旧工作。必须写出替代写法 |
| 新增 | 新命令、新选项、新能力 |
| 改进 | 原有行为变得更好，但不需要用户改动什么 |
| 修复 | 修掉的缺陷。写清症状，用户是靠症状认出自己遇没遇到过 |
| 安全 | 与凭据、权限、网络边界有关的改动 |

写法要求与项目其余文案一致：**说清楚发生了什么、为什么**，不堆功能名。
每条都从用户看得见的现象出发，不从函数名出发。

---

## [未发布]

（暂无）

---

## [0.9.0] — 2026-09-06

### 新增

- 交互登录和网页导入可选择默认 TokenFlux 网关或输入自定义地址；显式 `--host` 跳过选择，已有自建网关账户优先沿用原地址，成功导入后才保存。
- Windows 原生控制台支持登录、方向键选择、中文及补充字符过滤、隐藏输入、行编辑与取消。建议使用 Windows Terminal 的 Git Bash；npm/pnpm 客户端需要 Git Bash 在 PATH 中。
- `install.sh` 支持 Windows x64 Git Bash，下载并校验 ZIP 后安装 `tf.exe`；`uninstall.sh` 同步支持 Windows 二进制与更新备份，默认保留凭据。
- 新增独立的 `install.ps1` / `uninstall.ps1`，支持 Windows PowerShell 5.1 和 PowerShell 7，不依赖 Git Bash 或外部下载、解压工具。校验失败及程序被占用时保留旧版本，卸载默认保留凭据，`-Purge` 不遍历目录链接。

### 修复

- Windows 客户端退出后恢复控制台输入输出模式，将控制台中断状态转换为退出码 130。
- Windows npm/pnpm 启动器通过配套 shell shim 启动，保留空参数、空格、引号和特殊字符，不将参数作为命令字符串执行。
- 修复 Windows `go install` 来源识别；自更新替换失败时尝试恢复原可执行文件。

### 安全

- Windows 配置目录、凭据和事务日志使用受保护的 DACL，仅允许当前用户、Administrators 和 SYSTEM 访问，不再把 `chmod 0600` 当作 Windows 的访问控制。

---

## [0.8.0] — 2026-09-05

### 破坏性变更

- `TF_API_KEY` 改为仅供本次启动使用，不再覆盖已保存账户的凭据。默认连接默认网关，自建网关需显式传入 `--host`；指定 `-k` 时优先使用对应的本地 Key。`tf status` 和 `tf keys` 仍查询已保存的账户。
- 指定名称登录时，覆盖已有不同 Key 也需要确认，默认取消。非交互覆盖请使用 `tf login <name> --force`。

### 改进

- 容器和 CI 可直接通过 `TF_API_KEY` 启动，无需先保存本地凭据；运行时 Key、模型目录和槽位不落盘。
- `tf status` 对没有总限额的 Key 显示账户可用额度，不再显示 `0/0`；区分额度耗尽、真正不限额和数据缺失。
- `tf status` 的“今日”一行显示实际扣费和计费单位，使用 `usage.today.actual_cost`，最多保留四位小数；不以倍率前的费用代替实际扣费。
- 模型编辑器切换 Key 前先列出将被清空的槽位并确认，默认取消。
- 窄终端中的选择器按可用行列数布局，长文本显示省略号，选中后仍使用完整模型 ID；中文和带颜色文本的宽度计算与截断保持一致。

### 修复

- 切换 Key 或主模型时重新校验辅助槽，避免主会话、后台任务串用旧 Key 的模型；同名模型优先保留当前绑定账户。
- `-k`、`-e`、`--host` 与 `-m` 只影响本次启动，自动补齐槽位也不会修改已保存的绑定。
- 副模型选择器和模型编辑器中的 Ctrl-C 终止命令并返回 130，不再继续启动客户端；Esc 保留返回或跳过当前步骤的行为。
- 修复中文分组过滤无匹配项、退格截断多字节字符的问题。
- 带分组前缀的模型切换思考强度时保留分组，不再误报变体不存在或匹配到其他分组。
- 重复 `--set` 会校验全部赋值后一次保存，不再只保留最后一项；重复赋值同一槽或任一赋值无效时明确报错，不部分保存。
- 并发修改配置时拒绝旧快照覆盖，避免登录恢复刚被退出命令删除的 Key；双文件事务支持进程中断后的恢复。

### 安全

- 网关认证请求禁止跨主机、跨端口及 HTTPS 降级重定向，避免 `x-api-key` 被转发到其他 origin。
- `tf status` 不再将环境变量中的 Key 发送到所有已保存网关。
- 代理诊断不再输出 URL 中的用户名、密码和查询参数。
- 网页导入覆盖已有 Key 时，终端确认默认选择拒绝；配置事务恢复日志权限固定为 `0600`，完成后删除。

---

## [0.7.0] — 2026-09-04

### 新增

- 新增 Pi coding agent 支持（命令 `tf pi`，进程名 `pi`）：
  - 接入协议覆盖 `openai_responses`（映射 `openai-responses`）、`anthropic_messages`（映射 `anthropic-messages`）与 `openai_chat_completions`（映射 `openai-completions`），通常优先 Responses，Claude 模型在允许时优先原生 Anthropic，Chat 作为兼容回退；绝不伪装 Claude Code，`claude_code_only` 分组仍不可用于 Pi。
  - 启动采用纯进程环境与扩展方案：通过 `--extension` 加载权限 `0600` 的临时 JavaScript Extension 动态注册随机名 provider，退出后自动删除；网关 API Key 仅保留在进程环境变量中，不进入 argv、临时文件或 `~/.pi/agent/auth.json`、`~/.pi/agent/models.json` 等持久配置，Pi 原有的 settings、扩展、技能与会话仍正常加载。
  - 模型槽定义仅需 `default` 主模型槽；网关 `/v1/models` 仅提供模型 ID 时采用保守自定义模型缺省（128K context、16K output、text-only）；`tf pi -e high` 映射到 `pi --thinking high`，仅在传入独立 effort 时声明 reasoning。
  - harness 缺失时引导安装选项支持 `npm install -g --ignore-scripts @earendil-works/pi-coding-agent` 与 `pnpm add -g --ignore-scripts @earendil-works/pi-coding-agent`。
- 新增可重复执行的卸载脚本 `uninstall.sh`，用于删除由 `install.sh` 安装在 `TF_INSTALL_DIR`（默认 `~/.local/bin`）的 `tf` 二进制；默认保留当前生效的配置、凭据与缓存，支持通过 `--purge` 显式清理。

---

## [0.6.0] — 2026-09-04

### 改进

- 对 `claude`、`codex`、`opencode` 这类透传命令，harness 名后的 `-h` / `--help` 改为透传给底层工具（如 `tf opencode --help` 查看 OpenCode 帮助）；查看 tf 包装层对此 harness 的帮助改用 `tf --help opencode`。
- `tf keys --refresh` 与启动筛选无可用 Key 时的自动自愈路径采用统一的先拉取各 Key `/v1/models`、再以最新模型分组前缀探测协议的刷新顺序；网络请求失败时继续沿用已有缓存。
- npm 发布脚本为发布后的 dist-tags 校验增加有界重试（0、1、2、4、8、15、30 秒），解决成功发布后因注册表短时读取到旧 tag 导致流程误判失败的问题。
- 自更新版本比较遵循 SemVer 预发布版本优先级规则，npm 安装的 0.5.3-bootstrap.0 能正确识别正式版 0.5.3 为新版本并给出包管理器升级命令，不再误报已是最新或执行自替换。

---

## [0.5.3] — 2026-09-04

### 新增

- 新增 npm 分发方式：发布主包 `@tokenflux/tf`（暴露 `tf` 命令）及 5 个平台二进制可选依赖包（`@tokenflux/tf-darwin-arm64`、`@tokenflux/tf-darwin-x64`、`@tokenflux/tf-linux-arm64`、`@tokenflux/tf-linux-x64`、`@tokenflux/tf-win32-x64`）。支持 `npm install -g @tokenflux/tf` 全局安装与 `npx @tokenflux/tf` / `pnpx @tokenflux/tf` 单次调用。
- 主包采用平台二进制分发架构，仅需 Node >=18 启动轻量 JS launcher，无 postinstall 外部下载脚本且无运行时 JS 依赖；自动透传 stdio、信号与退出码。

### 改进

- `tf update` 自动识别 `node_modules` 下的安装路径，检测到更新时提示运行 `npm install -g @tokenflux/tf@latest`，避免自更新机制直接覆盖包管理器文件。
- npm 发布脚本在远端 package 文档尚未完成传播时改用 dist-tags 识别已存在版本，自动跳过不可变的已发布版本并校验目标 tag，同时对预发布版本临时占据 `latest` 输出明确提示。

---

## [0.5.2] — 2026-09-04

### 破坏性变更

- 项目仓库与 Go module 从 `github.com/tokenflux/tkr` 改为 `github.com/tokenflux/tf-cli`。源码安装请改用 `go install github.com/tokenflux/tf-cli/cmd/tf@latest`；命令名、二进制名、配置目录和 `TF_*` 环境变量保持不变。

### 新增

- `tf login --from-web` 现在支持 TokenFlux 网页通过本机回环地址导入 Key，并在终端确认后复用现有网关校验和凭据保存流程。
- 新增网页前端接入协议文档，说明端口发现、CORS/LNA/PNA、请求字段、响应状态和安全边界。

### 改进

- 同步实施计划、项目状态与测试数字；明确网页导入的“过渡版”是阶段名，不对应 SemVer `v0.5.0`。
- `tf login` 未指定方式时默认高亮网页导入，并在监听就绪后打开 Keys 页面；粘贴选项、管道输入及 `--with-key` / `--from-web` 直达方式保留。
- 网页导入未显式指定本地名称时，可在网关校验后选择按模型自动命名、采用合法的网页 Key 名称或自订名称；自动候选会避开已有名称。
- 统一中英文登录文案、网关展示术语与模型数单复数；帮助用法占位符保持语言中性的 ASCII。
- `tf keys` 现在显示网页导入的 Origin、分组和远端 Key 名，JSON 输出同步提供来源字段。
- harness 安装完成后不再写入无人消费且可能随外部升级失真的 `installs` 记录；实际安装状态继续以 PATH 探测为准，旧字段会在下次保存配置时清理。
- v0 继续保留 `tf config` 的 `cache_dir` 输出以兼容机器调用，但不再把该路径描述为现有存储层，也不会静默迁移或删除旧缓存文件。

### 修复

- 手动触发发版时明确检出输入 tag，避免用 main HEAD 构建旧 tag 的产物。
- 删除 CI 中重复的五目标交叉编译，并把发版检查移到构建矩阵之前。
- GitHub Actions 升级到 Node 24 运行时版本，Go 缓存改用现有的 `go.mod` 作为依赖键，消除零依赖项目缺少 `go.sum` 的恢复告警。
- 归档不再作为现行依据的早期分组识别、模型选择、下一批功能、产品决策、后端认证讨论稿及时间戳 TODO；仍有效的开放项已迁入 PLAN / STATUS。
- 修正文档中的缓存存储、参数透传、探针名、默认登录名称、XDG 卸载路径和网页前端落地状态。

### 安全

- 网页导入只监听 `127.0.0.1:43110-43119`，严格校验 Origin、限制请求体并拒绝未知 JSON 字段；Key 不回显到 HTTP 响应。
- 网页导入始终要求交互式终端确认，`--json` 和 `--no-input` 不能绕过确认。
- 网页导入链接新增短期 session secret；前端可通过绑定实际端口与随机 challenge 的 HMAC 验证 CLI，并用可选导入证明让终端显示相同状态。普通端口扫描和无证明导入保持兼容，但页面与终端必须显示未验证警告。
- `/import` 在读取请求体前占用单一事务，并将 body 读取限制为 10 秒，避免并发慢请求持续占用处理协程；会话关闭超时后会强制关闭活动连接。
- 收紧 challenge、网关 hostname 和终端展示元数据校验；前端示例会在 Keys 路由初始化时清除有效或畸形的 `#tfcli=...` fragment，并明确导入证明不提供防重放语义。

---

## [0.5.1] — 2026-08-31

### 改进

- 重写 README：安装、快速上手、模型槽、多 Key、状态诊断和注入机制现在按
  实际使用顺序组织，并明确 Linux、macOS 与 Windows 的支持边界。
- 统一登录、退出、补全、模型选择、状态检查及环境冲突提示的中文措辞；
  英文文案、命令参数和运行行为不变。
- 发布页现在同时包含手写变更记录和 GitHub 自动生成的完整变更链接，
  并列出各平台产物、校验方法、构建提交与 Go 版本。

---

## [0.5.0] — 2026-08-31

### 新增

- `tf status`：提供环境诊断命令，集中查看当前保存的凭据、剩余额度、harness 安装状态及环境配置覆盖项。
- 额度查询接入网关 `/v1/usage` 接口，准确区分 429 响应属于并发限流还是配额耗尽。

### 改进

- 启动前检查 `~/.claude/settings.json` 中的 `env` 配置覆盖项，若与本次注入冲突将提前输出警告。
- 交互选择器输入错误序号时支持重新输入（最多重试三次）。

### 修复

- 修复额度耗尽时协议探测被误判为「可用」的问题。
- 修复无交互环境下管道输入导致选择器提前读取 EOF 的问题。
- 修复补全脚本安装失败后仍错误记录为「已询问」的问题。
- 修复无法连接网关时返回错误码 `TF_NOT_LOGGED_IN` 的误导性提示，统一调整为 `TF_NETWORK`。
- 优化缺失环境下的安装提示信息，避免在无对应包管理器环境下输出无效安装指令。
- 补充 Linux 系统下 zsh 补全的标准安装路径 `/usr/share/zsh/site-functions`。

---

## [0.4.0] — 2026-08-30

### 破坏性变更

- **移除 `-y` 简写**。业界惯例里 `-y` 是「替我全答 yes」，而这里的含义
  相反：需要回答时直接失败。语义反过来的简写迟早会让人误操作。
  改用 `--no-input`，旧全名 `--yes` 仍然接受。
- **`tf model <harness>` 恒为只读**，要改用 `tf model <harness> --edit`。
  此前它在有终端时会打开编辑器、没终端时只打印 —— 同一条命令按环境
  决定写不写盘，是不能接受的。

### 改进

- 选择器里 `j` `k` `q` 不再当导航键。此前想过滤 `claude-haiku`、`qwen`、
  `kimi` 的人一按就跳走。
- 长列表会写明「第 1-8 个，共 13 个」，不再让人猜下面还有没有。
- 副槽位按 esc 会用推荐值补齐，不再要求逐个填完。
- 拉模型列表前会打一行「获取模型列表…」，不再空等。
- 取消操作统一走退出码 130。
- 全局选项可以写在子命令前面（`tf --json status`）。
- `logout --all` 需要确认。删单把 Key 同样需要 —— 两者都只能回网页重新取，
  没有理由区别对待。

### 修复

- `-m` 后面跟一个不是模型名的词时，那个词会被吞掉。现在会退还给 harness。
- 模型名打错时给出最接近的候选。
- 过滤会匹配到详情与说明，不再只看标题。

---

## [0.3.0] — 2026-08-30

### 新增

- Tab 补全支持 bash / zsh / fish：`tf completions <shell> --install`。
  登录后会问一次要不要装，答过就不再问。

### 修复

- zsh 下 `tf codex <TAB>` 会重复补出 `codex`。数组展开没加引号，
  zsh 把末尾的空词丢了。
- 补全在参数中间位置不给候选。
- 补全脚本装进了不在 `fpath` 里的目录，等于让用户自己去改 `.zshrc`。

---

## [0.2.0] — 2026-08-29

### 破坏性变更

- **命令名由 `tkr` 改为 `tf`**。配置目录 `~/.tkr` 改为 `~/.tf`，
  环境变量前缀 `TKR_` 改为 `TF_`，错误码同步。
  升级后请手动迁移：`mv ~/.tkr ~/.tf`。

### 改进

- 网关地址改为编译期注入，自托管方可以打出指向自己网关的二进制。
- 没有 Key 时当场引导登录，不再打发用户去跑另一条命令。

---

## [0.1.0] — 2026-08-29

首个版本。

- 启动三个 harness：`tkr claude`、`tkr codex`、`tkr opencode`。
  只做进程内注入（环境变量与命令行参数），**不改各 harness 的用户配置文件**。
- `tkr login` / `tkr logout` / `tkr keys` 管理 Key。
- `tkr model` 编辑各 harness 的模型与档位绑定。
- `tkr harness install` 引导安装缺失的 harness，绝不 sudo。
- `tkr update` 自更新。
- 方向键选择器，支持输入过滤。
- 中英双语，跟随系统 locale，`TKR_LANG` 可覆盖。

[未发布]: https://github.com/TokenFlux/tf-cli/compare/v0.7.0...HEAD
[0.7.0]: https://github.com/TokenFlux/tf-cli/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/TokenFlux/tf-cli/compare/v0.5.3...v0.6.0
[0.5.3]: https://github.com/TokenFlux/tf-cli/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/TokenFlux/tf-cli/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/TokenFlux/tf-cli/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/TokenFlux/tf-cli/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/TokenFlux/tf-cli/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/TokenFlux/tf-cli/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/TokenFlux/tf-cli/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/TokenFlux/tf-cli/releases/tag/v0.1.0
