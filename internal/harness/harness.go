// Package harness 描述 tkr 能启动的 AI 编码工具，以及如何探测与安装它们。
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

// Harness 是一个可被 tkr 启动的工具。
type Harness struct {
	Name     string
	Aliases  []string
	Bin      string
	Slots    []Slot
	Installs []InstallOption
	DocsURL  string
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
		Slots: []Slot{
			{Name: "default", Purpose: zhen("主模型（sonnet 档）", "main model (sonnet tier)"), Required: true},
			{Name: "fast", Purpose: zhen("快速档（haiku）", "fast tier (haiku)")},
			{Name: "heavy", Purpose: zhen("重型档（opus）", "heavy tier (opus)")},
		},
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
		Slots: []Slot{
			{Name: "default", Purpose: zhen("主模型", "main model"), Required: true},
			{Name: "review", Purpose: zhen("代码审查模型", "review model")},
		},
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
		Slots: []Slot{
			{Name: "default", Purpose: zhen("主模型", "main model"), Required: true},
			// 不注入 small 会回落到内置的 gpt-5.4-nano，标题生成静默失败。
			{Name: "small", Purpose: zhen("小模型（标题、摘要）", "small model (titles, summaries)"), Required: true},
		},
		Installs: []InstallOption{
			{Manager: "npm", Args: []string{"npm", "install", "-g", "opencode-ai"}},
			{Manager: "brew", Args: []string{"brew", "install", "sst/tap/opencode"}},
		},
		DocsURL: "https://docs.tokenflux.dev/docs/agents/opencode",
	},
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
