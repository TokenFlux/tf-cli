package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Input 是构建启动方案所需的全部外部信息。
type Input struct {
	Host  string            // 已归一化的网关地址，不含尾部斜杠与 /v1
	Key   string            // API Key
	Slots map[string]string // 槽名 → 模型 ID
	Args  []string          // 透传给 harness 的原始参数
}

// Plan 是一次启动的完整描述。
//
// 刻意只包含「进程怎么起」，不含任何落盘动作：tkr 不改用户的配置文件。
type Plan struct {
	Bin  string
	Args []string
	Env  []string // 完整环境，已在父进程环境基础上增删
	Note string   // 启动横幅里的补充说明
}

// AnthropicBase 返回 Anthropic 协议的 base：根路径，不带 /v1。
func AnthropicBase(host string) string { return strings.TrimRight(host, "/") }

// OpenAIBase 返回 OpenAI 协议的 base：带 /v1。
//
// 这两个函数的存在本身就是为了消灭一类高频错误 —— Anthropic 用根、
// OpenAI 用 /v1，用户手填时经常填反。
func OpenAIBase(host string) string { return strings.TrimRight(host, "/") + "/v1" }

// BuildPlan 依据适配表生成启动方案。
func (h *Harness) BuildPlan(in Input) (*Plan, error) {
	switch h.Name {
	case "claude":
		return planClaude(in)
	case "codex":
		return planCodex(in)
	case "opencode":
		return planOpencode(in)
	default:
		return nil, fmt.Errorf("no launch recipe for %s", h.Name)
	}
}

// env 在父进程环境上叠加修改。
//
// 值为空字符串表示「显式置空」而非「删除」，用于压制 harness 对
// 其它凭据来源的探测；真正要删除的用 drop。
func buildEnv(set map[string]string, drop []string) []string {
	dropped := map[string]bool{}
	for _, k := range drop {
		dropped[k] = true
	}

	out := make([]string, 0, len(os.Environ())+len(set))
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if dropped[k] {
			continue
		}
		if _, overridden := set[k]; overridden {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range set {
		out = append(out, k+"="+v)
	}
	return out
}

// planClaude 走 Anthropic Messages 协议，base 为根路径。
//
// 注意：绝不设置任何会改变 User-Agent 的变量。claude_code_only 分组
// 依赖 UA + TLS 指纹识别 Claude Code，而 tkr 只注入环境、不在请求路径上，
// 识别因此天然成立。见 docs/design/product-decisions.md 第 0 节。
func planClaude(in Input) (*Plan, error) {
	set := map[string]string{
		"ANTHROPIC_BASE_URL":   AnthropicBase(in.Host),
		"ANTHROPIC_AUTH_TOKEN": in.Key,
		// 置空而非删除：留着空值可压制 claude 去找别的凭据来源。
		"ANTHROPIC_API_KEY": "",
	}
	if m := in.Slots["default"]; m != "" {
		set["ANTHROPIC_MODEL"] = m
		set["ANTHROPIC_DEFAULT_SONNET_MODEL"] = m
	}
	if m := in.Slots["fast"]; m != "" {
		set["ANTHROPIC_DEFAULT_HAIKU_MODEL"] = m
		set["ANTHROPIC_SMALL_FAST_MODEL"] = m
	}
	if m := in.Slots["heavy"]; m != "" {
		set["ANTHROPIC_DEFAULT_OPUS_MODEL"] = m
	}

	// 清掉会把请求引去别处的变量，否则用户会看到「配置没生效」。
	drop := []string{
		"ANTHROPIC_BEDROCK_BASE_URL", "ANTHROPIC_VERTEX_BASE_URL",
		"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
		"CLAUDE_CODE_OAUTH_TOKEN",
	}

	return &Plan{Bin: "claude", Args: in.Args, Env: buildEnv(set, drop)}, nil
}

// planCodex 全部用 -c 覆盖，不落盘。
//
// wire_api 只有 responses 一个合法值（OpenAI 官方 config reference），
// 因此 codex 必须落在开启了 openai_responses 的分组上。
func planCodex(in Input) (*Plan, error) {
	const provider = "tokenflux"
	const keyEnv = "TKR_UPSTREAM_KEY"

	args := []string{
		"-c", "model_provider=" + provider,
		"-c", fmt.Sprintf("model_providers.%s.name=%s", provider, "TokenFlux"),
		"-c", fmt.Sprintf("model_providers.%s.base_url=%s", provider, OpenAIBase(in.Host)),
		"-c", fmt.Sprintf("model_providers.%s.wire_api=responses", provider),
		"-c", fmt.Sprintf("model_providers.%s.env_key=%s", provider, keyEnv),
	}
	if m := in.Slots["default"]; m != "" {
		args = append(args, "-c", "model="+m)
	}
	if m := in.Slots["review"]; m != "" {
		args = append(args, "-c", "review_model="+m)
	}
	args = append(args, in.Args...)

	set := map[string]string{keyEnv: in.Key}
	return &Plan{Bin: "codex", Args: args, Env: buildEnv(set, nil)}, nil
}

// planOpencode 用 OPENCODE_CONFIG_CONTENT 覆盖内置 openai provider。
//
// 必须同时给出 model 与 small_model：缺 small_model 时 opencode 会用
// 内置的 gpt-5.4-nano 跑标题生成，该模型通常不在分组里，且失败是静默的。
// 见 docs/research/harness-probe.md。
func planOpencode(in Input) (*Plan, error) {
	const provider = "openai"

	cfg := map[string]any{
		"provider": map[string]any{
			provider: map[string]any{
				"options": map[string]any{
					"baseURL": OpenAIBase(in.Host),
					"apiKey":  in.Key,
				},
			},
		},
	}
	if m := in.Slots["default"]; m != "" {
		cfg["model"] = provider + "/" + m
	}
	if m := in.Slots["small"]; m != "" {
		cfg["small_model"] = provider + "/" + m
	}

	blob, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}

	set := map[string]string{"OPENCODE_CONFIG_CONTENT": string(blob)}
	return &Plan{Bin: "opencode", Args: in.Args, Env: buildEnv(set, nil)}, nil
}
