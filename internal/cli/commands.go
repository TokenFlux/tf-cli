package cli

import (
	"github.com/tokenflux/tkr/internal/buildinfo"
	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/ui"
)

func newVersionCommand() *Command {
	return &Command{
		Name:    "version",
		Aliases: []string{"v"},
		Summary: func(u *ui.UI) string {
			return u.T("显示版本信息", "Show version information")
		},
		Run: func(c *Context) error {
			c.UI.Emit("version", map[string]string{
				"name":    "tf",
				"version": buildinfo.Version,
				"commit":  buildinfo.Commit,
			}, func() {
				c.UI.Printf("tf %s\n", buildinfo.Version)
			})
			return nil
		},
	}
}

// newConfigCommand 只负责「东西在哪」。
//
// 「有哪些 Key、各自能跑什么」归 tf keys —— 曾经两个命令各打印一遍，
// 同一份信息两处维护、两处会走样。
func newConfigCommand() *Command {
	return &Command{
		Name:  "config",
		Usage: "tf config",
		Summary: func(u *ui.UI) string {
			return u.T("显示配置文件位置", "Show where the files live")
		},
		Run: func(c *Context) error {
			paths, err := config.DefaultPaths()
			if err != nil {
				return ui.Errf(ui.CodeConfigRead,
					c.UI.T("无法定位配置目录", "cannot locate the config directory")).WithCause(err)
			}
			return runConfigPath(c, paths)
		},
	}
}

func runConfigPath(c *Context, paths config.Paths) error {
	data := map[string]string{
		"config_dir":  paths.ConfigDir,
		"config_file": paths.ConfigFile(),
		"credentials": paths.CredentialsFile(),
		"cache_dir":   paths.CacheDir,
	}
	c.UI.Emit("config path", data, func() {
		c.UI.Printf("%s %s\n", ui.Pad(c.UI.T("配置目录", "config dir"), 14), paths.ConfigDir)
		c.UI.Printf("%s %s\n", ui.Pad(c.UI.T("配置文件", "config file"), 14), paths.ConfigFile())
		c.UI.Printf("%s %s\n", ui.Pad(c.UI.T("凭据文件", "credentials"), 14), paths.CredentialsFile())
		c.UI.Printf("%s %s\n", ui.Pad(c.UI.T("缓存目录", "cache dir"), 14), paths.CacheDir)
	})
	return nil
}
