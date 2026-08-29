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
