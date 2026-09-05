package ui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

func (u *UI) readPrompt(prompt string, secret bool) (string, error) {
	tty, err := openRawTTY()
	if err != nil {
		return "", ErrNotInteractive
	}
	defer guardTerminal(tty)()
	defer fmt.Fprint(tty.f, "\r\n")
	var text []rune
	cursor := 0
	for {
		_, cols := tty.Size()
		prefix := ansi.Truncate(prompt, max(0, cols-2), "") + " "
		visible, before := "", ""
		if !secret {
			start := cursor
			room := max(0, cols-ansi.StringWidth(prefix)-1)
			for start > 0 && ansi.StringWidth(string(text[start-1:cursor])) <= room {
				start--
			}
			visible = ansi.Truncate(string(text[start:]), room, "")
			before = string(text[start:cursor])
		}
		fmt.Fprintf(tty.f, "\r\x1b[2K%s%s\r\x1b[%dC", prefix, visible, ansi.StringWidth(prefix)+ansi.StringWidth(before))
		k, r := tty.readKey()
		switch k {
		case keyEnter:
			return string(text), nil
		case keyCancel:
			return "", Errf(CodeCancelled, u.T("已取消", "cancelled")).WithCause(ErrInterrupted)
		case keyEscape:
			return "", Errf(CodeCancelled, u.T("已取消", "cancelled"))
		case keyLeft:
			cursor = max(0, cursor-1)
		case keyRight:
			cursor = min(len(text), cursor+1)
		case keyHome:
			cursor = 0
		case keyEnd:
			cursor = len(text)
		case keyBackspace:
			if cursor > 0 {
				text = append(text[:cursor-1], text[cursor:]...)
				cursor--
			}
		case keyDelete:
			if cursor < len(text) {
				text = append(text[:cursor], text[cursor+1:]...)
			}
		case keyClear:
			text, cursor = nil, 0
		case keyRune:
			text = append(text, 0)
			copy(text[cursor+1:], text[cursor:])
			text[cursor] = r
			cursor++
		}
	}
}
