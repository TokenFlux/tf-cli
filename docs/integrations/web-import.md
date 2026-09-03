# TokenFlux Web 导入协议接入指南

本文档面向 TokenFlux Web 前端工程师，说明网页前端如何接入 `tf` CLI 提供的本地网页导入（Web Import）协议，将用户在网页创建或选择的 API Key 一键导入至本地 CLI。

---

## 一、协议与交互全景

网页导入基于**短生命周期的本地回环 HTTP 通道**与**终端交互确认**实现。整个交互流程如下：

```
[ 用户终端 ]                                   [ TokenFlux Web 网页前端 ]
     │                                                     │
1. 运行 `tf login [名字]` 并选择“从网页导入”，或显式使用      │
   `tf login [名字] --from-web [--host <网关>]`              │
   CLI 按顺序在 127.0.0.1:43110-43119 绑定首个可用端口        │
   等待 10 分钟；终端打印来源、网关、端口等信息                      │
     │                                                     │
     │ 2. 用户点击“导入到 tf”后，页面并发或顺序扫描 43110..43119 │
     │ ─── GET http://127.0.0.1:4311x/ping ──────────────> │
     │ <── 200 OK {"ok":true,"service":"tf",...} ───────── │
     │    （若匹配到有效端口，点亮网页端「导入到 tf」按钮）     │
     │                                                     │
     │ 3. 用户在页面点击「导入」                               │
     │ ─── OPTIONS http://127.0.0.1:4311x/import ────────> │ (Chrome PNA 预检)
     │ <── 204 No Content (允许私网访问与当前 Origin) ───── │
     │                                                     │
     │ ─── POST http://127.0.0.1:4311x/import ───────────> │ (发送 Key 及元数据)
     │                                                     │
4. CLI 收到请求，在终端弹出确认框                              │
   展示：来源 Origin、网关 Host、分组、Key 掩码、目标名称        │
   用户选择 [写入 / 拒绝]                                    │
     │                                                     │
     │ <── 202 Accepted {"ok":true,"status":"accepted"} ── │ (用户确认写入)
     │  或 409 Conflict {"ok":false,"error":"rejected"} ── │ (用户拒绝)
     │                                                     │
5. CLI 在 HTTP 响应后连网关 /v1/models 校验 Key 并存盘                  │
   校验或写盘失败仍会在终端报告；关闭 HTTP 监听，结束流程                                  │
```

---

## 二、CLI 侧启动命令与监听规则

### 1. 用户启动命令

普通用户直接运行：

```bash
tf login [名字] [--host <网关地址>]
```

若 stdin 是终端，且没有指定 `--from-web` 或 `--with-key`，CLI 会显示“粘贴 API Key / 从网页导入”选择器。选择“从网页导入”后进入等待状态。管道输入本身视为已选择粘贴方式，不会弹选择器。

需要直接进入网页导入时使用：

```bash
tf login [名字] --from-web [--host <网关地址>]
```

- `[名字]`（可选）：指定保存的 Profile 凭据名称。如不传，默认目标为 `default`。
- `--from-web`（可选直达）：跳过方式选择器，直接开启本地回环 HTTP 导入监听。不能与 `--with-key` 同时使用。
- `--with-key`（可选直达）：跳过方式选择器，直接从管道或终端隐藏输入读取 Key。
- `--host <网关地址>`（可选）：指定网关 Base URL（默认使用 `https://tokenflux.dev` 或配置中已有的 host）。CLI 会将其归一化并提取出允许的 `Origin`。

### 2. 监听地址与端口范围
- **地址**：严格绑定在 IPv4 回环地址 `127.0.0.1`（不监听 `0.0.0.0`，也不监听公网 IP）。
- **端口范围**：固定为 **`43110` 至 `43119`**（共 10 个端口，常量 `43110` ~ `43119`）。CLI 启动时按顺序探测并绑定第一个可用的端口。
- **超时退出**：监听建立后若 **10 分钟** 内没有收到可确认的请求，CLI 会自动超时退出。
- **生命周期**：仅在用户选择网页导入或显式传入 `--from-web` 后短暂开启。用户拒绝请求后继续监听；用户确认一个请求后关闭 HTTP 服务并继续网关校验和写盘。服务**不常驻后台**。

---

## 三、网络安全、CORS 与浏览器本地网络权限

公网 HTTPS 页面访问 `http://127.0.0.1` 会受到浏览器本地网络策略约束。Chrome 142+ 使用 Local Network Access（LNA）权限提示；较早的 Chromium 实现使用 Private Network Access（PNA）预检。页面和 CLI 需要同时兼容这两代行为。

### 1. 页面发起请求的前置条件

- 只从 HTTPS secure context 接入。用户点击“导入到 tf”后再探测端口，不要在页面加载时自动扫描并触发本地网络权限提示。
- 页面 CSP 的 `connect-src` 必须允许 `http://127.0.0.1:43110` 至 `http://127.0.0.1:43119`。可以逐项列出，也可以在安全策略允许时使用 `http://127.0.0.1:*`。
- 对支持 LNA 的浏览器，可在 `fetch` / `Request` 参数中声明 `targetAddressSpace: "loopback"`。浏览器仍会要求用户授予 loopback 权限；该参数不绕过权限。
- 顶层同源页面通常使用默认 Permissions Policy 即可。若 Keys 页面运行在跨源 iframe 中，嵌入方还需委托 `loopback-network`；兼容旧 Chromium 时一并委托 `local-network-access`。

示例响应策略：

```http
Content-Security-Policy: connect-src 'self' https://tokenflux.dev http://127.0.0.1:43110 http://127.0.0.1:43111 http://127.0.0.1:43112 http://127.0.0.1:43113 http://127.0.0.1:43114 http://127.0.0.1:43115 http://127.0.0.1:43116 http://127.0.0.1:43117 http://127.0.0.1:43118 http://127.0.0.1:43119
```

iframe 场景：

```html
<iframe src="https://tokenflux.dev/keys" allow="loopback-network; local-network-access"></iframe>
```

### 2. 严格 Origin 校验
CLI 在启动时由 `--host`（如 `https://tokenflux.dev`）严格计算出期望的 `Origin`。
- 每个进来的请求，CLI 都会对比 HTTP 请求头中的 `Origin` 字段。
- 若 `Origin` 不匹配，CLI 直接返回 `403 Forbidden`（响应体 `{"ok": false, "error": "origin_not_allowed"}`），且**不附带**任何 CORS 允许头。

### 3. CORS 响应头与旧版 Chrome PNA 兼容
当 `Origin` 校验通过时，CLI 会在所有有效响应及 `OPTIONS` 预检中设置以下 Header：
```http
Access-Control-Allow-Origin: <当前页面Origin>
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type
Access-Control-Allow-Private-Network: true
Access-Control-Max-Age: 600
Cache-Control: no-store
Vary: Origin
```

使用旧 PNA 流程的浏览器可能对 `OPTIONS /ping` 或 `OPTIONS /import` 发送：
```http
OPTIONS /import HTTP/1.1
Origin: https://tokenflux.dev
Access-Control-Request-Method: POST
Access-Control-Request-Headers: content-type
Access-Control-Request-Private-Network: true
```
CLI 将响应 `204 No Content`，并带上上述 CORS 与 `Access-Control-Allow-Private-Network: true`。Chrome 142+ 的 LNA 权限提示由浏览器负责，不能用这个响应头绕过。

---

## 四、HTTP 接口定义

CLI 仅提供两个 endpoint：`GET /ping` 和 `POST /import`。不支持任何未声明的路径。

### 1. 服务发现：`GET /ping`

用于网页前端扫描本地是否有正在等待导入的 `tf` 实例。

- **Method**: `GET`
- **Path**: `/ping`
- **Request Headers**:
  - `Origin: <当前网页Origin>`
- **成功响应** (`200 OK`):
  ```json
  {
    "ok": true,
    "service": "tf",
    "protocol": 1,
    "version": "<tf-version>"
  }
  ```
  - `protocol`: 协议版本号（当前恒为整型 `1`）。
  - `version`: 当前 CLI 的版本号。

---

### 2. 凭据导入：`POST /import`

用于将 API Key 及关联元数据推送到 CLI。

- **Method**: `POST`
- **Path**: `/import`
- **Request Headers**:
  - `Content-Type: application/json`（必需，且不能带其它无关媒体类型）
  - `Origin: <当前网页Origin>`
- **Request Body 限制**: JSON 请求体最大不超过 **32 KB**，且**禁止包含任何未定义字段**（CLI 端使用 `DisallowUnknownFields` 解析）。

#### 请求字段说明（Protocol Version 1）

| 字段名 | 类型 | 必填 | 说明与约束 |
|---|---|---|---|
| `version` | `number` (int) | **是** | 必须为 `1`。非 `1` 会返回 `unsupported_protocol`。 |
| `key` | `string` | **是** | API Key 字符串。长度 ≤ 16KB，两端自动 Trim，内容必须全部是可打印 ASCII（`0x21`–`0x7e`），不能包含空白、控制字符或 Unicode。 |
| `host` | `string` | **是** | 目标网关 Base URL（例如 `https://tokenflux.dev` 或 `https://router.example.com`）。CLI 归一化后的 Origin 必须与 CLI 启动时的 host Origin 完全一致，否则返回 `host_mismatch`。 |
| `key_name` | `string` | 否 | Key 的显示名称（如 `"macbook-key"`）。UTF-8 编码长度 ≤ 256 字节，不得包含 Unicode 控制字符、双向文本覆盖或零宽格式字符。 |
| `group_id` | `number` (int64) | 否 | 分组 ID。必须 `≥ 0`。 |
| `group_name` | `string` | 否 | 分组名称（如 `"Claude"` 或 `"GPT"`）。UTF-8 编码长度 ≤ 256 字节，不得包含 Unicode 控制字符、双向文本覆盖或零宽格式字符。 |

#### 请求体示例
```json
{
  "version": 1,
  "key": "sk-example-token-flux-key-123456",
  "host": "https://tokenflux.dev",
  "key_name": "我的工作笔记本",
  "group_id": 7,
  "group_name": "Claude"
}
```

---

## 五、HTTP 响应状态码与错误类型矩阵

所有 JSON 错误响应均遵循 `{"ok": false, "error": "<error_code>"}` 格式。

| HTTP 状态码 | 响应内容 / error 代码 | 触发原因 / 场景 | 前端处理建议 |
|---|---|---|---|
| **`200 OK`** | `{"ok":true,"service":"tf",...}` | `GET /ping` 成功探测到 CLI。 | 标记该端口可用，点亮导入按钮。 |
| **`202 Accepted`** | `{"ok":true,"status":"accepted"}` | `POST /import` 已经通过终端人工确认「写入」；CLI 随后还会进行 `/v1/models` 校验和本地写盘。 | 页面提示“终端已确认，CLI 正在完成校验”；最终错误只会显示在终端，不能把 `202` 当作网关校验成功回执。 |
| **`204 No Content`** | *(无 Body)* | `OPTIONS` 预检请求通过。 | 浏览器内部流程。 |
| **`400 Bad Request`** | `{"ok":false,"error":"unsupported_protocol"}` | `version` 不为 1。 | 提示用户升级 CLI 或检查前端调用版本。 |
| | `{"ok":false,"error":"invalid_key"}` | `key` 为空、超长(>16KB)或含有空格/换行等空白字符。 | 检查前端 Key 格式。 |
| | `{"ok":false,"error":"host_mismatch"}` | 请求体中的 `host` 计算出的 Origin 与 CLI 期望的 Origin 不匹配。 | 确认请求 host 是否与当前站点一致。 |
| | `{"ok":false,"error":"invalid_metadata"}` | `group_id < 0` 或 `key_name`/`group_name` 超长(>256B)或含控制字符。 | 检查填入的元数据。 |
| | `{"ok":false,"error":"invalid_json"}` | JSON 格式错误，或含有未定义字段。 | 确保 JSON 纯净且字段无拼写错误。 |
| **`403 Forbidden`** | `{"ok":false,"error":"origin_not_allowed"}` | 请求 Header 的 `Origin` 与 CLI 允许的 Origin 不一致。 | 跨站请求被拦截。 |
| **`404 Not Found`** | `{"ok":false,"error":"not_found"}` | 访问了除 `/ping`、`/import` 外的其它路径。 | 检查请求路径。 |
| **`405 Method Not Allowed`** | `{"ok":false,"error":"method_not_allowed"}` | 在 `/ping` 上使用非 GET/OPTIONS，或在 `/import` 上使用非 POST/OPTIONS。 | 检查 HTTP Method。 |
| **`409 Conflict`** | `{"ok":false,"error":"rejected"}` | 用户在终端弹出的确认提示中主动选择了「拒绝」（Reject）。 | 页面提示“用户已在终端拒绝导入”。 |
| | `{"ok":false,"error":"cancelled"}` | 用户在终端用 ESC 取消确认，或确认输入无法继续。Ctrl+C 会直接终止 CLI，浏览器通常得到网络错误。 | 页面提示“终端已取消操作”或要求重新启动监听。 |
| | `{"ok":false,"error":"busy"}` | 已经有一个导入请求正在等待终端确认，同一时间收到并发请求。 | 提示“CLI 正在处理其他确认，请稍后重试”。 |
| **`415 Unsupported Media Type`** | `{"ok":false,"error":"content_type"}` | `Content-Type` 不是 `application/json`。 | 请求 Header 显式加上 `Content-Type: application/json`。 |

---

## 六、前端 TypeScript 接入实现示例

```typescript
export interface PingResponse {
  ok: boolean;
  service: string;
  protocol: number;
  version: string;
}

export interface ImportPayload {
  version: 1;
  key: string;
  host: string;
  key_name?: string;
  group_id?: number;
  group_name?: string;
}

export interface ImportResult {
  // accepted 只表示终端已确认，不表示网关校验和本地写盘已完成。
  accepted: boolean;
  port: number;
  error?: string;
}

const PORT_START = 43110;
const PORT_END = 43119;

type LoopbackRequestInit = RequestInit & {
  targetAddressSpace?: 'loopback';
};

function fetchLoopback(url: string, init: RequestInit = {}): Promise<Response> {
  return fetch(url, {
    ...init,
    // 新版浏览器会据此应用 loopback 权限；旧版会忽略未知字典字段。
    targetAddressSpace: 'loopback',
  } as LoopbackRequestInit);
}

/**
 * 用户点击“导入到 tf”后调用；不要在页面加载时自动扫描。
 */
export async function detectTfCli(timeoutMs = 500): Promise<number | null> {
  const ports = Array.from({ length: PORT_END - PORT_START + 1 }, (_, i) => PORT_START + i);

  // 并发探测所有端口
  const probePromises = ports.map(async (port) => {
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeoutMs);

      const resp = await fetchLoopback(`http://127.0.0.1:${port}/ping`, {
        method: 'GET',
        headers: {
          // 浏览器会自动附加 Origin: window.location.origin
        },
        signal: controller.signal,
      });
      clearTimeout(timer);

      if (resp.ok) {
        const data: PingResponse = await resp.json();
        if (data.service === 'tf' && data.protocol === 1) {
          return port;
        }
      }
    } catch {
      // 端口未开放、连接拒绝或超时，忽略。不要记录请求对象或 Key。
    }
    return null;
  });

  const results = await Promise.all(probePromises);
  return results.find((port): port is number => port !== null) ?? null;
}

/**
 * 推送 API Key 到已发现的 tf CLI 端口
 */
export async function importKeyToTf(
  port: number,
  payload: Omit<ImportPayload, 'version'>
): Promise<ImportResult> {
  const body: ImportPayload = {
    version: 1,
    ...payload,
  };

  try {
    const resp = await fetchLoopback(`http://127.0.0.1:${port}/import`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(body),
    });

    const resData = await resp.json();

    if (resp.status === 202 && resData.ok) {
      return { accepted: true, port };
    }

    return {
      accepted: false,
      port,
      error: resData.error || `HTTP_${resp.status}`,
    };
  } catch {
    // 不回传或记录原始异常对象，避免浏览器/监控 SDK 附带请求上下文。
    return {
      accepted: false,
      port,
      error: 'network_error',
    };
  }
}
```

---

## 七、安全规范与边界（前端开发必读）

1. **禁止在前端日志或 URL 中暴露 API Key**
   - 网页端不得将 API Key 作为 URL Query 参数传递。
   - 捕获异常或输出 `console.log` / `console.error` 调试信息时，**严禁打印包含 `key` 字段的完整对象**；如果需要打日志，必须对 Key 进行掩码处理（如只保留首尾）。
2. **终端交互确认绝不可绕过**
   - CLI 在将任何凭据写入 `credentials.json`（权限 `0600`）之前，**必须且必定**在本地交互终端上向用户展示请求来源 `Origin`、`Host`、`Group` 及脱敏后的 Key，等待用户手动确认（选择 `写入` 或 `拒绝`）。HTTP `202` 表示这一步已确认，不等于网关校验与写盘已经成功。
   - CLI 明确设计：`--json` 或 `--no-input` 标志**不会且不能**绕过该终端确认。在非交互终端下不会显示方式选择器；显式使用 `--from-web` 会直接报错退出。
   - `Origin` 与 CORS 只能限制普通浏览器页面，不能认证本机进程；任何本机进程都能伪造 HTTP `Origin`。最终授权边界是终端中展示的来源、网关和脱敏 Key，以及用户的手动确认。
3. **元数据落地（`SourceImport`）**
   - 网关校验和本地保存成功后，CLI 会在凭据库中写入 `source: "import"`（区别于手工粘贴的 `paste`），同时记录 `origin`、`key_name`、`group_id` 和 `group_name`。`tf keys` 的文本与 JSON 输出会带出这些来源信息。这些字段来自网页请求，供本地展示和追溯，不是网关签发的可信声明。
4. **协议兼容策略**
   - 当前协议主版本为 `version: 1`。CLI 收到非 1 版本的请求会直接拒绝并返回 `unsupported_protocol`。
   - CLI 开启了严格的未知字段拦截（`DisallowUnknownFields`），前端**不得**在 JSON 中传递未在协议中约定的额外扩展字段，否则会导致 `invalid_json`。

---

## 八、当前实现的已知限制

1. **不支持远程/SSH 环境的网页直连**
   - 协议基于浏览器到本机回环地址（`127.0.0.1`）的通信。若用户通过 SSH 在远程服务器选择网页导入或运行 `tf login --from-web`，本地浏览器访问本地 `127.0.0.1` 将无法触达远端 CLI（除非用户自行配置了 SSH 端口转发）。此类场景请引导用户选择“粘贴 API Key”，或使用 `tf login --with-key`。
2. **端口段固定为 43110-43119**
   - 若本机同时运行超过 10 个 `tf login --from-web` 实例，或者该范围内的 10 个端口全部被其他本地应用占用，CLI 会直接报错退出，前端也将无法探测到服务。
3. **单并发排队限制**
   - CLI 单个实例在同一时刻只能处理一个导入事务（进入确认期后原子标记 `busy`）。若并发发起多个 `/import` 请求，后续请求将立即收到 `409 Conflict (busy)`。
4. **网页与网关目前必须同源**
   - CLI 从 `--host` 同时推导网关地址和允许的网页 Origin。若自托管部署把 Keys 页面与 API 网关放在不同 Origin，目前需要调整为同源入口，不能在请求体中另行指定一个前端 Origin。
5. **Windows 交互环境限制**
   - 当前 CLI 终端交互界面依赖 Unix TTY 特性。在未支持完整终端交互的 Windows 环境下，`--from-web` 会因为无法进行交互确认而提示不受支持。
