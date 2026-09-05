package ui

import (
	"os"
	"os/signal"
	"syscall"
	"unicode/utf8"
)

// Restore console modes on external interruption as well as ordinary return.
func guardTerminal(tty *rawTTY) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-signals:
			tty.Restore()
			os.Exit(130)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
		tty.Restore()
	}
}

// key 是一次按键的语义化结果。
type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyEnter
	// keyEscape 是裸 ESC：意思是「退一步」，先清过滤再论退出。
	keyEscape
	// keyCancel 是 Ctrl-C / Ctrl-D / 输入结束：意思是「现在就结束」。
	keyCancel
	keyBackspace
	keyClear
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyDelete
	keyRune
)

// readByte 阻塞到读到一个字节。ok 为 false 表示输入真的结束了。
func (t *rawTTY) readByte() (b byte, ok bool) {
	var buf [1]byte
	idle := 0
	for {
		n, alive := t.rawRead(buf[:])
		if !alive {
			return 0, false
		}
		if n == 1 {
			return buf[0], true
		}
		// 纯超时：继续等。加上阐值只是为了避免在终端异常时死循环。
		idle++
		if idle > 36000 { // 100ms × 36000 ≈ 1 小时
			return 0, false
		}
	}
}

// readByteTimeout 只等一个读周期（约 100ms）。
// 超时返回 false，用于判定转义序列是否结束。
func (t *rawTTY) readByteTimeout() (b byte, ok bool) {
	var buf [1]byte
	n, alive := t.rawRead(buf[:])
	if !alive || n != 1 {
		return 0, false
	}
	return buf[0], true
}

// readKey 把字节流翻译成按键。r 仅在 keyRune 时有效。
//
// 转义序列必须逐字节读：一次 Read 不保证拿齐 \033[A 三个字节，
// 按“读满 2 个”写会在分段到达时把方向键错当成字符输入。
func (t *rawTTY) readKey() (k key, r rune) {
	c, ok := t.readByte()
	if !ok {
		return keyCancel, 0
	}

	switch c {
	case 0x03, 0x04: // Ctrl-C / Ctrl-D
		return keyCancel, 0
	case '\r', '\n':
		return keyEnter, 0
	case 0x7f, 0x08:
		return keyBackspace, 0
	case 0x0e: // Ctrl-N
		return keyDown, 0
	case 0x10: // Ctrl-P
		return keyUp, 0
	case 0x15: // Ctrl-U
		return keyClear, 0
	case 0x1b:
		intro, ok := t.readByteTimeout()
		if !ok {
			return keyEscape, 0 // 后续字节未到，则是裸 ESC
		}
		if intro != '[' && intro != 'O' {
			return keyNone, 0
		}
		final, ok := t.readByteTimeout()
		if !ok {
			return keyNone, 0
		}
		params := ""
		for final >= '0' && final <= '?' {
			params += string(final)
			if final, ok = t.readByteTimeout(); !ok {
				return keyNone, 0
			}
		}
		switch final {
		case 'A':
			return keyUp, 0
		case 'B':
			return keyDown, 0
		case 'C':
			return keyRight, 0
		case 'D':
			return keyLeft, 0
		case 'H':
			return keyHome, 0
		case 'F':
			return keyEnd, 0
		case '~':
			switch params {
			case "3":
				return keyDelete, 0
			case "1", "7":
				return keyHome, 0
			case "4", "8":
				return keyEnd, 0
			}
		}
		return keyNone, 0
	}

	// j / k / q 不能当快捷键：同一个选择器支持直接输入过滤，
	// 而目录里就有 claude-haiku（带 k）、qwen、kimi。
	// 不带字母的替代键在上面：Ctrl-P / Ctrl-N。

	if c >= utf8.RuneSelf {
		buf := []byte{c}
		for !utf8.FullRune(buf) {
			next, ok := t.readByteTimeout()
			if !ok {
				return keyNone, 0
			}
			buf = append(buf, next)
		}
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 {
			return keyNone, 0
		}
		return keyRune, r
	}
	if c >= 0x20 {
		return keyRune, rune(c)
	}
	return keyNone, 0
}
