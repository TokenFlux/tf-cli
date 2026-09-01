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

# 引用版本：这个二进制是从哪个提交、用什么工具链构建的。
#
# 不写默认网关：官方产物永远指向同一个地址，写出来是恒定的噪音。
SHA=$(git rev-parse --short "$TAG^{commit}" 2>/dev/null || echo "?")
GOV=$(awk '/^go /{print $2}' go.mod 2>/dev/null || echo "?")
printf '\n引用版本：提交 `%s` / Go `%s`\n' "$SHA" "$GOV"

# 贡献者不在这里写。
#
# GitHub 会在正文下面渲染一块带头像的 Contributors，但它的数据源是
# mentions_count，而那个数只从「What's Changed」里的 PR 作者算出来。
# 这个仓库全是直推 main，PR 数为 0 —— 所以那块本来就不会出现，
# 调格式补不出来，只能靠改协作方式（走 PR 合入）。
#
# 手写一份也不行：git 作者名不是 GitHub 账号，给不出头像和链接，
# 还会把 dev@tkr 这种早期的合成提交身份列成一个不存在的用户。
#
# 末尾留一个空行：自动生成的那段直接接在后面。
printf '\n'

# 完整变更那行不自己打：发布时会同时传 --generate-notes，
# GitHub 把「What'"'"'s Changed」和完整变更链接追加在这段后面。
# 两边都打就会出现两行一模一样的链接。
