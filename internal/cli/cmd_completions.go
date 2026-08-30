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
//   - 脚本本身是薄的，逻辑全在 `tf __complete` 里，这样升级 tf
//     就等于升级补全，用户不必重新安装脚本。

func newCompletionsCommand() *Command {
	return &Command{
		Name:  "completions",
		Usage: "tf completions <bash|zsh|fish>",
		Summary: func(u *ui.UI) string {
			return u.T("输出 shell 补全脚本", "Print the shell completion script")
		},
		Flags: []Flag{
			{Name: "install", Kind: KindBool,
				Desc: "写入该 shell 的补全目录||Write it into the shell's completion directory"},
		},
		Run: func(c *Context) error {
			if len(c.Args) == 0 {
				return ui.Errf(ui.CodeUsage,
					c.UI.T("需要指定 shell", "a shell is required")).
					WithHint("tf completions zsh")
			}
			shell := c.Args[0]
			script, ok := completionScripts[shell]
			if !ok {
				return ui.Errf(ui.CodeUsage,
					fmt.Sprintf(c.UI.T("不支持 shell %q", "unsupported shell %q"), shell)).
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
	case "completions":
		return filter([]string{"bash", "zsh", "fish"}, cur)
	case "login":
		return filter(append(storedKeys(), "--with-key", "--host", "--force"), cur)
	case "update":
		return filter([]string{"--check"}, cur)
	case "logout":
		return filter(append(storedKeys(), "--all"), cur)
	case "keys":
		return filter(dedupe(append([]string{"--refresh"}, globalFlagNames()...)), cur)
	}

	return filter(dedupe(globalFlagNames()), cur)
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
		case "m", "model", "e", "effort", "k", "key", "host":
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
			return cachedModels(h.Name)
		case "e", "effort":
			return effortNames(h.Name)
		}
	}

	// 光标停在参数位时也要给出 tf 自己的 flag。
	//
	// 空结果在 zsh 里不等于「什么都不补」：菜单补全会沿用上一次的候选，
	// 于是 tf codex <TAB> 会把 codex 再插一遍。没得补时也得说点什么。
	return dedupe(append([]string{"--model", "--effort"}, globalFlagNames()...))
}

// dedupe 去掉重复项并保持原有顺序。
//
// 启动命令自己的 flag 与全局 flag 有重叠（--key 两边都有），
// 补全列表里出现两次会让人以为是两个不同的东西。
func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// effortNames 优先给出缓存模型里真实存在的强度变体，
// 没有时才回落到通用档位。
func effortNames(harnessName string) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range model.Group(cachedModels(harnessName)) {
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
		for _, m := range cachedModels(h.Name) {
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
// cachedModels 读本地 config 里的模型列表。零网络。
//
// 按该 harness 的绑定取对应 Key，并按协议过滤 —— 补全不该提示
// 一个选了就会 403 的模型。
func cachedModels(harnessName string) []string {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return nil
	}
	keyName := cfg.Harness(harnessName).Key
	if keyName == "" {
		for _, n := range cfg.KeyNames() {
			keyName = n
			break
		}
	}
	meta := cfg.Keys[keyName]
	if meta == nil {
		return nil
	}
	h, ok := harness.Lookup(harnessName)
	if !ok {
		return meta.Models
	}
	out := make([]string, 0, len(meta.Models))
	for _, id := range meta.Models {
		if canRun(meta, model.Parse(id).Prefix, h) {
			out = append(out, id)
		}
	}
	return out
}

func storedKeys() []string {
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
	names := []string{"version", "config", "login", "logout", "keys", "harness", "model", "update", "completions"}
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
	"bash": `# tf bash completion. eval "$(tf completions bash)"
_tf_complete() {
    local IFS=$'\n'
    COMPREPLY=($("${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null))
}
# 同时注册裸命令名与常见的相对路径写法。
complete -o default -F _tf_complete tf ./tf ./bin/tf bin/tf
`,
	"zsh": `# tf zsh completion. eval "$(tf completions zsh)"
_tf_complete() {
    local -a candidates
    candidates=(${(f)"$(${words[1]} __complete ${words[2,$CURRENT]} 2>/dev/null)"})
    compadd -a candidates
}
compdef _tf_complete tf
# 模式注册：让 ./bin/tf、/usr/local/bin/tf 等写法也能补全。
compdef _tf_complete -p '*/tf'
`,
	"fish": `# tf fish completion. tf completions fish --install
function __tf_complete
    set -l tokens (commandline -opc) (commandline -ct)
    $tokens[1] __complete $tokens[2..-1] 2>/dev/null
end
complete -c tkr -f -a '(__tf_complete)'
`,
}

// installCompletion 把脚本写进该 shell 的补全目录。
//
// 只写专用的补全目录，**绝不去改 .bashrc / .zshrc** —— tkr 不改
// zshSiteFunctions 找一个已经在 zsh 默认 fpath 里、且可写的目录。
//
// 找不到就返回空，调用方退回到家目录下的写法。
func zshSiteFunctions() string {
	for _, d := range []string{
		"/opt/homebrew/share/zsh/site-functions",
		"/usr/local/share/zsh/site-functions",
	} {
		info, err := os.Stat(d)
		if err != nil || !info.IsDir() {
			continue
		}
		// 可写才算数：只读目录写下去只会得到一个权限错误。
		probe := filepath.Join(d, ".tf-write-probe")
		if f, err := os.Create(probe); err == nil {
			f.Close()
			os.Remove(probe)
			return d
		}
	}
	return ""
}

// 用户的配置文件，补全也不例外。需要用户自己动手的部分直接告知。
func installCompletion(c *Context, shell, script string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return ui.Errf(ui.CodeConfigWrite, err.Error())
	}

	var path, note string
	switch shell {
	case "fish":
		path = filepath.Join(home, ".config", "fish", "completions", "tf.fish")
	case "zsh":
		// 优先装进已经在 fpath 里的目录，用户就不必再改 .zshrc。
		//
		// 装到 ~/.zsh/completions 是最常见的写法，但那个目录不在任何人的
		// 默认 fpath 里 —— 于是「装好了却不生效」，还要用户自己去补一行，
		// 而且那一行必须排在 compinit 之前，追加到文件末尾是没用的。
		if dir := zshSiteFunctions(); dir != "" {
			path = filepath.Join(dir, "_tf")
			break
		}
		path = filepath.Join(home, ".zsh", "completions", "_tf")
		note = c.UI.T(
			"还需在 .zshrc 的 compinit 之前加：fpath=(~/.zsh/completions $fpath)",
			"also add this before compinit in .zshrc: fpath=(~/.zsh/completions $fpath)")
	case "bash":
		path = filepath.Join(home, ".local", "share", "bash-completion", "completions", "tf")
		note = c.UI.T("需要已安装 bash-completion（brew install bash-completion@2）",
			"requires bash-completion to be installed (brew install bash-completion@2)")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ui.Errf(ui.CodeConfigWrite, err.Error())
	}
	body := script
	if shell == "zsh" {
		// 放进 fpath 的文件需要 #compdef 头，且不能再调 compdef。
		body = "#compdef tf\n" + strings.ReplaceAll(script, "compdef _tf_complete", "# compdef _tf_complete")
		body += "\n_tf_complete \"$@\"\n"
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
