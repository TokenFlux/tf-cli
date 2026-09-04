package model

import "testing"

// 只有真正的强度后缀才该被拆开；名字里本来就带连字符的模型不能误伤。
func TestParse(t *testing.T) {
	cases := []struct {
		id     string
		base   string
		effort string
	}{
		{"gemini-3.1-pro-high", "gemini-3.1-pro", "high"},
		{"gemini-3.6-flash-medium", "gemini-3.6-flash", "medium"},
		{"gemini-3.7-flash-tiered", "gemini-3.7-flash", "tiered"},
		{"gpt-5.6-sol", "gpt-5.6-sol", ""}, // sol 不是强度
		{"claude-opus-5", "claude-opus-5", ""},
		{"codex-auto-review", "codex-auto-review", ""},
		{"gpt-5.4", "gpt-5.4", ""},
	}
	for _, c := range cases {
		got := Parse(c.id)
		if got.Base != c.base || got.Effort != c.effort {
			t.Errorf("Parse(%q) = %+v, want %s/%s", c.id, got, c.base, c.effort)
		}
		if round := got.String(); round != c.id {
			t.Errorf("round-trip of %q gave %q", c.id, round)
		}
	}
}

// 补全只需要模型列表里实际出现的强度名，不需要构造族对象。
func TestEfforts(t *testing.T) {
	ids := []string{
		"gemini-3.1-pro-high", "gemini-3.1-pro-low",
		"gemini-3.6-flash-high", "gemini-3.6-flash-low", "gemini-3.6-flash-medium",
		"gemini-3.7-flash-tiered", "claude-opus-5",
	}
	got := Efforts(ids)
	want := []string{"low", "high", "medium", "tiered"}
	if len(got) != len(want) {
		t.Fatalf("Efforts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Efforts()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// 族顺序由任意成员首次出现的位置决定，包括不带强度的裸 ID。
func TestEffortsUsesPlainModelToOrderFamilies(t *testing.T) {
	got := Efforts([]string{
		"alpha", "beta-high", "alpha-low", "beta-medium",
	})
	want := []string{"low", "medium", "high"}
	if len(got) != len(want) {
		t.Fatalf("Efforts() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Efforts()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// 档位猜测决定首次运行时三个槽会不会塞成同一个模型。
func TestGuessTier(t *testing.T) {
	cases := map[string]string{
		"claude-haiku-4-5-20251001": "fast",
		"claude-sonnet-5":           "",
		"claude-opus-5":             "heavy",
		"gemini-3.6-flash-high":     "fast",
		"gemini-3.1-pro-low":        "heavy",
		"gpt-5.4":                   "",
	}
	for id, want := range cases {
		if got := GuessTier(id); got != want {
			t.Errorf("GuessTier(%q) = %q, want %q", id, got, want)
		}
	}
}

// 复合 Key 的前缀只按第一个斜杠拆 —— 模型 ID 自身可能含斜杠。
func TestParseCompositePrefix(t *testing.T) {
	cases := []struct{ id, prefix, base, effort string }{
		{"GPT/gpt-5.6-sol", "GPT", "gpt-5.6-sol", ""},
		{"Gemini/gemini-3.1-pro-high", "Gemini", "gemini-3.1-pro", "high"},
		{"GPT/vendor/model", "GPT", "vendor/model", ""},
		{"claude-opus-5", "", "claude-opus-5", ""},
	}
	for _, c := range cases {
		got := Parse(c.id)
		if got.Prefix != c.prefix || got.Base != c.base || got.Effort != c.effort {
			t.Errorf("Parse(%q) = %+v", c.id, got)
		}
		if round := got.String(); round != c.id {
			t.Errorf("round-trip of %q gave %q", c.id, round)
		}
	}

	if got := Parse("Gemini/gemini-3.1-pro-high").Display(); got != "gemini-3.1-pro-high" {
		t.Errorf("Display() = %q, want the id without the group prefix", got)
	}
}

// 同一模型出现在多个分组时，前缀都必须保留。
func TestPrefixes(t *testing.T) {
	ids := []string{"Max/claude-opus-5", "Kiro/claude-opus-5"}
	if got := Prefixes(ids); len(got) != 2 || got[0] != "Max" || got[1] != "Kiro" {
		t.Errorf("Prefixes() = %v", got)
	}
}
