# 反向流：页面主导的「导入 tf」

> 状态：CLI 侧已实现，TokenRouter Keys 页前端集成已在开放 PR [TokenFlux/TokenRouter#1956](https://github.com/TokenFlux/TokenRouter/pull/1956) 中实现（状态为开放、待人工审查合并、尚未正式上线）。当前接入契约见 [`../integrations/web-import.md`](../integrations/web-import.md)。
> 早期文稿里的“v0.5”指过渡阶段，不对应已发布的 SemVer `v0.5.0`。

问题：既然 CLI→浏览器的授权流要改后端、要过 Cloudflare，为什么不反过来：**在网页上放一个「导入 tf」按钮，把 Key 推给本地 CLI**？

**方向是对的**，而且比 PKCE 流更省：页面本来就登录着、Key 本来就明文可读、后端一行不用改。分歧只在**用什么通道把 Key 从页面递给 CLI**。

---

## 通道一：自定义 URL scheme（`tf://import?key=...`）

这是 CC-Switch 那类工具的做法。对 tf 有两个硬伤：

**1. 光秃秃的二进制注册不了 scheme。**

| 平台 | 注册 scheme 的条件 |
|---|---|
| macOS | 必须是 `.app` bundle，`Info.plist` 里声明 `CFBundleURLTypes`，经 LaunchServices 注册；未签名/未公证还要过 Gatekeeper |
| Windows | 写注册表 `HKCU\Software\Classes\tf\shell\open\command`，需要安装器或首启动自注册 |
| Linux | `.desktop` 文件 + `xdg-mime default` |

当前 `curl install.sh` 会把二进制装进 `~/.local/bin`；规划中的 `pnpx tf` 也只会临时运行平台二进制，**这两条路径都产生不了可注册 scheme 的应用**。要支持就得加 `tf install-handler` 子命令去生成 `.app`/写注册表/写 `.desktop`，macOS 那份还牵扯签名公证。CC-Switch 能用这招是因为它本来就是带 GUI 的桌面应用，这个前提 tf 没有。

**2. Key 走 argv 是真的会泄露。**
`tf://` 最终会被展开成一次进程启动，Key 出现在命令行参数里：同机任何用户 `ps aux` 都能看到，macOS 的 URL 打开事件也会进系统日志。要绕开就得改成传一次性 handle，那又得回去加后端接口，正好是我们想省掉的东西。

**3. 远程场景失效**：浏览器在本机、CLI 在 SSH 的服务器上时，scheme 拉起的是本机的 tf。

---

## 通道二：本地回环 HTTP（推荐）

同一个「页面主导」的想法，把通道换成 CLI 自己起的 localhost 服务，两个硬伤同时消失：

```
用户在终端跑：tf login → 默认高亮「从网页导入」
  （也可用 tf login --from-web 直接进入）
  CLI 绑定 127.0.0.1:43110-43119 中首个可用端口
  打印带实际端口和本次监听 session secret 的 Keys 页链接
  调用系统默认浏览器打开

用户通过终端链接打开 Keys 页
  页面只访问指定端口，通过 challenge/HMAC 验证本次 tf 会话
  用户选择分组 / 勾数据共享 / 建 Key（走现有 POST /api/v1/keys）
  页面 POST http://127.0.0.1:4311x/import  { version, key, host, group_id, ... }
  并可附加绑定原始 JSON body 的 X-TF-Session-Proof

用户直接打开 Keys 页或前端未实现验证扩展
  页面仍可扫描固定端口，但必须标记「未验证」并警告；不强制阻断

CLI 收到 → 终端预览并确认 → 回应 202 → 关闭监听
  → 用 /v1/models 校验 → 选择自动识别、网页或自订名称
  → 写 ~/.tf/credentials.json (0600)
```

优点：

- **后端零改动**，只需前端在 keys 页加一个按钮 + 一段探测逻辑。
- **Key 不进 argv、不进 shell history**，只在回环 POST body 里；通过终端链接完成会话验证时，页面能在发送 Key 前排除固定端口上的伪装服务。
- **不需要注册 scheme**，curl 安装的二进制可直接使用，未来的 npm 平台包也不需要额外注册。
- CLI 不处理网页登录或 Cloudflare 质询；确认后只用导入的 Key 请求配置的网关 `/v1/models` 做现有登录校验。

需要处理的细节：

1. **浏览器本地网络权限**：Chrome 142+ 从 HTTPS 页面访问 `http://127.0.0.1` 会请求 Local Network Access 权限；旧 Chromium 还可能发送带 `Access-Control-Request-Private-Network: true` 的 PNA 预检。本地服务同时返回严格的 `Access-Control-Allow-Origin` 和兼容旧 PNA 的 `Access-Control-Allow-Private-Network: true`。前端应在用户点击后发起探测，并放行 CSP `connect-src`；细节见接入文档。
2. **固定端口段与可选会话验证**：CLI 仍在 43110–43119 中顺序选择端口，兼容旧前端扫描。CLI 打印的 Keys 页链接通过 URL fragment 携带实际端口和本次监听会话 secret，并调用系统默认浏览器打开。实现扩展的前端用 challenge/HMAC 验证该端口，成功时标记“已验证当前 tf 会话”，并用 `X-TF-Session-Proof` 让终端确认页显示相同状态。直接打开页面仍可扫描和导入，但页面与终端都只能标记“未验证”并显示警告。
3. **终端侧预览确认**：CLI 收到导入请求后**不直接落盘**，先在终端打印一份预览并等一个 y/n：

   ```
   收到网页导入请求
     验证  已验证当前 tf 会话
     来源  https://tokenflux.dev
     网关  https://tokenflux.dev
     分组  Claude（Anthropic 格式）
     Key   tk-a1b2…3f2c  「macbook」
     名称  校验后选择
   写入 ~/.tf/credentials.json？[写入 / 拒绝]（方向键选择，回车确认）
   ```

   预览里的 **Origin** 帮助用户识别预期网页，但不是本机进程的身份凭证；本机进程可以伪造该 Header，网页侧会话证明负责补足这个方向。非交互环境不能使用网页导入；`--json` 与 `--no-input` 都会拒绝需要终端确认的流程。
4. **只在用户选择网页导入或显式传入 `--from-web` 后监听**，用户确认后先返回 HTTP 响应并关闭监听，再继续网关校验和写盘；不做常驻守护进程。
5. **威胁模型**：CORS、LNA 和 Origin 预览限制网页请求并保护真实 CLI 的写入流程，但不能认证网页连接到的本机服务。另一个本机用户虽然不能读取当前用户的 `0600` 凭据文件，却能预占固定高位端口并伪装 `/ping`；终端确认发生在 POST 之后，来不及保护未验证路径上的 Key 机密性。

   终端链接提供可选的双向会话证明：secret 只放在 fragment 中，页面先验证绑定实际端口和随机 challenge 的 HMAC，再为 `/import` 原始 body 生成 `X-TF-Session-Proof`，让 CLI 确认请求持有同一 secret。为了兼容旧前端，普通扫描与无证明导入仍保留；页面应提示“未验证本机 CLI”，终端则提示“未验证本机 tf 会话；确认后仍可继续”，但按当前产品决策不强制阻断。

---

## 三条路的取舍

| | 后端改动 | 前端改动 | 能否 `pnpx`/curl 安装 | Key 泄露面 | 远程/SSH |
|---|---|---|---|---|---|
| PKCE 浏览器授权（原方案） | 2 个接口 + Cloudflare 豁免 | 新授权页 | ✅ | 小 | 打印 URL 可用 |
| `tf://` scheme 导入 | 无 | 一个按钮 | ❌ 需 install-handler + 签名 | **argv 可见** | ❌ |
| **localhost 导入（推荐）** | **无** | 一个按钮 + 可选会话验证 | ✅ | 已验证路径小；未验证扫描有本机伪装风险 | ❌（用 `--with-key` 兜底）|

安全边界分成两个方向：终端侧 Origin 预览确认负责授权“是否写入本地凭据”；终端链接中的可选 challenge/HMAC 与导入证明让网页和 CLI 验证双方持有同一会话 secret。未实现后者时只提供兼容路径，并明确显示未验证警告。

---

## 落地顺序（修订）

1. **v0**：`tf login --with-key`，页面上「复制 Key」，终端隐藏输入粘贴。零改动，所有场景可用，也是 SSH 场景的永久兜底。
2. **过渡版（CLI 侧已落地）**：CLI 已实现 localhost 通道和可选会话证明；TokenRouter Keys 页前端集成已在开放 PR [TokenFlux/TokenRouter#1956](https://github.com/TokenFlux/TokenRouter/pull/1956) 中实现（状态为开放、待人工审查合并、尚未正式上线），不改 TokenRouter 后端。接入字段和响应见 [`../integrations/web-import.md`](../integrations/web-import.md)。
3. **v1（可选）**：如果哪天真出了带 GUI 的桌面端，再顺手注册 `tf://`；单独为 CLI 做 scheme 不划算。
4. PKCE 那套授权服务器，留到「要让第三方工具也能接入」时再谈，那时它的价值不再是省事，而是开放能力。

这样历史稿 [`../archive/cli-auth-discussion.md`](../archive/cli-auth-discussion.md) 里那一堆待后端拍板的问题，**大部分可以先不问**：只剩「Key 要不要加来源标记以便撤销」和「默认参数」两条，其余都变成前端 + CLI 内部的事。
