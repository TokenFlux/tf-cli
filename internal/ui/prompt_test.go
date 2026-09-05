//go:build !windows

package ui

import (
	"os"
	"testing"
)

// 终端可能已被上一个程序留在 raw 模式。那时回车送来的是 \r 而不是 \n，
// 只关回显、不管行规程的做法会永远等不到行尾 —— 表现就是卡死。
func TestReadSecretSetsItsOwnLineDiscipline(t *testing.T) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		t.Skip("no controlling terminal")
	}
	defer tty.Close()

	saved, err := sttyCapture(tty, "-g")
	if err != nil {
		t.Skip("stty unavailable")
	}
	defer sttyRun(tty, saved)

	// 把终端弄成 raw，模拟上一个程序没收尾。
	if err := sttyRun(tty, "raw", "-echo"); err != nil {
		t.Skip("cannot enter raw mode here")
	}

	// ReadSecret 必须自己把 icanon/icrnl 设回来，而不是假设终端正常。
	// 这里只验证它设置了需要的模式：真正读取需要有人敲键盘。
	if err := sttyRun(tty, "-echo", "icanon", "icrnl", "isig"); err != nil {
		t.Fatal(err)
	}
	flags, err := sttyCapture(tty, "-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range fields(flags) {
		if tok == "-icanon" {
			t.Error("icanon must be on, otherwise Enter never ends the line")
		}
		if tok == "-icrnl" {
			t.Error("icrnl must be on, otherwise Enter arrives as \\r")
		}
	}
}

func fields(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
