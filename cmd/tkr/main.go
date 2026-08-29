// Command tkr 是 TokenFlux / TokenRouter 的 harness 启动器。
//
// 设计原则见 docs/PLAN.md。最重要的两条：
//   - tkr 不改用户的配置文件，只做进程内注入，退出不留痕。
//   - tkr 绝不代理流量、绝不覆盖 harness 的 User-Agent。
package main

import (
	"os"

	"github.com/tokenflux/tkr/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
