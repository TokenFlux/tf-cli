package ui

import (
	"bytes"
	"os"
	"testing"
)

// 终端可能已经处于 raw 模式，此时 \n 只换行不回车，输出会呈阶梯状。
func TestCRLFWriter(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a\nb\n", "a\r\nb\r\n"},
		{"no newline", "no newline"},
		{"", ""},
		// 已经带 \r 的不能再补，否则变成 \r\r\n。
		{"a\r\nb", "a\r\nb"},
		{"\n", "\r\n"},
		{"mixed\r\nand\n", "mixed\r\nand\r\n"},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		w := crlfWriter{w: &buf}
		n, err := w.Write([]byte(c.in))
		if err != nil {
			t.Fatal(err)
		}
		// 必须报告写入了「调用方给的字节数」，否则 fmt 会判定为短写。
		if n != len(c.in) {
			t.Errorf("Write(%q) = %d, want %d", c.in, n, len(c.in))
		}
		if got := buf.String(); got != c.want {
			t.Errorf("Write(%q) wrote %q, want %q", c.in, got, c.want)
		}
	}
}

// 管道和文件必须保持纯 \n：塞 \r 会污染下游解析。
func TestTerminalWriterLeavesPipesAlone(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Skip(err)
	}
	defer r.Close()
	defer w.Close()

	if _, ok := terminalWriter(w).(crlfWriter); ok {
		t.Error("a pipe must not get CRLF translation")
	}
}

// 直接写 /dev/tty 的地方必须自带 \r：那条路径绕开了 crlfWriter，
// 而终端可能处于 raw 模式（ONLCR 关闭）。
//
// 具体的坑：ReadSecret 关了回显，用户按的回车不会被终端回显，
// 换行得由我们补。只补 \n 的话，光标停在提示语末列，
// 下一行就从那里开始 —— 「✓ saved as key」会被顶到半屏之后。
func TestDirectTTYWritesCarryCR(t *testing.T) {
	src, err := os.ReadFile("prompt.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`fmt.Fprintln(tty)`, `fmt.Fprint(tty, "\n")`} {
		if bytes.Contains(src, []byte(bad)) {
			t.Errorf("%s writes a bare LF to the terminal; use \\r\\n", bad)
		}
	}
	if !bytes.Contains(src, []byte(`fmt.Fprint(tty, "\r\n")`)) {
		t.Error("ReadSecret must emit CRLF after the hidden input")
	}
}
