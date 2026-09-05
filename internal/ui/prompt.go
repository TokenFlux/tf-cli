package ui

import (
	"fmt"
	"strconv"
	"strings"
)

var ErrNotInteractive = Errf(CodeUsage, "not an interactive terminal")

// Interactive checks the controlling terminal, independently of redirected stdin.
func (u *UI) Interactive(assumeYes bool) bool {
	return !u.JSON && !assumeYes && hasControllingTTY()
}

// Choose is the numbered fallback when a raw selector is unavailable.
func (u *UI) Choose(title string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, Errf(CodeInternal, "no options to choose from")
	}
	if !hasControllingTTY() {
		return 0, ErrNotInteractive
	}
	fmt.Fprintf(u.Err, "%s\n\n", title)
	for i, o := range options {
		fmt.Fprintf(u.Err, "  %d) %s\n", i+1, o)
	}
	for attempt := 0; attempt < 3; attempt++ {
		line, err := u.ReadLine(u.T(fmt.Sprintf("选择 [1-%d]：", len(options)), fmt.Sprintf("Choose [1-%d]:", len(options))))
		if err != nil {
			return 0, err
		}
		if n, err := strconv.Atoi(strings.TrimSpace(line)); err == nil && n >= 1 && n <= len(options) {
			return n - 1, nil
		}
		u.Warnf(u.T("请输入 1 到 %d 之间的数字", "enter a number between 1 and %d"), len(options))
	}
	return 0, Errf(CodeUsage, u.T("无效的选择", "invalid choice"))
}

// ReadLine uses the controlling terminal rather than potentially piped stdin.
func (u *UI) ReadLine(prompt string) (string, error) {
	line, err := u.readPrompt(prompt, false)
	return strings.TrimSpace(line), err
}

// ReadSecret preserves spaces and never echoes the entered text.
func (u *UI) ReadSecret(prompt string) (string, error) {
	return u.readPrompt(prompt, true)
}
