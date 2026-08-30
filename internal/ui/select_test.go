package ui

import "testing"

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
