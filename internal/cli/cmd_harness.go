package cli

import (
	"fmt"
	"strings"

	"github.com/tokenflux/tf-cli/internal/harness"
	"github.com/tokenflux/tf-cli/internal/ui"
)

func newHarnessCommand() *Command {
	return &Command{
		Name:  "harness",
		Usage: "tf harness [list|install <name>]",
		Summary: func(u *ui.UI) string {
			return u.T("查看与安装 harness", "Inspect and install harnesses")
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
						WithHint("tf harness install claude")
				}
				return runHarnessInstall(c, c.Args[1])
			default:
				return ui.Errf(ui.CodeUsage,
					fmt.Sprintf(c.UI.T("未知子命令：%s", "unknown subcommand: %s"), action)).
					WithHint("tf harness [list|install <name>]")
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
				c.UI.Dim(fmt.Sprintf("%s %s", c.UI.T("模型槽", "slots"), strings.Join(r.Slots, " "))))
		}
	})
	return nil
}

func runHarnessInstall(c *Context, name string) error {
	h, ok := harness.Lookup(name)
	if !ok {
		return ui.Errf(ui.CodeHarnessNotFound,
			fmt.Sprintf(c.UI.T("没有名为 %q 的 harness", "no harness named %q"), name)).
			WithHint("tf harness list")
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

	// 本机没有任何可用的包管理器：只能给出命令，并说明它现在还跑不了。
	if len(options) == 0 {
		return notInstalledErr(c, h, h.Installs, false)
	}

	if !c.UI.Interactive(c.Flags.Bool("no-input")) {
		return notInstalledErr(c, h, options, true)
	}

	items := make([]ui.Item, 0, len(options)+1)
	for _, o := range options {
		items = append(items, ui.Item{Label: o.Command(), Detail: o.Manager})
	}
	items = append(items, ui.Item{Label: c.UI.T("退出", "quit")})

	idx, err := c.UI.Select(
		fmt.Sprintf(c.UI.T("未检测到 %s，现在安装？", "%s is not installed. Install it now?"), h.Name),
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

	c.UI.Logf("✓ %s %s", h.Name, st.Version)
	return nil
}

// notInstalledErr 构造「没装且不能替你装」的错误，附上可复制的命令。
//
// available 为假表示本机连那个包管理器都没有。这时必须说出来：
// 实测在一台没有 npm 的 Ubuntu 上，tf 给的建议是 npm install -g ...，
// 用户照做只会得到 command not found，白跑一趟。
func notInstalledErr(c *Context, h *harness.Harness, options []harness.InstallOption, available bool) error {
	hint := ""
	if len(options) > 0 {
		hint = options[0].Command()
		if !available {
			hint = fmt.Sprintf(c.UI.T("本机没有 %s，装上它再运行：%s",
				"%s is not on this machine; install it, then run: %s"),
				options[0].Manager, hint)
		}
	}
	return ui.Errf(ui.CodeHarnessNotInstalled,
		fmt.Sprintf(c.UI.T("未安装 %s", "%s is not installed"), h.Name)).
		WithHint(hint)
}
