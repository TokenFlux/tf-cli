// Package harness 描述 tf 能启动的 AI 编码工具，以及如何探测与安装它们。
//
// 这里只放「是什么、在哪、怎么装」。环境注入配方属于 M3，另置。
package harness

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Slot 是一个模型槽的声明。
//
// 适配表必须穷举 harness 会用到的每一个槽：漏掉一个，harness 就会
// 回落到它的内置默认模型，而那个模型大概率不在用户的分组里
// —— opencode 的 small_model 已实测坐实这一点。
type Slot struct {
	Name     string
	Purpose  func(zh bool) string
	Required bool
}

// InstallOption 是一条候选安装命令。
type InstallOption struct {
	Manager string   // 包管理器名，同时也是待探测的可执行文件名
	Args    []string // 完整 argv，第一项即 Manager
}

// Command 返回可展示、可复制的完整命令。
func (o InstallOption) Command() string { return strings.Join(o.Args, " ") }

// Protocol 是网关的客户端文本协议，取值与 TokenRouter 的
// allowed_client_protocols 一致。
type Protocol string

const (
	ProtoAnthropicMessages Protocol = "anthropic_messages"
	ProtoOpenAIResponses   Protocol = "openai_responses"
	ProtoOpenAIChat        Protocol = "openai_chat_completions"
)

// EffortKnob 表示该 harness 如何接受思考强度。
//
// 强度与模型槽是正交的两个维度，但 TokenFlux 上它们有时被混在一起：
// 部分分组把强度编进模型 ID（gemini-3.1-pro-high），此时换强度就是换
// 模型；另一些分组只能靠 harness 自己的旋钮。
type EffortKnob int

const (
	// EffortViaModelID 只支持模型 ID 内含的强度变体，没有独立旋钮。
	EffortViaModelID EffortKnob = iota
	// EffortViaConfig 通过配置项传递（codex 的 model_reasoning_effort）。
	EffortViaConfig
	// EffortViaFlag 通过命令行传递（opencode 的 --variant）。
	EffortViaFlag
)

// Harness 是一个可被 tf 启动的工具。
type Harness struct {
	Name    string
	Aliases []string
	Bin     string
	// Protocols 是该 harness 能说的客户端协议，按偏好排序。
	//
	// 多数 harness 不止会一种：opencode 同时内置 openai 与 anthropic
	// 两个 provider，所以它在只开 anthropic_messages 的分组上照样能跑。
	// 把 harness 钉死成单协议会凭空砍掉一半可用分组。
	Protocols []Protocol
	// IsClaudeCode 表示这个 harness 就是 Anthropic 官方的 Claude Code。
	//
	// 只有它能通过 claude_code_only 分组的客户端指纹检查。
	// tf 绝不伪装成它 —— 见 docs/design/product-decisions.md 第 0 节。
	IsClaudeCode bool
	Slots        []Slot
	Installs     []InstallOption
	EffortKnob   EffortKnob
	DocsURL      string
}

func zhen(zh, en string) func(bool) string {
	return func(isZH bool) string {
		if isZH {
			return zh
		}
		return en
	}
}

// All 是当前支持的 harness 清单。
var All = []*Harness{
	{
		Name:    "claude",
		Aliases: []string{"claude-code", "cc"},
		Bin:     "claude",
		// 描述写用途而不写 Anthropic 的档位名：用户面对的是网关上的
		// 模型列表，“sonnet 档”这类黑话无助于判断该填什么。
		// Claude Code 只会 Anthropic 协议。
		Protocols:    []Protocol{ProtoAnthropicMessages},
		IsClaudeCode: true,
		Slots: []Slot{
			{Name: "default", Purpose: zhen("主对话", "main conversation"), Required: true},
			{Name: "fast", Purpose: zhen("后台任务：标题、文件摘要", "background tasks: titles, file summaries")},
			{Name: "heavy", Purpose: zhen("/model 切到最强档时", "when /model picks the strongest tier")},
		},
		EffortKnob: EffortViaModelID,
		Installs: []InstallOption{
			{Manager: "npm", Args: []string{"npm", "install", "-g", "@anthropic-ai/claude-code"}},
			{Manager: "pnpm", Args: []string{"pnpm", "add", "-g", "@anthropic-ai/claude-code"}},
			{Manager: "bun", Args: []string{"bun", "add", "-g", "@anthropic-ai/claude-code"}},
		},
		DocsURL: "https://docs.tokenflux.dev/docs/agents/claude-code",
	},
	{
		Name:    "codex",
		Aliases: []string{"cx"},
		Bin:     "codex",
		// codex 只认 responses：官方 config reference 里 wire_api 已只剩这一个值。
		Protocols: []Protocol{ProtoOpenAIResponses},
		Slots: []Slot{
			{Name: "default", Purpose: zhen("主对话", "main conversation"), Required: true},
			{Name: "review", Purpose: zhen("代码审查", "code review")},
		},
		EffortKnob: EffortViaConfig,
		Installs: []InstallOption{
			{Manager: "npm", Args: []string{"npm", "install", "-g", "@openai/codex"}},
			{Manager: "pnpm", Args: []string{"pnpm", "add", "-g", "@openai/codex"}},
			{Manager: "brew", Args: []string{"brew", "install", "codex"}},
		},
		DocsURL: "https://docs.tokenflux.dev/docs/agents/codex",
	},
	{
		Name: "opencode",
		Bin:  "opencode",
		// 两种都会。顺序即偏好：responses 能拿到推理摘要，功能更全。
		Protocols: []Protocol{ProtoOpenAIResponses, ProtoAnthropicMessages},
		Slots: []Slot{
			{Name: "default", Purpose: zhen("主对话", "main conversation"), Required: true},
			// 不注入 small 会回落到内置的 gpt-5.4-nano，标题生成静默失败。
			{Name: "small", Purpose: zhen("后台任务：标题、摘要", "background tasks: titles, summaries"), Required: true},
		},
		EffortKnob: EffortViaFlag,
		Installs: []InstallOption{
			{Manager: "npm", Args: []string{"npm", "install", "-g", "opencode-ai"}},
			{Manager: "brew", Args: []string{"brew", "install", "sst/tap/opencode"}},
		},
		DocsURL: "https://docs.tokenflux.dev/docs/agents/opencode",
	},
}

// Speaks 报告该 harness 会不会某个协议。
func (h *Harness) Speaks(proto string) bool {
	for _, p := range h.Protocols {
		if string(p) == proto {
			return true
		}
	}
	return false
}

// Lookup 按名称或别名查找 harness。
func Lookup(name string) (*Harness, bool) {
	for _, h := range All {
		if h.Name == name {
			return h, true
		}
		for _, a := range h.Aliases {
			if a == name {
				return h, true
			}
		}
	}
	return nil, false
}

// Status 是一次探测结果。
type Status struct {
	Installed bool
	Path      string
	Version   string
}

// Detect 在 PATH 中查找 harness 并尝试取得版本号。
func (h *Harness) Detect() Status {
	path, err := exec.LookPath(h.Bin)
	if err != nil {
		return Status{}
	}
	return Status{Installed: true, Path: path, Version: probeVersion(path)}
}

// probeVersion 运行 `<bin> --version`。取不到就留空，绝不因此失败。
func probeVersion(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	// 有的工具会输出 "codex-cli 0.5.0"，只保留看起来像版本号的那段。
	if fields := strings.Fields(line); len(fields) > 1 {
		for _, f := range fields {
			if len(f) > 0 && f[0] >= '0' && f[0] <= '9' {
				return f
			}
		}
	}
	return line
}

// AvailableInstalls 过滤出本机真的存在对应包管理器的安装选项。
func (h *Harness) AvailableInstalls() []InstallOption {
	var out []InstallOption
	for _, o := range h.Installs {
		if _, err := exec.LookPath(o.Manager); err == nil {
			out = append(out, o)
		}
	}
	return out
}
