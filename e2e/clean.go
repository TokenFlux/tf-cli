package e2e

import (
	"regexp"
	"strings"
)

// ansi 匹配 CSI 序列、OSC 串与几种双字符转义。
//
// 这个正则自己错了会让所有 pty 断言失去意义 —— 屏幕上多一个残留的
// 转义字符，字符串比对就会失败，而失败原因看起来会像是被测代码的问题。
// 所以它有自己的测试，且不带 pty 标签：日常 make check 就该跑到。
var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[()][B0]|\x1b[=>]`)

// clean 剥掉转义序列并把 CRLF 归一成 LF。
func clean(s string) string {
	return strings.ReplaceAll(ansi.ReplaceAllString(s, ""), "\r\n", "\n")
}
