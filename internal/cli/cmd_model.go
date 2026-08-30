package cli

import (
	"fmt"
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/model"
	"github.com/tokenflux/tkr/internal/ui"
)

func newModelCommand() *Command {
	return &Command{
		Name:  "model",
		Usage: "tf model [<harness>] [--set slot=model ...] [--reset] [--list]",
		Summary: func(u *ui.UI) string {
			return u.T("查看与修改模型槽", "Inspect and change model slots")
		},
		Flags: []Flag{
			{Name: "list", Kind: KindBool, Desc: "列出全部 harness 的槽位||List slots for every harness"},
			{Name: "set", Kind: KindString, Desc: "设置一个槽，形如 slot=model||Set one slot, as slot=model"},
			{Name: "reset", Kind: KindBool, Desc: "清空该 harness 的槽位||Clear this harness's slots"},
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
			fmt.Sprintf(c.UI.T("没有名为 %q 的 harness", "no harness named %q"), name)).
			WithHint("tf harness list")
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
				c.UI.T("--set 的格式是 slot=model", "--set expects slot=model")).
				WithHint(fmt.Sprintf("tf model %s --set default=<model>", h.Name))
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

	// 没给任何 flag 且能交互：进编辑器。
	//
	// 光「看」不「改」是这个命令过去最别扭的地方 —— 想换个模型只能
	// 靠启动一次，或者手打完整的模型 ID。
	if !c.Flags.Present("set") && c.UI.Interactive(c.Flags.Bool("no-input")) {
		return editSlots(c, st, h)
	}
	return showHarnessSlots(c, cfg, h)
}

// editSlots 是一屏槽位编辑器：选槽 → 选模型 → 回到列表。
//
// 放在 tf model 而不是启动路径上：启动该让路，不该拦路；
// 而专门跑来改配置的人，本来就是为了看见全貌。
func editSlots(c *Context, st *state, h *harness.Harness) error {
	keys, err := eligibleKeys(c, st.cfg, st.creds, h)
	if err != nil {
		return err
	}
	cands := gatherCandidates(c, st.cfg, st.creds, keys, h)
	if len(cands) == 0 {
		return showHarnessSlots(c, st.cfg, h)
	}
	noteHiddenKeys(c, st.cfg, st.creds.Names(), keys, h)

	slots := st.cfg.Harness(h.Name).Slots
	if slots == nil {
		slots = config.ModelSlots{}
		st.cfg.Harness(h.Name).Slots = slots
	}

	for {
		items := make([]ui.Item, 0, len(h.Slots)+1)
		for _, sl := range h.Slots {
			v := slots[sl.Name]
			if v == "" {
				v = c.UI.Dim(c.UI.T("未配置", "unset"))
			} else {
				v = model.Parse(v).Display()
			}
			items = append(items, ui.Item{
				Label:  ui.Pad(sl.Name, 9) + v,
				Detail: sl.Purpose(c.UI.Lang == ui.LangZH),
			})
		}
		items = append(items, ui.Item{Label: c.UI.T("完成", "done")})

		// 每选一次就已经落盘（bindKey 会写配置），没有「放弃修改」这条路，
		// 所以 esc 就直说它真正的后果：退出，已改的保留。
		pick, err := c.UI.SelectWith(
			fmt.Sprintf(c.UI.T("%s 的模型槽", "Model slots for %s"), h.Name), items,
			ui.SelectOpt{CancelHint: c.UI.T("退出（已改的保留）", "exit (edits are kept)")})
		if err != nil || pick == len(items)-1 {
			break
		}

		sl := h.Slots[pick]
		choice, err := c.UI.SelectWith(
			fmt.Sprintf(c.UI.T("%s.%s 用哪个模型？（%s）", "Which model for %s.%s? (%s)"),
				h.Name, sl.Name, sl.Purpose(c.UI.Lang == ui.LangZH)),
			candidateItems(cands),
			ui.SelectOpt{CancelHint: c.UI.T("返回槽位列表", "back to the slot list")})
		if err != nil {
			continue // 取消只退回列表，不退出编辑器
		}
		// 一次启动只注入一把 Key，所以所有槽必须出自同一把。
		// 换 Key 就得把别的槽清掉 —— 留着的话那些模型这把 Key 根本调不到，
		// 而失败要等到启动之后才看得见。
		if prev := st.cfg.Harness(h.Name).Key; prev != "" && prev != cands[choice].Key {
			for name := range slots {
				if name != sl.Name {
					delete(slots, name)
				}
			}
			c.UI.Warnf(c.UI.T("已切到 Key %q，其余槽已清空（一次启动只能用一把 Key）",
				"switched to key %q; the other slots were cleared (one key per launch)"),
				cands[choice].Key)
		}
		slots[sl.Name] = cands[choice].Model
		bindKey(c, st.cfg, h, cands[choice].Key)
	}

	if err := st.saveConfig(c); err != nil {
		return err
	}
	return showHarnessSlots(c, st.cfg, h)
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

	width := slotWidth(c, cfg, harness.All)
	c.UI.Emit("model list", out, func() {
		for _, h := range harness.All {
			line := c.UI.Bold(h.Name)
			if k := cfg.Harness(h.Name).Key; k != "" {
				line += "   " + c.UI.Dim(c.UI.T("key", "key")) + " " + k
			}
			c.UI.Printf("%s\n", line)
			printSlots(c, cfg.Harness(h.Name).Slots, h, width)
		}
		// 这一屏只能看不能改，得说清楚改在哪儿。
		c.UI.Logf("%s", c.UI.Dim(c.UI.T(
			"改模型：tf model <harness>", "to change models: tf model <harness>")))
	})
	return nil
}

func showHarnessSlots(c *Context, cfg *config.Config, h *harness.Harness) error {
	hc := cfg.Harness(h.Name)
	slots := hc.Slots
	c.UI.Emit("model", map[string]any{"harness": h.Name, "key": hc.Key, "slots": slots}, func() {
		line := c.UI.Bold(h.Name)
		if hc.Key != "" {
			line += "   " + c.UI.Dim(c.UI.T("key", "key")) + " " + hc.Key
		}
		c.UI.Printf("%s\n", line)
		printSlots(c, slots, h, slotWidth(c, cfg, []*harness.Harness{h}))
	})
	return nil
}

// printSlots 必须显式标出未配置的槽 —— 未配置意味着 harness 会用它的
// 内置默认模型，而那个模型多半不在用户的分组里。
// slotWidth 量出一屏里槽位值需要的列宽。
//
// 必须按整屏算，不能各算各的：tf model 一次列出三个 harness，
// 每块自己算宽度会让三块的模型列互相错开。
func slotWidth(c *Context, cfg *config.Config, hs []*harness.Harness) int {
	width := 0
	for _, h := range hs {
		slots := cfg.Harness(h.Name).Slots
		for _, sl := range h.Slots {
			if w := ui.Width(slotValue(c, slots[sl.Name], sl)); w > width {
				width = w
			}
		}
	}
	return width
}

// slotValue 是槽位在界面上的显示值。
func slotValue(c *Context, v string, sl harness.Slot) string {
	if v != "" {
		return model.Parse(v).Display()
	}
	if sl.Required {
		return c.UI.T("启动时询问", "asked at launch")
	}
	return c.UI.T("未配置", "unset")
}

func printSlots(c *Context, slots config.ModelSlots, h *harness.Harness, width int) {
	for _, sl := range h.Slots {
		v := ui.Pad(slotValue(c, slots[sl.Name], sl), width)
		if slots[sl.Name] == "" {
			v = c.UI.Dim(v)
		}
		c.UI.Printf("  %s %s %s\n", ui.Pad(sl.Name, 9), v,
			c.UI.Dim("— "+sl.Purpose(c.UI.Lang == ui.LangZH)))
	}
}
