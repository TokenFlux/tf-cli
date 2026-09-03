# tf 实施计划

M0–M6 已完成，M7 分发仍缺 npm 平台包，见 [`STATUS.md`](STATUS.md)。这份文档现在只管往后。
设计依据在 `design/` 与 `research/`。

---

## 已锁定的关键决策速查

| | 决定 |
|---|---|
| 定位 | 启动器，进程内注入，退出不留痕，不改用户配置文件 |
| 语言 / 分发 | Go 单二进制；GitHub Actions 交叉编译 + install.sh；npm 平台包待做 |
| 进程模型 | fork + wait（信号转发自己写，换取确定性清理与终端收尾）|
| 绝对禁止 | 本地代理 / MITM / 覆盖 harness UA |
| 认证 | `tf login` 交互选择粘贴或网页导入；管道和显式 flag 直达对应方式。网页导入使用 localhost 回环、可选会话证明与 Origin 预览确认，契约见 [`integrations/web-import.md`](integrations/web-import.md) |
| 参数 | tf 只认自己的一小组 flag，遇到第一个陌生参数起全部透传；`--` 无条件透传 |
| 存储 | `config.json`(0644) / `credentials.json`(0600) 分开；支持 XDG；不用钥匙串 |
| 文案 | 跟随 locale + `TF_LANG`；错误码保持英文常量 |
| 输出 | 显式 `--json`，不因管道自动切换 |
| 模型 | 必须注入；`-m` 空值进选择器；槽按 harness 分开存；flag 绝不写盘 |
| harness 未装 | 交互式二选一，非交互只打印命令，绝不 sudo |
| Telemetry | 默认关 |
| License | Apache-2.0 |
| Windows | 出二进制，只能非交互跑 |

---

## 实际代码结构

```
cmd/tf/main.go
e2e/                pty 端到端测试（build tag pty，make pty）
internal/
  access/     Key / 分组 / harness 的准入判断（纯函数）
  buildinfo/  版本与 User-Agent（-X 注入）
  cli/        命令定义、参数解析、候选收集、状态检查
  completions/ shell 补全脚本与落盘位置
  config/     配置、凭据、XDG、文件权限
  gateway/    HTTP 客户端、协议探测、用量
  harness/    适配表 + 三个 harness 的注入配方 + 安装
  launch/     fork+wait、信号转发、终端复位
  model/      模型 ID 解析（纯函数）
  ui/         输出层、选择器、终端原始模式、--json、错误码
  update/     自更新与安装来源识别
```

原计划里的 `catalog/`（匿名目录）、`precheck/`、`tui/`、`doctor/` 都没有单独成包：
目录查询整个放弃了，其余三个的职责落在 `gateway`、`ui`、`cli` 里。

---

## 往后的路线

排序依据是风险，不是工作量。逐条的展开见仓库根目录的 `todo-*.md`。

### 近期：完善测试与验证

1. ~~启动时检查 settings.json 冲突~~ 已完成
2. ~~pty 端到端测试~~ 已完成（`e2e/`，`make pty`）
3. ~~补充 `gateway` 基于实测应答的固件测试~~ 已完成，覆盖率从 3% 提升至 38.3%。
4. ~~在 Linux 上验证终端防线~~ 已完成（`scripts/linux-check.sh`）。

### 中期：分发与代码结构

5. npm 平台包（optionalDependencies + JS shim，无 postinstall 下载），目标是 `pnpx tf` 开箱可用。
6. ~~拆出 Key 与协议准入、shell 补全逻辑~~ 已完成（`internal/access`、`internal/completions`）；继续拆分仍与命令 I/O 耦合的部分。
7. ~~完善 `CHANGELOG.md`~~ 已完成；`README.en.md` 待补。

### 过渡版：网页导入（已落地）

这里的“过渡版”是阶段名，不对应 SemVer 的 `v0.5.0`。CLI 侧已实现 localhost 回环、终端链接 challenge/HMAC、可选导入证明与 Origin 预览确认，不改 TokenRouter 后端；TokenFlux 网页前端仍需按接入文档接线并联调。前端可不实现会话证明，但未验证连接必须显示警告，不强制阻断。

普通入口为 `tf login [名字]` 后选择“从网页导入”；`--from-web` 保留为显式直达方式。完整字段与浏览器示例见 [`integrations/web-import.md`](integrations/web-import.md)。

### 待定：Windows 交互支持

Windows 交互需基于 `CONIN$` 与 `SetConsoleMode` 重写。待具备实际测试环境后再行支持。

---

## 并行推进：要后端做的三件事

不阻塞任何事，但第一条每天都在影响体验。清单见
[`STATUS.md` 五.C](STATUS.md#c-要后端做的三件事)。

---

## 风险与应对

| 风险 | 状态 | 说明 |
|---|---|---|
| `claude_code_only` 识别在 tf 路径下失效 | **已处理** | 服务端基于客户端指纹校验，tf 保持真实客户端行为，不可用时在候选列表中明确提示原因 |
| 用户配置文件覆盖 tf 注入参数 | **已处理** | `settings.json` 的 `env` 优先级更高，启动前执行检查并告警 |
| harness 迭代导致注入 flag 失效 | 持续跟进 | 记录已验证版本，保持架构轻量与直接透传 |
| 分组配置改动导致探测结果过期 | **已处理** | 启动失败后触发重探，支持 `tf keys --refresh` 刷新缓存 |
| harness 隐式模型槽位引发异常 | **已处理** | 适配表穷举各 harness 槽位并做补齐约束 |
| npm 平台包体积与发布复杂度 | 待规划 | 方案待设计验证 |
