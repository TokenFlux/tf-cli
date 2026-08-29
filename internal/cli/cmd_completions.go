package cli

import (
	"fmt"
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/ui"
)

// 补全的设计约束（按 Tab 是高频、低容忍度的交互）：
//
//   - **零网络**。候选一律来自本地配置与缓存；缓存冷就少给候选，
//     绝不为了补全去发 HTTP。
//   - **透传边界之后停止补全**。tkr 不知道 harness 自己的 flag，
//     猜测只会给出错误候选；沉默比乱猜诚实。
//   - 脚本本身是薄的，逻辑全在 `tkr __complete` 里，这样升级 tkr
//     就等于升级补全，用户不必重新安装脚本。

func newCompletionsCommand() *Command {
	return &Command{
		Name:  "completions",
		Usage: "tkr completions <bash|zsh|fish>",
		Summary: func(u *ui.UI) string {
			return u.T("输出 shell 补全脚本", "Print the shell completion script")
		},
		Run: func(c *Context) error {
			if len(c.Args) == 0 {
				return ui.Errf(ui.CodeUsage,
					c.UI.T("需要指定 shell", "a shell is required")).
					WithHint("tkr completions zsh")
			}
			script, ok := completionScripts[c.Args[0]]
			if !ok {
				return ui.Errf(ui.CodeUsage,
					fmt.Sprintf(c.UI.T("不支持的 shell：%s", "unsupported shell: %s"), c.Args[0])).
					WithHint("bash | zsh | fish")
			}
			c.UI.Printf("%s", script)
			return nil
		},
	}
}

// newCompleteCommand 是补全脚本回调的隐藏入口。
func newCompleteCommand() *Command {
	return &Command{
		Name:        "__complete",
		Hidden:      true,
		Passthrough: true, // 原样收下所有词，自己解析
		Summary: func(u *ui.UI) string {
			return u.T("内部：输出补全候选", "internal: emit completion candidates")
		},
		Run: func(c *Context) error {
			words := append(append([]string{}, c.Args...), c.Passthr...)
			for _, cand := range complete(words) {
				fmt.Fprintln(c.UI.Out, cand)
			}
			return nil
		},
	}
}

// complete 依据已输入的词给出候选。words 的最后一项是当前正在输入的词
// （可能为空）。
func complete(words []string) []string {
	cur := ""
	if len(words) > 0 {
		cur = words[len(words)-1]
		words = words[:len(words)-1]
	}

	// 还没输入子命令：补全命令名。
	if len(words) == 0 {
		return filter(commandNames(), cur)
	}

	cmdName := words[0]
	rest := words[1:]

	if h, ok := harness.Lookup(cmdName); ok {
		return filter(completeLaunch(h, rest, cur), cur)
	}

	switch cmdName {
	case "harness":
		if len(rest) == 0 {
			return filter([]string{"list", "install"}, cur)
		}
		if rest[0] == "install" {
			return filter(harnessNames(), cur)
		}
	case "model":
		return filter(completeModel(rest, cur), cur)
	case "config":
		return filter([]string{"path", "show"}, cur)
	case "completions":
		return filter([]string{"bash", "zsh", "fish"}, cur)
	case "login":
		return filter([]string{"--with-key", "--host", "--profile"}, cur)
	}

	if strings.HasPrefix(cur, "-") {
		return filter(globalFlagNames(), cur)
	}
	return nil
}

// completeLaunch 处理 `tkr claude ...`。
//
// 一旦越过透传边界就返回空：那之后的参数属于 harness，tkr 无从知晓。
func completeLaunch(h *harness.Harness, rest []string, cur string) []string {
	for i := 0; i < len(rest); i++ {
		w := rest[i]
		if w == "--" {
			return nil
		}
		if !strings.HasPrefix(w, "-") {
			return nil // 位置参数即透传起点
		}
		switch strings.TrimLeft(strings.SplitN(w, "=", 2)[0], "-") {
		case "m", "model", "profile", "host":
			i++ // 跳过其取值
		case "json", "yes", "y", "help", "h":
		default:
			return nil // 陌生 flag 即透传起点
		}
	}

	// 正在为 -m 补值：给出该 harness 已配置的槽位模型 + 缓存里的模型列表。
	if len(rest) > 0 {
		last := strings.TrimLeft(strings.SplitN(rest[len(rest)-1], "=", 2)[0], "-")
		if last == "m" || last == "model" {
			return cachedModels()
		}
	}

	if strings.HasPrefix(cur, "-") {
		return append([]string{"--model"}, globalFlagNames()...)
	}
	return nil
}

func completeModel(rest []string, cur string) []string {
	if len(rest) == 0 {
		return append(harnessNames(), "--list")
	}
	h, ok := harness.Lookup(rest[0])
	if !ok {
		return nil
	}
	// `--set slot=` 时补槽名；已经带 = 时补模型。
	if strings.HasPrefix(cur, "-") {
		return []string{"--set", "--reset", "--list"}
	}
	if slot, _, found := strings.Cut(cur, "="); found {
		out := []string{}
		for _, m := range cachedModels() {
			out = append(out, slot+"="+m)
		}
		return out
	}
	names := []string{}
	for _, s := range h.Slots {
		names = append(names, s.Name+"=")
	}
	return names
}

// cachedModels 只读缓存。缓存冷就返回空 —— 补全宁可少给候选，
// 也不能在这里发网络请求。
func cachedModels() []string {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil
	}
	var ids []string
	if _, err := paths.ReadCache("models", &ids); err != nil {
		return nil
	}
	return ids
}

func commandNames() []string {
	names := []string{"version", "config", "login", "harness", "model", "completions"}
	return append(names, harnessNames()...)
}

func harnessNames() []string {
	out := make([]string, 0, len(harness.All))
	for _, h := range harness.All {
		out = append(out, h.Name)
	}
	return out
}

func globalFlagNames() []string {
	out := make([]string, 0)
	for _, f := range globalFlags() {
		out = append(out, "--"+f.Name)
	}
	return out
}

func filter(candidates []string, prefix string) []string {
	if prefix == "" {
		return candidates
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

var completionScripts = map[string]string{
	"bash": `# tkr bash completion —— eval "$(tkr completions bash)"
_tkr_complete() {
    local IFS=$'\n'
    COMPREPLY=($(tkr __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null))
}
complete -o default -F _tkr_complete tkr
`,
	"zsh": `# tkr zsh completion —— eval "$(tkr completions zsh)"
_tkr_complete() {
    local -a candidates
    candidates=(${(f)"$(tkr __complete ${words[2,$CURRENT]} 2>/dev/null)"})
    compadd -a candidates
}
compdef _tkr_complete tkr
`,
	"fish": `# tkr fish completion —— tkr completions fish > ~/.config/fish/completions/tkr.fish
function __tkr_complete
    set -l tokens (commandline -opc) (commandline -ct)
    tkr __complete $tokens[2..-1] 2>/dev/null
end
complete -c tkr -f -a '(__tkr_complete)'
`,
}
