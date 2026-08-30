//go:build !windows

package ui

import "syscall"

// rawRead 绕开 os.File.Read 直接调用 syscall.Read。
//
// 必须这么做：os.File.Read 在读到 0 字节时会报 io.EOF，而 termios 的
// VTIME 超时恰好就是一次 0 字节读 —— 两者混淆的后果是：用户只要停顶
// 超过 100ms，选择器就以为输入结束并自行取消。
//
// 返回：n > 0 有数据；n == 0 && ok 是超时；!ok 才是真的结束。
func (t *rawTTY) rawRead(buf []byte) (n int, ok bool) {
	// Fd() 会把文件置为阻塞模式并脱离 runtime 的轮询器，
	// 这正是我们要的：让 VMIN/VTIME 真正生效。
	n, err := syscall.Read(int(t.f.Fd()), buf)
	if err != nil {
		if err == syscall.EINTR || err == syscall.EAGAIN {
			return 0, true
		}
		return 0, false
	}
	return n, true
}
