package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tokenflux/tf-cli/internal/buildinfo"
	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/harness"
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
		// 真实交互终端直接进入首页；帮助和显式非交互入口保持稳定、可组合。
		if len(argv) == 0 && u.Interactive(false) {
			return a.runHome(u)
		}
		if u.JSON {
			u.Emit("help", map[string]any{
				"usage":         "tf <command> [flags]",
				"interactive":   false,
				"next":          "tf --help",
				"command_count": len(a.commands),
			}, nil)
			return 0
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

func (a *App) runHome(u *ui.UI) int {
	items := []ui.Item{
		{Label: u.T("启动 AI 工具", "Launch an AI tool"), Detail: u.T("选择 Claude Code、Codex、OpenCode 或 Pi", "Choose Claude Code, Codex, OpenCode, or Pi")},
		{Label: u.T("登录", "Sign in"), Detail: u.T("保存 API Key 或从网页导入", "Save an API key or import from the web")},
		{Label: u.T("查看状态", "Check status"), Detail: u.T("查看 Key、模型和本地配置", "Inspect keys, models, and local setup")},
		{Label: u.T("编辑模型", "Edit models"), Detail: u.T("调整各工具的模型槽位", "Adjust model slots for each tool")},
		{Label: u.T("退出", "Exit")},
	}
	pick, err := u.SelectWith(u.T("tf 首页", "tf home"), items,
		ui.SelectOpt{CancelHint: u.T("退出", "exit")})
	if err != nil {
		if ui.AsError(err).Code == ui.CodeCancelled {
			return 130
		}
		u.Fail("home", err)
		return 1
	}
	if pick == len(items)-1 {
		return 0
	}

	switch pick {
	case 0:
		return a.runHomeLaunch(u)
	case 1:
		_ = a.runSelected(u, []string{"login"})
		u.ClearScreen()
		return a.runHome(u)
	case 2:
		_ = a.runSelected(u, []string{"status"})
		a.waitHome(u)
		u.ClearScreen()
		return a.runHome(u)
	case 3:
		return a.runHomeModel(u)
	default:
		return 0
	}
}

func (a *App) waitHome(u *ui.UI) {
	_, _ = u.ReadLine(u.T("按 Enter 返回首页", "press Enter to return home"))
}

func (a *App) homeLaunchDetail(u *ui.UI, name string, st *state, installed bool) string {
	if !installed {
		return u.T("未安装", "not installed")
	}
	if st == nil {
		return u.T("配置无法读取", "configuration unavailable")
	}
	hc := st.cfg.Harness(name)
	keyName := hc.Key
	if keyName == "" {
		keyName = ""
		for _, candidate := range st.creds.Names() {
			if _, ok := st.creds.Get(candidate); ok {
				keyName = candidate
				break
			}
		}
	}
	if keyName == "" {
		if len(st.creds.Names()) == 0 {
			return u.T("未登录；启动时引导登录", "not signed in; sign-in on launch")
		}
		return u.T("多把 Key；启动时选择", "multiple keys; choose on launch")
	}
	detail := ""
	if modelID := hc.Slots[config.SlotDefault]; modelID != "" {
		detail = modelID
	} else {
		detail = u.T("首次启动选择模型", "choose a model on first launch")
	}
	if host := st.cfg.HostOf(keyName); host != "" && normalizeHost(host) != normalizeHost(config.DefaultHost) {
		detail += " · " + host
	}
	return detail
}

func (a *App) runHomeLaunch(u *ui.UI) int {
	for {
		var st *state
		if loaded, err := loadState(&Context{UI: u}); err == nil {
			st = loaded
		}
		items := make([]ui.Item, 0, len(harnessNames())+1)
		for _, name := range harnessNames() {
			if cmd := a.index[name]; cmd != nil {
				h, _ := harness.Lookup(name)
				installed := h != nil && h.DetectInstalled().Installed
				items = append(items, ui.Item{Label: cmd.Name, Detail: a.homeLaunchDetail(u, name, st, installed)})
			}
		}
		items = append(items, ui.Item{Label: u.T("返回首页", "Back to home")})
		pick, err := u.SelectWith(u.T("选择要启动的工具", "Choose a tool to launch"), items,
			ui.SelectOpt{CancelHint: u.T("返回首页", "back to home")})
		if err != nil {
			if ui.AsError(err).Code == ui.CodeCancelled {
				return a.runHome(u)
			}
			u.Fail("home", err)
			return 1
		}
		if pick == len(items)-1 {
			return a.runHome(u)
		}
		u.ClearScreen()
		_ = a.runSelected(u, []string{items[pick].Label})
		u.ClearScreen()
	}
}

func (a *App) runHomeModel(u *ui.UI) int {
	for {
		items := make([]ui.Item, 0, len(harnessNames())+1)
		for _, name := range harnessNames() {
			if h, ok := harness.Lookup(name); ok {
				items = append(items, ui.Item{Label: h.Name, Detail: h.Name})
			}
		}
		items = append(items, ui.Item{Label: u.T("返回首页", "Back to home")})
		pick, err := u.SelectWith(u.T("选择要编辑的工具", "Choose a tool to edit"), items,
			ui.SelectOpt{CancelHint: u.T("返回首页", "back to home")})
		if err != nil {
			if ui.AsError(err).Code == ui.CodeCancelled {
				return a.runHome(u)
			}
			u.Fail("home", err)
			return 1
		}
		if pick == len(items)-1 {
			return a.runHome(u)
		}
		// 编辑器里的取消只退回到这个 harness 选择层；下一轮重建列表。
		u.ClearScreen()
		_ = a.runSelected(u, []string{"model", items[pick].Label})
		u.ClearScreen()
	}
}

func (a *App) runSelected(u *ui.UI, argv []string) int {
	if len(argv) == 0 {
		return 0
	}
	cmd := a.index[argv[0]]
	if cmd == nil {
		u.Fail("home", ui.Errf(ui.CodeInternal, "home command is not registered: "+argv[0]))
		return 1
	}
	ctx, err := parse(cmd, argv[1:])
	if err != nil {
		u.Fail(cmd.Name, err)
		return 2
	}
	ctx.UI = u
	if err := cmd.Run(ctx); err != nil {
		var ec *exitCodeError
		if errors.As(err, &ec) {
			return ec.code
		}
		if ui.AsError(err).Code == ui.CodeCancelled {
			return 130
		}
		u.Fail(cmd.Name, err)
		return 1
	}
	u.Flush(cmd.Name)
	return 0
}

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
	u.Printf("\n%s\n  tf\n  tf <command> [flags]\n", u.Bold(u.T("用法", "USAGE")))

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
