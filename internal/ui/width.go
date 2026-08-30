package ui

import "unicode"

// Width 返回字符串在终端里占的列数。
//
// 不能用 len()：中日韩字符一个字 3 字节却占 2 列，用字节数补齐会让
// 所有中文表格错位。也不能用 utf8.RuneCountInString：那会把宽字符算成 1 列。
func Width(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// Pad 把字符串按显示宽度右填充到 w 列。
//
// 相当于 fmt 的 %-*s，但按列而不是按字节。
func Pad(s string, w int) string {
	n := w - Width(s)
	if n <= 0 {
		return s
	}
	return s + spaces(n)
}

func spaces(n int) string {
	const blanks = "                                                                "
	if n <= len(blanks) {
		return blanks[:n]
	}
	out := make([]byte, n)
	for i := range out {
		out[i] = ' '
	}
	return string(out)
}

// wide 是占两列的码位区间（East Asian Wide / Fullwidth，以及常见 emoji）。
var wide = [...][2]rune{
	{0x1100, 0x115F},   // 韩文字母
	{0x2E80, 0x303E},   // 中日韩部首、假名补充
	{0x3041, 0x33FF},   // 平假名、片假名、中日韩符号
	{0x3400, 0x4DBF},   // 中日韩扩展 A
	{0x4E00, 0x9FFF},   // 中日韩统一表意文字
	{0xA000, 0xA4CF},   // 彝文
	{0xAC00, 0xD7A3},   // 韩文音节
	{0xF900, 0xFAFF},   // 兼容表意文字
	{0xFE10, 0xFE19},   // 竖排标点
	{0xFE30, 0xFE6F},   // 兼容形式
	{0xFF00, 0xFF60},   // 全角字符
	{0xFFE0, 0xFFE6},   // 全角符号
	{0x1F300, 0x1F64F}, // 杂项符号与emoji
	{0x1F900, 0x1F9FF}, // 补充符号
	{0x20000, 0x3FFFD}, // 中日韩扩展 B 及以后
}

func runeWidth(r rune) int {
	switch {
	case r == 0:
		return 0
	// 组合记号不占位；控制字符按 0 算，避免负宽度。
	case r < 32 || (r >= 0x7F && r < 0xA0):
		return 0
	case unicode.Is(unicode.Mn, r):
		return 0
	}
	for _, rng := range wide {
		if r >= rng[0] && r <= rng[1] {
			return 2
		}
	}
	return 1
}
