//go:build !windows

package ui

import "os"

// InteractiveSupported 报告这个平台上有没有交互界面。
//
// 拆成平台常量而不是让 hasControllingTTY 一律返回 false：
// 「这台机器现在没有终端」和「这个平台还没做交互」是两回事，
// 给用户的话也该不同。前者叫他换个跑法，后者只能叫他等版本。
const InteractiveSupported = true

// hasControllingTTY 报告能否打开控制终端。
func hasControllingTTY() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
