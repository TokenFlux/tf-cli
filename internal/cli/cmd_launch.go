package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/launch"
	"github.com/tokenflux/tkr/internal/model"
	"github.com/tokenflux/tkr/internal/ui"
)

// newLaunchCommand 为每个 harness 生成一个透传型子命令。
func newLaunchCommand(h *harness.Harness) *Command {
	return &Command{
		Name:        h.Name,
		Aliases:     h.Aliases,
		Passthrough: true,
		Usage:       fmt.Sprintf("tf %s [tf flags] [%s args...]", h.Name, h.Name),
		Summary: func(u *ui.UI) string {
			return fmt.Sprintf(u.T("通过 TokenFlux 启动 %s", "Launch %s against TokenFlux"), h.Name)
		},
		Flags: []Flag{
			{Name: "model", Short: "m", Kind: KindOptString,
				Desc: "本次使用的主模型；不带值则进入选择||Main model for this run; omit the value to pick interactively"},
			{Name: "effort", Short: "e", Kind: KindString,
				Desc: "思考强度：minimal|low|medium|high|xhigh||Reasoning effort: minimal|low|medium|high|xhigh"},
		},
		Run: func(c *Context) error { return runLaunch(c, h) },
	}
}

func runLaunch(c *Context, h *harness.Harness) error {
	st, err := loadState(c)
	if err != nil {
		return err
	}
	cfg, creds := st.cfg, st.creds

	// 一把 Key 都没有时当场问，而不是打发用户去跑 tkr login。
	//
	// harness 没装都会问「现在安装？」，而安装软件比粘一把 Key 重得多。
	// 既然那件事值得问，这件事更值得。
	if len(creds.Names()) == 0 && c.UI.Interactive(c.Flags.Bool("no-input")) {
		if err := runLogin(c); err != nil {
			return err
		}
		if st, err = loadState(c); err != nil {
			return err
		}
		cfg, creds = st.cfg, st.creds
	}

	// harness 缺失时按 E 项规则征求用户意见。
	if hs := h.Detect(); !hs.Installed {
		if err := EnsureInstalled(c, h); err != nil {
			return err
		}
	}

	keyName, slots, err := resolveTarget(c, cfg, creds, h)
	if err != nil {
		return err
	}
	// 这个 ok 绝不能丢：丢掉就是 nil 解引用，用户看到的是一屏 panic
	// 而不是一句话。resolveTarget 理应已经保证 Key 有效，
	// 但「理应」不该由下游用崩溃来验证。
	cred, ok := creds.Get(keyName)
	if !ok {
		return ui.Errf(ui.CodeKeyNotFound, fmt.Sprintf(
			c.UI.T("没有名为 %q 的 Key", "no key named %q"), keyName)).
			WithHint("tf login")
	}
	host := cfg.HostOf(keyName)
	if hv := c.Flags.String("host"); hv != "" {
		host = normalizeHost(hv)
	}

	effort, err := applyEffort(c, cfg, h, slots, keyName)
	if err != nil {
		return err
	}

	// 协议按本次选定的模型所在分组决定 —— 同一个 harness 在不同分组上
	// 可能走不同协议，注入配方要跟着变。
	main := slots[config.SlotDefault]
	proto, _ := pickProtocolFor(cfg.Keys[keyName], model.Parse(main).Prefix, main, h)

	plan, err := h.BuildPlan(harness.Input{
		Host: host, Key: cred.Key, Slots: slots, Effort: effort,
		Args: c.Passthr, Protocol: proto,
	})
	if err != nil {
		return ui.Errf(ui.CodeUsage, err.Error())
	}

	// 启动横幅：用户必须知道自己正在用什么，但只占一行。
	//
	// 有多把 Key 时必须写出用的是哪把：用错 Key 的表现是
	// “模型列表没见过”，不写出来用户很难联想到是 Key 的问题。
	banner := fmt.Sprintf("%s → %s", c.UI.Bold("tf"), h.Name)
	if len(creds.Names()) > 1 {
		banner += "   " + c.UI.Dim(c.UI.T("Key", "key")) + " " + keyName
	}
	banner += "   " + c.UI.Dim(c.UI.T("模型", "model")) + " " + slots[config.SlotDefault]
	// 其它槽只在与主模型不同时才列出 —— 相同就是噪音。
	// 但必须让用户看得见：fast 槽决定了后台任务花多少钱。
	for _, sl := range h.Slots {
		if v := slots[sl.Name]; v != "" && sl.Name != config.SlotDefault && v != slots[config.SlotDefault] {
			banner += "   " + c.UI.Dim(sl.Name) + " " + v
		}
	}
	if effort != "" {
		banner += "   " + c.UI.Dim(c.UI.T("强度", "effort")) + " " + effort
	}
	c.UI.Logf("%s", banner)

	res, err := launch.Run(launch.Spec{Bin: plan.Bin, Args: plan.Args, Env: plan.Env})
	if err != nil {
		return ui.Errf(ui.CodeInternal,
			fmt.Sprintf(c.UI.T("启动 %s 失败", "failed to launch %s"), h.Name)).WithCause(err)
	}

	c.UI.Logf("%s", c.UI.Dim(fmt.Sprintf(
		c.UI.T("%s 结束，耗时 %s，退出码 %d", "%s finished in %s, exit %d"),
		h.Name, res.Duration.Round(1e8), res.ExitCode)))

	if res.ExitCode != 0 {
		return &exitCodeError{code: res.ExitCode}
	}
	return nil
}

// resolveTarget 一次定下用哪把 Key、跑哪些模型。
//
// 顺序很重要：**先有模型，后有 Key**。用户想的是「我要用哪个模型」，
// Key 只是它的来源。先挑 Key 再列模型会凭空藏起另一把 Key 的选项 ——
// ChatGPT 分组同样开着 anthropic_messages，gpt 模型确实能走 Claude Code。
func resolveTarget(c *Context, cfg *config.Config, creds *config.Credentials,
	h *harness.Harness) (string, config.ModelSlots, error) {

	hc := cfg.Harness(h.Name)
	slots := config.ModelSlots{}
	for k, v := range hc.Slots {
		slots[k] = v
	}

	// -m 只影响本次运行，绝不写盘。
	//
	// 规矩要能一句话说清：**flag 管这一次，tf model 管以后。**
	// 之前 -m 的注释写着不写盘、代码却存了盘 —— 用户改一次就再也回不去，
	// 而他以为自己只是试一下。
	override := c.Flags.String("model")
	oneShot := c.Flags.Present("model")
	explicitPick := oneShot && override == ""
	if override != "" {
		slots[config.SlotDefault] = override
	}

	keys, err := eligibleKeys(c, cfg, creds, h)
	if err != nil {
		return "", nil, err
	}

	keyName := hc.Key
	if k := c.Flags.String("key"); k != "" {
		keyName = k
	}

	// 快路径：绑定仍然有效且槽位齐全，直接走，不联网也不提问。
	if !explicitPick && override == "" && contains(keys, keyName) && slotsComplete(h, slots) {
		return keyName, slots, nil
	}

	cands := gatherCandidates(c, cfg, creds, keys, h)
	if len(cands) == 0 {
		noteHiddenKeys(c, cfg, creds.Names(), keys, h)
		return "", nil, noModelError(c, cfg, keys, h)
	}
	// -m 分开写时，那个词未必是取值。
	//
	// tf codex -m exec "hi" 的 exec 是 codex 的子命令，
	// tf claude -m "解释这段代码" 那句是 prompt。解析期分不出来，
	// 拿到候选集才分得出：不在候选里就退还给透传参数，-m 按裸写处理。
	if override != "" && c.Flags.Detached("model") {
		if _, ok := ownerOf(cands, override); !ok {
			c.Passthr = append([]string{c.Flags.Detach("model")}, c.Passthr...)
			override = ""
			slots[config.SlotDefault] = ""
			// 退还之后 -m 就是裸写，含义是「让我挑」。
			explicitPick = true
		}
	}

	// 命令行给的模型不存在：那是刚敲错的，不是环境变了。
	//
	// 「现在没有 Key 能提供 X」是为存量配置写的，用在刚输入的值上会
	// 让人以为服务端撤了模型，转头去查网关 —— 而其实只是拼错了。
	if override != "" {
		if _, ok := ownerOf(cands, override); !ok {
			return "", nil, unknownModelError(c, override, cands)
		}
	}

	// 存着的模型现在没有 Key 能提供 —— Key 被 logout，或分组里撤了它。
	//
	// 能问就问，不要把人赶去另一条命令：选择器就在手边，而用户的目的
	// 本来就是启动。清空全部槽位而不只是主槽 —— 一次启动只注入一把 Key，
	// 留着旧槽会让新旧两把 Key 的模型混在一起。
	if m := slots[config.SlotDefault]; m != "" {
		if _, ok := ownerOf(cands, m); !ok {
			c.UI.Warnf(c.UI.T("存着的模型 %s 现在没有 Key 能提供，请重新选一个",
				"the saved model %s is no longer offered by any key; pick another"), m)
			slots = config.ModelSlots{}
		}
	}

	// -m 指定了具体模型：认出它属于哪把 Key。
	if override != "" {
		if k, ok := ownerOf(cands, override); ok {
			keyName = k
		}
	}

	if slots[config.SlotDefault] == "" {
		if !c.UI.Interactive(c.Flags.Bool("no-input")) {
			return "", nil, ui.Errf(ui.CodeUsage,
				fmt.Sprintf(c.UI.T("%s 尚未选定主模型", "no main model chosen for %s"), h.Name)).
				WithHint(fmt.Sprintf("tf model %s --set default=<model>", h.Name))
		}
		explicitPick = true
	}

	if explicitPick {
		// 只在真要选的时候说明藏了什么。已经定好用哪个模型时，
		// 另一把 Key 的模型能不能用与这次启动无关 —— 每次启动都讲一遍
		// 就成了噪音。
		noteHiddenKeys(c, cfg, creds.Names(), keys, h)

		// 目录里存在审查、嵌入、图像类的专用模型，拿它们当主模型会直接
		// 报错（实测：codex-auto-review 在 opencode 下失败）。所以不预选。
		pick, err := c.UI.Select(
			fmt.Sprintf(c.UI.T("为 %s 选择主模型", "Pick the main model for %s"), h.Name),
			candidateItems(cands))
		if err != nil {
			return "", nil, err
		}
		keyName = cands[pick].Key
		slots[config.SlotDefault] = cands[pick].Model
	}

	// 主模型决定用哪把 Key。
	//
	// 绑定可能压根不存在（tf model --set 只写槽位，它无从知道该绑谁），
	// 也可能指向一把已经 logout 的 Key。两种都是合法状态，
	// 之前会一路把空名字带到 creds.Get 那里去崩掉。
	if !contains(keys, keyName) {
		k, ok := ownerOf(cands, slots[config.SlotDefault])
		if !ok {
			return "", nil, ui.Errf(ui.CodeKeyNotFound, fmt.Sprintf(
				c.UI.T("没有 Key 能提供 %s", "no key offers %s"), slots[config.SlotDefault])).
				WithHint(fmt.Sprintf("tf model %s", h.Name))
		}
		keyName = k
	}

	// 其余槽只能取自同一把 Key —— 一次启动只注入一把 Key。
	own := modelsOf(cands, keyName)
	if !oneShot && c.UI.Interactive(c.Flags.Bool("no-input")) {
		if err := askSlots(c, h, slots, own); err != nil {
			return "", nil, err
		}
	}
	fill(h, slots, own)
	warnIdenticalSlots(c, h, slots)

	// -m/-e/-k 不写盘，这本来就是 flag 该有的样子，不必每次声明一遍。
	if oneShot {
		return keyName, slots, nil
	}

	hc.Slots = slots
	bindKey(c, cfg, h, keyName)
	if err := cfg.Save(); err != nil {
		c.UI.Warnf(c.UI.T("模型选择未保存：%v", "model choice not saved: %v"), err)
	}
	return keyName, slots, nil
}

// unknownModelError 是命令行给了不存在的模型时的报错。
//
// 附上最接近的几个候选：拼错通常只差几个字符，把它们摆出来比让人
// 自己去 tf model --list 里翻要快。
func unknownModelError(c *Context, want string, cands []candidate) error {
	err := ui.Errf(ui.CodeUsage,
		fmt.Sprintf(c.UI.T("没有模型 %q", "no such model %q"), want))
	if near := nearest(want, cands); len(near) > 0 {
		return err.WithHint(strings.Join(near, "  "))
	}
	return err.WithHint(fmt.Sprintf("tf model %s --list", c.Command))
}

// nearest 挑出与 want 最像的几个候选。
//
// 判据是共同的子串而不是编辑距离：模型名的错法多是漏了日期后缀、
// 记错了小版本号，共同前缀比逐字符距离更能抓住这类错。
func nearest(want string, cands []candidate) []string {
	type scored struct {
		id string
		n  int
	}
	var all []scored
	w := strings.ToLower(model.Parse(want).Display())
	for _, cd := range cands {
		id := model.Parse(cd.Model).Display()
		if n := commonPrefix(w, strings.ToLower(id)); n >= 3 {
			all = append(all, scored{id, n})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].n > all[j].n })

	out := make([]string, 0, 3)
	for _, s := range all {
		if len(out) == 3 {
			break
		}
		out = append(out, s.id)
	}
	return out
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// slotsComplete 报告必填槽是否都已填。
//
// 留空会让 harness 回落到它的内置默认模型，而那个模型通常不在用户的
// 分组里，且失败可能是静默的（实测：opencode 的 small 槽）。
func slotsComplete(h *harness.Harness, slots config.ModelSlots) bool {
	for _, s := range h.Slots {
		if s.Required && slots[s.Name] == "" {
			return false
		}
	}
	return true
}

// askSlots 逐个问还没定的非主槽。
//
// 只在首次配置该 harness 时问一次，之后不再打扰。不问的代价是实打实的：
// codex 的 review、claude 的 fast 决定了那部分工作用哪个模型、花多少钱，
// 而自动归位在分组里没有对应档位时只能回落到主模型。
//
// 每个槽的首项是推荐值，直接回车即可，所以「多问几屏」的成本接近于零。
// 也正因为接受推荐值只需回车，esc 就不必兼职「跳过」，能与别处一样只表示取消。
func askSlots(c *Context, h *harness.Harness, slots config.ModelSlots, ids []string) error {
	main := slots[config.SlotDefault]
	for _, s := range h.Slots {
		if s.Name == config.SlotDefault || slots[s.Name] != "" {
			continue
		}

		suggested := suggestForSlot(s.Name, main, ids)
		items := []ui.Item{{
			Label:  model.Parse(suggested).Display(),
			Detail: slotSuggestionReason(c, suggested, main),
		}}
		rest := make([]string, 0, len(ids))
		for _, id := range ids {
			if id != suggested {
				items = append(items, ui.Item{Label: model.Parse(id).Display(), Detail: model.Parse(id).Prefix})
				rest = append(rest, id)
			}
		}

		title := fmt.Sprintf(c.UI.T("%s.%s 用哪个模型？（%s）", "Which model for %s.%s? (%s)"),
			h.Name, s.Name, s.Purpose(c.UI.Lang == ui.LangZH))
		pick, err := c.UI.SelectWith(title, items, ui.SelectOpt{
			CancelHint: c.UI.T("用推荐值补齐", "take the suggestions"),
		})
		if err != nil {
			// esc 只该退掉这一屏，不该炸掉上游已完成的工作。
			//
			// 主模型是刚刚选完的，副槽是 tf 主动追问的 —— 因为不问就会
			// 静默失败。用户对追问说「算了」，意思是「按你说的来」，
			// 不是「把我选的主模型也扔掉」。剩下的槽用推荐值补齐。
			if ui.AsError(err).Code == ui.CodeCancelled {
				return nil
			}
			return err
		}
		if pick == 0 {
			slots[s.Name] = suggested
		} else {
			slots[s.Name] = rest[pick-1]
		}
	}
	return nil
}

// suggestForSlot 给出某个槽的推荐模型。
func suggestForSlot(slot, main string, ids []string) string {
	want := ""
	switch slot {
	case config.SlotFast, config.SlotSmall:
		want = "fast"
	case config.SlotHeavy:
		want = "heavy"
	}
	if want != "" {
		for _, id := range ids {
			if model.GuessTier(id) == want {
				return id
			}
		}
	}
	return main
}

func slotSuggestionReason(c *Context, suggested, main string) string {
	if suggested == main {
		return c.UI.T("跟随主模型", "same as the main model")
	}
	return c.UI.T("推荐", "recommended")
}

// fill 给空槽按档位归位。
//
// 分组里真有 haiku / opus 时就不该把几个槽塞同一个模型 ——
// 那会让 harness 内部的模型切换变成空操作，后台任务也会烧主模型的钱。
func fill(h *harness.Harness, slots config.ModelSlots, ids []string) {
	for _, s := range h.Slots {
		if slots[s.Name] != "" {
			continue
		}
		slots[s.Name] = slots[config.SlotDefault]
		if s.Name != config.SlotFast && s.Name != config.SlotSmall && s.Name != config.SlotHeavy {
			continue
		}
		want := "fast"
		if s.Name == config.SlotHeavy {
			want = "heavy"
		}
		for _, id := range ids {
			if model.GuessTier(id) == want {
				slots[s.Name] = id
				break
			}
		}
	}
}

func modelsOf(cands []candidate, key string) []string {
	var out []string
	for _, c := range cands {
		if c.Key == key {
			out = append(out, c.Model)
		}
	}
	return out
}

func ownerOf(cands []candidate, id string) (string, bool) {
	for _, c := range cands {
		if c.Model == id {
			return c.Key, true
		}
	}
	return "", false
}

// noModelError 说明为什么一个模型都用不了。
func noModelError(c *Context, cfg *config.Config, keys []string, h *harness.Harness) error {
	var lines []string
	for _, k := range keys {
		lines = append(lines, k+": "+strings.Join(cfg.Keys[k].ProtocolSummary(), " / "))
	}
	return ui.Errf(ui.CodeProtocolMismatch, fmt.Sprintf(
		c.UI.T("%s 没有可用模型（需要 %s）", "no model %s can use (needs %s)"), h.Name, protocolList(h))).
		WithHint(strings.Join(lines, "; "))
}

// warnIdenticalSlots 在所有槽位指向同一模型时提醒。
//
// 这不是错误，但后果不直观：harness 内部的档位切换会完全失效，
// 用户会以为自己切成了更强的模型。
func warnIdenticalSlots(c *Context, h *harness.Harness, slots config.ModelSlots) {
	if len(h.Slots) < 2 {
		return
	}
	first := slots[h.Slots[0].Name]
	for _, s := range h.Slots[1:] {
		if slots[s.Name] != first {
			return
		}
	}
	c.UI.Warnf(c.UI.T(
		"%s 的每个档位都是 %s，在 harness 里切换模型不会有变化",
		"every %s tier is %s, so switching models inside it does nothing"),
		h.Name, first)
}

// applyEffort 把思考强度落到具体机制上，返回仍需交给 harness 的强度值。
//
// 优先用模型 ID 变体（如 gemini-3.1-pro-high）：那是分组真正支持的形式，
// 比把参数交给 harness 再转发更可靠。没有变体时才回落到 harness 的旋钮。
func applyEffort(c *Context, cfg *config.Config, h *harness.Harness, slots config.ModelSlots, keyName string) (string, error) {
	effort := c.Flags.String("effort")
	if effort == "" {
		return "", nil
	}

	cur := slots[config.SlotDefault]
	base := model.Parse(cur).Base

	// 先看分组里有没有该强度的模型变体。有就直接换模型 ——
	// 那是分组真正支持的形式，比指望 harness 转发参数可靠。
	if ids := cfg.KeyMetaOf(keyName).Models; len(ids) > 0 {
		variant := model.Ref{Base: base, Effort: effort}.String()
		if contains(ids, variant) {
			slots[config.SlotDefault] = variant
			return "", nil
		}
		// 当前模型本就是变体之一，却没有请求的那个档：列出真实可选项。
		if model.Parse(cur).Effort != "" {
			available := []string{}
			for _, id := range ids {
				if r := model.Parse(id); r.Base == base && r.Effort != "" {
					available = append(available, r.Effort)
				}
			}
			return "", ui.Errf(ui.CodeUsage,
				fmt.Sprintf(c.UI.T("%s 没有 %s 强度的变体", "%s has no %s variant"), base, effort)).
				WithHint(strings.Join(available, " | "))
		}
	}

	if h.EffortKnob == harness.EffortViaModelID {
		return "", ui.Errf(ui.CodeUsage,
			fmt.Sprintf(c.UI.T("%s 没有独立的思考强度开关", "%s has no reasoning-effort switch"), h.Name)).
			WithHint(c.UI.T("改选带强度后缀的模型，如 -m gemini-3.1-pro-high",
				"pick a model variant instead, e.g. -m gemini-3.1-pro-high"))
	}
	return effort, nil
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// exitCodeError 把子进程的退出码原样带回顶层。
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *exitCodeError) ExitCode() int { return e.code }
