package ui

import (
	"os"
	"strings"
	"testing"
)

// 过滤是子串匹配且大小写不敏感，返回项要携带原始下标 ——
// 下标一旦错位，用户就会选到另一个模型。
func TestSelectorFilter(t *testing.T) {
	s := &selector{all: []Item{
		{Label: "codex-auto-review"},
		{Label: "gpt-5.4"},
		{Label: "gpt-5.5"},
		{Label: "gpt-5.6-sol"},
	}}

	if got := len(s.filtered()); got != 4 {
		t.Errorf("empty query should keep everything, got %d", got)
	}

	s.query = "sol"
	view := s.filtered()
	if len(view) != 1 || view[0].Label != "gpt-5.6-sol" {
		t.Fatalf("filter(sol) = %+v", view)
	}
	if view[0].index != 3 {
		t.Errorf("original index = %d, want 3", view[0].index)
	}

	s.query = "GPT-5.5"
	if view := s.filtered(); len(view) != 1 || view[0].index != 2 {
		t.Errorf("filter should be case-insensitive, got %+v", view)
	}

	s.query = "nothing"
	if view := s.filtered(); len(view) != 0 {
		t.Errorf("unmatched query should yield nothing, got %+v", view)
	}
}

// 显示宽度必须按列算：中日韩字符一个字 3 字节却占 2 列，
// 用字节数补齐会让所有中文表格错位。
func TestWidth(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"can run", 7},
		{"可跑", 4},
		{"已绑定", 6},
		{"key", 3},
		{"模型槽：", 8},
		{"a可b", 4},
		{"\x1b[1m中文\x1b[0m", 4},
		{"e\u0301", 1},
		{"\U0001F469\u200d\U0001F4BB", 2},
	}
	for _, c := range cases {
		if got := Width(c.s); got != c.want {
			t.Errorf("Width(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

// 中英文标签补到同一宽度后，右侧内容必须对齐。
func TestPadAligns(t *testing.T) {
	zh := Pad("可跑", 10) + "x"
	zh2 := Pad("已绑定", 10) + "x"
	if Width(zh) != Width(zh2) {
		t.Errorf("CJK labels misaligned: %q (%d) vs %q (%d)", zh, Width(zh), zh2, Width(zh2))
	}
	en := Pad("can run", 10) + "x"
	if Width(en) != Width(zh) {
		t.Errorf("mixed-language columns misaligned: %d vs %d", Width(en), Width(zh))
	}
	if got := Width(Pad("\x1b[1m中文\x1b[0m", 10)); got != 10 {
		t.Errorf("styled label padded to %d columns, want 10", got)
	}
	// 超长标签不截断，宁可错位也不丢信息。
	if got := Pad("averyverylonglabel", 4); got != "averyverylonglabel" {
		t.Errorf("Pad must not truncate: %q", got)
	}
}

// 不可用项要展示但不能被选中，光标也不能停在上面。
func TestDisabledItems(t *testing.T) {
	items := []Item{
		{Label: "paste a key"},
		{Label: "import from web", Disabled: true},
		{Label: "another"},
	}
	if got := firstEnabled(items); got != 0 {
		t.Errorf("firstEnabled = %d, want 0", got)
	}
	if got := firstEnabled([]Item{{Label: "x", Disabled: true}, {Label: "y"}}); got != 1 {
		t.Errorf("firstEnabled should skip the disabled head, got %d", got)
	}

	s := &selector{all: items}
	view := s.filtered()

	// 向下要跨过禁用项，落到 2 而不是停在 1。
	if got := s.seek(view, 0, +1); got != 2 {
		t.Errorf("seek down = %d, want 2 (skipping the disabled item)", got)
	}
	if got := s.seek(view, 2, -1); got != 0 {
		t.Errorf("seek up = %d, want 0", got)
	}
	// 没有下一个可选项时返回 -1，光标保持不动。
	if got := s.seek(view, 2, +1); got != -1 {
		t.Errorf("seek past the end = %d, want -1", got)
	}

	// 全部禁用时不能崩，也不能凭空选中。
	allOff := &selector{all: []Item{{Label: "a", Disabled: true}}}
	if got := allOff.seek(allOff.filtered(), 0, +1); got != -1 {
		t.Errorf("seek with no enabled item = %d, want -1", got)
	}
}

// 过滤要覆盖 Detail：屏幕上写着 ccmax/、work，用户就会照着敲，
// 而分组前缀和 Key 名恰好只出现在 Detail 里。
func TestFilterMatchesDetail(t *testing.T) {
	s := &selector{all: []Item{
		{Label: "claude-opus-5", Detail: "ccmax/"},
		{Label: "gpt-5.4", Detail: "gpt/"},
		{Label: "ccmax-lookalike", Detail: "gpt/"},
	}}

	s.query = "ccmax"
	got := s.filtered()
	if len(got) != 2 {
		t.Fatalf("filtered %d items, want 2", len(got))
	}
	// Label 命中的排在前面。
	if got[0].Label != "ccmax-lookalike" {
		t.Errorf("label match should sort first, got %q", got[0].Label)
	}
}

// 兜底选择器必须读控制终端，不能读 stdin。
//
// stdin 常被管道占住（echo $KEY | tf login），而用户仍坐在终端前。
// 此前这里读 os.Stdin，于是兜底路径在最需要它的场合直接拿到 EOF。
func TestChooseReadsControllingTerminal(t *testing.T) {
	src, err := os.ReadFile("prompt.go")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(src), "\r\n", "\n")
	start := strings.Index(body, "func (u *UI) Choose(")
	if start < 0 {
		t.Fatal("Choose not found")
	}
	fn := body[start : start+strings.Index(body[start:], "\n}\n")]

	if strings.Contains(fn, "os.Stdin") {
		t.Error("Choose 不能读 os.Stdin —— 管道会把它占掉")
	}
	if !strings.Contains(fn, `u.ReadLine(`) {
		t.Error("Choose must use the controlling-terminal line reader")
	}
	// 输错要再问，不能一次就放弃。
	if !strings.Contains(fn, "for attempt") {
		t.Error("输入无效时应当重问，而不是直接报错退出")
	}
}
