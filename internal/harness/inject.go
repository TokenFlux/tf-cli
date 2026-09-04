package harness

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Input 是构建启动方案所需的全部外部信息。
type Input struct {
	Host   string            // 已归一化的网关地址，不含尾部斜杠与 /v1
	Key    string            // API Key
	Slots  map[string]string // 槽名 → 模型 ID
	Effort string            // 思考强度；已能用模型 ID 表达时调用方会置空
	Args   []string          // 透传给 harness 的原始参数
	// Protocol 是本次选定的客户端协议。
	//
	// 同一个 harness 在不同分组上可能走不同协议（opencode 两种都会），
	// 注入配方必须跟着变，否则会被网关直接拒掉。
	Protocol Protocol
}

// Plan 是一次启动的完整描述。
//
// 不含任何用户配置写入。Pi 需要的临时 Extension 只在 Prepare 时创建，
// 子进程退出后删除。
type Plan struct {
	Bin  string
	Args []string
	Env  []string // 完整环境，已在父进程环境基础上增删

	// Managed 仅供已实测的 Claude settings.json 冲突检查使用。
	Managed map[string]string
}

// Prepare 把 Pi 的进程私有 Extension 落地，并返回实际 argv。
// 调用方必须执行 cleanup；文件只包含配方，凭据仍只在进程环境里。
func (p *Plan) Prepare() ([]string, func(), error) {
	if p.Bin != "pi" {
		return p.Args, func() {}, nil
	}

	args := append([]string(nil), p.Args...)
	if len(args) < 2 || args[0] != "--extension" {
		return nil, nil, fmt.Errorf("pi extension argument is missing")
	}
	f, err := os.CreateTemp("", "tf-pi-*.js")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.Remove(f.Name()) }
	if _, err := f.WriteString(piProviderExtension); err != nil {
		_ = f.Close()
		cleanup()
		return nil, nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return nil, nil, err
	}
	args[1] = f.Name()
	return args, cleanup, nil
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
		if in.Effort != "" {
			// Claude Code 没有外部的强度旋钮；能调的只有模型 ID 自带变体。
			// 静默忽略比报错更坏：用户会以为调生效了。
			return nil, fmt.Errorf("claude has no reasoning-effort switch; pick a model variant instead")
		}
		return planClaude(in)
	case "codex":
		return planCodex(in)
	case "opencode":
		return planOpencode(in)
	case "pi":
		return planPi(in)
	default:
		return nil, fmt.Errorf("no launch recipe for %s", h.Name)
	}
}

// buildEnv 在父进程环境上叠加修改。
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
// 依赖 UA + TLS 指纹识别 Claude Code，而 tf 只注入环境、不在请求路径上，
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

	// 模型名不在 Claude Code 的内置表里时，它会假定 200k 上下文窗口，
	// 并在启动时打一段很长的告警（而且是在自己进入 raw 模式之后打的，
	// 换行不回车，输出会错位）。
	//
	// 不去猜真实窗口大小：猜小了提前压缩上下文，猜大了直接溢出，
	// 而 tf 并不掌握这个数据。改为关掉这道强制检查，让 Claude Code
	// 回到「以 API 返回为准」—— 网关才是知道窗口大小的那一方。
	//
	// 用户已经自己设过任一相关变量时不覆盖：那是明确的选择。
	if os.Getenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS") == "" &&
		os.Getenv("CLAUDE_CODE_DISABLE_UNKNOWN_MODEL_WINDOW_ENFORCEMENT") == "" {
		set["CLAUDE_CODE_DISABLE_UNKNOWN_MODEL_WINDOW_ENFORCEMENT"] = "1"
	}

	// 清掉会把请求引去别处的变量，否则用户会看到「配置没生效」。
	drop := []string{
		"ANTHROPIC_BEDROCK_BASE_URL", "ANTHROPIC_VERTEX_BASE_URL",
		"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX",
		"CLAUDE_CODE_OAUTH_TOKEN",
	}

	return &Plan{Bin: "claude", Args: in.Args, Env: buildEnv(set, drop), Managed: set}, nil
}

// planCodex 全部用 -c 覆盖，不落盘。
//
// wire_api 只有 responses 一个合法值（OpenAI 官方 config reference），
// 因此 codex 必须落在开启了 openai_responses 的分组上。
func planCodex(in Input) (*Plan, error) {
	const provider = "tokenflux"
	const keyEnv = "TF_UPSTREAM_KEY"

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
	if in.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+in.Effort)
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
	// 覆盖哪个内置 provider，取决于这次走哪种协议。
	// 分组只开 anthropic_messages 时，openai provider 会被网关直接拒掉。
	//
	// 两个 provider 的 baseURL 都要带 /v1 —— 注意这与 Claude Code 相反：
	// CC 自己补 /v1/messages，而 @ai-sdk/anthropic 只补 /messages。
	// 写成根地址的话请求会打到 /messages，网关 404，且 opencode 静默吞掉：
	// 退出码 0、没有回答、没有报错。实测过。
	provider := "openai"
	if in.Protocol == ProtoAnthropicMessages {
		provider = "anthropic"
	}

	// 必须显式声明模型。opencode 会拿模型 ID 去比对内置目录，
	// 而网关的模型名（claude-opus-4-6、gpt-5.6-terra…）不在任何目录里，
	// 否则报 Model not found: <provider>/<id>。
	models := map[string]any{}
	for _, slot := range []string{"default", "small"} {
		if m := in.Slots[slot]; m != "" {
			models[m] = map[string]any{"name": m}
		}
	}

	cfg := map[string]any{
		"provider": map[string]any{
			provider: map[string]any{
				"options": map[string]any{
					"baseURL": OpenAIBase(in.Host),
					"apiKey":  in.Key,
				},
				"models": models,
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

	args := in.Args
	if in.Effort != "" {
		args = append([]string{"--variant", in.Effort}, args...)
	}

	set := map[string]string{"OPENCODE_CONFIG_CONTENT": string(blob)}
	return &Plan{Bin: "opencode", Args: args, Env: buildEnv(set, nil)}, nil
}

const piProviderExtension = `export default function (pi) {
  const config = JSON.parse(process.env.TF_PI_PROVIDER_CONFIG);
  const compat = config.api === "openai-completions" ? {
    supportsStore: false,
    supportsDeveloperRole: false,
    supportsStrictMode: false,
    maxTokensField: "max_tokens"
  } : undefined;

  pi.registerProvider(config.provider, {
    baseUrl: config.baseUrl,
    apiKey: "$TF_UPSTREAM_KEY",
    api: config.api,
    models: [{
      id: config.model,
      name: config.model,
      reasoning: config.reasoning,
      input: ["text"],
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
      contextWindow: 128000,
      maxTokens: 16384,
      ...(compat ? { compat } : {})
    }]
  });
}
`

type piProviderConfig struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"baseUrl"`
	API       string `json:"api"`
	Model     string `json:"model"`
	Reasoning bool   `json:"reasoning"`
}

// planPi 通过一次性 Extension 注册独立 provider。
//
// 不能改 PI_CODING_AGENT_DIR：那会连 settings、扩展、技能与会话目录一起换掉。
// 也不能把 Key 交给 --api-key：命令行会暴露在进程列表里。动态 provider 名
// 避免 ~/.pi/agent/auth.json 里碰巧同名的凭据按 Pi 的优先级覆盖本次注入。
func planPi(in Input) (*Plan, error) {
	modelID := in.Slots["default"]
	if modelID == "" {
		return nil, fmt.Errorf("pi requires a default model")
	}

	var api, baseURL string
	switch in.Protocol {
	case ProtoAnthropicMessages:
		api = "anthropic-messages"
		baseURL = AnthropicBase(in.Host)
	case ProtoOpenAIResponses:
		api = "openai-responses"
		baseURL = OpenAIBase(in.Host)
	case ProtoOpenAIChat:
		api = "openai-completions"
		baseURL = OpenAIBase(in.Host)
	default:
		return nil, fmt.Errorf("pi cannot use protocol %q", in.Protocol)
	}

	provider := "tf-" + strings.ToLower(rand.Text())
	blob, err := json.Marshal(piProviderConfig{
		Provider: provider, BaseURL: baseURL, API: api,
		Model: modelID, Reasoning: in.Effort != "",
	})
	if err != nil {
		return nil, err
	}

	args := []string{
		"--extension", "",
		"--model", provider + "/" + modelID,
	}
	if in.Effort != "" {
		args = append(args, "--thinking", in.Effort)
	}
	args = append(args, in.Args...)

	set := map[string]string{
		"TF_PI_PROVIDER_CONFIG": string(blob),
		"TF_UPSTREAM_KEY":       in.Key,
	}
	return &Plan{Bin: "pi", Args: args, Env: buildEnv(set, nil)}, nil
}
