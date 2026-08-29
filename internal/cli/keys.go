package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/gateway"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/model"
	"github.com/tokenflux/tkr/internal/ui"
)

// resolveKey 决定这次启动用哪把 Key。
//
//	-k 指定        → 用它
//	已有绑定且有效 → 用它，不提问
//	能力筛选后唯一 → 用它并记住，不提问
//	多个            → 问一次，记住
//	零个            → 报错，逐把说明差在哪
//
// 关键性质：不能跑这个 harness 的 Key 根本不会进入候选，
// 所以「用错 Key」不是需要小心避免的事，而是结构上不可能。
func resolveKey(c *Context, cfg *config.Config, creds *config.Credentials, h *harness.Harness) (string, error) {
	names := creds.Names()
	if len(names) == 0 {
		return "", ui.Errf(ui.CodeNotLoggedIn,
			c.UI.T("本机没有保存任何 Key", "no keys are stored on this machine")).
			WithHint("tkr login")
	}

	if want := c.Flags.String("key"); want != "" {
		if _, ok := creds.Get(want); !ok {
			return "", ui.Errf(ui.CodeKeyNotFound,
				fmt.Sprintf(c.UI.T("没有名为 %q 的 Key", "no key named %q"), want)).
				WithHint(strings.Join(names, " | "))
		}
		return want, nil
	}

	hc := cfg.Harness(h.Name)
	if hc.Key != "" {
		if _, ok := creds.Get(hc.Key); ok {
			return hc.Key, nil
		}
		// 绑定的 Key 已被删除：清掉残留，重新走筛选。
		hc.Key = ""
	}

	fit := fitting(cfg, names, h)

	switch len(fit) {
	case 0:
		// 探测结果会过期：用户在网页上改了分组绑定后，缓存里的
		// “不支持”会让一把现在可用的 Key 凭空消失，而用户无从得知。
		//
		// 所以失败前必须重探一次。放在失败路径上而不是设 TTL：
		// 顺利时零开销，出事时自愈。
		if reprobe(c, cfg, creds, names) {
			if fit = fitting(cfg, names, h); len(fit) == 1 {
				return bindKey(c, cfg, h, fit[0])
			}
		}
		if len(fit) == 0 {
			return "", noKeyFitsError(c, cfg, h, names)
		}
	case 1:
		return bindKey(c, cfg, h, fit[0])
	}

	if !c.UI.Interactive(c.Flags.Bool("yes")) {
		return "", ui.Errf(ui.CodeUsage,
			fmt.Sprintf(c.UI.T("有多把 Key 能跑 %s，请指定一把", "several keys can run %s; pick one"), h.Name)).
			WithHint("tkr " + h.Name + " -k " + strings.Join(fit, " | "))
	}

	items := make([]ui.Item, 0, len(fit))
	for _, n := range fit {
		cred, _ := creds.Get(n)
		items = append(items, ui.Item{Label: n, Detail: config.Mask(cred.Key)})
	}
	idx, err := c.UI.Select(fmt.Sprintf(
		c.UI.T("%s 用哪把 Key？（记住后不再问）", "Which key should %s use? (remembered afterwards)"),
		h.Name), items)
	if err != nil {
		return "", err
	}
	return bindKey(c, cfg, h, fit[idx])
}

// fitting 返回能跑该 harness 的 Key。
//
// 未探测过的 Key 一律视为可用 —— 没有证据就不拦。
func fitting(cfg *config.Config, names []string, h *harness.Harness) []string {
	var out []string
	for _, n := range names {
		if cfg.Keys[n].Supports(string(h.Protocol)) {
			out = append(out, n)
		}
	}
	return out
}

// reprobe 重新探测所有 Key，报告是否有任何结果发生变化。
func reprobe(c *Context, cfg *config.Config, creds *config.Credentials, names []string) bool {
	before := fmt.Sprint(cfg.Keys)
	c.UI.Logf("%s", c.UI.Dim(c.UI.T("重新检查各 Key 的可用协议…", "re-checking what each key allows…")))
	for _, n := range names {
		cred, ok := creds.Get(n)
		if !ok {
			continue
		}
		probeAndStore(cfg, n, cfg.HostOf(n), cred.Key)
	}
	_ = cfg.Save()
	return fmt.Sprint(cfg.Keys) != before
}

// bindKey 记住绑定关系。写盘失败不阻断启动。
func bindKey(c *Context, cfg *config.Config, h *harness.Harness, name string) (string, error) {
	hc := cfg.Harness(h.Name)
	if hc.Key == name {
		return name, nil
	}
	hc.Key = name
	if err := cfg.Save(); err != nil {
		c.UI.Warnf(c.UI.T("绑定未能写入配置：%v", "could not persist the binding: %v"), err)
	}
	return name, nil
}

// noKeyFitsError 解释为什么一把 Key 都用不了。
//
// 只说「没有可用的 Key」等于没说。要逐把讲清楚它支持什么、
// 缺什么，以及可以怎么办。
func noKeyFitsError(c *Context, cfg *config.Config, h *harness.Harness, names []string) error {
	var lines []string
	for _, n := range names {
		got := "?"
		if m := cfg.Keys[n]; m.Probed() {
			got = strings.Join(m.ProtocolSummary(), " / ")
		}
		lines = append(lines, fmt.Sprintf("%s: %s", n, got))
	}
	return ui.Errf(ui.CodeProtocolMismatch, fmt.Sprintf(
		c.UI.T("没有一把 Key 的分组允许 %s（%s 需要 %s）",
			"no key's group allows what %s needs (%s requires %s)"),
		h.Name, h.Name, h.Protocol)).
		WithHint(strings.Join(lines, "; ") + "  →  " +
			c.UI.T("换一个允许该协议的分组，或新建一把 Key",
				"switch to a group that allows this protocol, or create another key"))
}

// probeAndStore 探测某把 Key 的协议准入并写入配置。
//
// 零 token 成本，但仍是三次网络往返，所以只在 login 和缓存过期时做。
func probeAndStore(cfg *config.Config, name, host, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	meta := cfg.KeyMetaOf(name)

	// 复合 Key 必须逐前缀探：一把 Key 横跨多个分组，
	// 每个分组的协议准入各不相同。前缀由 /v1/models 的 ID 直接给出。
	prefixes := model.Prefixes(meta.Models)

	probed, err := gateway.New(host, key).ProbeProtocols(ctx, prefixes)
	if err != nil || len(probed) == 0 {
		return
	}
	meta.Host = host
	meta.Protocols = map[string][]string{}
	for prefix, protos := range probed {
		list := make([]string, 0, len(protos))
		for _, p := range protos {
			list = append(list, string(p))
		}
		meta.Protocols[prefix] = list
	}
	meta.ProbedAt = time.Now()
}

// runnable 列出这把 Key 能跑的 harness。
func runnable(cfg *config.Config, name string) []string {
	var out []string
	for _, h := range harness.All {
		if cfg.Keys[name].Supports(string(h.Protocol)) {
			out = append(out, h.Name)
		}
	}
	return out
}
