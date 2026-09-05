//go:build !windows

package ui

import (
	"io"
	"os"
)

func ansiWriter(f *os.File) io.Writer { return f }

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// hasControllingTTY 报告能否打开控制终端。
func hasControllingTTY() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
