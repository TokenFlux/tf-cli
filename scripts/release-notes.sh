#!/usr/bin/env bash
# 从 CHANGELOG.md 取出某个版本那一节，拼成发布说明。
#
# 工作流会在这段手写说明后追加 GitHub 自动生成的比较链接与 PR 列表。
# 手写部分负责回答「这次改动对用户意味着什么」—— 这句话无法从提交
# 标题里可靠提取。
set -euo pipefail

TAG="${1:?用法: release-notes.sh <tag> [changelog]}"
FILE="${2:-CHANGELOG.md}"
VER="${TAG#v}"

# 取 "## [x.y.z]" 到下一个 "## " 之间的内容。
BODY=$(awk -v v="$VER" '
  $0 ~ "^## \\[" v "\\]" { on=1; next }
  on && /^## / { exit }
  on { print }
' "$FILE" | sed -e '/^---$/d')

# 去掉首尾空行。不用 tac —— macOS 上没有它，而这个脚本要能在本地试跑。
BODY=$(printf '%s\n' "$BODY" | awk '
  { line[NR]=$0 }
  END {
    s=1; while (s<=NR && line[s]~/^[[:space:]]*$/) s++
    e=NR; while (e>=s && line[e]~/^[[:space:]]*$/) e--
    for (i=s;i<=e;i++) print line[i]
  }')

if [ -z "$BODY" ]; then
  echo "CHANGELOG.md 里没有 $VER 这一节" >&2
  exit 1
fi

# 标题头：发布页上方那行 v0.5.0 是 GitHub 画的，正文里再点一次名，
# 从别处引用这段内容时才知道说的是哪个版本。
printf '## tf v%s\n\n本版由 GitHub Actions 构建，产物与校验和见下。\n\n' "$VER"
printf '%s\n' "$BODY"

# 模板正文按安装方式（npm / 独立脚本）、手动资产和卸载组织。
#
# 不写 macOS 隔离属性那一段：实测打上 com.apple.quarantine 之后
# 从终端跑照样成功（Go 会给 macOS 二进制打临时签名，spctl 虽然判
# rejected，但那条路是 Finder 双击，命令行工具没人那样用）。
# 复现不出来的问题不写进说明，而且教人反射性敲 xattr -d 是坏习惯。
sed "s/<版本>/$VER/g" <<'ARTIFACTS'

## 安装

### npm（跨平台，需 Node.js 18+）

```sh
npm install -g @tokenflux/tf
```

升级请运行 `npm install -g @tokenflux/tf@latest`（`tf update` 不会直接修改 npm 托管的文件）。

### Windows PowerShell（独立脚本，无 Git Bash / Node 依赖）

```powershell
irm https://raw.githubusercontent.com/tokenflux/tf-cli/main/install.ps1 | iex
```

支持 Windows PowerShell 5.1 与 PowerShell 7，安装到 `%USERPROFILE%\.local\bin\tf.exe`，校验 SHA-256 后替换程序。无需管理员权限；按脚本提示配置 PATH。

### macOS / Linux / Windows Git Bash（独立脚本，无 Node 依赖）

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/install.sh | sh
```

默认安装到 `~/.local/bin/tf`，Windows x64 安装为 `tf.exe`（可通过 `TF_INSTALL_DIR` 自定义）。Windows 请在 Git Bash 中执行；脚本需要 curl、unzip 和 cygpath。升级直接运行 `tf update`。

## 手动下载

| 系统 | 文件 |
| --- | --- |
| macOS（Apple Silicon） | `tf_<版本>_darwin_arm64.tar.gz` |
| macOS（Intel） | `tf_<版本>_darwin_amd64.tar.gz` |
| Linux x86_64 | `tf_<版本>_linux_amd64.tar.gz` |
| Linux ARM64 | `tf_<版本>_linux_arm64.tar.gz` |
| Windows x64 | `tf_<版本>_windows_amd64.zip` |

macOS 与 Linux 产物解压后将 `tf` 放入 PATH 即可。

Windows 产物解压后将 `tf.exe` 放入 PATH。建议在 Windows Terminal 的 Git Bash 中运行；经典 mintty 使用 `winpty tf.exe`。通过 npm/pnpm 安装的客户端需要 Git Bash 在 PATH 中。

## 卸载

### Windows PowerShell

```powershell
irm https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.ps1 | iex
```

默认保留凭据；下载脚本后使用 `-Purge` 可同时清理配置与缓存，自定义安装目录使用 `-InstallDir` 或 `TF_INSTALL_DIR`。

### npm

```sh
npm uninstall -g @tokenflux/tf
```

### 独立脚本

仅删除 `tf` 二进制，保留配置、凭据与缓存：

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.sh | sh
```

同时清理配置、凭据与缓存目录：

```sh
curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.sh | sh -s -- --purge
```

若安装时指定了 `TF_INSTALL_DIR`，可在管道右侧传入对应环境变量：`curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.sh | TF_INSTALL_DIR=/原安装目录 sh`。

## 校验

```sh
sha256sum -c SHA256SUMS --ignore-missing
```
ARTIFACTS

# 引用版本：这个二进制是从哪个提交、用什么工具链构建的。
#
# 不写默认网关：官方产物永远指向同一个地址，写出来是恒定的噪音。
SHA=$(git rev-parse --short "$TAG^{commit}" 2>/dev/null || echo "?")
GOV=$(awk '/^go /{print $2}' go.mod 2>/dev/null || echo "?")
printf '\n引用版本：提交 `%s` / Go `%s`\n' "$SHA" "$GOV"

# 末尾留一个空行，避免 GitHub 自动生成的部分贴住上一行。
printf '\n'
