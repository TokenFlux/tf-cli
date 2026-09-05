//go:build !windows

package launch

import (
	"os"
	"os/exec"
	"strings"
)

// termState 是启动子进程之前的终端状态。
//
// 启动器必须自己保存它：子进程被 SIGKILL 或崩溃时来不及收尾，
// 终端会留在 raw 模式（换行不回车，此后所有输出呈阶梯状）、
// 备用屏、隐藏光标、括号粘贴或 kitty 键盘协议里。
//
// 这正是当初选 fork + wait 而不是 exec 的理由 —— 有人得负责收尾。
type termState struct {
	tty  *os.File
	stty string
}

// captureTerm 记下当前终端设置。不是终端时返回一个空壳。
func captureTerm() *termState {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return &termState{}
	}
	out, err := sttyCapture(f, "-g")
	if err != nil {
		f.Close()
		return &termState{}
	}
	return &termState{tty: f, stty: out}
}

// restore 把终端恢复到启动前。
//
// killed 表示子进程死于信号：那种情况下它没有机会自己收尾，
// 才需要额外发那串 ANSI 复位。正常退出时不发 —— 子进程已经清理过，
// 再发一遍是多余的噪音。
func (t *termState) restore(killed bool) {
	if t.tty == nil {
		return
	}
	defer func() {
		t.tty.Close()
		t.tty = nil
	}()

	_ = sttyRun(t.tty, t.stty)
	if killed {
		// 顺序要紧：先退出备用屏，再复位其余，否则复位会落在错误的屏上。
		_, _ = t.tty.WriteString(
			"\x1b[?1049l" + // 退出备用屏
				"\x1b[?25h" + // 显示光标
				"\x1b[r" + // 复位滚动区域
				"\x1b[?2004l" + // 关闭括号粘贴
				"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" + // 关闭鼠标上报
				"\x1b[<u" + // 弹出 kitty 键盘协议
				"\x1b[0m", // 复位颜色
		)
	}
}

// homeColumn 把光标拉回行首。
//
// 子进程会用绝对列定位画自己的界面（Claude Code 用 \x1b[12G 之类），
// 却假定起始列是 0。终端若停在别处，整块界面就歪一列或更多。
//
// 只发一个 \r，不清屏也不换行 —— 已经在行首时它什么都不做。
func (t *termState) homeColumn() {
	if t.tty == nil {
		return
	}
	_, _ = t.tty.WriteString("\r")
}

func sttyRun(f *os.File, args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = f
	return cmd.Run()
}

func sttyCapture(f *os.File, args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = f
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
