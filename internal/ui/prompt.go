package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ErrNotInteractive 表示当前环境无法征求用户意见。
//
// 调用方必须据此选择一个安全的默认行为 —— 对「要不要装东西」这类问题，
// 安全的默认永远是「不装」。
var ErrNotInteractive = Errf(CodeUsage, "not an interactive terminal")

// Interactive 报告是否可以向用户提问。
//
// --json 与 --yes 都视为非交互：前者的输出要能被机器解析，
// 后者是用户明确要求不要打断。
func (u *UI) Interactive(assumeYes bool) bool {
	return u.TTY && !u.JSON && !assumeYes && isTerminal(os.Stdin)
}

// Choose 展示编号选项并读取选择，返回选中项的下标。
//
// 刻意用编号而非方向键：不需要 raw mode，也就没有把用户终端
// 弄坏的风险；花哨的选择器留给 M4 的模型选择。
func (u *UI) Choose(title string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, Errf(CodeInternal, "no options to choose from")
	}

	fmt.Fprintf(u.Err, "%s\n\n", title)
	for i, o := range options {
		fmt.Fprintf(u.Err, "  %d) %s\n", i+1, o)
	}
	fmt.Fprintf(u.Err, "\n%s ", u.T(
		fmt.Sprintf("选择 [1-%d]：", len(options)),
		fmt.Sprintf("Choose [1-%d]:", len(options)),
	))

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return 0, ErrNotInteractive
	}

	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(options) {
		return 0, Errf(CodeUsage, u.T("无效的选择", "invalid choice"))
	}
	return n - 1, nil
}
