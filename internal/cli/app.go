package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tokenflux/tkr/internal/buildinfo"
	"github.com/tokenflux/tkr/internal/ui"
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

	// 找到第一个非 flag 的 token 作为子命令。
	cmdIdx := -1
	for i, s := range argv {
		if s == "--" {
			break
		}
		if !strings.HasPrefix(s, "-") {
			cmdIdx = i
			break
		}
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
			WithHint("tkr --help"))
		return 2
	}

	ctx, err := parse(cmd, argv[cmdIdx+1:])
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
		ctx.UI.Fail(cmd.Name, err)
		return 1
	}
	return 0
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
		"name":    "tkr",
		"version": buildinfo.Version,
		"commit":  buildinfo.Commit,
	}
	u.Emit("version", data, func() {
		u.Printf("tkr %s\n", buildinfo.Version)
	})
	return 0
}

// desc 从 "中文|English" 中取出对应语言。
func desc(u *ui.UI, s string) string {
	zh, en, ok := strings.Cut(s, "|")
	if !ok {
		return s
	}
	return u.T(zh, en)
}

func (a *App) printHelp(u *ui.UI) {
	u.Printf("%s\n", u.T(
		"tkr —— 用 TokenFlux / TokenRouter 启动你已经在用的 AI 编码工具。",
		"tkr — launch the AI coding harnesses you already use, against TokenFlux / TokenRouter.",
	))
	u.Printf("\n%s\n  tkr <command> [flags]\n", u.Bold(u.T("用法", "USAGE")))

	var visible []*Command
	for _, c := range a.commands {
		if !c.Hidden {
			visible = append(visible, c)
		}
	}
	sort.SliceStable(visible, func(i, j int) bool { return visible[i].Name < visible[j].Name })

	u.Printf("\n%s\n", u.Bold(u.T("命令", "COMMANDS")))
	for _, c := range visible {
		u.Printf("  %-12s %s\n", c.Name, c.Summary(u))
	}

	u.Printf("\n%s\n", u.Bold(u.T("全局选项", "GLOBAL FLAGS")))
	for _, f := range globalFlags() {
		u.Printf("  %-22s %s\n", flagLabel(f), desc(u, f.Desc))
	}
	u.Printf("  %-22s %s\n", "--version, -v", u.T("显示版本", "Show version"))
	u.Printf("\n%s\n", u.Dim(u.T(
		"提示：harness 命令会把无法识别的参数原样透传；用 -- 强制透传。",
		"Tip: harness commands pass unrecognized args through; use -- to force it.",
	)))
}

func (a *App) printCommandHelp(u *ui.UI, c *Command) {
	u.Printf("%s\n", c.Summary(u))
	usage := c.Usage
	if usage == "" {
		usage = "tkr " + c.Name + " [flags]"
	}
	u.Printf("\n%s\n  %s\n", u.Bold(u.T("用法", "USAGE")), usage)

	if len(c.Flags) > 0 {
		u.Printf("\n%s\n", u.Bold(u.T("选项", "FLAGS")))
		for _, f := range c.Flags {
			u.Printf("  %-22s %s\n", flagLabel(f), desc(u, f.Desc))
		}
	}

	u.Printf("\n%s\n", u.Bold(u.T("全局选项", "GLOBAL FLAGS")))
	for _, f := range globalFlags() {
		u.Printf("  %-22s %s\n", flagLabel(f), desc(u, f.Desc))
	}

	if c.Passthrough {
		u.Printf("\n%s\n", u.Dim(u.T(
			"以上选项之后的参数全部原样传给 "+c.Name+"；用 -- 强制透传。",
			"Everything after these flags is passed to "+c.Name+" untouched; use -- to force it.",
		)))
	}
}

func flagLabel(f Flag) string {
	label := "--" + f.Name
	if f.Short != "" {
		label += ", -" + f.Short
	}
	switch f.Kind {
	case KindString:
		label += " <value>"
	case KindOptString:
		label += " [value]"
	}
	return label
}
