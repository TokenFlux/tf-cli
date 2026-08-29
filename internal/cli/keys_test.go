package cli

import (
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

// 能力筛选后只剩一把时直接用，不打断用户。
func TestResolveKeyPicksTheOnlyFit(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	cfg, creds := fixture(t, map[string][]string{
		"gpt": {"openai_responses", "openai_chat_completions"},
		"max": {"anthropic_messages"},
	})

	got, err := resolveKey(testCtx(), cfg, creds, codex)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gpt" {
		t.Errorf("resolveKey = %q, want gpt", got)
	}
	// 选择结果要记进该 harness 的绑定，下次不再筛。
	if bound := cfg.Harness("codex").Key; bound != "gpt" {
		t.Errorf("binding = %q, want gpt", bound)
	}
}

// claude 需要 anthropic_messages：只有 max 合格。
func TestResolveKeyRespectsProtocol(t *testing.T) {
	claude, _ := harness.Lookup("claude")
	cfg, creds := fixture(t, map[string][]string{
		"gpt": {"openai_responses"},
		"max": {"anthropic_messages"},
	})

	got, err := resolveKey(testCtx(), cfg, creds, claude)
	if err != nil {
		t.Fatal(err)
	}
	if got != "max" {
		t.Errorf("resolveKey = %q, want max", got)
	}
}

// 一把都不合格时必须报错，并说清每把差在哪 —— 不能静默用一把跑不通的。
func TestResolveKeyFailsLoudly(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	cfg, creds := fixture(t, map[string][]string{
		"max":  {"anthropic_messages"},
		"kiro": {"anthropic_messages"},
	})

	_, err := resolveKey(testCtx(), cfg, creds, codex)
	if err == nil {
		t.Fatal("expected an error when no key fits")
	}
	e, ok := err.(*ui.Error)
	if !ok || e.Code != ui.CodeProtocolMismatch {
		t.Fatalf("error = %v, want TKR_PROTOCOL_MISMATCH", err)
	}
	// 提示里要逐把列出实际协议，否则用户无从下手。
	for _, want := range []string{"max", "kiro", "anthropic_messages"} {
		if !strings.Contains(e.Hint, want) {
			t.Errorf("hint %q should mention %q", e.Hint, want)
		}
	}
}

// 已有绑定直接生效，不再走筛选，也不提问。
func TestResolveKeyUsesExistingBinding(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	cfg, creds := fixture(t, map[string][]string{
		"a": {"openai_responses"},
		"b": {"openai_responses"},
	})
	cfg.Harness("codex").Key = "b"

	got, err := resolveKey(testCtx(), cfg, creds, codex)
	if err != nil {
		t.Fatal(err)
	}
	if got != "b" {
		t.Errorf("resolveKey = %q, want the bound key b", got)
	}
}

// 绑定指向的 Key 被删掉后要能自愈，而不是报错卡死。
func TestResolveKeyHealsStaleBinding(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	cfg, creds := fixture(t, map[string][]string{"gpt": {"openai_responses"}})
	cfg.Harness("codex").Key = "deleted"

	got, err := resolveKey(testCtx(), cfg, creds, codex)
	if err != nil {
		t.Fatal(err)
	}
	if got != "gpt" {
		t.Errorf("resolveKey = %q, want it to fall back to gpt", got)
	}
}

// 未探测过的 Key 不能被筛掉：预检只能证伪。
func TestResolveKeyKeepsUnprobed(t *testing.T) {
	codex, _ := harness.Lookup("codex")
	cfg, creds := fixture(t, map[string][]string{"fresh": nil})

	got, err := resolveKey(testCtx(), cfg, creds, codex)
	if err != nil {
		t.Fatal(err)
	}
	if got != "fresh" {
		t.Errorf("resolveKey = %q, want fresh", got)
	}
}

// 同一把 Key 里不同模型可调的端点不同：复合 Key 横跨多个分组，
// 选择器必须只列出该 harness 真能调的那些。
func TestFilterByProtocolWithinOneKey(t *testing.T) {
	dir := t.TempDir()
	cfg, err := config.Load(config.Paths{ConfigDir: dir, CacheDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	cfg.KeyMetaOf("multi").Protocols = map[string][]string{
		"GPT":    {"openai_responses", "openai_chat_completions"},
		"Claude": {"anthropic_messages"},
	}
	ids := []string{"GPT/gpt-5.6-sol", "Claude/claude-opus-5", "GPT/gpt-5.4"}

	codex, _ := harness.Lookup("codex")
	got := filterByProtocol(cfg, "multi", codex, ids)
	if len(got) != 2 || got[0] != "GPT/gpt-5.6-sol" || got[1] != "GPT/gpt-5.4" {
		t.Errorf("codex candidates = %v, want only the GPT ones", got)
	}

	claude, _ := harness.Lookup("claude")
	got = filterByProtocol(cfg, "multi", claude, ids)
	if len(got) != 1 || got[0] != "Claude/claude-opus-5" {
		t.Errorf("claude candidates = %v, want only the Claude one", got)
	}
}

// 未探测过的 Key 不过滤任何模型：预检只能证伪。
func TestFilterByProtocolSkipsUnprobed(t *testing.T) {
	dir := t.TempDir()
	cfg, _ := config.Load(config.Paths{ConfigDir: dir, CacheDir: dir})
	cfg.KeyMetaOf("fresh")

	codex, _ := harness.Lookup("codex")
	ids := []string{"a", "b"}
	if got := filterByProtocol(cfg, "fresh", codex, ids); len(got) != 2 {
		t.Errorf("unprobed key should not filter: %v", got)
	}
}
