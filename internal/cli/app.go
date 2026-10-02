package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tokenflux/tf-cli/internal/buildinfo"
	"github.com/tokenflux/tf-cli/internal/ui"
)

// App 是命令注册表与调度器。
type App struct {
	commands []*Command
	index    map[string]*Command
}

// NewApp 构造注册表。
func NewApp() *App {
	return &App{index: map[string]*Command{}}
}

// Register 注册一个子命令。
func (a *App) Register(c *Command) {
	a.commands = append(a.commands, c)
	a.index[c.Name] = c
	for _, alias := range c.Aliases {
		a.index[alias] = c
	}
}

// Run 执行一次调用，返回进程退出码。
func (a *App) Run(argv []string) int {
	// 先扫一遍全局 flag，让 UI 在解析出错前就具备正确的语言与输出模式。
	jsonMode := false
	for _, s := range argv {
		if s == "--" {
			break
		}
		if s == "--json" {
			jsonMode = true
		}
	}
	u := ui.New(jsonMode)

	// 子命令之前只允许全局 flag，且它们的值要一并吃掉。
	// 否则 `tf --key work claude` 里的 work 会被当成子命令。
	leading, cmdIdx, err := splitGlobals(argv)
	if err != nil {
		u.Fail("", err)
		return 2
	}

	if cmdIdx == -1 {
		if hasFlag(argv, "version", "v") {
			return a.runVersion(u)
		}
		a.printHelp(u)
		// 没给子命令时，显式请求帮助算成功，其余算用法错误。
		if hasFlag(argv, "help", "h") || len(argv) == 0 {
			return 0
		}
		return 2
	}

	name := argv[cmdIdx]
	cmd, ok := a.index[name]
	if !ok {
		u.Fail("", ui.Errf(ui.CodeUnknownCommand,
			u.T(fmt.Sprintf("未知命令：%s", name), fmt.Sprintf("unknown command: %s", name))).
			WithHint("tf --help"))
		return 2
	}

	// harness 后的 -h/--help 属于底层工具；tf 自己的帮助放在命令名前。
	if cmd.Passthrough && hasFlag(leading, "help", "h") {
		a.printCommandHelp(u, cmd)
		return 0
	}

	// 前置的全局 flag 当成写在子命令后面一样解析，两种写法因此等价。
	tail := append(append([]string{}, leading...), argv[cmdIdx+1:]...)
	ctx, err := parse(cmd, tail)
	if err != nil {
		u.Fail(cmd.Name, err)
		return 2
	}
	ctx.UI = u

	if ctx.Flags.Bool("help") {
		a.printCommandHelp(u, cmd)
		return 0
	}
	// --json 可能出现在子命令之后，此时要重建 UI。
	if ctx.Flags.Bool("json") && !u.JSON {
		ctx.UI = ui.New(true)
	}

	if err := cmd.Run(ctx); err != nil {
		// harness 的退出码必须原样穿透，否则脚本无法判断真实结果。
		var ec *exitCodeError
		if errors.As(err, &ec) {
			return ec.code
		}
		// 用户按 esc 不是出错，不能红字报一行「错误：已取消」。
		// 退出码沿用 130（与 Ctrl-C 一致），脚本依然分得清。
		// JSON 模式仍然给信封：机器需要知道为什么没有结果。
		if ui.AsError(err).Code == ui.CodeCancelled {
			if ctx.UI.JSON {
				ctx.UI.Fail(cmd.Name, err)
			}
			return 130
		}
		ctx.UI.Fail(cmd.Name, err)
		return 1
	}
	ctx.UI.Flush(cmd.Name)
	return 0
}

// splitGlobals 吃掉子命令之前的全局 flag，返回它们与子命令的下标。
// cmdIdx 为 -1 表示没给子命令。
func splitGlobals(argv []string) (leading []string, cmdIdx int, err error) {
	byName := map[string]*Flag{}
	globals := globalFlags()
	for i := range globals {
		f := &globals[i]
		for _, n := range f.names() {
			byName[n] = f
		}
	}

	for i := 0; i < len(argv); i++ {
		s := argv[i]
		if s == "--" {
			return leading, -1, nil
		}
		if !strings.HasPrefix(s, "-") || s == "-" {
			return leading, i, nil
		}

		name, _, hasInline := splitFlag(s)
		f, known := byName[name]
		if !known {
			// --version / -v 等由调用方处理；其余未知 flag 留给帮助与报错。
			leading = append(leading, s)
			continue
		}
		leading = append(leading, s)
		if (f.Kind == KindString || f.Kind == KindStrings) && !hasInline {
			if i+1 >= len(argv) {
				return nil, -1, ui.Errf(ui.CodeMissingValue,
					fmt.Sprintf("flag needs a value: %s", s))
			}
			i++
			leading = append(leading, argv[i])
		}
	}
	return leading, -1, nil
}

func hasFlag(argv []string, name, short string) bool {
	for _, s := range argv {
		if s == "--"+name || (short != "" && s == "-"+short) {
			return true
		}
	}
	return false
}

func (a *App) runVersion(u *ui.UI) int {
	data := map[string]string{
		"name":    "tf",
		"version": buildinfo.Version,
		"commit":  buildinfo.Commit,
	}
	u.Emit("version", data, func() {
		u.Printf("tf %s\n", buildinfo.Version)
	})
	return 0
}

// desc 从 "中文|English" 中取出对应语言。
// desc 取标志描述的中文或英文那一半。
//
// 分隔符是 "||" 而不是 "|"：描述里本来就会出现单个竖线
// （思考强度那条列的就是 minimal|low|medium|high|xhigh），
// 用单竖线切会把描述从中间劈开，帮助里显示成一串乱码。
func desc(u *ui.UI, s string) string {
	zh, en, ok := strings.Cut(s, "||")
	if !ok {
		return s
	}
	return u.T(zh, en)
}

func (a *App) printHelp(u *ui.UI) {
	u.Printf("%s\n", u.T(
		"tf —— 用 TokenFlux / TokenRouter 启动 AI 编码工具。",
		"tf — launch AI coding tools against TokenFlux / TokenRouter.",
	))
	u.Printf("\n%s\n  tf <command> [flags]\n", u.Bold(u.T("用法", "USAGE")))

	u.Printf("\n%s\n", u.Bold(u.T("从这里开始", "START HERE")))
	u.Printf("  %-22s %s\n", "tf login", u.T("登录并保存一把 Key", "sign in and save a key"))
	u.Printf("  %-22s %s\n", "tf claude", u.T("启动 Claude Code（首次会引导配置）", "launch Claude Code; first run guides setup"))
	u.Printf("  %-22s %s\n", "tf status", u.T("查看本地配置是否就绪", "check whether local setup is ready"))

	u.Printf("\n%s\n", u.Bold(u.T("Agent / 脚本", "AGENTS / SCRIPTS")))
	u.Printf("  %-22s %s\n", "tf agent-readme", u.T("输出机器使用契约", "print the machine-use contract"))
	u.Printf("  %-22s %s\n", "tf auth --json", u.T("解释当前实际使用的 Key", "explain the credential in effect"))
	u.Printf("  %-22s %s\n", "tf claude --no-tui", u.T("无选择器启动 harness", "launch without selectors"))

	groups := []struct {
		title string
		names []string
	}{
		{u.T("启动工具", "RUN AGENTS"), harnessNames()},
		{u.T("配置与模型", "SETUP & MODELS"), []string{"login", "model", "keys", "completions", "harness"}},
		{u.T("查看与诊断", "INSPECT & DIAGNOSE"), []string{"status", "auth", "config"}},
		{u.T("维护", "MAINTENANCE"), []string{"logout", "update", "version"}},
	}
	byName := make(map[string]*Command, len(a.commands))
	for _, c := range a.commands {
		byName[c.Name] = c
	}
	for _, group := range groups {
		a.printHelpGroup(u, group.title, group.names, byName)
	}

	u.Printf("\n%s\n", u.Bold(u.T("常用选项", "COMMON OPTIONS")))
	for _, f := range globalFlags() {
		u.Printf("  %-22s %s\n", flagLabel(f), desc(u, f.Desc))
	}
	u.Printf("  %-22s %s\n", "--version, -v", u.T("显示版本", "Show version"))
	u.Printf("\n%s\n", u.Dim(u.T(
		"查看命令详情：tf --help <command>。harness 后未识别的参数会透传；用 -- 强制透传。",
		"For command details: tf --help <command>. Unknown arguments after a harness pass through; use -- to force passthrough.",
	)))
}

func (a *App) printHelpGroup(u *ui.UI, title string, names []string, byName map[string]*Command) {
	visible := make([]*Command, 0, len(names))
	for _, name := range names {
		if c := byName[name]; c != nil && !c.Hidden {
			visible = append(visible, c)
		}
	}
	if len(visible) == 0 {
		return
	}
	u.Printf("\n%s\n", u.Bold(title))
	for _, c := range visible {
		u.Printf("  %-12s %s\n", c.Name, c.Summary(u))
	}
}

func (a *App) printCommandHelp(u *ui.UI, c *Command) {
	u.Printf("%s\n", c.Summary(u))
	usage := c.Usage
	if usage == "" {
		usage = "tf " + c.Name + " [flags]"
	}
	u.Printf("\n%s\n  %s\n", u.Bold(u.T("用法", "USAGE")), usage)

	if len(c.Flags) > 0 {
		u.Printf("\n%s\n", u.Bold(u.T("命令选项", "COMMAND OPTIONS")))
		for _, f := range c.Flags {
			u.Printf("  %-22s %s\n", flagLabel(f), desc(u, f.Desc))
		}
	}

	u.Printf("\n%s\n", u.Bold(u.T("通用选项", "COMMON OPTIONS")))
	for _, f := range globalFlags() {
		u.Printf("  %-22s %s\n", flagLabel(f), desc(u, f.Desc))
	}

	if c.Passthrough {
		u.Printf("\n%s\n", u.Dim(u.T(
			"命令后的 -h/--help 交给 "+c.Name+"；用 tf --help "+c.Name+" 查看本页，用 -- 强制透传其它同名选项。",
			"-h/--help after the command goes to "+c.Name+"; use tf --help "+c.Name+" for this page, or -- to force other colliding flags through.",
		)))
	}
}

func flagLabel(f Flag) string {
	label := "--" + f.Name
	if f.Short != "" {
		label += ", -" + f.Short
	}
	if f.Name == "no-input" {
		label += ", --no-tui"
	}
	switch f.Kind {
	case KindString, KindStrings:
		label += " <value>"
	case KindOptString:
		label += " [value]"
	}
	return label
}
