#!/usr/bin/env bash
# 在一台 Linux 机器上验证终端相关的行为。
#
# 为什么需要它：终端那几条防线（CRLF、光标归零、信号后复位、读输入时
# 自设行规程）全部建在 /dev/tty 与 stty 上，而普通 CI 没有 tty。这个脚本
# 保留真实 Linux PTY 的可重复验证；stty 与 script 的行为在两个平台并不一样。
#
# 已经因此抓到过一个真问题：util-linux 的 script 不加 -e 恒返回 0，
# 于是所有退出码断言在 Linux 上悄悄失效。
#
# 用法：scripts/linux-check.sh user@host
#
# 交叉编译测试二进制送过去跑，对面不需要 Go 工具链。
set -euo pipefail

HOST="${1:?用法: scripts/linux-check.sh user@host}"
DIR="${2:-tf-check}"  # 相对 $HOME，ssh 会自己展开
PKGS="ui launch config harness model gateway"

say() { printf '\n\033[1m%s\033[0m\n' "$*"; }

say "编译 linux/amd64"
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
export GOOS=linux GOARCH=amd64 CGO_ENABLED=0
go build -o "$out/tf" ./cmd/tf
for p in $PKGS; do go test -c -o "$out/$p.test" "./internal/$p/"; done
go test -c -tags pty -o "$out/e2e.test" ./e2e/

say "送到 $HOST"
ssh "$HOST" "rm -rf \$HOME/$DIR && mkdir -p \$HOME/$DIR"
# 只送测试真正要读的东西：有测试会读自己的源文件，testdata 要在原位。
# 不用 tar 整个目录 —— .opencode/ 之类的会话状态会混进去。
tar czf - cmd internal e2e go.mod Makefile | ssh "$HOST" "tar xzf - -C \$HOME/$DIR"
scp -q "$out"/* "$HOST:$DIR/"  # scp 的路径相对 $HOME
ssh "$HOST" "cd \$HOME/$DIR && mkdir -p bin && cp tf bin/tf && chmod +x *.test bin/tf"

# 每个测试都在自己的包目录下跑，且套一层 script 造真 tty ——
# 不这样的话要 tty 的测试会全部 skip，看起来像通过了。
run() {
  local dir="$1" bin="$2"
  ssh "$HOST" "cd \$HOME/$DIR/$dir && script -qec '\$HOME/$DIR/$bin -test.v' /dev/null" 2>&1 \
    | tr -d '\r' | grep -aE '^(--- (PASS|FAIL|SKIP)|(ok|FAIL|PASS)$)' || true
}

fail=0
for p in $PKGS; do
  say "internal/$p"
  o=$(run "internal/$p" "$p.test")
  echo "$o"
  grep -q FAIL <<<"$o" && fail=1
done

say "e2e（真 pty 驱动真二进制）"
o=$(run e2e e2e.test)
echo "$o"
grep -q FAIL <<<"$o" && fail=1

say "结果"
if [ "$fail" = 0 ]; then echo "linux ok"; else echo "有失败项"; exit 1; fi
