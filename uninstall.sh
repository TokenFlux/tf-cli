#!/bin/sh
# 卸载由 install.sh 安装的 tf 二进制。
#
#   curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/tokenflux/tf-cli/main/uninstall.sh | sh -s -- --purge
#
# 默认保留凭据和配置；--purge 才删除当前生效的配置与缓存目录。
set -eu

DIR="${TF_INSTALL_DIR:-$HOME/.local/bin}"
PURGE=0

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
usage() {
  say "用法 / usage: uninstall.sh [--purge]"
  say "  --purge  同时删除凭据、配置与缓存 / also remove credentials, config, and cache"
}

case "${1:-}" in
  "") ;;
  --purge) PURGE=1; shift ;;
  -h|--help) usage; exit 0 ;;
  *) usage >&2; die "未知参数 / unknown argument: $1" ;;
esac
[ "$#" -eq 0 ] || { usage >&2; die "参数过多 / too many arguments"; }

target="$DIR/tf"
if [ -d "$target" ] && [ ! -L "$target" ]; then
  die "$target 是目录，拒绝删除 / is a directory; refusing to remove it"
fi
if [ -e "$target" ] || [ -L "$target" ]; then
  rm -f -- "$target"
  say "已删除 / removed: $target"
else
  say "未找到二进制 / binary not found: $target"
fi

if [ -n "${XDG_CONFIG_HOME:-}" ]; then
  config_dir="$XDG_CONFIG_HOME/tf"
else
  config_dir="$HOME/.tf"
fi
if [ -n "${XDG_CACHE_HOME:-}" ]; then
  cache_dir="$XDG_CACHE_HOME/tf"
else
  cache_dir="$config_dir/cache"
fi

remove_tree() {
  path=$1
  case "$path" in
    ""|/|.) die "拒绝删除不安全路径 / refusing unsafe path: $path" ;;
  esac
  if [ -e "$path" ] || [ -L "$path" ]; then
    rm -rf -- "$path"
    say "已删除 / removed: $path"
  fi
}

if [ "$PURGE" -eq 1 ]; then
  case "$cache_dir" in
    "$config_dir"/*) ;;
    *) remove_tree "$cache_dir" ;;
  esac
  remove_tree "$config_dir"
else
  say "凭据与配置已保留 / credentials and config kept: $config_dir"
  if [ "$cache_dir" != "$config_dir/cache" ]; then
    say "缓存已保留 / cache kept: $cache_dir"
  fi
  say "如需一并删除，请使用 --purge / use --purge to remove them too"
fi
