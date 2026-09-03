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

# 该下哪个文件 —— 发布页上摆着六个产物，不说明就得让人自己猜。
# 一行装的写法放最前面：多数人要的是这个，而不是手动挑架构。
#
# 不写 macOS 隔离属性那一段：实测打上 com.apple.quarantine 之后
# 从终端跑照样成功（Go 会给 macOS 二进制打临时签名，spctl 虽然判
# rejected，但那条路是 Finder 双击，命令行工具没人那样用）。
# 复现不出来的问题不写进说明，而且教人反射性敲 xattr -d 是坏习惯。
sed "s/<版本>/$VER/g" <<'ARTIFACTS'

## 下载

一行装（自动挑架构，装到 `~/.local/bin`，不需要 sudo）：

```
curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/install.sh | sh
```

已经装过的话直接 `tf update`。

手动下载：

| 系统 | 文件 |
| --- | --- |
| macOS（Apple 芯片） | `tf_<版本>_darwin_arm64.tar.gz` |
| macOS（Intel） | `tf_<版本>_darwin_amd64.tar.gz` |
| Linux x86_64 | `tf_<版本>_linux_amd64.tar.gz` |
| Linux ARM64 | `tf_<版本>_linux_arm64.tar.gz` |
| Windows | `tf_<版本>_windows_amd64.zip` |

解开就是单个可执行文件，没有依赖，放进 PATH 即可。

**Windows 目前只能非交互使用**：选择器与登录提问需要终端交互，
Windows 上尚未实现，请用 `--no-input` 配合 `-m`、`-k` 明确指定。

## 校验

```
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
