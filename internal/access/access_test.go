package access

import (
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/harness"
)

// claude_code_only 分组拦的是客户端指纹而不是协议：
// 只有 Claude Code 本身过得去，codex 拿同一把 Key 也不行。
func TestCanRunRespectsClaudeCodeLock(t *testing.T) {
	locked := &config.KeyMeta{ClaudeCodeOnly: map[string]bool{config.GroupScope: true}}

	claude, _ := harness.Lookup("claude")
	codex, _ := harness.Lookup("codex")
	oc, _ := harness.Lookup("opencode")

	if !CanRun(locked, config.GroupScope, claude) {
		t.Error("Claude Code itself passes the fingerprint check")
	}
	if CanRun(locked, config.GroupScope, codex) {
		t.Error("codex must not be offered a claude-code-only group")
	}
	if CanRun(locked, config.GroupScope, oc) {
		t.Error("opencode must not be offered a claude-code-only group either")
	}
}

// 一把 Key 只要有一个分组能跑该 harness，就该进候选；
// 全都不能跑才排除。
func TestFittingWithClaudeCodeLock(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	claude, _ := harness.Lookup("claude")

	dir := t.TempDir()
	cfg, _ := config.Load(config.Paths{ConfigDir: dir, CacheDir: dir})
	cfg.KeyMetaOf("max").ClaudeCodeOnly = map[string]bool{config.GroupScope: true}
	cfg.KeyMetaOf("gpt").Protocols = map[string][]string{
		config.GroupScope: {"anthropic_messages", "openai_responses"},
	}
	names := []string{"gpt", "max"}

	if got := Fitting(cfg, names, codex); len(got) != 1 || got[0] != "gpt" {
		t.Errorf("codex candidates = %v, want [gpt] only", got)
	}
	if got := Fitting(cfg, names, claude); len(got) != 2 {
		t.Errorf("claude candidates = %v, want both keys", got)
	}
}

// 模型的原生协议优先：网关两种都能翻译，但翻译只会丢信息，
// 而且 opencode 会照实显示 provider —— 用 openai 跑 claude 模型
// 界面上写着「OpenAI」，看起来像配错了。
func TestNativeProtocolWins(t *testing.T) {
	meta := &config.KeyMeta{Protocols: map[string][]string{
		config.GroupScope: {"anthropic_messages", "openai_responses"},
	}}
	oc, _ := harness.Lookup("opencode")

	got, ok := PickFor(meta, config.GroupScope, "claude-opus-4-6", oc)
	if !ok || got != harness.ProtoAnthropicMessages {
		t.Errorf("claude model → %v, want anthropic_messages", got)
	}
	// 非 Claude 模型仍按 harness 的偏好顺序。
	got, ok = PickFor(meta, config.GroupScope, "gpt-5.6-sol", oc)
	if !ok || got != harness.ProtoOpenAIResponses {
		t.Errorf("gpt model → %v, want openai_responses", got)
	}
	// codex 不会 anthropic，Claude 模型也只能走 responses（网关翻译）。
	cx, _ := harness.Lookup("codex")
	got, ok = PickFor(meta, config.GroupScope, "claude-opus-4-6", cx)
	if !ok || got != harness.ProtoOpenAIResponses {
		t.Errorf("codex → %v, want openai_responses", got)
	}
	// 分组不准入原生协议时回落，不能因为偏好而挑一个跑不通的。
	only := &config.KeyMeta{Protocols: map[string][]string{
		config.GroupScope: {"openai_responses"},
	}}
	got, ok = PickFor(only, config.GroupScope, "claude-opus-4-6", oc)
	if !ok || got != harness.ProtoOpenAIResponses {
		t.Errorf("fallback → %v, want openai_responses", got)
	}
}

// 没探测过的 Key 一律按可用处理。
//
// 判据是「不知道」而不是「不行」：探测要花一次往返，不能因为还没探
// 就把用户的 Key 藏起来。真跑不通时网关会拒，那时的错误信息比
// 客户端的猜测准确得多。
func TestUnprobedIsUsable(t *testing.T) {
	meta := &config.KeyMeta{}
	for _, h := range harness.All {
		if got := Fitting(&config.Config{Keys: map[string]*config.KeyMeta{"k": meta}},
			[]string{"k"}, h); len(got) != 1 {
			t.Errorf("%s: 未探测的 Key 应当保留，得到 %v", h.Name, got)
		}
	}
}

// 协议偏好按 harness 声明的顺序走，而不是按分组里碰巧存了什么顺序。
//
// 顺序即偏好：harness 把最合适的协议排在前面，分组允许哪些是另一回事。
func TestPreferenceFollowsHarnessOrder(t *testing.T) {
	h := &harness.Harness{
		Name:      "two",
		Protocols: []harness.Protocol{harness.ProtoOpenAIResponses, harness.ProtoAnthropicMessages},
	}
	// 分组两种都开：应当取 harness 排在前面的那个。
	meta := &config.KeyMeta{Protocols: map[string][]string{
		config.GroupScope: {string(harness.ProtoAnthropicMessages), string(harness.ProtoOpenAIResponses)},
	}}
	if got, ok := Pick(meta, config.GroupScope, h); !ok || got != harness.ProtoOpenAIResponses {
		t.Errorf("应当取 harness 的首选 %s，得到 %s", harness.ProtoOpenAIResponses, got)
	}
}

// 知道具体模型时，模型的原生协议优先于 harness 的偏好。
//
// 翻译只会丢信息不会补信息，能不翻译就不翻译。猜错也无害 ——
// 网关照样翻译，所以这是偏好而不是准入。
func TestNativeProtocolOverridesPreference(t *testing.T) {
	h := &harness.Harness{
		Name:      "two",
		Protocols: []harness.Protocol{harness.ProtoOpenAIResponses, harness.ProtoAnthropicMessages},
	}
	meta := &config.KeyMeta{Protocols: map[string][]string{
		config.GroupScope: {string(harness.ProtoAnthropicMessages), string(harness.ProtoOpenAIResponses)},
	}}
	got, ok := PickFor(meta, config.GroupScope, "claude-sonnet-5", h)
	if !ok || got != harness.ProtoAnthropicMessages {
		t.Errorf("claude 模型应当走 %s，得到 %s", harness.ProtoAnthropicMessages, got)
	}
	// 认不出来的模型名不该改变偏好。
	if got, _ := PickFor(meta, config.GroupScope, "gpt-5.4", h); got != harness.ProtoOpenAIResponses {
		t.Errorf("非 claude 模型应当沿用 harness 首选，得到 %s", got)
	}
}

// 分组前缀是独立的：一把复合 Key 在一个分组被锁，不影响另一个分组。
func TestScopesAreIndependent(t *testing.T) {
	meta := &config.KeyMeta{
		Protocols: map[string][]string{
			"gpt":   {string(harness.ProtoOpenAIResponses)},
			"ccmax": {},
		},
		ClaudeCodeOnly: map[string]bool{"ccmax": true},
	}
	codex, ok := harness.Lookup("codex")
	if !ok {
		t.Skip("codex 未注册")
	}
	if !CanRun(meta, "gpt", codex) {
		t.Error("gpt 分组应当能跑 codex")
	}
	if CanRun(meta, "ccmax", codex) {
		t.Error("ccmax 是 claude_code_only，codex 不该通过")
	}
}
