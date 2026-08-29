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

// 折叠后，带变体的族要按强弱排序，无变体的族保持独立。
func TestGroup(t *testing.T) {
	fams := Group([]string{
		"gemini-3.1-pro-low", "gemini-3.1-pro-high",
		"gemini-3.6-flash-high", "gemini-3.6-flash-low", "gemini-3.6-flash-medium",
		"claude-opus-5",
	})
	if len(fams) != 3 {
		t.Fatalf("got %d families, want 3", len(fams))
	}

	if !fams[0].HasVariants() {
		t.Error("gemini-3.1-pro should have variants")
	}
	if got := fams[0].Efforts; got[0] != "low" || got[1] != "high" {
		t.Errorf("efforts not ordered weak→strong: %v", got)
	}
	if got := fams[1].Efforts; len(got) != 3 || got[0] != "low" || got[2] != "high" {
		t.Errorf("flash efforts = %v", got)
	}
	if fams[2].HasVariants() {
		t.Error("claude-opus-5 should have no variants")
	}

	if id, ok := fams[1].ID("high"); !ok || id != "gemini-3.6-flash-high" {
		t.Errorf("ID(high) = %q", id)
	}
	// 没有裸 ID 时应落到中间档，而不是最弱或最贵的一端。
	if id, _ := fams[1].ID(""); id != "gemini-3.6-flash-medium" {
		t.Errorf("default ID = %q, want medium", id)
	}
	if id, _ := fams[2].ID(""); id != "claude-opus-5" {
		t.Errorf("plain family default = %q", id)
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

// 同一模型出现在多个分组时不能被合并 —— 倍率可能差好几倍。
func TestSameModelInDifferentGroups(t *testing.T) {
	ids := []string{"Max/claude-opus-5", "Kiro/claude-opus-5"}
	fams := Group(ids)
	if len(fams) != 2 {
		t.Fatalf("got %d families, want 2 (one per group)", len(fams))
	}
	if fams[0].Prefix != "Max" || fams[1].Prefix != "Kiro" {
		t.Errorf("prefixes lost: %+v", fams)
	}
	if id, _ := fams[1].ID(""); id != "Kiro/claude-opus-5" {
		t.Errorf("ID() = %q, want the prefixed form", id)
	}

	if !IsComposite(ids) {
		t.Error("a slash in the id means a composite key")
	}
	if IsComposite([]string{"gpt-5.4"}) {
		t.Error("plain ids must not be treated as composite")
	}
	if got := Prefixes(ids); len(got) != 2 || got[0] != "Max" {
		t.Errorf("Prefixes() = %v", got)
	}
}
