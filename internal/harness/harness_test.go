package harness

import (
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// 别名要能命中，未知名字要落空。
func TestLookup(t *testing.T) {
	for _, name := range []string{"claude", "cc", "claude-code", "codex", "cx", "opencode", "pi"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("Lookup(%q) missed", name)
		}
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup(nope) should miss")
	}
}

// 每个 harness 都必须声明主模型槽，否则启动时无从注入。
//
// 槽位漏声明是会静默坑用户的：harness 会回落到内置默认模型，
// 而那个模型通常不在用户的分组里（opencode 的 small_model 已实测坐实）。
func TestEveryHarnessDeclaresRequiredSlots(t *testing.T) {
	for _, h := range All {
		var hasDefault bool
		seen := map[string]bool{}
		for _, s := range h.Slots {
			if seen[s.Name] {
				t.Errorf("%s: duplicate slot %q", h.Name, s.Name)
			}
			seen[s.Name] = true
			if s.Name == "default" {
				hasDefault = true
				if !s.Required {
					t.Errorf("%s: the default slot must be required", h.Name)
				}
			}
			if s.Purpose == nil {
				t.Errorf("%s: slot %q has no purpose text", h.Name, s.Name)
			}
		}
		if !hasDefault {
			t.Errorf("%s: no default slot declared", h.Name)
		}
	}
}

// opencode 的 small 槽必须是必填：不注入会导致标题生成静默失败。
func TestOpencodeSmallSlotIsRequired(t *testing.T) {
	h, ok := Lookup("opencode")
	if !ok {
		t.Fatal("opencode missing")
	}
	for _, s := range h.Slots {
		if s.Name == "small" {
			if !s.Required {
				t.Error("opencode small slot must be required; see research/harness-probe.md")
			}
			return
		}
	}
	t.Error("opencode must declare a small slot")
}

// 安装命令必须自洽，且绝不含提权。
func TestInstallOptionsAreSane(t *testing.T) {
	for _, h := range All {
		if len(h.Installs) == 0 {
			t.Errorf("%s: no install options", h.Name)
		}
		for _, o := range h.Installs {
			if len(o.Args) == 0 {
				t.Errorf("%s: empty install argv", h.Name)
				continue
			}
			for _, a := range o.Args {
				if a == "sudo" {
					t.Errorf("%s: install command must never use sudo: %s", h.Name, o.Command())
				}
			}
		}
	}
}

// 探测不到的二进制不能报告为已安装。
func TestDetectMissingBinary(t *testing.T) {
	h := &Harness{Name: "ghost", Bin: "tf-definitely-not-a-real-binary"}
	if st := h.Detect(); st.Installed {
		t.Errorf("phantom binary reported as installed: %+v", st)
	}
}

// 只有 Claude Code 需要把强度编码进模型 ID；其他 harness 有自己的旋钮。
func TestEffortUsesModelVariantsOnlyForClaude(t *testing.T) {
	want := map[string]bool{"claude": true, "codex": false, "opencode": false, "pi": false}
	for name, wantModelID := range want {
		h, ok := Lookup(name)
		if !ok {
			t.Fatalf("%s missing", name)
		}
		if h.EffortViaModelID != wantModelID {
			t.Errorf("%s EffortViaModelID = %v, want %v", name, h.EffortViaModelID, wantModelID)
		}
	}
}

// 请求强度时 claude 必须报错而非静默忽略 —— 静默会让用户以为调生效了。
func TestClaudeRejectsEffort(t *testing.T) {
	h, _ := Lookup("claude")
	_, err := h.BuildPlan(Input{
		Host: "https://example.com", Key: "k",
		Slots: map[string]string{"default": "claude-opus-5"}, Effort: "high",
	})
	if err == nil {
		t.Error("claude should reject an effort request instead of ignoring it")
	}
}

// codex 与 opencode 的强度必须真的出现在启动参数里。
func TestEffortReachesArgs(t *testing.T) {
	codex, _ := Lookup("codex")
	plan, err := codex.BuildPlan(Input{
		Host: "https://example.com", Key: "k",
		Slots: map[string]string{"default": "gpt-5.6-sol"}, Effort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Args, " "), "model_reasoning_effort=high") {
		t.Errorf("codex args missing effort: %v", plan.Args)
	}

	oc, _ := Lookup("opencode")
	plan, err = oc.BuildPlan(Input{
		Host: "https://example.com", Key: "k",
		Slots: map[string]string{"default": "gpt-5.6-sol", "small": "gpt-5.4"}, Effort: "high",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Args, " "), "--variant high") {
		t.Errorf("opencode args missing variant: %v", plan.Args)
	}

	pi, _ := Lookup("pi")
	plan, err = pi.BuildPlan(Input{
		Host: "https://example.com", Key: "k",
		Slots: map[string]string{"default": "gpt-5.6-sol"}, Effort: "high",
		Protocol: ProtoOpenAIResponses,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plan.Args, " "), "--thinking high") {
		t.Errorf("pi args missing thinking level: %v", plan.Args)
	}
}

// 注入必须走 Anthropic 根路径 / OpenAI 的 /v1，这是最高频的手填错误。
func TestBaseURLShapes(t *testing.T) {
	if got := AnthropicBase("https://tokenflux.dev"); got != "https://tokenflux.dev" {
		t.Errorf("anthropic base = %q", got)
	}
	if got := OpenAIBase("https://tokenflux.dev"); got != "https://tokenflux.dev/v1" {
		t.Errorf("openai base = %q", got)
	}
}

// 模型名不在 Claude Code 的内置表里时，它会假定 200k 窗口并打一段长告警。
// tf 不猜真实窗口，而是让它以 API 返回为准。
func TestClaudeDefersContextWindowToAPI(t *testing.T) {
	h, _ := Lookup("claude")
	plan, err := h.BuildPlan(Input{
		Host: "https://tokenflux.dev", Key: "sk-x",
		Slots: map[string]string{"default": "gpt-5.6-terra"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := envOf(plan.Env, "CLAUDE_CODE_DISABLE_UNKNOWN_MODEL_WINDOW_ENFORCEMENT"); got != "1" {
		t.Errorf("enforcement flag = %q, want 1", got)
	}
	// 绝不替用户猜窗口大小：猜错的两个方向都有害。
	if got := envOf(plan.Env, "CLAUDE_CODE_MAX_CONTEXT_TOKENS"); got != "" {
		t.Errorf("tf must not invent a context window, got %q", got)
	}
}

// 用户自己设过就不覆盖 —— 那是明确的选择。
func TestClaudeRespectsExplicitContextWindow(t *testing.T) {
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "1000000")

	h, _ := Lookup("claude")
	plan, err := h.BuildPlan(Input{
		Host: "https://tokenflux.dev", Key: "sk-x",
		Slots: map[string]string{"default": "gpt-5.6-terra"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := envOf(plan.Env, "CLAUDE_CODE_MAX_CONTEXT_TOKENS"); got != "1000000" {
		t.Errorf("user's value = %q, want it kept", got)
	}
	if got := envOf(plan.Env, "CLAUDE_CODE_DISABLE_UNKNOWN_MODEL_WINDOW_ENFORCEMENT"); got != "" {
		t.Errorf("must not fight the user's explicit setting, got %q", got)
	}
}

// envOf 取注入环境里某个变量的值。
func envOf(env []string, key string) string {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v
		}
	}
	return ""
}

// opencode 两个 provider 都内置，因此它在只开 anthropic_messages 的
// 分组上照样能跑 —— 把 harness 钉死成单协议会凭空砍掉一半可用分组。
func TestOpencodeSpeaksBothProtocols(t *testing.T) {
	h, _ := Lookup("opencode")
	if !h.Speaks("openai_responses") || !h.Speaks("anthropic_messages") {
		t.Fatalf("opencode should speak both, got %v", h.Protocols)
	}
	// 顺序即偏好：responses 功能更全，排在前面。
	if h.Protocols[0] != ProtoOpenAIResponses {
		t.Errorf("preferred protocol = %v, want responses first", h.Protocols[0])
	}

	// codex 只认 responses；Claude Code 只会 Anthropic。
	cx, _ := Lookup("codex")
	if cx.Speaks("anthropic_messages") {
		t.Error("codex has no anthropic wire_api")
	}
	cl, _ := Lookup("claude")
	if cl.Speaks("openai_responses") {
		t.Error("Claude Code speaks only anthropic_messages")
	}

	pi, _ := Lookup("pi")
	for _, proto := range []string{"anthropic_messages", "openai_responses", "openai_chat_completions"} {
		if !pi.Speaks(proto) {
			t.Errorf("pi should speak %s, got %v", proto, pi.Protocols)
		}
	}
}

// 走 anthropic 协议时必须覆盖 anthropic provider，且用不带 /v1 的根地址。
func TestOpencodeAnthropicRecipe(t *testing.T) {
	h, _ := Lookup("opencode")
	plan, err := h.BuildPlan(Input{
		Host: "https://tokenflux.dev", Key: "sk-x",
		Slots:    map[string]string{"default": "claude-opus-5", "small": "claude-haiku-4-5"},
		Protocol: ProtoAnthropicMessages,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := envOf(plan.Env, "OPENCODE_CONFIG_CONTENT")
	for _, want := range []string{
		`"anthropic"`,
		// 与 Claude Code 相反：@ai-sdk/anthropic 只补 /messages，
		// 所以 baseURL 必须带 /v1。写成根地址会 404 且被静默吞掉。
		`"baseURL":"https://tokenflux.dev/v1"`,
		`"anthropic/claude-opus-5"`,
		`"anthropic/claude-haiku-4-5"`, // 缺 small_model 会静默回落到内置模型
		// 网关的模型名不在 opencode 的内置目录里，必须显式声明。
		`"claude-opus-5":{"name":"claude-opus-5"}`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config %s\n missing %s", cfg, want)
		}
	}
}

// 默认仍走 openai：/v1 与 openai provider。
func TestOpencodeOpenAIRecipe(t *testing.T) {
	h, _ := Lookup("opencode")
	plan, err := h.BuildPlan(Input{
		Host: "https://tokenflux.dev", Key: "sk-x",
		Slots:    map[string]string{"default": "gpt-5.6-sol", "small": "gpt-5.4"},
		Protocol: ProtoOpenAIResponses,
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := envOf(plan.Env, "OPENCODE_CONFIG_CONTENT")
	if !strings.Contains(cfg, `"baseURL":"https://tokenflux.dev/v1"`) {
		t.Errorf("openai base needs /v1: %s", cfg)
	}
	if !strings.Contains(cfg, `"openai/gpt-5.6-sol"`) {
		t.Errorf("model id needs the provider prefix: %s", cfg)
	}
	// gpt-5.6-terra 之类同样不是真实的 OpenAI 模型名，一样要声明。
	if !strings.Contains(cfg, `"gpt-5.6-sol":{"name":"gpt-5.6-sol"}`) {
		t.Errorf("gateway model ids must be declared: %s", cfg)
	}
}

func TestPiRecipesUseProcessPrivateProvider(t *testing.T) {
	pi, _ := Lookup("pi")
	for _, tc := range []struct {
		name     string
		protocol Protocol
		api      string
		baseURL  string
	}{
		{name: "anthropic", protocol: ProtoAnthropicMessages, api: "anthropic-messages", baseURL: "https://tokenflux.dev"},
		{name: "responses", protocol: ProtoOpenAIResponses, api: "openai-responses", baseURL: "https://tokenflux.dev/v1"},
		{name: "chat", protocol: ProtoOpenAIChat, api: "openai-completions", baseURL: "https://tokenflux.dev/v1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const secret = "sk-must-not-reach-argv-or-file"
			plan, err := pi.BuildPlan(Input{
				Host: "https://tokenflux.dev/", Key: secret,
				Slots:    map[string]string{"default": "model/id"},
				Protocol: tc.protocol, Effort: "high",
				Args: []string{"--print", "hello"},
			})
			if err != nil {
				t.Fatal(err)
			}

			var cfg piProviderConfig
			if err := json.Unmarshal([]byte(envOf(plan.Env, "TF_PI_PROVIDER_CONFIG")), &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.API != tc.api || cfg.BaseURL != tc.baseURL || cfg.Model != "model/id" || !cfg.Reasoning {
				t.Errorf("provider config = %+v", cfg)
			}
			if !strings.HasPrefix(cfg.Provider, "tf-") || len(cfg.Provider) == len("tf-") {
				t.Errorf("ephemeral provider id = %q", cfg.Provider)
			}
			wantArgs := []string{
				"--extension", "", "--model", cfg.Provider + "/model/id",
				"--thinking", "high", "--print", "hello",
			}
			if !slices.Equal(plan.Args, wantArgs) {
				t.Errorf("args = %q, want %q", plan.Args, wantArgs)
			}
			if strings.Contains(strings.Join(plan.Args, " "), secret) ||
				strings.Contains(envOf(plan.Env, "TF_PI_PROVIDER_CONFIG"), secret) {
				t.Fatal("pi key leaked outside its dedicated environment variable")
			}
			if got := envOf(plan.Env, "TF_UPSTREAM_KEY"); got != secret {
				t.Errorf("injected key = %q", got)
			}
		})
	}
}

func TestPiExtensionIsTemporaryAndContainsNoKey(t *testing.T) {
	pi, _ := Lookup("pi")
	const secret = "sk-file-secret"
	plan, err := pi.BuildPlan(Input{
		Host: "https://tokenflux.dev", Key: secret,
		Slots: map[string]string{"default": "gpt-5.4"}, Protocol: ProtoOpenAIResponses,
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg piProviderConfig
	if err := json.Unmarshal([]byte(envOf(plan.Env, "TF_PI_PROVIDER_CONFIG")), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Reasoning {
		t.Error("pi must not add a separate thinking control without an explicit effort")
	}
	args, cleanup, err := plan.Prepare()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	path := args[1]
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), secret) {
		t.Fatal("temporary extension contains the API key")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("temporary extension mode = %o, want 600", got)
		}
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("temporary extension still exists after cleanup: %v", err)
	}
}

func TestPiRejectsIncompleteRecipe(t *testing.T) {
	pi, _ := Lookup("pi")
	if _, err := pi.BuildPlan(Input{Protocol: ProtoOpenAIResponses}); err == nil {
		t.Error("pi should require a default model")
	}
	if _, err := pi.BuildPlan(Input{
		Slots: map[string]string{"default": "m"}, Protocol: Protocol("unknown"),
	}); err == nil {
		t.Error("pi should reject an unknown protocol")
	}
}

// 只有 Claude Code 有经过实测的配置覆盖路径，因此也只有它需要维护冲突键名单。
func TestClaudePlanReportsManagedEnv(t *testing.T) {
	h, _ := Lookup("claude")
	p, err := h.BuildPlan(Input{
		Host: "https://x", Key: "k", Protocol: ProtoAnthropicMessages,
		Slots: map[string]string{"default": "m"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Managed) == 0 {
		t.Fatal("claude reported no managed environment variables")
	}
	for k := range p.Managed {
		if envOf(p.Env, k) == "" && k != "ANTHROPIC_API_KEY" {
			t.Errorf("claude claims to manage %s but it is not in Env", k)
		}
	}
}
