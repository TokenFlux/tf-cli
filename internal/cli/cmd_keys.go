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
		Flags: []Flag{
			{Name: "refresh", Kind: KindBool,
				Desc: "重新探测各 Key 的准入情况|Re-probe what each key allows"},
		},
		Run: runKeys,
	}
}

func runKeys(c *Context) error {
	st, err := loadState(c)
	if err != nil {
		return err
	}
	cfg, creds := st.cfg, st.creds

	names := creds.Names()
	if len(names) > 0 && c.Flags.Bool("refresh") {
		reprobe(c, cfg, creds, names)
	}
	if len(names) == 0 {
		return ui.Errf(ui.CodeNotLoggedIn, c.UI.T("本机没有保存任何 Key", "no keys are stored on this machine")).
			WithHint("tkr login")
	}

	type scope struct {
		Prefix    string   `json:"prefix,omitempty"`
		Protocols []string `json:"protocols,omitempty"`
		Harnesses []string `json:"harnesses"`
	}
	type row struct {
		Name    string   `json:"name"`
		Host    string   `json:"host"`
		Key     string   `json:"key"`
		Scopes  []scope  `json:"scopes"`
		BoundTo []string `json:"bound_to,omitempty"`
		Probed  bool     `json:"probed"`
	}

	rows := make([]row, 0, len(names))
	for _, name := range names {
		cred, _ := creds.Get(name)
		meta := cfg.Keys[name]
		r := row{Name: name, Host: cfg.HostOf(name), Key: config.Mask(cred.Key), Probed: meta.Probed()}

		// 复合 Key 一把横跨多个分组，各分组能跑的 harness 不同。
		// 笼统地说这把 Key「能跑 claude codex」是谎报。
		for _, prefix := range meta.Scopes() {
			sc := scope{Prefix: prefix, Harnesses: runnableIn(meta, prefix)}
			if meta.LockedToClaudeCode(prefix) {
				sc.Protocols = []string{"claude-code-only"}
			} else if meta != nil {
				sc.Protocols = meta.Protocols[prefix]
			}
			r.Scopes = append(r.Scopes, sc)
		}
		for _, h := range harness.All {
			if hc, ok := cfg.Harnesses[h.Name]; ok && hc.Key == name {
				r.BoundTo = append(r.BoundTo, h.Name)
			}
		}
		rows = append(rows, r)
	}

	c.UI.Emit("keys", rows, func() {
		for _, r := range rows {
			c.UI.Printf("%s  %s\n", c.UI.Bold(r.Name), c.UI.Dim(r.Key))
			for _, sc := range r.Scopes {
				label := sc.Prefix
				if label == "" {
					label = c.UI.T("可跑", "can run")
				}
				c.UI.Printf("  %s %s\n", ui.Pad(label, 10), strings.Join(sc.Harnesses, " "))
			}
			if len(r.BoundTo) > 0 {
				c.UI.Printf("  %s %s\n", ui.Pad(c.UI.T("已绑定", "bound to"), 10), strings.Join(r.BoundTo, " "))
			}
			if !r.Probed {
				c.UI.Printf("  %s\n", c.UI.Dim(c.UI.T("协议未探测，启动时会自动补上",
					"protocols not probed yet; will be filled in at launch")))
			}
		}
	})
	return nil
}
