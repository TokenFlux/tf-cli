# 架构与设计决策

决策记录与技术选型依据。

---

## 核心架构决策

### A. 进程替换（`exec`）与子进程管理（`fork + wait`）

Unix 上 `syscall.Exec` 直接将自身进程替换为 harness 进程，信号与退出码由系统原生接管，无需转发逻辑；但主进程退出后无法执行后续清理与状态收集。

| 维度 | exec 替换 | fork + wait |
|---|---|---|
| 信号 / TTY / 退出码 | 原生透传 | 需显式转发与终端复位 |
| 退出后清理临时配置 | 不支持 | 支持 |
| 退出后执行清理或输出摘要 | 不支持 | 支持 |
| 跨平台行为一致性 | 平台差异大 | 一致 |

**选定方案：fork + wait。** 当前用于信号转发、退出码穿透和子进程退出后的终端状态复位；架构允许以后增加退出摘要，但目前不在 harness 退出后查询用量。

### B. 参数透传规则与 `--model` 归属

透传原则：

1. 全局 Flag 可写在子命令前或后；命令专属 Flag 写在对应子命令后。
2. 对 `claude`、`codex`、`opencode`、`pi` 等透传命令，首个非内置参数开启透传；写在 harness 名后的 `-h`/`--help` 也开启透传，后续参数全部原样交给底层子进程。查看 tf 对该 harness 的包装帮助请写在命令名前（如 `tf --help opencode` 或 `tf --help pi`）。
3. `--` 之后的内容无条件全量透传。
4. harness 命令中的 `--model` 由 `tf` 解析并转换，处理模型补全、前缀匹配及格式适配后注入。普通管理命令遇到未知 Flag 会报错，不会透传。

### C. 配置与凭据存储结构

存储规划：

- `~/.tf/config.json`（`0644`）：存储 Key 元数据、harness 绑定、默认模型槽及网关映射配置；不存在全局 current profile。
- `~/.tf/credentials.json`（`0600`）：独立存储 API Key 凭据。
- 探测结果和模型列表保存在 `config.json` 的 `keys.<name>` 下，不另写缓存文件。
- `CacheDir` 只为保持 v0 的 `tf config` 文本/JSON 契约而继续报告，生产代码不创建也不读写；旧 `models.json` 不会被静默删除，v1 再评估移除该兼容字段。
- 配置和凭据遵循 `XDG_CONFIG_HOME`；保留的缓存路径计算遵循 `XDG_CACHE_HOME`。未设置时使用 `~/.tf`。

### D. 国际化、文案与机器接口

当前支持中英双语：依次读取 `TF_LANG`、`LC_ALL`、`LC_MESSAGES`、`LANG`，无法识别时回退英文。面向人的 CLI 文案就近写为 `u.T(中文, English)`，不引入 catalog 或第三方框架；只有真实需要第三语言时，才统一评估 `Slot.Purpose(bool)` 一类二元语言接口。

人类输出可本地化：交互标签、提示、错误 `message` / `hint`、帮助标题和 CLI 自己生成的 JSON `warnings` / `notes` 都随语言切换。机器接口不可本地化：错误码（如 `TF_NETWORK`、`TF_NOT_LOGGED_IN`）、命令和 flag、JSON 字段与枚举值、协议端点与参数、模型 ID 和槽位名均保持稳定 ASCII/英文。`Command.Usage` 的占位符同样使用语言中性的 ASCII（如 `tf login [<name>]`）。

展示层把接入端点称作“网关 / gateway”；兼容性接口仍保持 `--host` 与配置、JSON 中的 `host`。登录保存的是命名 Key，不存在全局 profile。

---

## 运行时行为决策

### E. 未登录与未安装 harness 的处理

- 未登录时运行 `tf <harness>`：引导执行 `tf login`。
- 未安装对应 harness：交互模式下提供系统包管理器安装选项，非交互模式下仅打印安装指令，避免隐式越权修改环境。

### F. `--json` 触发原则

采用显式 `--json` 标志控制结构化输出，避免依赖管道隐式推断导致 CI/CD 场景输出不可预期。

### G. 探测结果与模型刷新策略

- 模型列表、协议探测结果与 `probed_at` 保存在 `config.json` 的 Key 元数据中。
- 采用混合模型刷新策略：
  - 登录/网页导入及需要模型候选或校验的路径自动拉取远端 `/v1/models`；
  - 绑定和模型槽完整的正常启动继续直接读取本地缓存，补全只读本地，不设 TTL、不每次联网；
  - `tf keys --refresh` 与启动筛选发现缓存中无可用 Key 的自动自愈路径，均先刷新每把 Key 的 `/v1/models`，再用最新模型 ID 中的分组前缀重探协议；
  - 网络获取模型失败时继续沿用已有缓存。
- 旧 `CacheDir` 和 `~/.tf/cache/models.json` 没有生产读写方；v0 保留路径输出但不自动迁移或删除旧文件。
