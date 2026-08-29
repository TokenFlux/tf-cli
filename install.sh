#!/bin/sh
# tkr 安装脚本。
#
#   curl -fsSL https://raw.githubusercontent.com/tokenflux/tkr/main/install.sh | sh
#
# 装到 ~/.local/bin（不需要 sudo）。用 TKR_INSTALL_DIR 可以改。
set -eu

REPO="tokenflux/tkr"
DIR="${TKR_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "需要 $1 / $1 is required"; }
need curl
need tar

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) die "暂不支持 $os / unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "暂不支持 $arch / unsupported architecture: $arch" ;;
esac

say "查询最新版本…"
tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
      sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
[ -n "$tag" ] || die "还没有任何发布 / no releases yet"
version="${tag#v}"

asset="tkr_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "下载 $asset"
curl -fsSL "$base/$asset" -o "$tmp/$asset" || die "下载失败 / download failed: $asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" || die "缺少 SHA256SUMS / missing SHA256SUMS"

# 校验和不是可选项：这一步会把可执行文件放进你的 PATH。
say "校验 SHA256"
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
  got=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
else
  die "需要 sha256sum 或 shasum / need sha256sum or shasum"
fi
want=$(grep " $asset\$" "$tmp/SHA256SUMS" | cut -d' ' -f1)
[ -n "$want" ] || die "SHA256SUMS 里没有 $asset"
[ "$got" = "$want" ] || die "校验和不符 / checksum mismatch"

tar xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$DIR"
mv "$tmp/tkr" "$DIR/tkr"
chmod +x "$DIR/tkr"

say ""
say "✓ tkr $version → $DIR/tkr"

case ":$PATH:" in
  *":$DIR:"*) say "运行 tkr login 开始。" ;;
  *)
    say ""
    say "$DIR 不在 PATH 里，把这行加进你的 shell 配置："
    say "  export PATH=\"$DIR:\$PATH\""
    ;;
esac
