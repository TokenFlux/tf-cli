package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/gateway"
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
		Usage:       fmt.Sprintf("tkr %s [tkr flags] [%s args...]", h.Name, h.Name),
		Summary: func(u *ui.UI) string {
			return fmt.Sprintf(u.T("用 TokenFlux 环境启动 %s", "Launch %s against TokenFlux"), h.Name)
		},
		Flags: []Flag{
			{Name: "model", Short: "m", Kind: KindOptString,
				Desc: "本次使用的主模型；不带值则进入选择|Main model for this run; omit the value to pick interactively"},
			{Name: "effort", Short: "e", Kind: KindString,
				Desc: "思考强度：minimal|low|medium|high|xhigh|Reasoning effort: minimal|low|medium|high|xhigh"},
		},
		Run: func(c *Context) error { return runLaunch(c, h) },
	}
}

func runLaunch(c *Context, h *harness.Harness) error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return ui.Errf(ui.CodeConfigRead, c.UI.T("无法定位配置目录", "cannot locate the config directory")).WithCause(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return ui.Errf(ui.CodeConfigRead, c.UI.T("配置文件无法读取", "cannot read the config file")).WithCause(err)
	}

	profileName := c.Flags.String("profile")
	if profileName == "" {
		profileName = cfg.Current
	}
	profile, ok := cfg.Profile(profileName)
	if !ok {
		return ui.Errf(ui.CodeProfileNotFound,
			fmt.Sprintf(c.UI.T("找不到 profile：%s", "no such profile: %s"), profileName))
	}
	host := profile.Host
	if hv := c.Flags.String("host"); hv != "" {
		host = normalizeHost(hv)
	}

	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		return ui.Errf(ui.CodeCredentialsRead, c.UI.T("凭据文件无法读取", "cannot read the credentials file")).WithCause(err)
	}
	cred, loggedIn := creds.Get(profileName)
	if !loggedIn {
		return ui.Errf(ui.CodeNotLoggedIn,
			c.UI.T("尚未保存 API Key", "no API key stored")).
			WithHint("tkr login")
	}

	// harness 缺失时按 E 项规则征求用户意见。
	if st := h.Detect(); !st.Installed {
		if err := EnsureInstalled(c, h); err != nil {
			return err
		}
	}

	slots, err := resolveSlots(c, cfg, profile, h, profileName, host, cred.Key)
	if err != nil {
		return err
	}

	effort, err := applyEffort(c, h, slots, profileName, host, cred.Key)
	if err != nil {
		return err
	}

	plan, err := h.BuildPlan(harness.Input{
		Host: host, Key: cred.Key, Slots: slots, Effort: effort, Args: c.Passthr,
	})
	if err != nil {
		return ui.Errf(ui.CodeUsage, err.Error())
	}

	// 启动横幅：用户必须知道自己正在用什么，但只占一行。
	//
	// 有多个 profile 时必须把 profile 名括进来：用错 Key 的表现是
	// “模型列表没见过”，不写出来用户很难想到是 profile 选错了。
	banner := fmt.Sprintf("%s → %s", c.UI.Bold("tkr"), h.Name)
	if len(cfg.Profiles) > 1 {
		banner += "   " + c.UI.Dim(c.UI.T("profile", "profile")) + " " + profileName
	}
	banner += "   " + c.UI.Dim(c.UI.T("模型", "model")) + " " + slots[config.SlotDefault]
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

// resolveSlots 解析该 harness 的全部模型槽。
//
// 必须填满所有已声明的槽：留空会让 harness 回落到它的内置默认模型，
// 而那个模型通常不在用户的分组里，且失败可能是静默的。
func resolveSlots(c *Context, cfg *config.Config, profile *config.Profile,
	h *harness.Harness, profileName, host, key string) (map[string]string, error) {

	slots := profile.Slots(h.Name)

	// -m 带值：本次覆盖主模型，不写盘。
	override := c.Flags.String("model")
	explicitPick := c.Flags.Present("model") && override == ""

	if override != "" {
		slots[config.SlotDefault] = override
	}

	missing := false
	for _, s := range h.Slots {
		if s.Required && slots[s.Name] == "" {
			missing = true
		}
	}
	if !missing && !explicitPick {
		return slots, nil
	}

	ids, err := listModels(c, profileName, host, key)
	if err != nil {
		return nil, err
	}

	interactive := c.UI.Interactive(c.Flags.Bool("yes"))

	// 首次运行先让用户明确选定主模型，而不是预选一个。
	//
	// 目录里存在审查、嵌入、图像类的专用模型，拿它们当主模型会直接
	// 报错（实测：codex-auto-review 在 opencode 下直接失败）。在拿到价格与
	// 能力元数据之前，宁可多问一屏，也不替用户瞎猜。
	if slots[config.SlotDefault] == "" {
		if !interactive {
			return nil, ui.Errf(ui.CodeUsage,
				fmt.Sprintf(c.UI.T("%s 尚未选定主模型", "no main model chosen for %s"), h.Name)).
				WithHint(fmt.Sprintf("tkr model %s --set default=<model>", h.Name))
		}
		choices := modelItems(ids)
		pick, err := c.UI.Select(fmt.Sprintf(c.UI.T("为 %s 选择主模型", "Pick the main model for %s"), h.Name), choices)
		if err != nil {
			return nil, err
		}
		slots[config.SlotDefault] = ids[pick]
	}

	// 其余空槽按名字归位：分组里真有 haiku / opus 时，就不该把三个槽
	// 塞同一个模型 —— 那会让 Claude Code 的 /model 切换变成空操作。
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

	if !interactive {
		c.UI.Warnf(c.UI.T("非交互环境，沿用 %s", "non-interactive, using %s"), slots[config.SlotDefault])
	} else if err := editSlots(c, h, slots, ids); err != nil {
		return nil, err
	}

	profile.SetSlots(h.Name, slots)
	if err := cfg.Save(); err != nil {
		c.UI.Warnf(c.UI.T("模型已选定，但写入配置失败：%v", "model chosen, but saving config failed: %v"), err)
	}
	warnIdenticalSlots(c, h, slots)
	return slots, nil
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
		"%s 的所有档位都指向 %s，harness 内部的模型切换将没有区别",
		"every %s tier points at %s, so switching models inside the harness will do nothing"),
		h.Name, first)
}

// editSlots 是一屏确认：列出该 harness 的全部槽位及当前取值，
// 回车接受，或选中某槽单独修改。
//
// 不逐槽追问：启动器的本分是快，三个问题连着弹会把人退坏。
func editSlots(c *Context, h *harness.Harness, slots config.ModelSlots, ids []string) error {
	zh := c.UI.Lang == ui.LangZH

	for {
		items := make([]ui.Item, 0, len(h.Slots)+1)
		for _, s := range h.Slots {
			items = append(items, ui.Item{
				Label:  s.Name,
				Detail: slots[s.Name],
				Note:   c.UI.Dim("— " + s.Purpose(zh)),
			})
		}
		accept := c.UI.T("✓ 接受并启动", "✓ accept and launch")
		items = append(items, ui.Item{Label: accept})

		idx, err := c.UI.Select(fmt.Sprintf(c.UI.T(
			"%s 的模型槽（选中某项可修改）", "Model slots for %s (select one to change)"), h.Name), items)
		if err != nil {
			return err
		}
		if idx == len(h.Slots) {
			return nil
		}

		slot := h.Slots[idx]
		choices := modelItems(ids)
		for i, id := range ids {
			if id == slots[slot.Name] {
				choices[i].Note = c.UI.Dim(c.UI.T("← 当前", "← current"))
			}
		}

		pick, err := c.UI.Select(fmt.Sprintf(c.UI.T("为 %s.%s 选择模型", "Pick a model for %s.%s"),
			h.Name, slot.Name), choices)
		if err != nil {
			// 单次取消只退回上一层，不应该把整个启动流程也取消。
			continue
		}
		slots[slot.Name] = ids[pick]
	}
}

// applyEffort 把思考强度落到具体机制上，返回仍需交给 harness 的强度值。
//
// 优先用模型 ID 变体（如 gemini-3.1-pro-high）：那是分组真正支持的形式，
// 比把参数交给 harness 再转发更可靠。没有变体时才回落到 harness 的旋钮。
func applyEffort(c *Context, h *harness.Harness, slots config.ModelSlots, profileName, host, key string) (string, error) {
	effort := c.Flags.String("effort")
	if effort == "" {
		return "", nil
	}

	cur := slots[config.SlotDefault]
	base := model.Parse(cur).Base

	// 先看分组里有没有该强度的模型变体。有就直接换模型 ——
	// 那是分组真正支持的形式，比指望 harness 转发参数可靠。
	if ids, err := listModels(c, profileName, host, key); err == nil {
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

// modelItems 把模型 ID 变成选择器条目。
//
// 复合 Key 下同一个模型会在多个分组里重复出现，且倒率可能相差好几倍，
// 所以把分组前缀单独成列 —— 否则列表里就是一堆看不出区别的同名项。
func modelItems(ids []string) []ui.Item {
	items := make([]ui.Item, 0, len(ids))
	for _, id := range ids {
		r := model.Parse(id)
		items = append(items, ui.Item{Label: r.Display(), Detail: r.Prefix})
	}
	return items
}

// listModels 取模型列表，网络不可用时降级用缓存。
//
// 启动路径上的网络调用必须可降级：一次瞬时抖动不应该让用户
// 根本启动不了 harness。只有在既无网络又无缓存时才真正失败。
func listModels(c *Context, profileName, host, key string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	client := gateway.New(host, key)
	models, err := client.Models(ctx)
	if err == nil && len(models) > 0 {
		ids := make([]string, 0, len(models))
		for _, m := range models {
			ids = append(ids, m.ID)
		}
		sort.Strings(ids)
		if paths, perr := config.DefaultPaths(); perr == nil {
			_ = paths.WriteCache(config.ModelsCacheKey(profileName), ids)
		}
		return ids, nil
	}

	// Key 被明确拒绝时不该退回缓存 —— 那是配置问题，不是网络问题。
	var apiErr *gateway.APIError
	if asAPIError(err, &apiErr) && apiErr.InvalidKey() {
		return nil, ui.Errf(ui.CodeNotLoggedIn,
			c.UI.T("网关拒绝了当前的 Key", "the gateway rejected the stored key")).
			WithHint("tkr login")
	}

	var cached []string
	if paths, perr := config.DefaultPaths(); perr == nil {
		if age, cerr := paths.ReadCache(config.ModelsCacheKey(profileName), &cached); cerr == nil && len(cached) > 0 {
			c.UI.Warnf(c.UI.T("模型列表取不到，暂用 %s 前的缓存",
				"could not refresh models, using a cache from %s ago"), age.Round(time.Second))
			return cached, nil
		}
	}

	if err == nil {
		return nil, ui.Errf(ui.CodeInternal,
			c.UI.T("这把 Key 看不到任何模型", "this key can see no models")).
			WithHint(host + "/keys")
	}
	return nil, ui.Errf(ui.CodeNetwork,
		fmt.Sprintf(c.UI.T("无法获取模型列表：%v", "cannot list models: %v"), err)).
		WithHint(host)
}

// exitCodeError 让 harness 的退出码穿透 tkr。
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *exitCodeError) ExitCode() int { return e.code }
