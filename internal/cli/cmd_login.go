package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/gateway"
	"github.com/tokenflux/tkr/internal/model"
	"github.com/tokenflux/tkr/internal/ui"
)

func newLoginCommand() *Command {
	return &Command{
		Name:  "login",
		Usage: "tkr login [<名字>]",
		Summary: func(u *ui.UI) string {
			return u.T("保存 API Key", "Store an API key")
		},
		Flags: []Flag{
			// 保留作为显式写法；管道输入本来就会自动识别，不必写。
			{Name: "with-key", Kind: KindBool, Desc: "从 stdin 或隐藏输入读取 Key|Read the key from stdin or a hidden prompt"},
			{Name: "force", Kind: KindBool, Desc: "覆盖已有凭据，不询问|Overwrite the existing credential without asking"},
		},
		Run: runLogin,
	}
}

func runLogin(c *Context) error {
	st, err := loadState(c)
	if err != nil {
		return err
	}
	cfg, creds := st.cfg, st.creds

	// 标签来源：位置参数 > --key。都没有时先落到 default，
	// 冲突时再询问。显式指定则不追问。
	keyName := c.Flags.String("key")
	if len(c.Args) > 0 {
		keyName = c.Args[0]
	}
	explicit := keyName != ""
	if !explicit {
		keyName = "default"
	}
	host := config.DefaultHost
	if m, ok := cfg.Keys[keyName]; ok && m.Host != "" {
		host = m.Host
	}
	if h := c.Flags.String("host"); h != "" {
		host = normalizeHost(h)
	}

	if err := chooseLoginMethod(c); err != nil {
		return err
	}

	key, err := readKey(c)
	if err != nil {
		return err
	}
	if key == "" {
		return ui.Errf(ui.CodeUsage, c.UI.T("没有读到 Key", "no key was provided")).
			WithHint("echo $KEY | tkr login")
	}

	// 当场校验：/v1/models 是唯一能用 API Key 读到的目录接口。
	client := gateway.New(host, key)
	models, err := client.Models(context.Background())
	if err != nil {
		var apiErr *gateway.APIError
		if ok := asAPIError(err, &apiErr); ok && apiErr.InvalidKey() {
			return ui.Errf(ui.CodeNotLoggedIn,
				c.UI.T("这把 Key 不被网关接受", "the gateway rejected this key")).
				WithHint(host + "/keys").WithCause(err)
		}
		return ui.Errf(ui.CodeNotLoggedIn,
			fmt.Sprintf(c.UI.T("无法校验 Key：%v", "could not verify the key: %v"), err)).
			WithHint(host)
	}

	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}

	keyName, err = resolveLoginName(c, creds, cfg, keyName, explicit, key, ids)
	if err != nil {
		return err
	}
	meta := cfg.KeyMetaOf(keyName)
	meta.Host = host
	meta.Models = ids

	// 顺带探一次协议准入：零 token 成本，却决定了这把 Key 之后
	// 会不会出现在各 harness 的候选里。
	probeAndStore(cfg, keyName, host, key)

	creds.Set(keyName, &config.Credential{Key: key, Source: config.SourcePaste})
	if err := creds.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("凭据无法写入", "cannot write credentials")).WithCause(err)
	}
	if err := cfg.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("配置无法写入", "cannot write config")).WithCause(err)
	}

	c.UI.Emit("login", map[string]any{
		"name": keyName, "host": host,
		"key": config.Mask(key), "models": ids, "protocols": cfg.Keys[keyName].Protocols,
	}, func() {
		c.UI.Printf("✓ %s\n", fmt.Sprintf(c.UI.T("已保存为 Key %q", "saved as key %q"), keyName))
		c.UI.Printf("  %s %s\n", ui.Pad("host", 8), host)
		c.UI.Printf("  %s %s\n", ui.Pad("key", 8), config.Mask(key))
		c.UI.Printf("  %s %d %s\n", ui.Pad(c.UI.T("模型", "models"), 8), len(ids), c.UI.Dim(strings.Join(ids, ", ")))
		if protos := cfg.Keys[keyName].ProtocolSummary(); len(protos) > 0 {
			c.UI.Printf("  %s %s\n", ui.Pad(c.UI.T("协议", "protocols"), 8), c.UI.Dim(strings.Join(protos, " / ")))
			c.UI.Printf("  %s %s\n", ui.Pad(c.UI.T("可跑", "can run"), 8), strings.Join(runnable(cfg, keyName), " "))
		}
	})
	return nil
}

// resolveLoginName 处理“这个 profile 已经有另一把 Key”的情况。
//
// 默认行为绝不能是静默覆盖：覆掉的 Key 本地无处可找，用户得重新
// 去网页拿。但也不能要求用户自己想名字 —— 直接根据这把 Key 看得到的
// 模型猫一个。
func resolveLoginName(c *Context, creds *config.Credentials, cfg *config.Config,
	target string, explicit bool, key string, ids []string) (string, error) {

	existing, ok := creds.Get(target)
	switch {
	case !ok: // 该 profile 还没凭据
		return target, nil
	case existing.Key == key: // 同一把，重写无害
		return target, nil
	case explicit: // 用户点名了这个 profile，意图明确
		return target, nil
	case c.Flags.Bool("force"):
		return target, nil
	}

	suggestion := suggestKeyName(ids, creds.Names())

	if !c.UI.Interactive(c.Flags.Bool("yes")) {
		return "", ui.Errf(ui.CodeUsage, fmt.Sprintf(
			c.UI.T("%q 下已存着另一把 Key（%s）", "%q already holds a different key (%s)"),
			target, config.Mask(existing.Key))).
			WithHint(fmt.Sprintf("tkr login %s   |   tkr login --force", suggestion))
	}

	idx, err := c.UI.Select(fmt.Sprintf(
		c.UI.T("%q 下已存着另一把 Key（%s）", "%q already holds a different key (%s)"),
		target, config.Mask(existing.Key)), []ui.Item{
		{Label: fmt.Sprintf(c.UI.T("另存为 %q", "save as %q"), suggestion),
			Detail: c.UI.T("保留原有凭据", "keeps the existing one")},
		{Label: fmt.Sprintf(c.UI.T("覆盖 %q", "replace %q"), target),
			Detail: config.Mask(existing.Key) + " → " + config.Mask(key)},
		{Label: c.UI.T("自订名称…", "custom name…"),
			Detail: c.UI.T("自己输入一个", "type your own")},
	})
	if err != nil {
		return "", err
	}
	switch idx {
	case 0:
		return suggestion, nil
	case 1:
		return target, nil
	}
	return askProfileName(c, creds, suggestion)
}

// askProfileName 让用户输入 profile 名，并当场校验。
//
// 已存在的名字不直接拒绝 —— 用户可能就是想覆盖那一个，
// 但必须把影响说清楚。
func askProfileName(c *Context, creds *config.Credentials, suggestion string) (string, error) {
	for {
		name, err := c.UI.ReadLine(fmt.Sprintf(
			c.UI.T("名字 [%s]：", "name [%s]:"), suggestion))
		if err != nil {
			return "", err
		}
		if name == "" {
			return suggestion, nil
		}
		if !validProfileName(name) {
			c.UI.Warnf("%s", c.UI.T("名称只能用字母、数字、下划线和连字符，最长 32 位",
				"names may only contain letters, digits, underscores and hyphens, max 32"))
			continue
		}
		if old, exists := creds.Get(name); exists {
			c.UI.Warnf(c.UI.T("%q 已存在（%s），保存会覆盖它",
				"%q already exists (%s); saving will replace it"), name, config.Mask(old.Key))
		}
		return name, nil
	}
}

func validProfileName(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		ok := r == '-' || r == '_' ||
			(r >= '0' && r <= '9') ||
			(r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}

// suggestKeyName 从这把 Key 看得到的模型里猫一个名字。
//
// 模型名的首词元往往就是分组的性格（claude-opus-5 → claude），
// 比让用户现想一个名字要强。
func suggestKeyName(ids []string, taken []string) string {
	// 复合 Key 的分组前缀就是最好的名字来源。先看有几个分组。
	seen := map[string]bool{}
	var prefixes []string
	for _, id := range ids {
		if p := strings.ToLower(model.Parse(id).Prefix); p != "" && !seen[p] {
			seen[p] = true
			prefixes = append(prefixes, p)
		}
	}
	sort.Strings(prefixes)

	best := ""
	switch {
	// 横跨多个分组时绝不能只取其中一个 —— 按「模型最多的分组」命名会把
	// 一把 gpt+ccmax 的复合 Key 叫成 ccmax，用户会当成那把纯 Claude Max。
	case len(prefixes) > 2:
		best = "multi"
	case len(prefixes) == 2:
		best = prefixes[0] + "+" + prefixes[1]
	case len(prefixes) == 1:
		best = prefixes[0]
	default:
		// 非复合 Key：从模型基名取首词元（gpt-5.6-sol → gpt）。
		//
		// 这里和前缀不是一回事，规则也不该一样：前缀是真实的分组，
		// 每一个都算数；而词元只是标签，用来描述这把 Key 大体是什么。
		//
		// 关键是多数决要有下限。没有下限时少数派也能赢：
		// {gpt-5.6-sol, codex-auto-review} 会被叫成 codex —— 一个两模型的
		// 分组按其中的辅助审查模型命名。过半才算「能代表」，
		// 否则就老实并列，跟复合 Key 一个待遇。
		counts := map[string]int{}
		for _, id := range ids {
			if tok := leadingWord(model.Parse(id).Base); tok != "" {
				counts[tok]++
			}
		}
		toks := make([]string, 0, len(counts))
		for tok := range counts {
			toks = append(toks, tok)
		}
		sort.Slice(toks, func(i, j int) bool {
			if counts[toks[i]] != counts[toks[j]] {
				return counts[toks[i]] > counts[toks[j]]
			}
			return toks[i] < toks[j]
		})
		switch {
		case len(toks) == 0:
		case counts[toks[0]]*2 > len(ids):
			best = toks[0]
		case len(toks) == 2:
			best = toks[0] + "+" + toks[1]
			if toks[1] < toks[0] {
				best = toks[1] + "+" + toks[0]
			}
		default:
			best = "multi"
		}
	}
	if best == "" {
		best = "key"
	}

	used := map[string]bool{}
	for _, t := range taken {
		used[t] = true
	}
	name := best
	for i := 2; used[name]; i++ {
		name = fmt.Sprintf("%s-%d", best, i)
	}
	return name
}

// leadingWord 取模型 ID 的首个字母词元。
func leadingWord(id string) string {
	for i, r := range id {
		if r < 'a' || r > 'z' {
			return id[:i]
		}
	}
	return id
}

// chooseLoginMethod 让用户挑登录方式。
//
// 网页导入还没做，但要在这里露出来 —— 直接不显示会让用户以为只能贴 Key，
// 显示成可选又会白选一次。所以列出来、灰掉、写明原因。
//
// 只在真交互且没有别的输入渠道时才问：管道喂 Key、--with-key、非交互
// 都已经表明了方式，再问一遍纯属打断。
func chooseLoginMethod(c *Context) error {
	if c.Flags.Present("with-key") || !c.UI.Interactive(c.Flags.Bool("yes")) {
		return nil
	}
	if !isTerminal(os.Stdin) {
		return nil // Key 正从管道进来
	}

	idx, err := c.UI.Select(c.UI.T("怎么登录？", "How do you want to sign in?"), []ui.Item{
		{
			Label:  c.UI.T("粘贴 API Key", "Paste an API key"),
			Detail: c.UI.T("从 tokenflux.dev/keys 复制", "copy one from tokenflux.dev/keys"),
		},
		{
			Label:    c.UI.T("从网页导入", "Import from the web"),
			Detail:   c.UI.T("在浏览器里授权", "authorise in your browser"),
			Note:     c.UI.Dim(c.UI.T("v0.5", "v0.5")),
			Disabled: true,
		},
	})
	if err != nil {
		return err
	}
	_ = idx // 目前只有一个可选项；网页导入落地后在此分流
	return nil
}

// isTerminal 报告文件是否是终端。
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// readKey 依次尝试：管道 stdin → 隐藏输入。
//
// 绝不接受把 Key 写在命令行参数里 —— 那会进 shell 历史，也会被
// 同机其它进程从 ps 看到。
func readKey(c *Context) (string, error) {
	stat, err := os.Stdin.Stat()
	if err == nil && stat.Mode()&os.ModeCharDevice == 0 {
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		return strings.TrimSpace(line), nil
	}

	if !c.UI.Interactive(c.Flags.Bool("yes")) {
		return "", ui.Errf(ui.CodeUsage,
			c.UI.T("非交互环境下请用管道提供 Key", "pipe the key in when running non-interactively")).
			WithHint("echo $KEY | tkr login")
	}

	return c.UI.ReadSecret(c.UI.T("粘贴 API Key（输入不回显）：", "Paste your API key (input hidden):"))
}

// normalizeHost 归一化用户输入的 host。
//
// 用户会填 `gw.example.com`、带尾斜杠、或误带 `/v1`。协议前缀由 tkr
// 按需拼接（Anthropic 用根、OpenAI 用 /v1），这里必须还原成裸 base。
func normalizeHost(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return h
	}
	if !strings.Contains(h, "://") {
		h = "https://" + h
	}
	h = strings.TrimRight(h, "/")
	return strings.TrimSuffix(h, "/v1")
}

func asAPIError(err error, target **gateway.APIError) bool {
	if e, ok := err.(*gateway.APIError); ok {
		*target = e
		return true
	}
	return false
}
