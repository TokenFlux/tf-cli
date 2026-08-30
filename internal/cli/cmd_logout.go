package cli

import (
	"fmt"
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/ui"
)

func newLogoutCommand() *Command {
	return &Command{
		Name:  "logout",
		Usage: "tf logout [<名字>] [--all]",
		Summary: func(u *ui.UI) string {
			return u.T("删除本机的 Key", "Remove a key stored here")
		},
		Flags: []Flag{
			{Name: "all", Kind: KindBool, Desc: "删除本机保存的所有 Key||Remove every key stored locally"},
		},
		Run: runLogout,
	}
}

// pickProfile 确定要删哪一把。
//
// 优先级：位置参数 > --profile > 只有一把时直接选它 > 交互选择。
// 多把凭据下绝不静默猜：删错一把就要重新去网页拿 Key。
func pickProfile(c *Context, creds *config.Credentials, stored []string) (string, error) {
	if len(c.Args) > 0 {
		return c.Args[0], nil
	}
	if name := c.Flags.String("key"); name != "" {
		return name, nil
	}
	if len(stored) == 1 {
		return stored[0], nil
	}

	if !c.UI.Interactive(c.Flags.Bool("yes")) {
		return "", ui.Errf(ui.CodeUsage,
			c.UI.T("存了多把 Key，指定删哪一把", "several keys are stored; name the one to remove")).
			WithHint("tf logout " + strings.Join(stored, " | "))
	}

	items := make([]ui.Item, 0, len(stored))
	for _, name := range stored {
		cred, _ := creds.Get(name)
		items = append(items, ui.Item{Label: name, Detail: config.Mask(cred.Key)})
	}
	idx, err := c.UI.Select(c.UI.T("删除哪一把 Key？", "Which key should be removed?"), items)
	if err != nil {
		return "", err
	}
	return stored[idx], nil
}

func runLogout(c *Context) error {
	st, err := loadState(c)
	if err != nil {
		return err
	}
	paths, creds := st.paths, st.creds

	stored := creds.Names()
	if len(stored) == 0 {
		return ui.Errf(ui.CodeNotLoggedIn,
			c.UI.T("本机没有保存任何 Key", "no keys are stored on this machine")).
			WithHint("tf login")
	}

	var removed []string
	switch {
	case c.Flags.Bool("all"):
		removed = stored
		creds.Clear()
	default:
		name, err := pickProfile(c, creds, stored)
		if err != nil {
			return err
		}
		cred, ok := creds.Get(name)
		if !ok {
			return ui.Errf(ui.CodeNotLoggedIn,
				fmt.Sprintf(c.UI.T("%q 下没有 Key", "%q has no stored key"), name)).
				WithHint(c.UI.T("已保存的：", "stored: ") + strings.Join(stored, " "))
		}
		c.UI.Logf("%s", c.UI.Dim(fmt.Sprintf("%s  %s", name, config.Mask(cred.Key))))
		creds.Remove(name)
		removed = []string{name}
	}

	if err := creds.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("凭据文件无法写入", "cannot write the credentials file")).WithCause(err)
	}

	// 模型列表是由这把 Key 推导出来的，退出登录时一并清掉，
	// 免得补全继续泄露「这把 Key 能看到哪些模型」。

	host := config.DefaultHost
	if cfg, err := config.Load(paths); err == nil && len(removed) > 0 {
		host = cfg.HostOf(removed[0])
		// 同时清掉元数据与指向它的绑定，避免留下悬空引用。
		for _, name := range removed {
			delete(cfg.Keys, name)
			for _, hc := range cfg.Harnesses {
				if hc.Key == name {
					hc.Key = ""
				}
			}
		}
		_ = cfg.Save()
	}

	c.UI.Emit("logout", map[string]any{"removed": removed}, func() {
		c.UI.Printf("✓ %s\n", fmt.Sprintf(
			c.UI.T("已删除本机的 Key：%s", "removed local keys: %s"),
			strings.Join(removed, ", ")))
		// 必须说清楚：删本地文件不等于吊销 Key。
		c.UI.Printf("  %s\n", c.UI.Dim(c.UI.T(
			"服务端的 Key 仍然有效，吊销请到：",
			"the key still works server-side; revoke it at:")))
		c.UI.Printf("  %s\n", c.UI.Dim(host+"/keys"))
	})
	return nil
}
