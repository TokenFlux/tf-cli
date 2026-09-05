package ui

import (
	"golang.org/x/sys/windows"
	"io"
	"os"
)

type consoleWriter struct{ f *os.File }

func ansiWriter(f *os.File) io.Writer { return consoleWriter{f: f} }

func (w consoleWriter) Write(p []byte) (int, error) {
	handle := windows.Handle(w.f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return 0, err
	}
	if err := windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT); err != nil {
		return 0, err
	}
	defer windows.SetConsoleMode(handle, mode)
	return w.f.Write(p)
}

func isTerminal(f *os.File) bool {
	var mode uint32
	if windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) != nil {
		return false
	}
	return true
}

func hasControllingTTY() bool {
	f, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}
