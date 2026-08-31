package ui

import (
	"bytes"
	"io"
	"os"
)

// crlfWriter 把 \n 写成 \r\n。
//
// 终端可能已经处于 raw 模式（ONLCR 关闭）—— 上一个程序被 kill -9、
// 崩溃、或干脆没收尾都会留下这种状态。此时 \n 只换行不回车，
// 于是后续每一行都从上一行的结尾处开始，输出呈阶梯状。
//
// tf 不去改终端设置替用户「修好」它 —— 那是用户的终端。
// 但 tf 自己的输出必须在两种模式下都正确：cooked 模式下 ONLCR 会把
// \n 变成 \r\n，多出来的那个 \r 落在行首，没有任何视觉影响。
//
// 这同时保证了把终端交给子进程时光标停在第 0 列。子进程（如 Claude Code）
// 会用绝对列定位画自己的界面，却假定起始列是 0，差一列整块就歪了。
type crlfWriter struct{ w io.Writer }

func (c crlfWriter) Write(p []byte) (int, error) {
	if !bytes.ContainsRune(p, '\n') {
		return c.w.Write(p)
	}
	// 已经是 \r\n 的不要再加，否则会变成 \r\r\n。
	var b bytes.Buffer
	b.Grow(len(p) + 8)
	for i, ch := range p {
		if ch == '\n' && (i == 0 || p[i-1] != '\r') {
			b.WriteByte('\r')
		}
		b.WriteByte(ch)
	}
	if _, err := c.w.Write(b.Bytes()); err != nil {
		return 0, err
	}
	return len(p), nil
}

// terminalWriter 只在目标是终端时套上 CRLF 转换。
//
// 管道和文件必须保持纯 \n：给它们塞 \r 会污染下游的解析。
func terminalWriter(f *os.File) io.Writer {
	if !isTerminal(f) {
		return f
	}
	return crlfWriter{w: f}
}
