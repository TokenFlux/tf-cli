# tf 实施计划

M0–M7 已经走完，见 [`STATUS.md`](STATUS.md)。这份文档现在只管往后。
设计依据在 `design/` 与 `research/`。

---

## 已锁定的关键决策速查

| | 决定 |
|---|---|
| 定位 | 启动器，进程内注入，退出不留痕，不改用户配置文件 |
| 语言 / 分发 | Go 单二进制；GitHub Actions 交叉编译 + install.sh；npm 平台包待做 |
| 进程模型 | fork + wait（信号转发自己写，换取确定性清理与终端收尾）|
| 绝对禁止 | 本地代理 / MITM / 覆盖 harness UA |
| 认证 | v0 `--with-key`；v0.5 网页「导入 tf」+ localhost 回环 + 带 Origin 的预览确认 |
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
  buildinfo/  版本与 User-Agent（-X 注入）
  cli/        命令定义、参数解析、候选收集、补全、状态检查
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

### 近期：把已知的坑填上

1. ~~启动时检查 settings.json 冲突~~ 已完成
2. ~~pty 端到端测试~~ 已完成（`e2e/`，`make pty`）
3. 用实测应答给 `gateway` 做固件测试。它只有 3% 覆盖，却承担最微妙的
   判断（从错误文案反推分组准入），现在只有活体探针测过 —— 要联网、
   要额度，今天还因为配额用尽失败过
4. 在 Linux 上验证终端那四条防线。CI 里没有 tty，它们在 Linux 上
   从未真正跑过，而 `stty` 的行为两个平台不完全一样

### 中期：分发与可读性

5. npm 平台包（optionalDependencies + JS shim，无 postinstall 下载），
   目标是 `pnpx tf` 开箱可用
6. 拆 `internal/cli`。3771 行占一半代码，候选收集与 Key 选择是纯逻辑，
   也最该被测，先把它们拆出去
7. `CHANGELOG.md` 与 `README.en.md`

### v0.5：网页导入

网页「导入 tf」按钮 + localhost 回环 + Origin 预览确认。只动前端，
零后端改动。`cmd_login.go` 里已经留了分流点。

不用 `tf://` scheme：curl / npx 装的二进制注册不了，且 Key 会经 argv 泄露。

### 待定：Windows 交互

交互栈整个建在 `/dev/tty` 与 `stty` 上，Windows 要走 `CONIN$` 与
`SetConsoleMode` 重写。**没有 Windows 机器验证之前不动手** ——
交付没测过的交互实现比诚实的报错更糟。

---

## 并行推进：要后端做的三件事

不阻塞任何事，但第一条每天都在影响体验。清单见
[`STATUS.md` 五.C](STATUS.md#c-要后端做的三件事)。

---

## 风险

| 风险 | 状态 |
|---|---|
| `claude_code_only` 识别在 tf 路径下失效 | **已兑现**。它认 UA + TLS 指纹，tf 不伪装所以用不了。不再是未知，隐藏候选时会说明原因 |
| 用户的配置文件盖掉 tf 的注入 | **已兑现**。`settings.json` 的 `env` 会赢。启动前检查并警告 |
| harness 迭代导致注入 flag 失效 | 未发生。记录已验证版本，不做版本嗅探分支。tf 目前无法察觉这类失效 —— 这是结构性缺口 |
| 分组配置改动导致探测结果过期 | 不设 TTL，走失败路径重探。启动失败后 `tf keys --refresh` 会自愈 |
| harness 隐式模型槽撞分组限制 | 适配表穷举每个 harness 的全部槽。opencode 的 `small_model` 已暴露过这类问题 |
| npm 平台包体积与发布复杂度 | 未开始 |
