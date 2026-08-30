package cli

import (
	"fmt"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/ui"
)

func newHarnessCommand() *Command {
	return &Command{
		Name:  "harness",
		Usage: "tkr harness [list|install <name>]",
		Summary: func(u *ui.UI) string {
			return u.T("查看与安装可启动的 harness", "Inspect and install supported harnesses")
		},
		Run: func(c *Context) error {
			action := "list"
			if len(c.Args) > 0 {
				action = c.Args[0]
			}
			switch action {
			case "list":
				return runHarnessList(c)
			case "install":
				if len(c.Args) < 2 {
					return ui.Errf(ui.CodeUsage,
						c.UI.T("需要指定 harness 名称", "a harness name is required")).
						WithHint("tkr harness install claude")
				}
				return runHarnessInstall(c, c.Args[1])
			default:
				return ui.Errf(ui.CodeUsage,
					fmt.Sprintf(c.UI.T("未知子命令：%s", "unknown subcommand: %s"), action)).
					WithHint("tkr harness [list|install <name>]")
			}
		},
	}
}

func runHarnessList(c *Context) error {
	type row struct {
		Name      string   `json:"name"`
		Installed bool     `json:"installed"`
		Path      string   `json:"path,omitempty"`
		Version   string   `json:"version,omitempty"`
		Slots     []string `json:"slots"`
	}

	var rows []row
	for _, h := range harness.All {
		st := h.Detect()
		slots := make([]string, 0, len(h.Slots))
		for _, s := range h.Slots {
			slots = append(slots, s.Name)
		}
		rows = append(rows, row{
			Name: h.Name, Installed: st.Installed,
			Path: st.Path, Version: st.Version, Slots: slots,
		})
	}

	c.UI.Emit("harness list", rows, func() {
		for _, r := range rows {
			mark := c.UI.Dim("—")
			detail := c.UI.Dim(c.UI.T("未安装", "not installed"))
			if r.Installed {
				mark = "✓"
				detail = r.Version
				if detail == "" {
					detail = r.Path
				}
			}
			c.UI.Printf("%s %s %s %s\n", mark, ui.Pad(r.Name, 10), ui.Pad(detail, 12),
				c.UI.Dim(fmt.Sprintf("%s %v", c.UI.T("模型槽：", "slots:"), r.Slots)))
		}
	})
	return nil
}

func runHarnessInstall(c *Context, name string) error {
	h, ok := harness.Lookup(name)
	if !ok {
		return ui.Errf(ui.CodeHarnessNotFound,
			fmt.Sprintf(c.UI.T("未知 harness：%s", "unknown harness: %s"), name)).
			WithHint("tkr harness list")
	}

	if st := h.Detect(); st.Installed {
		c.UI.Emit("harness install", map[string]any{
			"name": h.Name, "installed": true, "path": st.Path, "version": st.Version,
		}, func() {
			c.UI.Printf("%s %s %s\n", "✓", h.Name,
				c.UI.Dim(fmt.Sprintf("%s (%s)", st.Version, st.Path)))
		})
		return nil
	}

	return EnsureInstalled(c, h)
}

// EnsureInstalled 在 harness 缺失时征求用户意见。M3 启动流程会复用它。
//
// 护栏（见 docs/design/open-decisions.md E 项）：
//   - 非交互环境一律拒绝安装，只打印命令并以错误退出。
//   - 完整命令必须先展示，用户看着它做选择。
//   - 绝不 sudo，绝不替用户挑包管理器。
//   - 安装失败时原样透出底层错误。
func EnsureInstalled(c *Context, h *harness.Harness) error {
	options := h.AvailableInstalls()

	// 本机没有任何可用的包管理器：只能给出命令。
	if len(options) == 0 {
		return notInstalledErr(c, h, h.Installs)
	}

	if !c.UI.Interactive(c.Flags.Bool("yes")) {
		return notInstalledErr(c, h, options)
	}

	items := make([]ui.Item, 0, len(options)+1)
	for _, o := range options {
		items = append(items, ui.Item{Label: o.Command(), Detail: o.Manager})
	}
	items = append(items, ui.Item{Label: c.UI.T("退出", "quit")})

	idx, err := c.UI.Select(
		fmt.Sprintf(c.UI.T("未检测到 %s，要现在装吗？", "%s is not installed. Install it now?"), h.Name),
		items,
	)
	if err != nil {
		return err
	}
	if idx == len(options) {
		return ui.Errf(ui.CodeCancelled, c.UI.T("已取消", "cancelled"))
	}

	chosen := options[idx]
	c.UI.Logf("\n$ %s\n", chosen.Command())

	// 安装过程的输出全部给用户看，失败时不做任何包装。
	if err := harness.Install(chosen, c.UI.Err, c.UI.Err); err != nil {
		return ui.Errf(ui.CodeInstallFailed,
			fmt.Sprintf(c.UI.T("安装失败：%s", "install failed: %s"), chosen.Command())).
			WithCause(err)
	}

	st := h.Detect()
	if !st.Installed {
		return ui.Errf(ui.CodeInstallIncomplete,
			c.UI.T("安装命令已执行，但仍找不到可执行文件；可能不在 PATH 中",
				"the install command finished but the binary is still not on PATH")).
			WithHint(chosen.Command())
	}

	recordInstall(c, h.Name, chosen, st.Version)
	c.UI.Logf("✓ %s %s", h.Name, st.Version)
	return nil
}

// notInstalledErr 构造「没装且不能替你装」的错误，附上可复制的命令。
func notInstalledErr(c *Context, h *harness.Harness, options []harness.InstallOption) error {
	hint := ""
	if len(options) > 0 {
		hint = options[0].Command()
	}
	return ui.Errf(ui.CodeHarnessNotInstalled,
		fmt.Sprintf(c.UI.T("未安装 %s", "%s is not installed"), h.Name)).
		WithHint(hint)
}

// recordInstall 登记安装来源，供 doctor 回溯。写失败不影响主流程。
func recordInstall(c *Context, name string, opt harness.InstallOption, version string) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return
	}
	cfg.RecordInstall(name, config.InstallRecord{
		Manager: opt.Manager,
		Command: opt.Command(),
		Version: version,
	})
	if err := cfg.Save(); err != nil {
		c.UI.Warnf(c.UI.T("安装已完成，但记录安装来源失败：%v",
			"installed, but recording the install source failed: %v"), err)
	}
}
