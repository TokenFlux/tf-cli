package cli

import (
	"context"
	"fmt"
	"sort"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/gateway"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/launch"
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

	slots, err := resolveSlots(c, cfg, profile, h, host, cred.Key)
	if err != nil {
		return err
	}

	plan, err := h.BuildPlan(harness.Input{
		Host: host, Key: cred.Key, Slots: slots, Args: c.Passthr,
	})
	if err != nil {
		return ui.Errf(ui.CodeInternal, err.Error())
	}

	// 启动横幅：用户必须知道自己正在用什么，但只占一行。
	c.UI.Logf("%s → %s   %s %s", c.UI.Bold("tkr"), h.Name,
		c.UI.Dim(c.UI.T("模型", "model")), slots["default"])

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
	h *harness.Harness, host, key string) (map[string]string, error) {

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

	client := gateway.New(host, key)
	models, err := client.Models(context.Background())
	if err != nil {
		return nil, ui.Errf(ui.CodeNotLoggedIn,
			fmt.Sprintf(c.UI.T("无法获取模型列表：%v", "cannot list models: %v"), err)).
			WithHint("tkr login")
	}
	if len(models) == 0 {
		return nil, ui.Errf(ui.CodeInternal,
			c.UI.T("这把 Key 看不到任何模型", "this key can see no models")).
			WithHint(host + "/keys")
	}

	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)

	chosen := ids[0]
	if c.UI.Interactive(c.Flags.Bool("yes")) {
		idx, err := c.UI.Choose(
			fmt.Sprintf(c.UI.T("为 %s 选择主模型：", "Pick the main model for %s:"), h.Name), ids)
		if err != nil {
			return nil, err
		}
		chosen = ids[idx]
	} else {
		c.UI.Warnf(c.UI.T("非交互环境，自动选择 %s", "non-interactive, picked %s"), chosen)
	}

	// 其余槽位先跟随主模型，保证不会有槽落空。
	// 精细的分档推荐属于 M4，届时接入 marketplace 的价格数据。
	for _, s := range h.Slots {
		if slots[s.Name] == "" {
			slots[s.Name] = chosen
		}
	}
	slots[config.SlotDefault] = chosen

	profile.SetSlots(h.Name, slots)
	if err := cfg.Save(); err != nil {
		c.UI.Warnf(c.UI.T("模型已选定，但写入配置失败：%v", "model chosen, but saving config failed: %v"), err)
	} else if len(h.Slots) > 1 {
		c.UI.Logf("%s", c.UI.Dim(fmt.Sprintf(c.UI.T(
			"其余槽位暂时同用该模型，可用 `tkr model %s` 调整。",
			"other slots reuse the same model for now; adjust with `tkr model %s`."), h.Name)))
	}
	return slots, nil
}

// exitCodeError 让 harness 的退出码穿透 tkr。
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *exitCodeError) ExitCode() int { return e.code }
