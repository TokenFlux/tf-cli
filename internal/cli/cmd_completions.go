package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tokenflux/tkr/internal/access"
	"github.com/tokenflux/tkr/internal/completions"
	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/model"
	"github.com/tokenflux/tkr/internal/ui"
)

// 补全的设计约束（按 Tab 是高频、低容忍度的交互）：
//
//   - **零网络**。候选一律来自本地配置与缓存；缓存冷就少给候选，
//     绝不为了补全去发 HTTP。
//   - **透传边界之后停止补全**。tf 不知道 harness 自己的 flag，
//     猜测只会给出错误候选；沉默比乱猜诚实。
//   - 脚本本身是薄的，逻辑全在 `tf __complete` 里，这样升级 tf
//     就等于升级补全，用户不必重新安装脚本。

// offerCompletions 在 login 之后问一次要不要装 shell 补全。
//
// tf 不擅自改用户的环境，但问一句和偷偷写是两回事 —— 装 harness
// 走的就是同一条规矩。放在 login 末尾：那本来就是配置时刻，
// 用户正在回答问题，而且一辈子只经历一次。
func offerCompletions(c *Context, cfg *config.Config) {
	if cfg.CompletionsAsked || !c.UI.Interactive(c.Flags.Bool("no-input")) {
		return
	}
	shell := completions.CurrentShell()
	if shell == "" || completions.Installed(shell) {
		return
	}

	idx, err := c.UI.Select(
		fmt.Sprintf(c.UI.T("是否安装 %s 的自动补全？", "Install %s completions?"), shell),
		[]ui.Item{
			{Label: c.UI.T("安装", "yes"), Detail: mustPath(shell)},
			{Label: c.UI.T("跳过", "no")},
		})

	// 只有“这件事已经有结果”才记下来。
	//
	// 答了不用 —— 记。答了装且装成了 —— 记。
	// 答了装但写失败 —— 不记：用户明明要了，我没给成，
	// 这时候记上“问过了”等于把一件没办成的事永久关掉。
	remember := func() {
		cfg.CompletionsAsked = true
		_ = cfg.Save()
	}

	if err != nil || idx != 0 {
		remember()
		return
	}
	if err := installCompletion(c, shell, completions.Scripts[shell]); err != nil {
		c.UI.Warnf("%s", err.Error())
		return // 下次再问
	}
	remember()
}

// mustPath 给出补全文件位置，用于让用户看清将要写到哪里。
func mustPath(shell string) string {
	p, err := completions.Path(shell)
	if err != nil {
		return ""
	}
	return p
}

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
			script, ok := completions.Scripts[shell]
			if !ok {
				return ui.Errf(ui.CodeUsage,
					fmt.Sprintf(c.UI.T("不支持 shell %q", "unsupported shell %q"), shell)).
					WithHint("bash | zsh | fish")
			}
			if c.Flags.Bool("install") {
				return installCompletion(c, shell, script)
			}
			// 直接打到终端多半不是用户想要的：这串东西是给 eval 用的。
			// 提示走 stderr，eval "$(tf completions zsh)" 照常工作。
			if c.UI.Interactive(false) {
				c.UI.Logf("%s", c.UI.Dim(
					c.UI.T("该脚本用于 eval 加载。如需直接写入配置请添加 --install 参数",
						"this script is meant for eval. add --install to install it")))
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
		return filter(append(storedKeys(), "--all", "--force"), cur)
	case "keys":
		return filter(dedupe(append([]string{"--refresh"}, globalFlagNames()...)), cur)
	}

	return filter(dedupe(globalFlagNames()), cur)
}

// completeLaunch 处理 `tf claude ...`。
//
// 一旦越过透传边界就返回空：那之后的参数属于 harness，tf 无从知晓。
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
		case "json", "no-input", "yes", "y", "help", "h":
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
		return []string{"--edit", "--set", "--reset", "--list"}
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
		if access.CanRun(meta, model.Parse(id).Prefix, h) {
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
	cmds := allCommands()
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		if !c.Hidden {
			out = append(out, c.Name)
		}
	}
	return out
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

// bashCompletionNote 说明还缺什么，并且只给本机真跑得动的命令。
//
// 原先写死的是 brew install bash-completion@2 —— 实测在 Ubuntu 上
// 照着做只会得到 command not found。与「没有 npm 却让人 npm install」
// 是同一类错误：建议里的命令必须在这台机器上存在。
func bashCompletionNote(c *Context) string {
	// 连标点都得跟着语言走：全角冒号出现在英文句子里和当初把
	// 「推理积分」塞进英文句子是同一类错误。
	base := c.UI.T("需要先装 bash-completion：", "requires bash-completion: ")
	for _, m := range []struct{ bin, cmd string }{
		{"brew", "brew install bash-completion@2"},
		{"apt", "sudo apt install bash-completion"},
		{"dnf", "sudo dnf install bash-completion"},
		{"pacman", "sudo pacman -S bash-completion"},
	} {
		if _, err := exec.LookPath(m.bin); err == nil {
			return base + m.cmd
		}
	}
	return strings.TrimRight(base, "：: ")
}

// installCompletion 把脚本写进该 shell 的补全目录。
//
// 只写专用的补全目录，绝不去改 .bashrc / .zshrc —— tf 不改用户的
// 配置文件，补全也不例外。需要用户自己动手的部分直接告知。
func installCompletion(c *Context, shell, script string) error {
	path, err := completions.Path(shell)
	if err != nil {
		return ui.Errf(ui.CodeConfigWrite, err.Error())
	}

	var note string
	switch {
	case shell == "zsh" && completions.ZshSiteDir() == "":
		note = c.UI.T(
			"还需在 .zshrc 的 compinit 之前加：fpath=(~/.zsh/completions $fpath)",
			"also add this before compinit in .zshrc: fpath=(~/.zsh/completions $fpath)")
	case shell == "bash" && !completions.BashRuntimePresent():
		note = bashCompletionNote(c)
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
