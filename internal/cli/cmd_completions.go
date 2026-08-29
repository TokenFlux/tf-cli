package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/model"
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
		Flags: []Flag{
			{Name: "install", Kind: KindBool,
				Desc: "写入该 shell 的补全目录|Write it into the shell's completion directory"},
		},
		Run: func(c *Context) error {
			if len(c.Args) == 0 {
				return ui.Errf(ui.CodeUsage,
					c.UI.T("需要指定 shell", "a shell is required")).
					WithHint("tkr completions zsh")
			}
			shell := c.Args[0]
			script, ok := completionScripts[shell]
			if !ok {
				return ui.Errf(ui.CodeUsage,
					fmt.Sprintf(c.UI.T("不支持的 shell：%s", "unsupported shell: %s"), shell)).
					WithHint("bash | zsh | fish")
			}
			if c.Flags.Bool("install") {
				return installCompletion(c, shell, script)
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
	case "use":
		return filter(storedProfiles(), cur)
	case "logout":
		return filter(append(storedProfiles(), "--all", "--profile"), cur)
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
		case "m", "model", "e", "effort", "profile", "host":
			i++ // 跳过其取值
		case "json", "yes", "y", "help", "h":
		default:
			return nil // 陌生 flag 即透传起点
		}
	}

	// 正在为某个 flag 补值。
	if len(rest) > 0 {
		switch strings.TrimLeft(strings.SplitN(rest[len(rest)-1], "=", 2)[0], "-") {
		case "m", "model":
			return cachedModels()
		case "e", "effort":
			return effortNames()
		}
	}

	if strings.HasPrefix(cur, "-") {
		return append([]string{"--model", "--effort"}, globalFlagNames()...)
	}
	return nil
}

// effortNames 优先给出缓存模型里真实存在的强度变体，
// 没有时才回落到通用档位。
func effortNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range model.Group(cachedModels()) {
		for _, e := range f.Efforts {
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	return []string{"minimal", "low", "medium", "high", "xhigh"}
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

// storedProfiles 读本地配置列出 profile 名，供 logout 补全。仍是零网络。
func storedProfiles() []string {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil
	}
	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		return nil
	}
	return creds.Names()
}

func commandNames() []string {
	names := []string{"version", "config", "login", "logout", "use", "harness", "model", "completions"}
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

// 脚本里一律用「用户实际输入的那个路径」回调，而不是写死 `tkr`。
// 否则 ./bin/tkr 这种未入 PATH 的跑法会因找不到命令而静默无补全。
var completionScripts = map[string]string{
	"bash": `# tkr bash completion —— eval "$(tkr completions bash)"
_tkr_complete() {
    local IFS=$'\n'
    COMPREPLY=($("${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null))
}
# 同时注册裸命令名与常见的相对路径写法。
complete -o default -F _tkr_complete tkr ./tkr ./bin/tkr bin/tkr
`,
	"zsh": `# tkr zsh completion —— eval "$(tkr completions zsh)"
_tkr_complete() {
    local -a candidates
    candidates=(${(f)"$(${words[1]} __complete ${words[2,$CURRENT]} 2>/dev/null)"})
    compadd -a candidates
}
compdef _tkr_complete tkr
# 模式注册：让 ./bin/tkr、/usr/local/bin/tkr 等写法也能补全。
compdef _tkr_complete -p '*/tkr'
`,
	"fish": `# tkr fish completion —— tkr completions fish --install
function __tkr_complete
    set -l tokens (commandline -opc) (commandline -ct)
    $tokens[1] __complete $tokens[2..-1] 2>/dev/null
end
complete -c tkr -f -a '(__tkr_complete)'
`,
}

// installCompletion 把脚本写进该 shell 的补全目录。
//
// 只写专用的补全目录，**绝不去改 .bashrc / .zshrc** —— tkr 不改
// 用户的配置文件，补全也不例外。需要用户自己动手的部分直接告知。
func installCompletion(c *Context, shell, script string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return ui.Errf(ui.CodeConfigWrite, err.Error())
	}

	var path, note string
	switch shell {
	case "fish":
		path = filepath.Join(home, ".config", "fish", "completions", "tkr.fish")
	case "zsh":
		path = filepath.Join(home, ".zsh", "completions", "_tkr")
		note = c.UI.T(
			"若补全未生效，请确保 .zshrc 里有：fpath=(~/.zsh/completions $fpath) 与 autoload -U compinit && compinit",
			"if it does not kick in, ensure .zshrc has: fpath=(~/.zsh/completions $fpath) and autoload -U compinit && compinit")
	case "bash":
		path = filepath.Join(home, ".local", "share", "bash-completion", "completions", "tkr")
		note = c.UI.T("需要已安装 bash-completion（brew install bash-completion@2）",
			"requires bash-completion to be installed (brew install bash-completion@2)")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ui.Errf(ui.CodeConfigWrite, err.Error())
	}
	body := script
	if shell == "zsh" {
		// 放进 fpath 的文件需要 #compdef 头，且不能再调 compdef。
		body = "#compdef tkr\n" + strings.ReplaceAll(script, "compdef _tkr_complete", "# compdef _tkr_complete")
		body += "\n_tkr_complete \"$@\"\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return ui.Errf(ui.CodeConfigWrite, err.Error())
	}

	c.UI.Emit("completions", map[string]string{"shell": shell, "path": path}, func() {
		c.UI.Printf("✓ %s\n", path)
		if note != "" {
			c.UI.Printf("  %s\n", c.UI.Dim(note))
		}
		c.UI.Printf("  %s\n", c.UI.Dim(c.UI.T("重开终端后生效", "restart your shell to pick it up")))
	})
	return nil
}
