package cli

import (
	"fmt"
	"os"

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
				"name":    "tkr",
				"version": buildinfo.Version,
				"commit":  buildinfo.Commit,
			}, func() {
				c.UI.Printf("tkr %s\n", buildinfo.Version)
			})
			return nil
		},
	}
}

func newConfigCommand() *Command {
	return &Command{
		Name:  "config",
		Usage: "tkr config [path|show]",
		Summary: func(u *ui.UI) string {
			return u.T("查看配置与凭据的位置和状态", "Inspect config and credential locations")
		},
		Run: func(c *Context) error {
			paths, err := config.DefaultPaths()
			if err != nil {
				return ui.Errf(ui.CodeConfigRead,
					c.UI.T("无法定位配置目录", "cannot locate the config directory")).WithCause(err)
			}

			action := "show"
			if len(c.Args) > 0 {
				action = c.Args[0]
			}

			switch action {
			case "path":
				return runConfigPath(c, paths)
			case "show":
				return runConfigShow(c, paths)
			default:
				return ui.Errf(ui.CodeUsage,
					fmt.Sprintf(c.UI.T("未知子命令：%s", "unknown subcommand: %s"), action)).
					WithHint("tkr config [path|show]")
			}
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
		c.UI.Printf("%-14s %s\n", c.UI.T("配置目录", "config dir"), paths.ConfigDir)
		c.UI.Printf("%-14s %s\n", c.UI.T("配置文件", "config file"), paths.ConfigFile())
		c.UI.Printf("%-14s %s\n", c.UI.T("凭据文件", "credentials"), paths.CredentialsFile())
		c.UI.Printf("%-14s %s\n", c.UI.T("缓存目录", "cache dir"), paths.CacheDir)
	})
	return nil
}

func runConfigShow(c *Context, paths config.Paths) error {
	cfg, err := config.Load(paths)
	if err != nil {
		return ui.Errf(ui.CodeConfigRead,
			c.UI.T("配置文件无法读取", "cannot read the config file")).
			WithHint(paths.ConfigFile()).WithCause(err)
	}

	// 首次运行时把默认配置落盘，让用户有一个可编辑的起点。
	created := false
	if _, statErr := os.Stat(paths.ConfigFile()); os.IsNotExist(statErr) {
		if err := cfg.Save(); err != nil {
			return ui.Errf(ui.CodeConfigWrite,
				c.UI.T("配置文件无法写入", "cannot write the config file")).
				HintPath(paths.ConfigDir).WithCause(err)
		}
		created = true
	}

	creds, repaired, err := config.LoadCredentials(paths)
	if err != nil {
		return ui.Errf(ui.CodeCredentialsRead,
			c.UI.T("凭据文件无法读取", "cannot read the credentials file")).
			HintPath(paths.CredentialsFile()).WithCause(err)
	}
	if repaired {
		c.UI.Warnf(c.UI.T(
			"凭据文件权限过宽，已收紧为 0600：%s",
			"credentials file had loose permissions, tightened to 0600: %s",
		), paths.CredentialsFile())
	}

	profileName := c.Flags.String("profile")
	if profileName == "" {
		profileName = cfg.Current
	}
	profile, ok := cfg.Profile(profileName)
	if !ok {
		return ui.Errf(ui.CodeProfileNotFound,
			fmt.Sprintf(c.UI.T("找不到 profile：%s", "no such profile: %s"), profileName)).
			WithHint("tkr config show")
	}

	host := profile.Host
	if h := c.Flags.String("host"); h != "" {
		host = h
	}

	cred, loggedIn := creds.Get(profileName)
	keyDisplay := ""
	source := ""
	if loggedIn {
		keyDisplay = config.Mask(cred.Key)
		source = cred.Source
	}

	c.UI.Emit("config show", map[string]any{
		"profile":     profileName,
		"host":        host,
		"logged_in":   loggedIn,
		"key":         keyDisplay,
		"key_source":  source,
		"config_file": paths.ConfigFile(),
		"created":     created,
	}, func() {
		c.UI.Printf("%-10s %s\n", "profile", profileName)
		c.UI.Printf("%-10s %s\n", "host", host)
		if loggedIn {
			c.UI.Printf("%-10s %s %s\n", "key", keyDisplay, c.UI.Dim("("+source+")"))
		} else {
			c.UI.Printf("%-10s %s\n", "key", c.UI.Dim(c.UI.T("未登录", "not logged in")))
			c.UI.Logf("%s", c.UI.Dim(c.UI.T(
				"运行 tkr login 以保存 API Key。",
				"Run `tkr login` to store an API key.",
			)))
		}
		if created {
			c.UI.Logf("%s", c.UI.Dim(fmt.Sprintf(c.UI.T(
				"已创建默认配置：%s", "created default config: %s",
			), paths.ConfigFile())))
		}
	})
	return nil
}
