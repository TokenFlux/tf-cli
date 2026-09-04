package config

import (
	"os"
	"path/filepath"
	"testing"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{ConfigDir: dir, CacheDir: filepath.Join(dir, "cache")}
}

// 新装的机器上不该有任何 Key，也不该有隐藏的「当前模式」。
func TestLoadEmpty(t *testing.T) {
	cfg, err := Load(testPaths(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Keys) != 0 {
		t.Errorf("fresh config should have no keys, got %v", cfg.KeyNames())
	}
	if len(cfg.Harnesses) != 0 {
		t.Errorf("fresh config should have no bindings, got %v", cfg.Harnesses)
	}
}

// 配置 0644、目录 0700、凭据 0600 —— 凭据文件是唯一的机密。
func TestFilePermissions(t *testing.T) {
	paths := testPaths(t)
	cfg, _ := Load(paths)
	cfg.KeyMetaOf("default").Host = DefaultHost
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	creds, _, err := LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	creds.Set("default", &Credential{Key: "sk-secret", Source: SourcePaste})
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		path string
		want os.FileMode
	}{
		{paths.ConfigFile(), 0o644},
		{paths.CredentialsFile(), 0o600},
		{paths.ConfigDir, 0o700},
	} {
		st, err := os.Stat(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := st.Mode().Perm(); got != tc.want {
			t.Errorf("%s mode = %o, want %o", tc.path, got, tc.want)
		}
	}
}

// 权限过宽的凭据文件要被自动收紧，并让调用方知道发生过修复。
func TestCredentialsPermissionRepair(t *testing.T) {
	paths := testPaths(t)
	creds, _, _ := LoadCredentials(paths)
	creds.Set("default", &Credential{Key: "sk-secret", Source: SourcePaste})
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.CredentialsFile(), 0o644); err != nil {
		t.Fatal(err)
	}

	_, repaired, err := LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	if !repaired {
		t.Fatal("loose permissions should be reported as repaired")
	}
	st, _ := os.Stat(paths.CredentialsFile())
	if got := st.Mode().Perm(); got != 0o600 {
		t.Errorf("mode after repair = %o, want 600", got)
	}
}

// 环境变量优先于落盘凭据，供容器与 CI 使用。
func TestEnvKeyWins(t *testing.T) {
	paths := testPaths(t)
	creds, _, _ := LoadCredentials(paths)
	creds.Set("default", &Credential{Key: "sk-file", Source: SourcePaste})

	t.Setenv("TF_API_KEY", "sk-env")
	cred, ok := creds.Get("default")
	if !ok || cred.Key != "sk-env" || cred.Source != SourceEnv {
		t.Errorf("env key should win, got %+v", cred)
	}
}

// 模型槽按 harness 分开：改一个不能动到另一个。
func TestSlotsArePerHarness(t *testing.T) {
	paths := testPaths(t)
	cfg, _ := Load(paths)

	cfg.Harness("claude").Slots[SlotDefault] = "claude-sonnet-5"
	cfg.Harness("claude").Slots[SlotFast] = "claude-haiku-4-5"
	cfg.Harness("codex").Slots[SlotDefault] = "gpt-5.6-sol"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Harness("claude").Slots[SlotFast]; got != "claude-haiku-4-5" {
		t.Errorf("claude fast slot = %q", got)
	}
	if got := reloaded.Harness("codex").Slots[SlotDefault]; got != "gpt-5.6-sol" {
		t.Errorf("codex default slot = %q", got)
	}
	if got := reloaded.Harness("codex").Slots[SlotFast]; got != "" {
		t.Errorf("codex should not have inherited claude's fast slot: %q", got)
	}
}

// 绑定属于 harness：不同 harness 可以用不同的 Key。
func TestBindingsArePerHarness(t *testing.T) {
	paths := testPaths(t)
	cfg, _ := Load(paths)

	cfg.Harness("claude").Key = "max"
	cfg.Harness("codex").Key = "gpt"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, _ := Load(paths)
	if reloaded.Harness("claude").Key != "max" || reloaded.Harness("codex").Key != "gpt" {
		t.Errorf("bindings not preserved: %+v", reloaded.Harnesses)
	}
}

// 凭据按名字分开存：删一把不能影响另一把。
func TestCredentialsRemoveIsPerName(t *testing.T) {
	paths := testPaths(t)
	creds, _, _ := LoadCredentials(paths)
	creds.Set("default", &Credential{Key: "sk-aaa", Source: SourcePaste})
	creds.Set("work", &Credential{Key: "sk-bbb", Source: SourcePaste})
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}

	if got := creds.Names(); len(got) != 2 || got[0] != "default" || got[1] != "work" {
		t.Fatalf("Names() = %v, want sorted [default work]", got)
	}

	creds.Remove("work")
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, _, _ := LoadCredentials(paths)
	if _, ok := reloaded.Get("work"); ok {
		t.Error("work credential survived removal")
	}
	if cred, ok := reloaded.Get("default"); !ok || cred.Key != "sk-aaa" {
		t.Errorf("default credential was disturbed: %+v", cred)
	}
}

// Mask 是所有 Key 展示的唯一出口，中间段绝不能露出。
func TestMask(t *testing.T) {
	full := "sk-d616520b0071d4df6ccc3ad743362bd4"
	masked := Mask(full)
	if masked == full {
		t.Fatal("key was not masked")
	}
	if len(masked) > 16 {
		t.Errorf("mask too long: %q", masked)
	}
	if Mask("") != "" {
		t.Error("empty key should mask to empty")
	}
}

// 一把 Key 里不同模型可调的端点可能不同：复合 Key 横跨多个分组，
// 每个分组的协议准入各不相同。
func TestProtocolsArePerGroupPrefix(t *testing.T) {
	m := &KeyMeta{Protocols: map[string][]string{
		"GPT":    {"openai_responses", "openai_chat_completions"},
		"Claude": {"anthropic_messages"},
	}}

	if !m.SupportsIn("GPT", "openai_responses") {
		t.Error("GPT should allow responses")
	}
	if m.SupportsIn("Claude", "openai_responses") {
		t.Error("Claude prefix must not allow responses")
	}
	if !m.SupportsIn("Claude", "anthropic_messages") {
		t.Error("Claude should allow messages")
	}

	// 没探到的前缀不拦。
	if !m.SupportsIn("Unknown", "openai_responses") {
		t.Error("an unprobed prefix must not be filtered out")
	}
}

// claude_code_only 分组：协议问不出来，但不等于什么都不支持。
func TestClaudeCodeOnlyScope(t *testing.T) {
	m := &KeyMeta{ClaudeCodeOnly: map[string]bool{GroupScope: true}}

	if !m.Probed() {
		t.Error("a recorded lock is a probe result, not an absence of one")
	}
	if !m.LockedToClaudeCode(GroupScope) {
		t.Error("lock not reported")
	}
	// Claude Code 走 messages，所以那条协议算通；其余不通。
	if !m.SupportsIn(GroupScope, "anthropic_messages") {
		t.Error("Claude Code speaks anthropic_messages")
	}
	if m.SupportsIn(GroupScope, "openai_responses") {
		t.Error("a claude-code-only group must not admit codex")
	}
	if got := m.ProtocolSummary(); len(got) != 1 || got[0] != "claude-code-only" {
		t.Errorf("summary = %v, want it to name the lock", got)
	}
}

// DefaultHost 必须是变量而不是常量，否则 -X 注入不进去。
//
// 自建 TokenRouter 的地址属于部署方的决定：部署方构建一次，
// 团队里的人照常 tf login。把它做成登录时的提问，等于向每个人
// 转嫁一个他们答不上来的部署问题。
func TestDefaultHostIsBuildTimeInjectable(t *testing.T) {
	orig := DefaultHost
	defer func() { DefaultHost = orig }()

	// 能赋值即证明它是 var；常量在这一行就编译不过。
	DefaultHost = "https://router.acme.com"

	cfg := &Config{Keys: map[string]*KeyMeta{}}
	if got := cfg.HostOf("nobody"); got != "https://router.acme.com" {
		t.Errorf("HostOf fell back to %q, want the injected host", got)
	}
	// 已保存的 host 仍然优先：换二进制不该改掉存量 Key 的归属。
	cfg.Keys["kept"] = &KeyMeta{Host: "https://tokenflux.dev"}
	if got := cfg.HostOf("kept"); got != "https://tokenflux.dev" {
		t.Errorf("stored host = %q, want it to win over the build-time default", got)
	}
}
