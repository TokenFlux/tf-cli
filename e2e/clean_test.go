package e2e

import "testing"

// clean 自己错了会让所有 pty 断言失去意义，所以它单独测，
// 而且不带 pty 标签 —— 它是纯函数，日常 make check 就该跑到。
func TestCleanStripsEscapes(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{"\x1b[2mdim\x1b[0m", "dim"},
		{"\x1b[1;31mbold red\x1b[0m", "bold red"},
		{"a\r\nb", "a\nb"},                     // CRLF 归一
		{"\x1b[?25lhidden\x1b[?25h", "hidden"}, // 私有参数（光标显隐）
		{"\x1b[2J\x1b[Hcleared", "cleared"},    // 清屏与归位
		{"\x1b]0;title\x07text", "text"},       // OSC 以 BEL 收尾
		{"\x1b]0;title\x1b\\text", "text"},     // OSC 以 ST 收尾
		{"\x1b[38;5;244mgrey\x1b[39m", "grey"}, // 256 色
		{"keep [brackets] and \\ backslash", "keep [brackets] and \\ backslash"},
	} {
		if got := clean(tc.in); got != tc.want {
			t.Errorf("clean(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
