package cli

import (
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/ui"
)

func newKeysCommand() *Command {
	return &Command{
		Name:  "keys",
		Usage: "tkr keys",
		Summary: func(u *ui.UI) string {
			return u.T("列出本机的 Key 及其能跑的 harness", "List local keys and what each can run")
		},
		Run: runKeys,
	}
}

func runKeys(c *Context) error {
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

	names := creds.Names()
	if len(names) == 0 {
		return ui.Errf(ui.CodeNotLoggedIn, c.UI.T("本机没有保存任何 Key", "no keys are stored on this machine")).
			WithHint("tkr login")
	}

	type row struct {
		Name      string   `json:"name"`
		Host      string   `json:"host"`
		Key       string   `json:"key"`
		Protocols []string `json:"protocols,omitempty"`
		Harnesses []string `json:"harnesses"`
		BoundTo   []string `json:"bound_to,omitempty"`
	}
	rows := make([]row, 0, len(names))
	for _, name := range names {
		cred, _ := creds.Get(name)
		meta := cfg.Keys[name]
		r := row{Name: name, Host: cfg.HostOf(name), Key: config.Mask(cred.Key)}
		if meta != nil {
			r.Protocols = meta.ProtocolSummary()
		}
		for _, h := range harness.All {
			if meta.Supports(string(h.Protocol)) {
				r.Harnesses = append(r.Harnesses, h.Name)
			}
		}
		for _, hname := range sortedHarnessNames(cfg) {
			if cfg.Harnesses[hname].Key == name {
				r.BoundTo = append(r.BoundTo, hname)
			}
		}
		rows = append(rows, r)
	}

	c.UI.Emit("keys", rows, func() {
		for _, r := range rows {
			c.UI.Printf("%s  %s\n", c.UI.Bold(r.Name), c.UI.Dim(r.Key))
			c.UI.Printf("  %-10s %s\n", c.UI.T("可跑", "can run"), strings.Join(r.Harnesses, " "))
			if len(r.BoundTo) > 0 {
				c.UI.Printf("  %-10s %s\n", c.UI.T("已绑定", "bound to"), strings.Join(r.BoundTo, " "))
			}
			if len(r.Protocols) == 0 {
				c.UI.Printf("  %s\n", c.UI.Dim(c.UI.T("协议未探测，启动时会自动补上", "protocols not probed yet; will be filled in at launch")))
			}
		}
	})
	return nil
}

func sortedHarnessNames(cfg *config.Config) []string {
	out := make([]string, 0, len(cfg.Harnesses))
	for _, h := range harness.All {
		if _, ok := cfg.Harnesses[h.Name]; ok {
			out = append(out, h.Name)
		}
	}
	return out
}
