package cli

import (
	"strings"

	"github.com/tokenflux/tkr/internal/access"
	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/ui"
)

func newKeysCommand() *Command {
	return &Command{
		Name:  "keys",
		Usage: "tf keys",
		Summary: func(u *ui.UI) string {
			return u.T("列出本机的 Key", "List the keys stored here")
		},
		Flags: []Flag{
			{Name: "refresh", Kind: KindBool,
				Desc: "重新探测各 Key 的准入情况||Re-probe what each key allows"},
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
			WithHint("tf login")
	}

	type scope struct {
		Prefix    string   `json:"prefix,omitempty"`
		Protocols []string `json:"protocols,omitempty"`
		Harnesses []string `json:"harnesses"`
	}
	type row struct {
		Name   string  `json:"name"`
		Host   string  `json:"host"`
		Key    string  `json:"key"`
		Scopes []scope `json:"scopes"`
		Probed bool    `json:"probed"`
	}

	rows := make([]row, 0, len(names))
	for _, name := range names {
		cred, _ := creds.Get(name)
		meta := cfg.Keys[name]
		r := row{Name: name, Host: cfg.HostOf(name), Key: config.Mask(cred.Key), Probed: meta.Probed()}

		// 复合 Key 一把横跨多个分组，各分组能跑的 harness 不同。
		// 笼统地说这把 Key「能跑 claude codex」是谎报。
		for _, prefix := range meta.Scopes() {
			sc := scope{Prefix: prefix, Harnesses: access.RunnableIn(meta, prefix)}
			if meta.LockedToClaudeCode(prefix) {
				sc.Protocols = []string{"claude-code-only"}
			} else if meta != nil {
				sc.Protocols = meta.Protocols[prefix]
			}
			r.Scopes = append(r.Scopes, sc)
		}
		rows = append(rows, r)
	}

	c.UI.Emit("keys", rows, func() {
		for _, r := range rows {
			c.UI.Printf("%s  %s\n", c.UI.Bold(r.Name), c.UI.Dim(r.Key))
			for _, sc := range r.Scopes {
				label := sc.Prefix
				if label == "" {
					label = c.UI.T("可用于", "can run")
				} else {
					// 加斜杠才看得出这是分组前缀，而不是又一个标签。
					// 同一列里混着标签和数据时，「可用于」会被读成分组名。
					// 斜杠也正好对上模型 ID 的写法：ccmax/claude-opus-5。
					label += "/"
				}
				c.UI.Printf("  %s %s\n", ui.Pad(label, 10), strings.Join(sc.Harnesses, " "))
			}
			if !r.Probed {
				c.UI.Printf("  %s\n", c.UI.Dim(c.UI.T("启动时自动检查", "checked at launch")))
			}
		}
	})
	return nil
}
