package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/ui"
)

func testCtx() *Context {
	return &Context{UI: ui.New(true), Flags: newValues()}
}

func fixture(t *testing.T, protos map[string][]string) (*config.Config, *config.Credentials) {
	t.Helper()
	dir := t.TempDir()
	paths := config.Paths{ConfigDir: dir, CacheDir: dir}
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range protos {
		creds.Set(name, &config.Credential{Key: "sk-" + name, Source: config.SourcePaste})
		if p != nil {
			cfg.KeyMetaOf(name).Protocols = map[string][]string{config.GroupScope: p}
		}
	}
	return cfg, creds
}

// 协议不符的 Key 不进候选。
func TestEligibleKeysFiltersByProtocol(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	cfg, creds := fixture(t, map[string][]string{
		"gpt": {"openai_responses", "openai_chat_completions"},
		"max": {"anthropic_messages"},
	})

	got, err := eligibleKeys(testCtx(), cfg, creds, codex)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "gpt" {
		t.Errorf("eligibleKeys = %v, want [gpt]", got)
	}
}

// 多把 Key 都支持某协议时**全部**保留 —— 这正是用户看得见更多模型的前提。
//
// ChatGPT 分组同样开着 anthropic_messages，它的 gpt 模型确实能走 Claude Code，
// 只列绑定那把的模型等于凭空藏起一半选项。
func TestEligibleKeysKeepsEveryQualifyingKey(t *testing.T) {
	claude, _ := harness.Lookup("claude")
	cfg, creds := fixture(t, map[string][]string{
		"gpt": {"anthropic_messages", "openai_responses"},
		"max": {"anthropic_messages"},
	})

	got, err := eligibleKeys(testCtx(), cfg, creds, claude)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("eligibleKeys = %v, want both keys", got)
	}
}

// 一把都不合格时必须报错，并说清每把差在哪。
func TestEligibleKeysFailsLoudly(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	cfg, creds := fixture(t, map[string][]string{
		"max":  {"anthropic_messages"},
		"kiro": {"anthropic_messages"},
	})

	_, err := eligibleKeys(testCtx(), cfg, creds, codex)
	if err == nil {
		t.Fatal("expected an error when no key fits")
	}
	e, ok := err.(*ui.Error)
	if !ok || e.Code != ui.CodeProtocolMismatch {
		t.Fatalf("error = %v, want TKR_PROTOCOL_MISMATCH", err)
	}
	for _, want := range []string{"max", "kiro", "anthropic_messages"} {
		if !strings.Contains(e.Hint, want) {
			t.Errorf("hint %q should mention %q", e.Hint, want)
		}
	}
}

// 未探测过的 Key 不能被筛掉：预检只能证伪。
func TestEligibleKeysKeepsUnprobed(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	cfg, creds := fixture(t, map[string][]string{"fresh": nil})

	got, err := eligibleKeys(testCtx(), cfg, creds, codex)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "fresh" {
		t.Errorf("eligibleKeys = %v, want [fresh]", got)
	}
}

// 候选是「模型 + 提供它的 Key」的组合，多把 Key 的模型要合并成一份列表。
func TestCandidateItemsLabelKeysOnlyWhenAmbiguous(t *testing.T) {
	single := []candidate{
		{Key: "max", Model: "claude-opus-5"},
		{Key: "max", Model: "claude-haiku-4-5"},
	}
	for _, it := range candidateItems(single) {
		if strings.Contains(it.Detail, "max") {
			t.Errorf("single-key list should not repeat the key name: %+v", it)
		}
	}

	mixed := []candidate{
		{Key: "max", Model: "claude-opus-5"},
		{Key: "gpt", Model: "gpt-5.6-sol"},
	}
	items := candidateItems(mixed)
	if !strings.Contains(items[0].Detail, "max") || !strings.Contains(items[1].Detail, "gpt") {
		t.Errorf("multi-key list must show which key provides each model: %+v", items)
	}
}

// 复合 Key 内部仍按分组前缀过滤：GPT/* 能跑 codex，Claude/* 不能。
func TestCandidateItemsKeepPrefix(t *testing.T) {
	items := candidateItems([]candidate{{Key: "multi", Model: "GPT/gpt-5.6-sol"}})
	if items[0].Label != "gpt-5.6-sol" {
		t.Errorf("label = %q, want the id without the group prefix", items[0].Label)
	}
	if !strings.Contains(items[0].Detail, "GPT") {
		t.Errorf("detail = %q, want the group prefix", items[0].Detail)
	}
}

// 选中的模型决定用哪把 Key。
func TestOwnerOf(t *testing.T) {
	cands := []candidate{
		{Key: "max", Model: "claude-opus-5"},
		{Key: "gpt", Model: "gpt-5.6-sol"},
	}
	if k, ok := ownerOf(cands, "gpt-5.6-sol"); !ok || k != "gpt" {
		t.Errorf("ownerOf = %q,%v want gpt", k, ok)
	}
	if _, ok := ownerOf(cands, "nope"); ok {
		t.Error("unknown model must not resolve to a key")
	}
}

// 其余槽只能取自同一把 Key —— 一次启动只注入一把 Key。
func TestModelsOfIsScopedToOneKey(t *testing.T) {
	cands := []candidate{
		{Key: "max", Model: "claude-opus-5"},
		{Key: "max", Model: "claude-haiku-4-5-20251001"},
		{Key: "gpt", Model: "gpt-5.6-sol"},
	}
	got := modelsOf(cands, "max")
	if len(got) != 2 {
		t.Fatalf("modelsOf = %v, want only max's models", got)
	}
}

// 必填槽没填就不能走快路径，否则 harness 会回落到它的内置模型。
func TestSlotsComplete(t *testing.T) {
	oc, _ := harness.Lookup("opencode")
	if slotsComplete(oc, config.ModelSlots{"default": "x"}) {
		t.Error("opencode also requires the small slot")
	}
	if !slotsComplete(oc, config.ModelSlots{"default": "x", "small": "y"}) {
		t.Error("both required slots are set")
	}
}

// 空槽按档位归位：有 haiku 就别让后台任务烧 opus 的钱。
func TestFillPicksCheaperModelForFastSlot(t *testing.T) {
	claude, _ := harness.Lookup("claude")
	slots := config.ModelSlots{"default": "claude-opus-5"}
	fill(claude, slots, []string{"claude-opus-5", "claude-haiku-4-5-20251001", "claude-sonnet-5"})

	if slots["fast"] != "claude-haiku-4-5-20251001" {
		t.Errorf("fast slot = %q, want the haiku", slots["fast"])
	}
	if slots["default"] != "claude-opus-5" {
		t.Errorf("default slot was disturbed: %q", slots["default"])
	}
	// 没有便宜档时回落到主模型，而不是留空。
	slots2 := config.ModelSlots{"default": "gpt-5.6-sol"}
	fill(claude, slots2, []string{"gpt-5.6-sol", "gpt-5.5"})
	if slots2["fast"] == "" {
		t.Error("an empty slot would make the harness fall back to its built-in model")
	}
}

// claude_code_only 分组拦的是客户端指纹而不是协议：
// 只有 Claude Code 本身过得去，codex 拿同一把 Key 也不行。
func TestCanRunRespectsClaudeCodeLock(t *testing.T) {
	locked := &config.KeyMeta{ClaudeCodeOnly: map[string]bool{config.GroupScope: true}}

	claude, _ := harness.Lookup("claude")
	codex, _ := harness.Lookup("codex")
	oc, _ := harness.Lookup("opencode")

	if !canRun(locked, config.GroupScope, claude) {
		t.Error("Claude Code itself passes the fingerprint check")
	}
	if canRun(locked, config.GroupScope, codex) {
		t.Error("codex must not be offered a claude-code-only group")
	}
	if canRun(locked, config.GroupScope, oc) {
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

	if got := fitting(cfg, names, codex); len(got) != 1 || got[0] != "gpt" {
		t.Errorf("codex candidates = %v, want [gpt] only", got)
	}
	if got := fitting(cfg, names, claude); len(got) != 2 {
		t.Errorf("claude candidates = %v, want both keys", got)
	}
}

// flag 管这一次，tkr model 管以后：-m 绝不写盘。
func TestOneShotModelDoesNotPersist(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{ConfigDir: dir, CacheDir: dir}
	cfg, _ := config.Load(paths)
	cfg.Harness("codex").Slots = config.ModelSlots{"default": "gpt-5.4"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// 模拟一次 -m 覆盖后重新读盘。
	reloaded, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Harness("codex").Slots["default"]; got != "gpt-5.4" {
		t.Errorf("stored model = %q, want the original gpt-5.4", got)
	}
}

// 一次启动只注入一把 Key，所以所有槽必须出自同一把。
// 混着放的话，那些模型这把 Key 根本调不到，而失败要等到启动之后才看得见。
func TestSlotsMustComeFromOneKey(t *testing.T) {
	cands := []candidate{
		{Key: "max", Model: "claude-haiku-4-5-20251001"},
		{Key: "gpt", Model: "gpt-5.6-terra"},
	}
	// fill 只从选定 Key 的模型里取，绝不会跨 Key。
	claude, _ := harness.Lookup("claude")
	slots := config.ModelSlots{"default": "gpt-5.6-terra"}
	fill(claude, slots, modelsOf(cands, "gpt"))

	for name, v := range slots {
		if _, ok := ownerOf(cands, v); !ok {
			continue
		}
		if k, _ := ownerOf(cands, v); k != "gpt" {
			t.Errorf("slot %s = %q comes from key %q, want gpt", name, v, k)
		}
	}
}

// 静默过滤是最难排查的行为：用户看到「我的模型不见了」，却没有线索。
// 能藏就必须能解释。
func TestHiddenKeysAreExplained(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(config.Paths{ConfigDir: dir, CacheDir: dir})
	cfg.KeyMetaOf("max").ClaudeCodeOnly = map[string]bool{config.GroupScope: true}
	cfg.KeyMetaOf("max").Models = []string{"claude-opus-5", "claude-haiku-4-5"}
	cfg.KeyMetaOf("gpt").Protocols = map[string][]string{
		config.GroupScope: {"openai_responses"},
	}
	cfg.KeyMetaOf("gpt").Models = []string{"gpt-5.6-sol"}

	oc, _ := harness.Lookup("opencode")
	// 不能用 JSON 模式的 UI：Logf 在那种模式下是静默的。
	var buf bytes.Buffer
	c := &Context{UI: ui.New(false), Flags: newValues()}
	c.UI.Err = &buf

	noteHiddenKeys(c, cfg, []string{"gpt", "max"}, []string{"gpt"}, oc)

	out := buf.String()
	if !strings.Contains(out, "max") {
		t.Errorf("must name the hidden key: %q", out)
	}
	if !strings.Contains(out, "Claude Code") {
		t.Errorf("must give the reason: %q", out)
	}
	if !strings.Contains(out, "2") {
		t.Errorf("must say how many models were hidden: %q", out)
	}

	// 用上的 Key 不该被提及 —— 那只是噪音。
	if strings.Contains(out, `"gpt"`) {
		t.Errorf("must not mention keys that were used: %q", out)
	}
}

// 全部协议都判定为拒绝时不能记进配置。
//
// claude_code_only 靠拒绝文案识别，网关改词就会退化成「全部拒绝」；
// 若照单全收，一把好端端的 Key 会从所有 harness 里消失且毫无线索。
// 没有证据就不过滤 —— 让网关在启动时明确报错，比静默抹掉可恢复得多。
func TestAllDeniedIsTreatedAsUnknown(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(config.Paths{ConfigDir: dir, CacheDir: dir})
	meta := cfg.KeyMetaOf("k")
	meta.Models = []string{"claude-opus-5"}

	// 模拟探测「什么都不准入」：不该留下任何过滤依据。
	meta.Protocols = map[string][]string{}
	meta.ClaudeCodeOnly = map[string]bool{}

	if meta.Probed() {
		t.Error("an empty probe result must not count as probed")
	}
	claude, _ := harness.Lookup("claude")
	if !canRun(meta, config.GroupScope, claude) {
		t.Error("without evidence tkr must not filter the key out")
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

	got, ok := pickProtocolFor(meta, config.GroupScope, "claude-opus-4-6", oc)
	if !ok || got != harness.ProtoAnthropicMessages {
		t.Errorf("claude model → %v, want anthropic_messages", got)
	}
	// 非 Claude 模型仍按 harness 的偏好顺序。
	got, ok = pickProtocolFor(meta, config.GroupScope, "gpt-5.6-sol", oc)
	if !ok || got != harness.ProtoOpenAIResponses {
		t.Errorf("gpt model → %v, want openai_responses", got)
	}
	// codex 不会 anthropic，Claude 模型也只能走 responses（网关翻译）。
	cx, _ := harness.Lookup("codex")
	got, ok = pickProtocolFor(meta, config.GroupScope, "claude-opus-4-6", cx)
	if !ok || got != harness.ProtoOpenAIResponses {
		t.Errorf("codex → %v, want openai_responses", got)
	}
	// 分组不准入原生协议时回落，不能因为偏好而挑一个跑不通的。
	only := &config.KeyMeta{Protocols: map[string][]string{
		config.GroupScope: {"openai_responses"},
	}}
	got, ok = pickProtocolFor(only, config.GroupScope, "claude-opus-4-6", oc)
	if !ok || got != harness.ProtoOpenAIResponses {
		t.Errorf("fallback → %v, want openai_responses", got)
	}
}
