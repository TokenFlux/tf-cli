# 模型选择：不指定 `--model` 时怎么办

## 前提

**「不注入模型」不是选项。** harness 自带的默认是官方模型 id（`claude-sonnet-4-5`），网关分组里的 id 通常不同名，不注入必然撞 403。所以每次启动都必须解析出一个具体模型。

问题因此变成：**这个模型从哪来，什么时候需要打断用户。**

---

## 结论：三层，默认不打断

### 第一层：有默认就直接用

profile 里已存有该 harness 的模型槽 → **直接启动，不打断**，只在启动横幅打一行：

```
tf → claude   模型 gpt-5.6-sol · 分组 ChatGPT ×4 · 可用率 99.2%
```

启动器的本分是快，每次弹选择器是灾难。但用户必须**知道**自己在用什么，所以这一行不能省。

### 第二层：没有默认（首次）→ 一次性交互选择器

**一次把该 harness 的全部槽位选完**，不只是主模型。选完写进 profile，之后不再问。

理由已被实测证实：opencode 不注入 `small_model` 时会用内置默认的 `gpt-5.4-nano`，
该模型不在分组里，标题生成**静默失败**（主对话正常、退出码 0）。
只选主模型 = 留一个用户看不见的坑。见 `research/harness-probe.md`。

### 第三层：非 TTY 或 `--yes` → 启发式自动选

CI、脚本、`tf run --` 场景绝不能卡住。自动选完打印结果，并提示怎么固定下来：

```
未配置默认模型，自动选择 gpt-5.6-sol。
固定：tf config set model.default gpt-5.6-sol
```

### 显式请求选择：`-m` 不带值

```
tf claude -m          → 进选择器（本次生效，可选择是否记住）
tf claude -m gpt-5.4  → 直接用
tf claude             → 用默认
```

这样「默认不打断」和「想选随时能选」两个需求都满足，不需要额外的子命令。

---

## 选择器长什么样

我们有 marketplace 的公开数据（定价、上下文分段、模态、容量、可用率），所以列表的信息密度可以远高于任何 harness 自带的模型选择：

```
选择 claude 使用的模型                      Key: macbook · 分组 ChatGPT

>  gpt-5.6-sol      $10/$60 /M   272K   ×4    可用率 99.2%   并发 15/310
   gpt-5.6-terra    $10/$60 /M   272K   ×4    可用率 99.2%
   gpt-5.4          $10/$60 /M   272K   ×4    可用率 99.2%
   gpt-5.5          $12/$72 /M   272K   ×4    可用率 98.7%

   ↑↓ 选择   enter 确认   a 全部分组   ? 详情
```

要点：

- **和 `tf models` 复用同一套渲染**，一套代码两个入口。
- 复合 Key 时按分组分组显示，并标出每个分组的倍率差异（实测倍率从 0.8 到 50，这个差异用户必须看见）。
- 只列**该 harness 协议可用**的分组里的模型（预检前置到这里，用户根本选不到会 403 的组合）。
- 分组只有一个模型时（如 ChatGPT Image）**直接用，不弹选择器**。

---

## 每个 harness 的槽位

适配表必须**穷举**自己用到的全部槽位。漏掉一个，用户就会遇到一次静默失败。

| harness | 槽位 | 注入到 |
|---|---|---|
| claude | `fast` / `default` / `heavy` | `ANTHROPIC_DEFAULT_{HAIKU,SONNET,OPUS}_MODEL` |
| codex | `default` / `review` | `-c model=` / `-c review_model=` |
| opencode | `default` / `small` | 配置内容里的 `model` / `small_model` |

## 一屏确认，不是逐槽追问

无论几个槽，都用**智能预选 + 一屏确认**，回车即走：

```
claude 需要三个模型档位，已按价格自动推荐：

  fast (haiku)     gpt-5.4          $10/$60 /M
  default (sonnet) gpt-5.6-sol      $10/$60 /M
  heavy (opus)     gpt-5.5          $12/$72 /M

  enter 接受   ↑↓ 选中某槽 + enter 单独修改   e 全部逐槽编辑
```

预选启发式：按输出单价排序，最便宜 → `fast` / `small`，最贵 → `heavy`，
`sort_order` 最靠前 → `default`；`review` 默认跟随 `default`。

---

## 修改接口：`tf model`

选择不是一锤子买卖，必须能随时改。交互与非交互两条路都要有：

```bash
tf model                       # 交互：先选 harness，再一屏编辑其全部槽位
tf model claude                # 直接编辑 claude 的槽位
tf model --list                # 表格展示所有 harness 的当前槽位

tf model claude --set default=gpt-5.6-sol --set fast=gpt-5.4   # 非交互，可脚本化
tf model claude --reset        # 清空，下次启动重新引导
```

约束：

- `--set` 的模型 ID 要经过校验（存在于分组、且该 harness 的协议可用），
  校验不过直接拒绝并列出可选项 —— 不要等到启动 harness 时才炸。
- `--list` 要标出**未配置**的槽位，因为未配置意味着 harness 会用它的内置默认值，
  而那个值大概率不在分组里。
- 与启动期的 `-m` 分工明确：`tf claude -m` 是**本次运行**改主模型，
  `tf model claude` 是**持久修改全部槽位**。

**档位不足时的回退**：分组模型少于三个时，多个槽填同一个模型（比如只有一个模型就三槽全填）。要明确告诉用户「该分组模型不足三档，`/model` 切换不会有区别」，否则用户会以为切了没生效。

---

## 一个容易写错的地方：模型槽必须按 harness 分开存

不能在 profile 里存一份全局的模型槽。原因：

- claude 走 `anthropic_messages`，codex 走 `openai_responses`，**可能落在完全不同的分组**（复合 Key 场景下尤其如此）。
- 同一个模型 id 在不同协议下未必都可用。

所以存储结构是 `profile → harness → 模型槽`：

```json
{
  "profiles": {
    "default": {
      "host": "https://tokenflux.dev",
      "harnesses": {
        "claude": { "fast": "...", "default": "...", "heavy": "..." },
        "codex":  { "default": "..." },
        "opencode": { "default": "...", "small": "..." }
      }
    }
  }
}
```

---

# 附：harness 未安装时的处理（E 项定案）

定为**交互式选择：退出 / 由 tf 安装**。需要配套的护栏：

1. **必须展示将要执行的完整命令**，用户看着它按回车，而不是「正在安装…」。
   ```
   未检测到 claude。
     > 用 npm 安装：npm install -g @anthropic-ai/claude-code
       退出
   ```
2. **非交互环境（非 TTY / `--yes` / `--json`）默认拒绝安装**，只打印命令并以非零退出。自动化场景里静默装东西是不可接受的。
3. **绝不 sudo，绝不切换用户的包管理器**。检测到什么用什么（npm / pnpm / bun / brew），检测不到就只打印命令。
4. **记录安装来源**，`tf doctor` 能说清「claude 是 tf 在某时用某命令装的」，卸载时有据可循。
5. 安装失败时输出原始错误，不做包装，让用户能直接搜。

（未登录的情形与此不同：目录类命令 `models` / `groups` 公开可用，仍正常工作；只有需要凭据的操作才提示 `tf login`。）
