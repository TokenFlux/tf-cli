package launch

import (
	"golang.org/x/sys/windows"
	"os"
)

type termState struct {
	tty, input            *os.File
	inputMode, outputMode uint32
}

func captureTerm() *termState {
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return &termState{}
	}
	output, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		input.Close()
		return &termState{}
	}
	t := &termState{tty: output, input: input}
	if windows.GetConsoleMode(windows.Handle(input.Fd()), &t.inputMode) != nil || windows.GetConsoleMode(windows.Handle(output.Fd()), &t.outputMode) != nil {
		input.Close()
		output.Close()
		return &termState{}
	}
	return t
}

func (t *termState) restore(killed bool) {
	if t.tty == nil {
		return
	}
	_ = windows.SetConsoleMode(windows.Handle(t.input.Fd()), t.inputMode)
	if killed {
		_ = windows.SetConsoleMode(windows.Handle(t.tty.Fd()), t.outputMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
		_, _ = t.tty.WriteString("\x1b[?1049l\x1b[?25h\x1b[r\x1b[?2004l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[<u\x1b[0m")
	}
	_ = windows.SetConsoleMode(windows.Handle(t.tty.Fd()), t.outputMode)
	_ = t.input.Close()
	_ = t.tty.Close()
	t.tty, t.input = nil, nil
}

func (t *termState) homeColumn() {
	if t.tty != nil {
		_, _ = t.tty.WriteString("\r")
	}
}
