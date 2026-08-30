# tf 认证与 Key 发放：与后端讨论用的事实与议题

本文只做两件事：把 TokenRouter 现状**查清楚写下来**，再列出需要后端拍板的问题。不预设实现。

代码依据：`backend/internal/server/routes/{auth,user}.go`、`backend/internal/handler/api_key_handler.go`、`backend/internal/handler/dto/mappers.go`、`docs/interfaces/http_api.md`。

---

## 一、现状事实：登录是"自订协议"，不是 OAuth

**TokenRouter 是 OAuth 的消费方（client），不是提供方（authorization server）。**

- `/api/v1/auth/oauth/{github,google,linuxdo,wechat,dingtalk,oidc}/{start,callback}` 全部是**第三方登录接入**，外加 Google One Tap（`/oauth/google/one-tap`）与 Passkey（`/passkey/login/{begin,finish}`）。
- 自身会话协议是自订的：
  - `POST /api/v1/auth/login` → 统一 envelope 返回 `access_token`/`refresh_token`/`expires_in`/`token_type`
  - `POST /api/v1/auth/login/2fa`、`/auth/refresh`（轮换）、`/auth/logout`（撤销，公开）
  - 第三方登录新用户走 `oauth/pending/*` 的补全状态机 + HttpOnly pending cookie
- 全部认证入口挂 `BackendModeAuthGuard` + 审计中间件 + Redis fail-close 限流（login 20/min，register 5/min…）。

**结论**：没有现成的 `authorization_code` / `device_code` 端点可以给 CLI 复用；也不能指望第三方 IdP 直接给 CLI 发 token——第三方登录的最终产物仍是这套自订 JWT。**CLI 授权是新增能力，不是"接一下 OAuth"。**

同时它解释了为什么 CLI 不能直接调 `/auth/login`：这条路径前面挂着 Cloudflare 质询、可能的 Turnstile/天御动作验证码、2FA、passkey，全都是**为浏览器设计的**。

### 需要后端拍板（登录侧）

1. **走标准还是走自订？**
   - A. 把 TokenRouter 做成最小 OAuth 2.0 authorization server：只服务 first-party client `tf`，只支持 `authorization_code + PKCE`（public client、无 client_secret）。收益是以后 CC-Switch、编辑器插件、第三方工具能统一接，`device_code` 顺带解决无浏览器场景；成本是要引入 client 注册 / scope / consent 三个新概念。
   - B. 非标准的一次性 code 交换（`grant` + `exchange` 两个接口）。实现最小，语义清楚，但只能自己用。
   - 我的倾向：**先 B 后 A**，B 的接口形状按 OAuth 命名（`code_challenge`/`code_verifier`/`S256`），将来升级 A 不破坏 CLI。
2. **授权码存哪**：Redis（项目已重度依赖，且有 fail-close 惯例）还是 DB？建议 Redis、TTL 60s、一次性消费、`S256(verifier)==challenge` 校验。
3. **授权页放哪**：新增前端路由 `/cli/auth`，还是在 `/keys` 页加一个授权模式？（现有 router 已有一堆 `/auth/*` callback 页，新增路由风格一致）
4. **backend mode 下**（`BackendModeAuthGuard`）CLI 授权是禁用还是放行？
5. **审计与限流**：这是一次凭据发放，是否进 `auditLog`？限流阈值取多少？
6. **Cloudflare（运维侧，必须同步确认）**：CLI 调用的 `exchange` 端点要和网关 `/v1/*` 一样豁免 bot fight / managed challenge，或者放独立子域（如 `cli.tokenflux.dev`）。否则换个路径继续被拦。

---

## 二、现状事实：Key 是怎么建出来的

`POST /api/v1/keys`（用户 JWT，`internal/handler/api_key_handler.go`）：

| 字段 | 说明 |
|---|---|
| `name` | **必填** |
| `scope` | `personal` / `team`（团队 Key 的付款主体是 Team Owner）|
| `group_id` / `is_composite` + `composite_groups` | 分组，决定协议格式与可用模型 |
| `custom_key` | 允许自定义 key 值 |
| `billing_mode` / `preferred_subscription_id` | `auto`/`subscription`/`balance` |
| `model_mapping` / `quota` / `expires_in_days` | 配额与重定向 |
| `rate_limit_{5h,1d,7d}` / `ip_whitelist` / `ip_blacklist` / `fast_mode_policy` | 限制 |
| **`data_sharing_confirmed` + `data_sharing_notice_version`** | **数据共享同意** |

三个对设计影响最大的事实：

1. **有数据共享同意门槛。** 这是法律/合规确认，**CLI 不应该代替用户勾选**。这条本身就足以决定：Key 应该在浏览器页面里创建，CLI 只接收结果。
2. **写操作已有幂等**：`executeUserIdempotentJSON("user.api_keys.create", ...)` 配合 `Idempotency-Key`，重试安全，不会重复建 key。
3. **Key 明文存储、可再次读取**（`dto.APIKeyFromService` 直出 `Key` 字段，无掩码），所以"复用已有 Key"是可行的，不必每次新建。

配套只读接口（CLI 或授权页可用）：
- `GET /api/v1/groups/available?scope=personal|team&subscription_id=`
- `GET /api/v1/keys/billing-options?scope=`
- `GET /api/v1/groups/rates`、`GET /api/v1/keys`

### 「优雅地新建 Key」的三个候选

**A. 浏览器建、CLI 取（倾向方案）**
授权页展示分组下拉 + 数据共享勾选 + Key 名称，用户确认后页面用**现有 JWT 调现有 `POST /api/v1/keys`**，再把 `key_id` 绑到一次性 code；CLI 只做 exchange。
- 后端新增面最小：两个接口，`POST /keys` 一行不改。
- 合规确认、2FA、passkey、分组选择全在浏览器里天然解决。
- 缺点：需要前端页面。

**B. CLI 传参、服务端代建**
exchange 时服务端顺手建 key。
- 缺点：分组选择、命名、数据共享同意都要在终端里表达，等于把一套 UI 搬进 CLI；合规确认由 CLI 代劳，风险不对。

**C. 不新建，只授权导出已有 Key**
授权页列出用户已有 Key，选一个授权给 CLI（因为 key 明文可读，实现最省）。
- 缺点：CLI 与桌面客户端共用同一把 key，用户想撤销 CLI 时会误伤别处。

**建议：A 为主，授权页上顺带提供 C 作为「使用已有 Key」选项。**

### 需要后端拍板（Key 侧）

1. **可识别与可撤销**：CLI 发的 Key 是否需要标记来源？
   - 轻方案：命名约定 `tf@<hostname>`，零后端改动。
   - 重方案：给 api_key 加 `source`/`origin` 字段，`/keys` 页能筛选出「CLI 授权的 Key」并一键撤销。
   - 这是需要确认的取舍点。
2. **默认参数**：CLI Key 默认 `scope=personal`？默认给 `expires_in_days` 吗？默认 quota / rate limit 给不给？
3. **重复授权**：同一台机器第二次 `tf login`，是复用同名 Key 还是每次建新的？（建议复用，靠 `Idempotency-Key` + 命名判重）
4. **团队场景**：团队成员用 CLI 时默认建 personal Key，还是允许选 team scope（付款走 Owner）？
5. **撤销回路**：用户在网页删掉这把 Key 后，CLI 侧应该收到什么错误码，才能提示「请重新 `tf login`」而不是笼统 401？

---

## 三、无浏览器场景（SSH / 容器 / 无 GUI）

浏览器流解决不了纯终端环境，需要一条兜底路径。三选一，也需要讨论：

1. `tf login --with-key`：用户从 `/keys` 页复制粘贴。**零后端改动，v0 就该先做这个。**
2. `--no-browser`：打印 URL，用户在别的设备打开，页面显示一段一次性 code 粘回终端（device-code 变体，需要后端支持轮询或粘贴码）。
3. 标准 `device_code` 流（若走上面的方案 A）。

---

## 四、给这次沟通的最小结论

- 登录不是 OAuth，是自订 JWT + 一堆浏览器专属防护 → **CLI 不碰登录，授权挪进浏览器**，这点基本没有别的选择。
- Key 创建有数据共享同意门槛 → **Key 在页面里建，CLI 只接结果**。
- 后端新增面可以压到两个接口（grant + exchange）+ 一个前端路由；`POST /keys` 完全不动。
- **v0 先发 `--with-key`**，零后端改动即可上线，浏览器流作为 v0.2，给后端留出讨论与排期空间。
- 另需运维确认 Cloudflare 对 exchange 端点的豁免规则。
