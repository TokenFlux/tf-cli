package ui

import (
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

type rawTTY struct {
	f                     *os.File
	input                 *os.File
	inputMode, outputMode uint32
	pending               []byte
	surrogate             uint16
	once                  sync.Once
}

func openRawTTY() (*rawTTY, error) {
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	output, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		input.Close()
		return nil, err
	}
	t := &rawTTY{f: output, input: input}
	if err = windows.GetConsoleMode(windows.Handle(input.Fd()), &t.inputMode); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	if err = windows.GetConsoleMode(windows.Handle(output.Fd()), &t.outputMode); err != nil {
		input.Close()
		output.Close()
		return nil, err
	}
	mode := t.inputMode &^ (windows.ENABLE_LINE_INPUT | windows.ENABLE_ECHO_INPUT | windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_QUICK_EDIT_MODE | windows.ENABLE_MOUSE_INPUT | windows.ENABLE_VIRTUAL_TERMINAL_INPUT)
	mode |= windows.ENABLE_EXTENDED_FLAGS | windows.ENABLE_WINDOW_INPUT
	if err = windows.SetConsoleMode(windows.Handle(input.Fd()), mode); err != nil {
		t.Restore()
		return nil, err
	}
	if err = windows.SetConsoleMode(windows.Handle(output.Fd()), t.outputMode|windows.ENABLE_PROCESSED_OUTPUT|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		t.Restore()
		return nil, err
	}
	return t, nil
}

func (t *rawTTY) Restore() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		if t.input != nil {
			_ = windows.SetConsoleMode(windows.Handle(t.input.Fd()), t.inputMode)
			_ = t.input.Close()
		}
		if t.f != nil {
			_ = windows.SetConsoleMode(windows.Handle(t.f.Fd()), t.outputMode)
			_ = t.f.Close()
		}
	})
}

func (t *rawTTY) Size() (rows, cols int) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(t.f.Fd()), &info); err != nil {
		return 24, 80
	}
	return max(1, int(info.Window.Bottom-info.Window.Top+1)), max(1, int(info.Window.Right-info.Window.Left+1))
}

// INPUT_RECORD is a 4-byte header followed by a 16-byte event union.
// This layout represents KEY_EVENT_RECORD; non-key events are discarded.
type consoleInputRecord struct {
	EventType                       uint16
	_                               uint16
	KeyDown                         int32
	Repeat, KeyCode, ScanCode, Char uint16
	Control                         uint32
}

var readConsoleInput = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReadConsoleInputW")

func (t *rawTTY) rawRead(buf []byte) (int, bool) {
	if t.input == nil {
		n, err := t.f.Read(buf)
		return n, err == nil
	}
	deadline := time.Now().Add(100 * time.Millisecond)
	for len(t.pending) == 0 {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return 0, true
		}
		result, err := windows.WaitForSingleObject(windows.Handle(t.input.Fd()), uint32(max(1, remaining.Milliseconds())))
		if err != nil {
			return 0, false
		}
		if result == uint32(windows.WAIT_TIMEOUT) {
			return 0, true
		}
		if result != windows.WAIT_OBJECT_0 {
			return 0, false
		}
		var event consoleInputRecord
		var count uint32
		ok, _, _ := readConsoleInput.Call(t.input.Fd(), uintptr(unsafe.Pointer(&event)), 1, uintptr(unsafe.Pointer(&count)))
		if ok == 0 {
			return 0, false
		}
		if count != 0 {
			t.queueEvent(event)
		}
	}
	n := copy(buf, t.pending)
	t.pending = t.pending[n:]
	return n, true
}

func (t *rawTTY) queueEvent(e consoleInputRecord) {
	// ConPTY emits characters outside the active code page on Alt key release.
	if e.EventType != 1 || (e.KeyDown == 0 && (e.KeyCode != 0x12 || e.Char == 0)) {
		return
	}
	var text string
	switch e.KeyCode {
	case 0x26:
		text = "\x1b[A"
	case 0x28:
		text = "\x1b[B"
	case 0x27:
		text = "\x1b[C"
	case 0x25:
		text = "\x1b[D"
	case 0x24:
		text = "\x1b[H"
	case 0x23:
		text = "\x1b[F"
	case 0x2e:
		text = "\x1b[3~"
	default:
		if e.Char == 0 {
			return
		}
		r := rune(e.Char)
		if r >= 0xd800 && r <= 0xdbff {
			t.surrogate = e.Char
			return
		}
		if r >= 0xdc00 && r <= 0xdfff {
			r = utf16.DecodeRune(rune(t.surrogate), r)
		}
		t.surrogate = 0
		if r == utf8.RuneError {
			return
		}
		text = string(r)
	}
	t.pending = append(t.pending, strings.Repeat(text, max(1, int(e.Repeat)))...)
}
