package cli

import (
	"fmt"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/ui"
)

func newUseCommand() *Command {
	return &Command{
		Name:  "use",
		Usage: "tkr use [<profile>]",
		Summary: func(u *ui.UI) string {
			return u.T("切换当前 profile", "Switch the current profile")
		},
		Run: runUse,
	}
}

func runUse(c *Context) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return ui.Errf(ui.CodeConfigRead, c.UI.T("无法定位配置目录", "cannot locate the config directory")).WithCause(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return ui.Errf(ui.CodeConfigRead, c.UI.T("配置文件无法读取", "cannot read the config file")).WithCause(err)
	}
	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		return ui.Errf(ui.CodeCredentialsRead, c.UI.T("凭据文件无法读取", "cannot read the credentials file")).WithCause(err)
	}

	names := cfg.ProfileNames()
	if len(names) == 0 {
		return ui.Errf(ui.CodeNotLoggedIn, c.UI.T("还没有任何 profile", "no profiles exist yet")).
			WithHint("tkr login")
	}

	target := ""
	if len(c.Args) > 0 {
		target = c.Args[0]
	}
	if target == "" {
		if !c.UI.Interactive(c.Flags.Bool("yes")) {
			// 非交互下只报告现状，不猜测意图。
			c.UI.Emit("use", map[string]any{"current": cfg.Current, "profiles": names}, func() {
				for _, n := range names {
					mark := "  "
					if n == cfg.Current {
						mark = "❯ "
					}
					c.UI.Printf("%s%s\n", mark, n)
				}
			})
			return nil
		}
		items := make([]ui.Item, 0, len(names))
		for _, n := range names {
			it := ui.Item{Label: n}
			if cred, ok := creds.Get(n); ok {
				it.Detail = config.Mask(cred.Key)
			} else {
				it.Detail = c.UI.T("未登录", "not logged in")
			}
			if n == cfg.Current {
				it.Note = c.UI.Dim(c.UI.T("← 当前", "← current"))
			}
			items = append(items, it)
		}
		idx, err := c.UI.Select(c.UI.T("切换到哪个 profile？", "Switch to which profile?"), items)
		if err != nil {
			return err
		}
		target = names[idx]
	}

	if _, ok := cfg.Profile(target); !ok {
		return ui.Errf(ui.CodeUsage,
			fmt.Sprintf(c.UI.T("没有名为 %q 的 profile", "no profile named %q"), target)).
			WithHint("tkr use")
	}

	cfg.Current = target
	if err := cfg.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("配置无法写入", "cannot write config")).WithCause(err)
	}

	// 切到一个没有凭据的 profile 是合法的，但下一条命令就会失败，
	// 所以现在就说清楚。
	_, hasCred := creds.Get(target)

	c.UI.Emit("use", map[string]any{"current": target, "logged_in": hasCred}, func() {
		c.UI.Printf("✓ %s\n", fmt.Sprintf(c.UI.T("当前 profile：%s", "current profile: %s"), target))
		if !hasCred {
			c.UI.Printf("  %s\n", c.UI.Dim(c.UI.T("该 profile 还没有 Key，运行 tkr login",
				"this profile has no key yet; run tkr login")))
		}
	})
	return nil
}
