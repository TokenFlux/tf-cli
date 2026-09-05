#!/bin/sh
# Linux/macOS and Windows Git Bash installer. No administrator privileges needed.
# curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/install.sh | sh
set -eu

REPO="tokenflux/tf-cli"
DIR="${TF_INSTALL_DIR:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required"; }
need curl

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  mingw*|msys*|cygwin*) os=windows ;;
  *) die "unsupported OS: $os" ;;
esac
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

binary=tf
extension=tar.gz
if [ "$os" = windows ]; then
  [ "$arch" = amd64 ] || die "Windows releases currently support x64 only"
  need unzip
  need cygpath
  DIR=$(cygpath -u "$DIR")
  binary=tf.exe
  extension=zip
else
  need tar
fi

[ ! -d "$DIR/$binary" ] || die "$DIR/$binary is a directory; refusing to replace it"

say "查询最新版本 / Checking the latest release..."
# Follow GitHub's documented latest-release redirect instead of parsing JSON in sh.
release_url=$(curl -fsSL -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")
case "$release_url" in
  "https://github.com/$REPO/releases/tag/v"*) tag=${release_url##*/} ;;
  *) die "could not resolve the latest release" ;;
esac
version=${tag#v}
case "$version" in
  ""|*[!0-9A-Za-z.+-]*) die "invalid release version" ;;
esac
asset="tf_${version}_${os}_${arch}.$extension"
base="https://github.com/$REPO/releases/download/$tag"

tmp=$(mktemp -d)
staged=""
cleanup() {
  rm -rf -- "$tmp"
  [ -z "$staged" ] || rm -f -- "$staged"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

say "下载 / Downloading $asset"
curl -fsSL "$base/$asset" -o "$tmp/$asset" || die "download failed: $asset"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" || die "missing SHA256SUMS"

say "校验 / Verifying SHA256"
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/$asset" | cut -d' ' -f1)
elif command -v shasum >/dev/null 2>&1; then
  got=$(shasum -a 256 "$tmp/$asset" | cut -d' ' -f1)
else
  die "need sha256sum or shasum"
fi
want=$(awk -v asset="$asset" '$2 == asset || $2 == "*" asset { print $1 }' "$tmp/SHA256SUMS")
[ -n "$want" ] || die "SHA256SUMS contains no entry for $asset"
[ "$got" = "$want" ] || die "checksum mismatch"

# Extract only the expected binary to stdout, never archive-provided paths.
if [ "$os" = windows ]; then
  unzip -p "$tmp/$asset" "$binary" > "$tmp/$binary" || die "invalid Windows archive"
else
  tar -xzOf "$tmp/$asset" "$binary" > "$tmp/$binary" || die "invalid archive"
fi
[ -s "$tmp/$binary" ] || die "archive contains an empty binary"
mkdir -p -- "$DIR"
# Stage on the destination filesystem so installation never exposes a partial copy.
staged=$(mktemp "$DIR/.tf-install.XXXXXX")
cp "$tmp/$binary" "$staged"
chmod 755 "$staged"
mv -f -- "$staged" "$DIR/$binary" || die "cannot replace $binary; close running tf processes and retry"
staged=""

say ""
say "已安装 / Installed tf $version: $DIR/$binary"
case ":$PATH:" in
  *":$DIR:"*) say "运行 tf login 开始 / Run tf login to get started." ;;
  *)
    say ""
    say "将目录加入 shell 配置的 PATH / Add this directory to PATH in your shell configuration:"
    say "  export PATH=\"$DIR:\$PATH\""
    ;;
esac
