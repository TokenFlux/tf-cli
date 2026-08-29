package cli

import (
	"fmt"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/ui"
)

func newLogoutCommand() *Command {
	return &Command{
		Name:  "logout",
		Usage: "tkr logout [--all]",
		Summary: func(u *ui.UI) string {
			return u.T("删除本机保存的 API Key", "Remove the API key stored on this machine")
		},
		Flags: []Flag{
			{Name: "all", Kind: KindBool, Desc: "删除全部 profile 的凭据|Remove credentials for every profile"},
		},
		Run: runLogout,
	}
}

func runLogout(c *Context) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return ui.Errf(ui.CodeConfigRead, c.UI.T("无法定位配置目录", "cannot locate the config directory")).WithCause(err)
	}
	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		return ui.Errf(ui.CodeCredentialsRead, c.UI.T("凭据文件无法读取", "cannot read the credentials file")).WithCause(err)
	}

	var removed []string
	if c.Flags.Bool("all") {
		removed = creds.Names()
		creds.Clear()
	} else {
		name := c.Flags.String("profile")
		if name == "" {
			cfg, err := config.Load(paths)
			if err != nil {
				return ui.Errf(ui.CodeConfigRead, c.UI.T("配置文件无法读取", "cannot read the config file")).WithCause(err)
			}
			name = cfg.Current
		}
		if _, ok := creds.Get(name); !ok {
			return ui.Errf(ui.CodeNotLoggedIn,
				fmt.Sprintf(c.UI.T("profile %q 本来就没有保存凭据", "profile %q has no stored credential"), name))
		}
		creds.Remove(name)
		removed = []string{name}
	}

	if err := creds.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("凭据无法写入", "cannot write credentials")).WithCause(err)
	}

	// 模型列表是由这把 Key 推导出来的，退出登录时一并清掉，
	// 免得补全继续泄露「这把 Key 能看到哪些模型」。
	_ = paths.RemoveCache("models")

	host := config.DefaultHost
	if cfg, err := config.Load(paths); err == nil {
		if p, ok := cfg.Profile(""); ok && p.Host != "" {
			host = p.Host
		}
	}

	c.UI.Emit("logout", map[string]any{"removed": removed}, func() {
		c.UI.Printf("✓ %s\n", fmt.Sprintf(
			c.UI.T("已删除本机凭据：%v", "removed local credentials: %v"), removed))
		// 必须说清楚：删本地文件不等于吊销 Key。
		c.UI.Printf("  %s\n", c.UI.Dim(c.UI.T(
			"这只删除了本机保存的副本，Key 在服务端依然有效。要真正吊销请到：",
			"this only deletes the local copy; the key remains valid server-side. To revoke it:")))
		c.UI.Printf("  %s\n", c.UI.Dim(host+"/keys"))
	})
	return nil
}
