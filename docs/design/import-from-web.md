# 反向流：页面主导的「导入 tf」

问题：既然 CLI→浏览器 的授权流要改后端、要过 Cloudflare，为什么不反过来——**在网页上放一个「导入 tf」按钮，把 Key 推给本地 CLI**？

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

我们刚定的分发方式是 `curl install.sh` 装进 `~/.local/bin` 和 `pnpx tf` —— **这两条路径都产生不了可注册 scheme 的东西**。要支持就得加 `tf install-handler` 子命令去生成 `.app`/写注册表/写 `.desktop`，macOS 那份还牵扯签名公证。CC-Switch 能用这招是因为它本来就是带 GUI 的桌面应用，这个前提 tf 没有。

**2. Key 走 argv 是真的会泄露。**
`tf://` 最终会被展开成一次进程启动，Key 出现在命令行参数里 —— 同机任何用户 `ps aux` 都能看到，macOS 的 URL 打开事件也会进系统日志。要绕开就得改成传一次性 handle，那又得回去加后端接口，正好是我们想省掉的东西。

**3. 远程场景失效**：浏览器在本机、CLI 在 SSH 的服务器上时，scheme 拉起的是本机的 tf。

---

## 通道二：本地回环 HTTP（推荐）

同一个「页面主导」的想法，把通道换成 CLI 自己起的 localhost 服务，两个硬伤同时消失：

```
用户在终端跑：tf login
  CLI 在 127.0.0.1 的固定端口段（如 43110-43119）监听，终端显示配对码 例如 7F2A

用户在浏览器打开 tokenflux.dev/keys
  页面探测 http://127.0.0.1:4311x/ping  → 探到就把「导入 tf」按钮点亮，
                                          并显示 CLI 返回的配对码 7F2A
  用户核对码一致 → 选分组 / 勾数据共享 / 建 Key（走现有 POST /api/v1/keys）
  页面 POST http://127.0.0.1:4311x/import  { key, host, group_id }

CLI 收到 → 写 ~/.tf/credentials.json (0600) → 终端打印「已导入」→ 关闭监听
```

优点：

- **后端零改动**，只需前端在 keys 页加一个按钮 + 一段探测逻辑。
- **Key 不进 argv、不进 shell history**，只在回环 POST body 里。
- **不需要注册 scheme**，`pnpx tf` / curl 安装的二进制原样可用。
- CLI 全程不发起任何对 tokenflux.dev 的认证请求，**和 Cloudflare 完全无关**。

需要处理的细节：

1. **Private Network Access**：Chrome 从 HTTPS 页面访问 `http://127.0.0.1` 需要 CORS 预检带 `Access-Control-Request-Private-Network: true`，本地服务要回 `Access-Control-Allow-Private-Network: true` + `Access-Control-Allow-Origin: https://tokenflux.dev`（自托管实例要把自己的域名也加进允许列表，可由 `--host` 推导）。localhost 本身是 secure context 例外，不会触发 mixed content 拦截。
2. **固定端口段**：页面无法得知随机端口，所以要约定一个小范围（如 43110–43119）依次探测；被占用就往后顺延。
3. **终端侧预览确认**（取代配对码）：CLI 收到导入请求后**不直接落盘**，先在终端打印一份预览并等一个 y/n：

   ```
   收到导入请求
     来源  https://tokenflux.dev        ← 请求的 Origin，最关键的一行
     主机  https://tokenflux.dev
     分组  Claude（Anthropic 格式）
     Key   tk-a1b2…抉3f  「macbook」
   写入 ~/.tf/credentials.json？[y/N]
   ```

   预览里写出 **Origin** 是重点：用户能一眼看出这把 Key 是不是从自家站推过来的，比对一串无意义的配对码有信息量得多。非交互环境用 `--yes` 跳过。
4. **只在 `tf login` 运行期间监听**，导入成功即关闭；不做常驻守护进程。
5. **威胁模型**：监听只绑 `127.0.0.1`，恶意的**本地**进程本来就能读凭据文件，不是新增攻击面。真正要防的只有一件事：恶意网页往你的 CLI 里塞一把**攻击者的 Key**，让你的流量（连同 prompt）跑在对方账号上。带 Origin 的预览确认直接堵住这条；再加 CORS origin 白名单（一个 header）和「只在 login 期间开窗」，就够了。

   不做配对码的理由：它只多解决一个边缘情形——同时开着多个 `tf login`，页面连上了不是你盯着的那个。而有了预览确认，这种情形的后果也只是「另一个窗口在等确认、这个窗口没反应」，用户扫一眼就知道，不值得为它给所有人加一道核对手续。

---

## 三条路的取舍

| | 后端改动 | 前端改动 | 能否 `pnpx`/curl 安装 | Key 泄露面 | 远程/SSH |
|---|---|---|---|---|---|
| PKCE 浏览器授权（原方案） | 2 个接口 + Cloudflare 豁免 | 新授权页 | ✅ | 小 | 打印 URL 可用 |
| `tf://` scheme 导入 | 无 | 一个按钮 | ❌ 需 install-handler + 签名 | **argv 可见** | ❌ |
| **localhost 导入（推荐）** | **无** | 一个按钮 + 探测 | ✅ | 小 | ❌（用 `--with-key` 兜底）|

安全兵力全部压在**终端侧带 Origin 的预览确认**一处：不管请求从哪来、辛不辛苦地劈了哪个端口，写盘前都要用户看一眼。它同时盖住了「导错终端」和「恶意页面推 Key」，所以不再需要额外的配对机制。

---

## 落地顺序（修订）

1. **v0**：`tf login --with-key`，页面上「复制 Key」，终端隐藏输入粘贴。零改动，所有场景可用，也是 SSH 场景的永久兜底。
2. **v0.5**：keys 页加「导入 tf」按钮 + localhost 通道。**只动前端**，把主流程做到一键。
3. **v1（可选）**：如果哪天真出了带 GUI 的桌面端，再顺手注册 `tf://`；单独为 CLI 做 scheme 不划算。
4. PKCE 那套授权服务器，留到「要让第三方工具也能接入」时再谈，那时它的价值不再是省事，而是开放能力。

这样 `docs/design/cli-auth-discussion.md` 里那一堆待后端拍板的问题，**大部分可以先不问**：只剩「Key 要不要加来源标记以便撤销」和「默认参数」两条，其余都变成前端 + CLI 内部的事。
