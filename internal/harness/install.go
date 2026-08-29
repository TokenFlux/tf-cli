package harness

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Install 执行一条安装命令，输出直接串流给用户。
//
// 三条硬性约束（见 docs/design/open-decisions.md E 项）：
//   - 绝不 sudo。命令原样执行，不做任何提权包装。
//   - 绝不替用户挑包管理器，候选项由调用方展示、用户选定。
//   - 失败时原样返回底层错误，不包装 —— 用户要能直接拿去搜索。
func Install(opt InstallOption, stdout, stderr io.Writer) error {
	if strings.EqualFold(opt.Manager, "sudo") {
		return fmt.Errorf("refusing to run a privileged install command")
	}

	cmd := exec.Command(opt.Args[0], opt.Args[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	return cmd.Run()
}
