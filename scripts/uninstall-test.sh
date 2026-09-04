#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

install_dir="$TMP/bin"
config_home="$TMP/config"
cache_home="$TMP/cache"
config_dir="$config_home/tf"
cache_dir="$cache_home/tf"
mkdir -p "$install_dir" "$config_dir" "$cache_dir"
printf 'binary\n' > "$install_dir/tf"
printf 'credential\n' > "$config_dir/credentials.json"
printf 'cache\n' > "$cache_dir/models.json"

run_uninstall() {
  HOME="$TMP/home" \
  XDG_CONFIG_HOME="$config_home" \
  XDG_CACHE_HOME="$cache_home" \
  TF_INSTALL_DIR="$install_dir" \
    sh "$ROOT/uninstall.sh" "$@"
}

run_uninstall >/dev/null
[ ! -e "$install_dir/tf" ]
[ -f "$config_dir/credentials.json" ]
[ -f "$cache_dir/models.json" ]

# 未安装时重复执行仍应成功，也仍然保留用户数据。
run_uninstall >/dev/null
[ -f "$config_dir/credentials.json" ]
[ -f "$cache_dir/models.json" ]

printf 'binary\n' > "$install_dir/tf"
run_uninstall --purge >/dev/null
[ ! -e "$install_dir/tf" ]
[ ! -e "$config_dir" ]
[ ! -e "$cache_dir" ]

mkdir -p "$install_dir/tf"
if run_uninstall >/dev/null 2>&1; then
  echo "uninstall accepted a directory in place of the binary" >&2
  exit 1
fi
rm -rf "$install_dir/tf"

if run_uninstall --unknown >/dev/null 2>&1; then
  echo "uninstall accepted an unknown argument" >&2
  exit 1
fi

run_uninstall --help >/dev/null
printf 'uninstall tests passed\n'
