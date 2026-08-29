package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/ui"
)

func newModelCommand() *Command {
	return &Command{
		Name:  "model",
		Usage: "tkr model [<harness>] [--set slot=model ...] [--reset] [--list]",
		Summary: func(u *ui.UI) string {
			return u.T("查看与修改各 harness 的模型槽", "Inspect and change per-harness model slots")
		},
		Flags: []Flag{
			{Name: "list", Kind: KindBool, Desc: "列出全部 harness 的槽位|List slots for every harness"},
			{Name: "set", Kind: KindString, Desc: "设置一个槽，形如 slot=model|Set one slot, as slot=model"},
			{Name: "reset", Kind: KindBool, Desc: "清空该 harness 的槽位|Clear this harness's slots"},
		},
		Run: runModel,
	}
}

func runModel(c *Context) error {
	st, err := loadState(c)
	if err != nil {
		return err
	}
	cfg := st.cfg

	// 不指定 harness 时列出全部。
	if len(c.Args) == 0 {
		return listModelSlots(c, cfg)
	}

	name := c.Args[0]
	h, ok := harness.Lookup(name)
	if !ok {
		return ui.Errf(ui.CodeHarnessNotFound,
			fmt.Sprintf(c.UI.T("未知 harness：%s", "unknown harness: %s"), name)).
			WithHint("tkr harness list")
	}

	if c.Flags.Bool("reset") {
		cfg.Harness(h.Name).Slots = config.ModelSlots{}
		if err := cfg.Save(); err != nil {
			return ui.Errf(ui.CodeConfigWrite, c.UI.T("配置无法写入", "cannot write config")).WithCause(err)
		}
		c.UI.Emit("model reset", map[string]string{"harness": h.Name}, func() {
			c.UI.Printf("%s\n", fmt.Sprintf(c.UI.T("已清空 %s 的槽位，下次启动会重新引导。",
				"cleared %s slots; the next launch will ask again."), h.Name))
		})
		return nil
	}

	if c.Flags.Present("set") {
		slot, model, found := strings.Cut(c.Flags.String("set"), "=")
		if !found || slot == "" || model == "" {
			return ui.Errf(ui.CodeUsage,
				c.UI.T("--set 需要 slot=model 形式", "--set expects slot=model")).
				WithHint(fmt.Sprintf("tkr model %s --set default=<model>", h.Name))
		}
		if !hasSlot(h, slot) {
			return ui.Errf(ui.CodeUsage,
				fmt.Sprintf(c.UI.T("%s 没有名为 %q 的槽", "%s has no slot named %q"), h.Name, slot)).
				WithHint(c.UI.T("可用槽位：", "available slots: ") + slotList(h))
		}
		cfg.Harness(h.Name).Slots[slot] = model
		if err := cfg.Save(); err != nil {
			return ui.Errf(ui.CodeConfigWrite, c.UI.T("配置无法写入", "cannot write config")).WithCause(err)
		}
	}

	return showHarnessSlots(c, cfg, h)
}

func hasSlot(h *harness.Harness, slot string) bool {
	for _, s := range h.Slots {
		if s.Name == slot {
			return true
		}
	}
	return false
}

func slotList(h *harness.Harness) string {
	names := make([]string, 0, len(h.Slots))
	for _, s := range h.Slots {
		names = append(names, s.Name)
	}
	return strings.Join(names, ", ")
}

func listModelSlots(c *Context, cfg *config.Config) error {
	type entry struct {
		Harness string            `json:"harness"`
		Slots   map[string]string `json:"slots"`
	}
	var out []entry
	for _, h := range harness.All {
		out = append(out, entry{Harness: h.Name, Slots: cfg.Harness(h.Name).Slots})
	}

	c.UI.Emit("model list", out, func() {
		for _, h := range harness.All {
			c.UI.Printf("%s\n", c.UI.Bold(h.Name))
			printSlots(c, cfg.Harness(h.Name).Slots, h)
		}
	})
	return nil
}

func showHarnessSlots(c *Context, cfg *config.Config, h *harness.Harness) error {
	slots := cfg.Harness(h.Name).Slots
	c.UI.Emit("model", map[string]any{"harness": h.Name, "slots": slots}, func() {
		c.UI.Printf("%s\n", c.UI.Bold(h.Name))
		printSlots(c, slots, h)
	})
	return nil
}

// printSlots 必须显式标出未配置的槽 —— 未配置意味着 harness 会用它的
// 内置默认模型，而那个模型多半不在用户的分组里。
func printSlots(c *Context, slots config.ModelSlots, h *harness.Harness) {
	names := make([]string, 0, len(h.Slots))
	for _, s := range h.Slots {
		names = append(names, s.Name)
	}
	sort.Strings(names)

	for _, s := range h.Slots {
		v := slots[s.Name]
		if v == "" {
			mark := c.UI.T("未配置", "unset")
			if s.Required {
				mark = c.UI.T("未配置（启动时会询问）", "unset (will ask at launch)")
			}
			c.UI.Printf("  %-9s %s %s\n", s.Name, c.UI.Dim(mark),
				c.UI.Dim("— "+s.Purpose(c.UI.Lang == ui.LangZH)))
			continue
		}
		c.UI.Printf("  %-9s %-18s %s\n", s.Name, v,
			c.UI.Dim("— "+s.Purpose(c.UI.Lang == ui.LangZH)))
	}
}
