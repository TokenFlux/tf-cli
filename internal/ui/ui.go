// Package ui 负责所有面向用户的输出：文案本地化、TTY 检测、颜色、
// 以及 --json 的统一信封。
//
// 约定（见 docs/PLAN.md）：
//   - 文案跟随 locale，TKR_LANG 可覆盖；错误码始终是英文常量。
//   - --json 必须显式指定，不因为管道而自动切换格式。
//   - 非 TTY 时日志转 stderr、去掉颜色与动画。
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Lang 是文案语言。错误码不受它影响。
type Lang string

const (
	LangEN Lang = "en"
	LangZH Lang = "zh"
)

// UI 是输出的唯一入口。命令不应直接 fmt.Println。
type UI struct {
	Out   io.Writer
	Err   io.Writer
	Lang  Lang
	JSON  bool
	Color bool
	TTY   bool
}

// New 依据环境构造 UI。jsonMode 来自显式的 --json。
func New(jsonMode bool) *UI {
	tty := isTerminal(os.Stdout)
	return &UI{
		Out:   terminalWriter(os.Stdout),
		Err:   terminalWriter(os.Stderr),
		Lang:  detectLang(),
		JSON:  jsonMode,
		Color: tty && os.Getenv("NO_COLOR") == "" && !jsonMode,
		TTY:   tty,
	}
}

// detectLang 依次读取 TKR_LANG、LC_ALL、LC_MESSAGES、LANG。
func detectLang() Lang {
	for _, k := range []string{"TKR_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.ToLower(strings.TrimSpace(os.Getenv(k)))
		if v == "" {
			continue
		}
		switch {
		case strings.HasPrefix(v, "zh"):
			return LangZH
		case strings.HasPrefix(v, "en"):
			return LangEN
		}
	}
	return LangEN
}

// isTerminal 不引入第三方依赖，直接看文件模式。
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// T 在两种语言之间选择。文案与代码放在一起，避免维护一份易腐烂的目录。
func (u *UI) T(zh, en string) string {
	if u.Lang == LangZH {
		return zh
	}
	return en
}

// Printf 输出正文到 stdout。JSON 模式下静默，正文应改由 Emit 承载。
func (u *UI) Printf(format string, a ...any) {
	if u.JSON {
		return
	}
	fmt.Fprintf(u.Out, format, a...)
}

// Logf 输出提示性信息。始终走 stderr，保证 stdout 可被安全地重定向。
func (u *UI) Logf(format string, a ...any) {
	if u.JSON {
		return
	}
	fmt.Fprintf(u.Err, format+"\n", a...)
}

// Warnf 输出警告，走 stderr。
func (u *UI) Warnf(format string, a ...any) {
	if u.JSON {
		return
	}
	fmt.Fprintf(u.Err, "%s %s\n", u.paint("warning:", yellow), fmt.Sprintf(format, a...))
}

type color string

const (
	reset  color = "\x1b[0m"
	red    color = "\x1b[31m"
	yellow color = "\x1b[33m"
	dim    color = "\x1b[2m"
	bold   color = "\x1b[1m"
)

func (u *UI) paint(s string, c color) string {
	if !u.Color {
		return s
	}
	return string(c) + s + string(reset)
}

// Dim 用于次要信息。
func (u *UI) Dim(s string) string { return u.paint(s, dim) }

// Bold 用于强调。
func (u *UI) Bold(s string) string { return u.paint(s, bold) }

// envelope 是 --json 的统一信封。
type envelope struct {
	OK      bool       `json:"ok"`
	Command string     `json:"command"`
	Data    any        `json:"data,omitempty"`
	Error   *jsonError `json:"error,omitempty"`
}

type jsonError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Cause   string `json:"cause,omitempty"`
}

// Emit 在 JSON 模式下输出成功信封；非 JSON 模式下交给 human 回调渲染。
func (u *UI) Emit(command string, data any, human func()) {
	if !u.JSON {
		if human != nil {
			human()
		}
		return
	}
	enc := json.NewEncoder(u.Out)
	enc.SetIndent("", "  ")
	_ = enc.Encode(envelope{OK: true, Command: command, Data: data})
}

// Fail 输出错误。JSON 模式下也保持同一信封，便于其它 agent 消费。
func (u *UI) Fail(command string, err error) {
	e := AsError(err)
	if u.JSON {
		enc := json.NewEncoder(u.Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(envelope{
			OK:      false,
			Command: command,
			Error:   &jsonError{Code: string(e.Code), Message: e.Message, Hint: e.Hint, Cause: causeText(e)},
		})
		return
	}
	fmt.Fprintf(u.Err, "%s %s\n", u.paint(u.T("错误：", "error:"), red), e.Message)
	// 底层原因必须显示。“update failed” 不带原因等于没说，
	// 而用户没有别的途径知道到底差了什么。
	if cause := causeText(e); cause != "" {
		fmt.Fprintf(u.Err, "  %s\n", u.Dim(cause))
	}
	if e.Hint != "" {
		fmt.Fprintf(u.Err, "  %s\n", u.Dim(e.Hint))
	}
	fmt.Fprintf(u.Err, "  %s\n", u.Dim(string(e.Code)))
}
