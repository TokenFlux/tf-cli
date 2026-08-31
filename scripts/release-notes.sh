#!/usr/bin/env bash
# 从 CHANGELOG.md 取出某个版本那一节，拼成发布说明。
#
# 为什么不用 gh --generate-notes：它是按合并的 PR 生成的，而这个仓库
# 全是直推 main，生成出来只有一行「Full Changelog」。v0.1.0 到 v0.4.0
# 四个版本的发布说明都是那一行，等于没有。
#
# 变更记录是手写的，因为「这次改动对用户意味着什么」这句话，
# 从提交标题里自动提取不出来。
set -euo pipefail

TAG="${1:?用法: release-notes.sh <tag> [changelog] [repo]}"
FILE="${2:-CHANGELOG.md}"
REPO="${3:-TokenFlux/tkr}"
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

# 上一个 tag。注意 grep -A1 在列表末尾只会回显自身 —— 那说明这是
# 首个版本，范围要取整个历史而不是 TAG..TAG（空区间）。
PREV=$(git tag --sort=-v:refname | grep -A1 -x "$TAG" | tail -1 || true)
[ "$PREV" = "$TAG" ] && PREV=""

printf '%s\n' "$BODY"

# 该下哪个文件 —— 发布页上摆着六个产物，不说明就得让人自己猜。
# 一行装的写法放最前面：多数人要的是这个，而不是手动挑架构。
#
# 不写 macOS 隔离属性那一段：实测打上 com.apple.quarantine 之后
# 从终端跑照样成功（Go 会给 macOS 二进制打临时签名，spctl 虽然判
# rejected，但那条路是 Finder 双击，命令行工具没人那样用）。
# 复现不出来的问题不写进说明，而且教人反射性敲 xattr -d 是坏习惯。
cat <<'ARTIFACTS'

## 下载

一行装（自动挑架构，装到 `~/.local/bin`，不需要 sudo）：

```
curl -fsSL https://raw.githubusercontent.com/tokenflux/tkr/main/install.sh | sh
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

# 贡献者按提交作者列出。写清楚哪些提交出自 AI —— 仓库里绝大多数提交
# 由 AI 代理写成，把它们记在一个人名下会误导读者。
printf '\n## 贡献者\n\n'
if [ -n "$PREV" ] && git rev-parse -q --verify "$PREV" >/dev/null 2>&1; then
  RANGE="$PREV..$TAG"
else
  RANGE="$TAG" # 首个版本：整段历史
fi
git log --format='%an|%ae' "$RANGE" | sort -u | while IFS='|' read -r name email; do
  case "$email" in
    dev@tf|dev@tkr) printf -- '- %s（早期的合成提交身份，非真人）\n' "$name" ;;
    *) printf -- '- %s\n' "$name" ;;
  esac
done

if [ -n "$PREV" ]; then
  printf '\n**完整变更**: https://github.com/%s/compare/%s...%s\n' "$REPO" "$PREV" "$TAG"
else
  printf '\n**完整变更**: https://github.com/%s/commits/%s\n' "$REPO" "$TAG"
fi
