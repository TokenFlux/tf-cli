package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/gateway"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/model"
	"github.com/tokenflux/tkr/internal/ui"
)

// candidate 是「某把 Key 提供的某个模型」。
//
// 模型和 Key 必须成对出现：同一个模型 ID 可能由多把 Key 提供，
// 而它们的分组、倍率、可用性都不同。
type candidate struct {
	Key   string
	Model string
}

// eligibleKeys 返回协议上能跑该 harness 的 Key。
//
// -k 指定时只保留那一把。未探测过的 Key 一律保留 —— 预检只能证伪。
func eligibleKeys(c *Context, cfg *config.Config, creds *config.Credentials, h *harness.Harness) ([]string, error) {
	names := creds.Names()
	if len(names) == 0 {
		return nil, ui.Errf(ui.CodeNotLoggedIn,
			c.UI.T("本机没有保存任何 Key", "no keys are stored on this machine")).
			WithHint("tf login")
	}

	if want := c.Flags.String("key"); want != "" {
		if _, ok := creds.Get(want); !ok {
			return nil, ui.Errf(ui.CodeKeyNotFound,
				fmt.Sprintf(c.UI.T("没有名为 %q 的 Key", "no key named %q"), want)).
				WithHint(strings.Join(names, " | "))
		}
		return []string{want}, nil
	}

	fit := fitting(cfg, names, h)
	if len(fit) == 0 {
		// 探测结果会过期：用户在网页上改了分组绑定后，缓存的「不支持」
		// 会让一把现在可用的 Key 凭空消失，而用户无从得知。
		// 放在失败路径上重探：顺利时零开销，出事时自愈。
		if reprobe(c, cfg, creds, names) {
			fit = fitting(cfg, names, h)
		}
	}
	if len(fit) == 0 {
		return nil, noKeyFitsError(c, cfg, h, names)
	}
	return fit, nil
}

// canRun 报告某个分组能否跑这个 harness。
//
// claude_code_only 分组拦的是客户端指纹而不是协议：只有 Claude Code
// 本身过得去，其它 harness 无论用什么协议都会被拒。
func canRun(meta *config.KeyMeta, prefix string, h *harness.Harness) bool {
	_, ok := pickProtocol(meta, prefix, h)
	return ok
}

// pickProtocol 选出这次该走哪种协议。
//
// harness 会的协议按偏好排序，取第一个该分组也允许的。
// 多数 harness 不止会一种：opencode 两个 provider 都内置，
// 因此它在只开 anthropic_messages 的分组上照样能跑。
func pickProtocol(meta *config.KeyMeta, prefix string, h *harness.Harness) (harness.Protocol, bool) {
	return pickProtocolFor(meta, prefix, "", h)
}

// pickProtocolFor 在知道具体模型时按模型的原生协议优先。
//
// 网关两种协议都能翻译（实测 /v1/responses 打 claude-opus-4-6 确实能用），
// 但翻译只会丢信息不会补信息 —— 思考块、缓存标记、工具调用的细节都要
// 过一道映射。而且 opencode 会照实显示 provider：用 openai 跑 claude 模型
// 界面上写着「OpenAI」，看起来像配错了。
//
// 猜错也无害：网关照样翻译。所以这只是偏好排序，不是准入判断。
func pickProtocolFor(meta *config.KeyMeta, prefix, modelID string, h *harness.Harness) (harness.Protocol, bool) {
	if meta.LockedToClaudeCode(prefix) {
		if h.IsClaudeCode {
			return harness.ProtoAnthropicMessages, true
		}
		return "", false
	}
	if native := nativeProtocol(modelID); native != "" &&
		h.Speaks(string(native)) && meta.SupportsIn(prefix, string(native)) {
		return native, true
	}
	for _, p := range h.Protocols {
		if meta.SupportsIn(prefix, string(p)) {
			return p, true
		}
	}
	return "", false
}

// nativeProtocol 报告该模型「原生」说哪种协议。认不出来就返回空。
//
// 只按模型名判断，不去猜分组的 platform：认错的代价仅仅是多一道翻译，
// 而为此在客户端建一套推断才是真的得不偿失。
func nativeProtocol(modelID string) harness.Protocol {
	if modelID == "" {
		return ""
	}
	if strings.HasPrefix(model.Parse(modelID).Base, "claude-") {
		return harness.ProtoAnthropicMessages
	}
	return ""
}

// protocolList 列出该 harness 会的协议，用于解释为什么没有 Key 合格。
func protocolList(h *harness.Harness) string {
	out := make([]string, 0, len(h.Protocols))
	for _, p := range h.Protocols {
		out = append(out, string(p))
	}
	return strings.Join(out, " / ")
}

// fitting 返回至少有一个分组能跑该 harness 的 Key。未探测过的视为可用。
func fitting(cfg *config.Config, names []string, h *harness.Harness) []string {
	var out []string
	for _, n := range names {
		meta := cfg.Keys[n]
		if !meta.Probed() {
			out = append(out, n)
			continue
		}
		for _, prefix := range meta.Scopes() {
			if canRun(meta, prefix, h) {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// gatherCandidates 汇总所有合格 Key 能提供的模型。
//
// 关键在于**不按 Key 分割候选集**：用户想的是「我要用哪个模型」，
// Key 是实现细节。同一个 harness 可能有多把 Key 都能跑
// （ChatGPT 分组也开了 anthropic_messages，gpt 模型确实能走 Claude Code），
// 只列绑定那把的模型等于凭空藏起一半选项。
func gatherCandidates(c *Context, cfg *config.Config, creds *config.Credentials,
	keys []string, h *harness.Harness) []candidate {

	lists := refreshModels(c, cfg, creds, keys)

	var out []candidate
	for _, name := range keys {
		meta := cfg.Keys[name]
		for _, id := range lists[name] {
			// 同一把 Key 内不同分组的准入也不同：复合 Key 里
			// GPT/* 能跑 codex，Claude/* 只能跑 claude。
			if canRun(meta, model.Parse(id).Prefix, h) {
				out = append(out, candidate{Key: name, Model: id})
			}
		}
	}
	return out
}

// refreshModels 并发刷新各 Key 的模型列表，失败的沿用本地那份。
func refreshModels(c *Context, cfg *config.Config, creds *config.Credentials, keys []string) map[string][]string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		out     = map[string][]string{}
		changed bool
		stale   []string
	)
	for _, name := range keys {
		cred, ok := creds.Get(name)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(name, key string) {
			defer wg.Done()
			ids, err := fetchModels(ctx, cfg.HostOf(name), key)

			mu.Lock()
			defer mu.Unlock()
			if err == nil && len(ids) > 0 {
				out[name] = ids
				if !sameStrings(cfg.KeyMetaOf(name).Models, ids) {
					cfg.KeyMetaOf(name).Models = ids
					changed = true
				}
				return
			}
			out[name] = cfg.KeyMetaOf(name).Models
			if len(out[name]) > 0 {
				stale = append(stale, name)
			}
		}(name, cred.Key)
	}
	wg.Wait()

	if changed {
		_ = cfg.Save()
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		c.UI.Warnf(c.UI.T("模型列表取不到，沿用上次的结果：%s",
			"could not refresh the model list, using the last known one: %s"), strings.Join(stale, " "))
	}
	return out
}

func fetchModels(ctx context.Context, host, key string) ([]string, error) {
	models, err := gateway.New(host, key).Models(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	// 保持网关给的顺序，不要按字典序重排。
	//
	// 网关把 codex-auto-review 这类专用模型放在末尾，字典序却把它顶到
	// 第一个 —— 选择器默认高亮首项，回车就选中了一个当主模型必定失败的
	// 模型。上游的排序本身就是信息，重排等于把它扔掉。
	return ids, nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// candidateItems 把候选变成选择器条目。
//
// 只有当候选来自多把 Key 时才显示 Key 名 —— 单 Key 时那是噪音。
func candidateItems(cands []candidate) []ui.Item {
	multi := false
	for _, c := range cands {
		if c.Key != cands[0].Key {
			multi = true
			break
		}
	}

	items := make([]ui.Item, 0, len(cands))
	for _, c := range cands {
		r := model.Parse(c.Model)
		// 分组带斜杠，与 tf keys 一致，也对上模型 ID 的写法。
		detail := r.Prefix
		if detail != "" {
			detail += "/"
		}
		if multi {
			detail = strings.TrimSpace(c.Key + "  " + detail)
		}
		items = append(items, ui.Item{Label: r.Display(), Detail: detail})
	}
	return items
}

// bindKey 记住绑定关系。写盘失败不阻断启动。
func bindKey(c *Context, cfg *config.Config, h *harness.Harness, name string) {
	hc := cfg.Harness(h.Name)
	if hc.Key == name {
		return
	}
	hc.Key = name
	if err := cfg.Save(); err != nil {
		c.UI.Warnf(c.UI.T("绑定未能写入配置：%v", "could not persist the binding: %v"), err)
	}
}

// reprobe 重新探测所有 Key，报告是否有任何结果发生变化。
func reprobe(c *Context, cfg *config.Config, creds *config.Credentials, names []string) bool {
	before := fmt.Sprint(cfg.Keys)
	c.UI.Logf("%s", c.UI.Dim(c.UI.T("重新检查各 Key…", "re-checking each key…")))
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
		c.UI.T("没有一把 Key 的分组允许 %s 所需的协议（%s）",
			"no key's group allows what %s needs (%s)"),
		h.Name, protocolList(h))).
		WithHint(strings.Join(lines, "; ") + "  →  " +
			c.UI.T("换一个允许该协议的分组，或新建一把 Key",
				"switch to a group that allows this protocol, or create another key"))
}

// probeAndStore 探测某把 Key 各分组的协议准入并写入配置。
//
// 零 token 成本，但仍是若干次网络往返，所以只在 login 与失败重试时做。
func probeAndStore(cfg *config.Config, name, host, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	meta := cfg.KeyMetaOf(name)

	// 复合 Key 必须逐前缀探：一把 Key 横跨多个分组，
	// 每个分组的协议准入各不相同。前缀由 /v1/models 的 ID 直接给出。
	probed, err := gateway.New(host, key).ProbeProtocols(ctx, model.Prefixes(meta.Models))
	if err != nil || len(probed) == 0 {
		return
	}
	meta.Host = host
	meta.Protocols = map[string][]string{}
	meta.ClaudeCodeOnly = map[string]bool{}
	for prefix, adm := range probed {
		if adm.ClaudeCodeOnly {
			meta.ClaudeCodeOnly[prefix] = true
			continue
		}
		// 一个协议都不准入，多半是我们没读懂回答，而不是真有一个
		// 什么都做不了的分组。此时宁可不记 —— 没有证据就不过滤。
		//
		// 这条是给文案变化留的余地：claude_code_only 靠拒绝文案识别，
		// 网关一旦改词，这里会退化成「全部拒绝」，进而把一把好端端的
		// Key 从所有 harness 里抹掉，且没有任何线索。相比之下，
		// 放它进候选、让网关在启动时明确报错要好得多。
		if len(adm.Protocols) == 0 {
			continue
		}
		list := make([]string, 0, len(adm.Protocols))
		for _, p := range adm.Protocols {
			list = append(list, string(p))
		}
		meta.Protocols[prefix] = list
	}
	meta.ProbedAt = time.Now()
}

// runnable 列出这把 Key 能跑的 harness。
func runnable(cfg *config.Config, name string) []string {
	return runnableIn(cfg.Keys[name], config.GroupScope)
}

// runnableIn 列出某个分组能跑的 harness。
func runnableIn(meta *config.KeyMeta, prefix string) []string {
	var out []string
	for _, h := range harness.All {
		if canRun(meta, prefix, h) {
			out = append(out, h.Name)
		}
	}
	return out
}

// noteHiddenKeys 说明哪些模型没有出现在候选里，以及为什么。
//
// 静默过滤是最难排查的一种行为：用户看到的是「我的模型不见了」，
// 而没有任何线索指向原因。能藏就必须能解释。
//
// 粒度必须是「Key + 分组」而不是整把 Key：复合 Key 横跨多个分组，
// 各自的准入不同 —— 一把 gpt+ccmax 的 Key 对 opencode 完全合格，
// 但其中 8 个 ccmax 模型仍然一个都用不了。
func noteHiddenKeys(c *Context, cfg *config.Config, all, _ []string, h *harness.Harness) {
	type miss struct {
		key, prefix, why string
		n                int
	}
	var out []miss

	for _, name := range all {
		meta := cfg.Keys[name]
		if meta == nil || !meta.Probed() {
			continue
		}
		counts := map[string]int{}
		for _, id := range meta.Models {
			counts[model.Parse(id).Prefix]++
		}
		prefixes := make([]string, 0, len(counts))
		for p := range counts {
			prefixes = append(prefixes, p)
		}
		sort.Strings(prefixes)

		for _, prefix := range prefixes {
			if canRun(meta, prefix, h) {
				continue
			}
			why := fmt.Sprintf(c.UI.T("该分组不允许 %s 需要的协议（%s）",
				"that group does not allow what %s needs (%s)"), h.Name, protocolList(h))
			if meta.LockedToClaudeCode(prefix) {
				why = c.UI.T("该分组只接受 Claude Code 客户端",
					"that group only accepts the Claude Code client")
			}
			out = append(out, miss{key: name, prefix: prefix, why: why, n: counts[prefix]})
		}
	}

	for _, m := range out {
		// 复合 Key 要指到分组，否则用户不知道该去改哪一半。
		where := fmt.Sprintf("%q", m.key)
		if m.prefix != "" {
			where = fmt.Sprintf("%q (%s)", m.key, m.prefix)
		}
		if c.UI.Lang == ui.LangZH {
			c.UI.Logf("%s", c.UI.Dim(fmt.Sprintf("已隐藏 %s 的 %d 个模型：%s", where, m.n, m.why)))
		} else {
			c.UI.Logf("%s", c.UI.Dim(fmt.Sprintf("hiding %d models from %s: %s", m.n, where, m.why)))
		}
	}
}
