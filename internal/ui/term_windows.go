package ui

// rawRead 在 Windows 上退回 os.File.Read。
//
// Unix 版之所以要绕开它，是因为 termios 的 VTIME 超时表现为一次 0 字节读，
// 而 os.File.Read 会把它报成 io.EOF。Windows 没有 termios，也就没有这个
// 歧义 —— 而且 syscall.Read 在这里收的是 Handle 而不是 int，
// 照抄 Unix 那份根本编译不过。
//
// 注意 Windows 支持在 v0 标注为实验性：能编译，交互路径未经验证。
func (t *rawTTY) rawRead(buf []byte) (n int, ok bool) {
	n, err := t.f.Read(buf)
	if err != nil {
		return 0, false
	}
	return n, true
}
