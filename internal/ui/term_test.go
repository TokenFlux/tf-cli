package ui

import (
	"os"
	"testing"
)

// keyOf 把一段字节喂给 readKey，返回它翻译出的按键。
func keyOf(t *testing.T, in []byte) (key, rune) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	if _, err := w.Write(in); err != nil {
		t.Fatalf("write: %v", err)
	}
	w.Close()
	return (&rawTTY{f: r}).readKey()
}

// j / k / q 必须是普通字符：同一个选择器支持直接输入过滤，而目录里
// 有 claude-haiku（带 k）、qwen、kimi。当过滤用不了的模型正是最需要过滤的那些。
func TestLettersAreFilterInput(t *testing.T) {
	for _, c := range []byte{'j', 'k', 'q'} {
		k, r := keyOf(t, []byte{c})
		if k != keyRune || r != rune(c) {
			t.Errorf("%q → key %v rune %q, want keyRune %q", c, k, r, c)
		}
	}
}

func TestUTF8FilterInput(t *testing.T) {
	for _, r := range []rune{'中', '文', 'é', '\U0001F600'} {
		k, got := keyOf(t, []byte(string(r)))
		if k != keyRune || got != r {
			t.Errorf("%q decoded as key=%v rune=%q", r, k, got)
		}
	}
	for _, invalid := range [][]byte{{0xff}, {0xe4, 0xb8}} {
		if k, _ := keyOf(t, invalid); k != keyNone {
			t.Errorf("invalid UTF-8 accepted: %x", invalid)
		}
	}
}

// 不带字母的移动键仍要有：方向键之外还认 Ctrl-P / Ctrl-N。
func TestControlKeys(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want key
	}{
		{"ctrl-p", []byte{0x10}, keyUp},
		{"ctrl-n", []byte{0x0e}, keyDown},
		{"ctrl-u", []byte{0x15}, keyClear},
		{"ctrl-c", []byte{0x03}, keyCancel},
		{"ctrl-d", []byte{0x04}, keyCancel},
		{"up", []byte{0x1b, '[', 'A'}, keyUp},
		{"down", []byte{0x1b, '[', 'B'}, keyDown},
		{"enter", []byte{'\r'}, keyEnter},
		{"backspace", []byte{0x7f}, keyBackspace},
	}
	for _, c := range cases {
		if k, _ := keyOf(t, c.in); k != c.want {
			t.Errorf("%s → %v, want %v", c.name, k, c.want)
		}
	}
}

// 裸 ESC 与 Ctrl-C 不是一回事：ESC 是「退一步」，先清过滤；
// Ctrl-C 才是「现在就结束」。两者混同的话，打错一个字符就只能退出整个选择器。
func TestBareEscapeIsNotCancel(t *testing.T) {
	if k, _ := keyOf(t, []byte{0x1b}); k != keyEscape {
		t.Errorf("bare ESC → %v, want keyEscape", k)
	}
}

// ESC 在有过滤时只清过滤，没过滤时才退出。
func TestEscapeClearsQueryFirst(t *testing.T) {
	s := &selector{query: "hai", all: []Item{{Label: "claude-haiku-4-5"}}}
	if !s.escape() {
		t.Fatal("ESC with a query should be consumed by clearing it")
	}
	if s.query != "" {
		t.Errorf("query = %q, want empty", s.query)
	}
	if s.escape() {
		t.Error("ESC without a query should fall through to cancel")
	}
}
