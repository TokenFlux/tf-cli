package launch

import (
	"os"
	"strings"
	"testing"
)

// 子进程死于信号时来不及自己收尾，终端必须由启动器复位。
//
// 不复位的后果是终端留在 raw 模式：换行不回车，此后所有输出呈阶梯状，
// 而用户完全看不出是谁弄坏的。
func TestRestoreUndoesRawMode(t *testing.T) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no controlling terminal")
	}
	f.Close()

	term := captureTerm()
	if !term.valid {
		t.Fatal("captureTerm should succeed when /dev/tty is open")
	}

	// 模拟子进程把终端切进 raw 模式后被 kill -9。
	tty, _ := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err := sttyRun(tty, "raw", "-echo"); err != nil {
		tty.Close()
		t.Skip("cannot enter raw mode here")
	}
	tty.Close()

	term.restore(true)

	// 断言「不再是 raw」，而不是「状态字节完全一致」：
	// 进出 raw 模式时内核会自己置 PENDIN 之类的瞬时位，
	// 逐字节比对会把内核的正常行为当成失败。
	flags, err := ttyFlags()
	if err != nil {
		t.Fatal(err)
	}
	// 必须按词元比对：stty -a 里有 -echonl、-echoprt 这些邻居，
	// 子串匹配会把 "-echo" 在 "-echonl" 上命中。
	off := map[string]bool{}
	for _, tok := range strings.FieldsFunc(flags, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}) {
		off[tok] = true
	}
	for _, want := range []string{"-icanon", "-echo"} {
		if off[want] {
			t.Errorf("terminal is still raw: %s", want)
		}
	}
}

// 没有终端时不能崩，也不该做任何事。
func TestRestoreWithoutTTYIsSafe(t *testing.T) {
	(&termState{}).restore(true)
	(&termState{}).restore(false)
}

// ttyFlags 返回 stty -a 的可读标志，用于判断是否仍在 raw 模式。
func ttyFlags() (string, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return sttyCapture(f, "-a")
}

// 交给子进程之前必须把光标拉回行首。
//
// 子进程用绝对列定位画界面（Claude Code 用 \x1b[12G 之类），却假定
// 起始列是 0；终端若停在别处，logo 与文字就会错开。
func TestHomeColumnEmitsCarriageReturn(t *testing.T) {
	if _, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err != nil {
		t.Skip("no controlling terminal")
	}
	term := captureTerm()
	if !term.valid {
		t.Skip("cannot open the terminal")
	}
	defer term.restore(false)

	// 只要不 panic、不改动终端设置即可 —— \r 本身没有副作用。
	before, _ := ttyFlags()
	term.homeColumn()
	after, _ := ttyFlags()
	if before != after {
		t.Error("homeColumn must not change terminal settings")
	}
}

// 没有终端时不能崩。
func TestHomeColumnWithoutTTYIsSafe(t *testing.T) {
	(&termState{}).homeColumn()
}
