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
