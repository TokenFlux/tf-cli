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
//
// 判据是「能不能拿到控制终端」而不是「stdin 是不是终端」：
// 选择器直接读写 /dev/tty，所以 `echo $KEY | tf login` 这种
// stdin 被管道占用的场景依然可以交互。
func (u *UI) Interactive(assumeYes bool) bool {
	return !u.JSON && !assumeYes && hasControllingTTY()
}

// Choose 展示编号选项并读取选择，返回选中项的下标。
//
// 刻意用编号而非方向键：不需要 raw mode，也就没有把用户终端
// 弄坏的风险。这是方向键选择器用不了时的兜底路径。
//
// 读 /dev/tty 而不是 stdin，理由与 ReadLine 一样：stdin 可能已经
// 被管道占住（echo $KEY | tf login），而用户仍然坐在终端前。
// 此前这里读 os.Stdin，于是兜底路径在最需要它的场合直接拿到 EOF。
func (u *UI) Choose(title string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, Errf(CodeInternal, "no options to choose from")
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return 0, ErrNotInteractive
	}
	defer tty.Close()

	fmt.Fprintf(u.Err, "%s\n\n", title)
	for i, o := range options {
		fmt.Fprintf(u.Err, "  %d) %s\n", i+1, o)
	}

	// 输错就再问一遍，别把人踢回命令行重来一次。
	// 给次数上限：管道喂进来的垃圾不该让 tf 无限循环。
	r := bufio.NewReader(tty)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprintf(u.Err, "\n%s ", u.T(
			fmt.Sprintf("选择 [1-%d]：", len(options)),
			fmt.Sprintf("Choose [1-%d]:", len(options)),
		))

		line, err := r.ReadString('\n')
		if err != nil {
			return 0, ErrNotInteractive
		}
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		u.Warnf(u.T("请输入 1 到 %d 之间的数字", "enter a number between 1 and %d"), len(options))
	}
	return 0, Errf(CodeUsage, u.T("无效的选择", "invalid choice"))
}

// ReadLine 在控制终端上提问并读一行（回显可见）。
//
// 直接读写 /dev/tty 而不是 stdin：stdin 可能已被管道占用
// （echo $KEY | tf login），但用户仍然坐在终端前。
func (u *UI) ReadLine(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", ErrNotInteractive
	}
	defer tty.Close()

	saved, err := sttyCapture(tty, "-g")
	if err != nil {
		return "", ErrNotInteractive
	}
	// 与 ReadSecret 同理：不能假设终端本来就是行模式。
	// 区别只是这里要回显，用户得看见自己输入了什么。
	if err := sttyRun(tty, "echo", "icanon", "icrnl", "isig"); err != nil {
		return "", ErrNotInteractive
	}
	defer func() { _ = sttyRun(tty, saved) }()

	fmt.Fprintf(tty, "%s ", prompt)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && line == "" {
		return "", ErrNotInteractive
	}
	return strings.TrimSpace(line), nil
}

// ReadSecret 在控制终端上提示并读一行，不回显。
//
// 关键是**自己把行规程设成需要的样子**，而不是假设终端本来就正常：
// 终端可能已经处于 raw 模式（上一个程序没收尾），那时回车送来的是 \r
// 而不是 \n，只关回显的做法会永远等不到行尾 —— 表现就是卡死。
//
// 直接读写 /dev/tty 而不是 stdin：stdin 可能已被管道占用，
// 但用户仍然坐在终端前。
func (u *UI) ReadSecret(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", ErrNotInteractive
	}
	defer tty.Close()

	saved, err := sttyCapture(tty, "-g")
	if err != nil {
		return "", ErrNotInteractive
	}
	// icanon 给行编辑与行尾，icrnl 把回车规整成 \n，isig 保住 Ctrl-C。
	if err := sttyRun(tty, "-echo", "icanon", "icrnl", "isig"); err != nil {
		return "", ErrNotInteractive
	}
	defer func() {
		_ = sttyRun(tty, saved)
		// 回显是关的，用户按的回车不会被终端回显，得由我们补一个换行。
		// 必须写 \r\n：此刻终端已恢复原状，而原状可能是 raw（ONLCR 关闭），
		// 只写 \n 会让光标停在提示语的末列，下一行就从那里开始。
		fmt.Fprint(tty, "\r\n")
	}()

	fmt.Fprintf(tty, "%s ", prompt)

	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && line == "" {
		return "", ErrNotInteractive
	}
	return strings.TrimRight(line, "\r\n"), nil
}
