package ui

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"
)

// rawTTY 是一个进入了 raw 模式的终端。
//
// 直接操作 /dev/tty 而非 stdin/stdout：这样即便 stdout 被重定向、
// stdin 是管道，交互选择依然可用，而重定向出去的内容不会被界面污染。
type rawTTY struct {
	f     *os.File
	saved string
}

// openRawTTY 进入 raw 模式。失败时调用方应降级为编号选择器，而不是报错。
func openRawTTY() (*rawTTY, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}

	// 先存下当前设置，退出时原样恢复；不用 `stty sane`，
	// 那会顺手改掉用户本来就自定义过的设置。
	saved, err := sttyCapture(f, "-g")
	if err != nil {
		f.Close()
		return nil, err
	}
	// min 0 time 1：读取最多阻塞 100ms 即可返回 0 字节。
	// 这是区分「裸 ESC 键」与「方向键的转义序列」的唯一可靠办法：
	// 两者首字节相同，只能看后续字节是否及时到达。
	if err := sttyRun(f, "raw", "-echo", "min", "0", "time", "1"); err != nil {
		f.Close()
		return nil, err
	}
	return &rawTTY{f: f, saved: saved}, nil
}

// Restore 恢复终端设置。必须保证被调用，否则用户的终端会留在 raw 模式。
func (t *rawTTY) Restore() {
	if t == nil || t.f == nil {
		return
	}
	if t.saved != "" {
		_ = sttyRun(t.f, t.saved)
	}
	t.f.Close()
	t.f = nil
}

// Size 返回终端行列数，取不到就给一个保守的默认值。
func (t *rawTTY) Size() (rows, cols int) {
	rows, cols = 24, 80
	out, err := sttyCapture(t.f, "size")
	if err != nil {
		return
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return
	}
	if r, err := strconv.Atoi(parts[0]); err == nil && r > 0 {
		rows = r
	}
	if c, err := strconv.Atoi(parts[1]); err == nil && c > 0 {
		cols = c
	}
	return
}

func sttyRun(f *os.File, args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = f
	cmd.Stdout = os.Stderr
	return cmd.Run()
}

func sttyCapture(f *os.File, args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = f
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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
		switch final {
		case 'A':
			return keyUp, 0
		case 'B':
			return keyDown, 0
		}
		// 其它序列（Home/PageUp 等）可能带参数，一直读到终结字节为止，
		// 否则残留字节会被当成过滤输入。
		for final >= '0' && final <= '?' {
			if final, ok = t.readByteTimeout(); !ok {
				break
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
