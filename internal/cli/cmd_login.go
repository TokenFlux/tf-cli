package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tokenflux/tf-cli/internal/access"
	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/gateway"
	"github.com/tokenflux/tf-cli/internal/model"
	"github.com/tokenflux/tf-cli/internal/ui"
)

func newLoginCommand() *Command {
	return &Command{
		Name:  "login",
		Usage: "tf login [<name>]",
		Summary: func(u *ui.UI) string {
			return u.T("保存 API Key", "Store an API key")
		},
		Flags: []Flag{
			// 保留作为显式写法；管道输入本来就会自动识别，不必写。
			{Name: "with-key", Kind: KindBool, Desc: "从 stdin 或隐藏输入读取 Key||Read the key from stdin or a hidden prompt"},
			{Name: "from-web", Kind: KindBool, Desc: "等待网页通过本机回环地址导入 Key||Wait for a web page to import a key over loopback"},
			{Name: "force", Kind: KindBool, Desc: "名称冲突时直接覆盖；网页导入仍需确认||Overwrite on name conflicts; web import still requires confirmation"},
		},
		Run: runLogin,
	}
}

func loginMethodItems(u *ui.UI) []ui.Item {
	return []ui.Item{
		{Label: u.T("从网页导入", "Import from web"), Detail: u.T("自动打开 Keys 页面", "open the Keys page")},
		{Label: u.T("粘贴 API Key", "Paste API key"), Detail: u.T("终端隐藏输入", "hidden terminal input")},
	}
}

func runLogin(c *Context) error {
	st, err := loadState(c)
	if err != nil {
		return err
	}
	cfg, creds := st.cfg, st.creds

	// 标签来源：位置参数 > --key。都没有时先落到 default，
	// 冲突时再询问；指定名称并不等于授权覆盖凭据。
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

	fromWeb, withKey := c.Flags.Bool("from-web"), c.Flags.Bool("with-key")
	if fromWeb && withKey {
		return ui.Errf(ui.CodeUsage,
			c.UI.T("--from-web 不能和 --with-key 一起使用", "--from-web cannot be used with --with-key"))
	}
	// 管道输入本身就是明确选择，不能为了问方式而先去读 /dev/tty。
	if !fromWeb && !withKey && isTerminal(os.Stdin) && c.UI.Interactive(c.Flags.Bool("no-input")) {
		idx, err := c.UI.Select(c.UI.T("选择登录方式", "Choose a login method"), loginMethodItems(c.UI))
		if err != nil {
			return err
		}
		fromWeb = idx == 0
	}

	if c.Flags.String("host") == "" && c.UI.Interactive(c.Flags.Bool("no-input")) && (fromWeb || isTerminal(os.Stdin)) {
		host, err = selectLoginHost(c, host)
		if err != nil {
			return err
		}
	}

	var key string
	var imported *webImportRequest
	if fromWeb {
		if !c.UI.Interactive(c.Flags.Bool("no-input")) {
			return ui.Errf(ui.CodeUsage,
				c.UI.T("网页导入需要交互式终端确认", "web import requires an interactive terminal for confirmation")).
				WithHint("echo $KEY | tf login")
		}
		req, err := waitForWebImport(c, host, st.paths.CredentialsFile(), keyName,
			creds.Items[keyName], explicit || c.Flags.Bool("force"))
		if err != nil {
			return err
		}
		key, imported = req.Key, &req
	} else {
		key, err = readKey(c)
		if err != nil {
			return err
		}
		if key == "" {
			return ui.Errf(ui.CodeUsage, c.UI.T("没有读到 Key", "no key was provided")).
				WithHint("echo $KEY | tf login")
		}
	}

	// 当场校验：/v1/models 是唯一能用 API Key 读到的目录接口。
	client := gateway.New(host, key)
	ids, err := client.Models(context.Background())
	if err != nil {
		apiErr, ok := err.(*gateway.APIError)
		if ok && apiErr.InvalidKey() {
			return ui.Errf(ui.CodeNotLoggedIn,
				c.UI.T("API Key 无效或未被网关接受", "the gateway rejected this key")).
				WithHint(host + "/keys").WithCause(err)
		}
		// 连不上网关和 Key 被拒是两件事。都报 TF_NOT_LOGGED_IN 会把人
		// 引向重新登录，而重新登录解决不了网络不通 —— 实测在一台需要
		// 走代理的机器上，登录失败给出的正是这个误导性的码。
		return ui.Errf(ui.CodeNetwork,
			fmt.Sprintf(c.UI.T("无法连接网关 %s", "cannot reach the gateway at %s"), host)).
			WithHint(c.UI.T("请检查网络连接，或使用 --host 指定其他网关地址",
				"check your connection, or point --host elsewhere")).
			WithCause(err)
	}

	if imported != nil && !explicit && !c.Flags.Bool("force") {
		keyName, err = chooseImportedKeyName(c, creds, imported.KeyName, key, ids)
	} else if imported == nil {
		keyName, err = resolveLoginName(c, creds, keyName, explicit, key, ids)
	}
	if err != nil {
		return err
	}
	meta := cfg.KeyMetaOf(keyName)
	meta.Host = host
	meta.Models = ids

	// 顺带探一次协议准入：零 token 成本，却决定了这把 Key 之后
	// 会不会出现在各 harness 的候选里。
	probeAndStore(cfg, keyName, host, key)

	cred := &config.Credential{Key: key, Source: config.SourcePaste}
	if imported != nil {
		cred.Source = config.SourceImport
		cred.Origin = imported.Origin
		cred.KeyName = imported.KeyName
		cred.GroupID = imported.GroupID
		cred.GroupName = imported.GroupName
	}
	creds.Set(keyName, cred)
	if err := config.SaveState(cfg, creds); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("配置无法写入", "cannot write config")).WithCause(err)
	}

	c.UI.Emit("login", map[string]any{
		"name": keyName, "host": host,
		"key": config.Mask(key), "models": ids, "protocols": cfg.Keys[keyName].Protocols,
	}, func() {
		c.UI.Printf("✓ %s\n", fmt.Sprintf(c.UI.T("已保存为 Key %q", "saved as key %q"), keyName))
		c.UI.Printf("  %s %s\n", ui.Pad(c.UI.T("网关", "gateway"), 8), host)
		c.UI.Printf("  %s %s\n", ui.Pad(c.UI.T("Key", "key"), 8), config.Mask(key))
		c.UI.Printf("  %s %d %s\n", ui.Pad(c.UI.T("模型", "models"), 8), len(ids), c.UI.Dim(strings.Join(ids, ", ")))
		if protos := cfg.Keys[keyName].ProtocolSummary(); len(protos) > 0 {
			c.UI.Printf("  %s %s\n", ui.Pad(c.UI.T("协议", "protocols"), 8), c.UI.Dim(strings.Join(protos, " / ")))
			c.UI.Printf("  %s %s\n", ui.Pad(c.UI.T("可用于", "can run"), 8), strings.Join(access.Runnable(cfg, keyName), " "))
		}
	})

	// 放在结果之后：先让用户看见登录成功，再问补全。
	// 顺序反了会像是「还没成功就又要我做事」。
	offerCompletions(c, cfg)
	return nil
}

// chooseImportedKeyName 在网关返回模型目录后，让用户在自动识别、网页名称
// 和自订名称之间选择。网页名称只是候选，不能绕过本地名称校验。
func chooseImportedKeyName(c *Context, creds *config.Credentials, webName, key string,
	ids []string) (string, error) {
	automatic := suggestKeyName(ids, creds.Names())
	items := importedKeyNameItems(c.UI, creds, automatic, webName, key)
	idx, err := c.UI.Select(c.UI.T("选择本地 Key 名称", "Choose a local key name"), items)
	if err != nil {
		return "", err
	}
	switch {
	case idx == 0:
		return automatic, nil
	case idx == len(items)-1:
		return askKeyName(c, creds, automatic, key)
	default:
		return webName, confirmKeyReplacement(c, creds, webName, key)
	}
}

func importedKeyNameItems(u *ui.UI, creds *config.Credentials, automatic, webName,
	key string) []ui.Item {
	items := []ui.Item{{
		Label:  fmt.Sprintf(u.T("自动识别为 %q", "detect automatically as %q"), automatic),
		Detail: u.T("根据可用模型，避开已用名称", "based on available models; avoids existing names"),
	}}

	if webName != "" && webName != automatic {
		item := ui.Item{
			Label:  fmt.Sprintf(u.T("使用网页名称 %q", "use web name %q"), webName),
			Detail: u.T("网页提供的名称", "name provided by the web page"),
		}
		if !validKeyName(webName) {
			item.Detail = u.T("不符合本地名称规则", "not a valid local name")
			item.Disabled = true
		} else if old, ok := creds.Items[webName]; ok && old != nil && old.Key != "" {
			if old.Key == key {
				item.Detail = u.T("同一把 Key 已存在，将更新来源信息",
					"the same key exists; its source metadata will be updated")
			} else {
				item.Detail = fmt.Sprintf(u.T("将覆盖 %s", "replaces %s"), config.Mask(old.Key))
			}
		}
		items = append(items, item)
	}

	return append(items, ui.Item{
		Label:  u.T("自订名称…", "custom name…"),
		Detail: u.T("自己输入一个", "type your own"),
	})
}

// resolveLoginName 处理“这个 Key 名称已经有另一把 Key”的情况。
//
// 默认行为绝不能是静默覆盖：覆掉的 Key 本地无处可找，用户得重新
// 去网页拿。但也不能要求用户自己想名字 —— 直接根据这把 Key 看得到的
// 模型挑一个。
func resolveLoginName(c *Context, creds *config.Credentials,
	target string, explicit bool, key string, ids []string) (string, error) {

	existing, ok := creds.Get(target)
	switch {
	case !ok: // 该名称还没凭据
		return target, nil
	case existing.Key == key: // 同一把，重写无害
		return target, nil
	case c.Flags.Bool("force"):
		return target, nil
	case explicit:
		return target, confirmKeyReplacement(c, creds, target, key)
	}

	suggestion := suggestKeyName(ids, creds.Names())

	if !c.UI.Interactive(c.Flags.Bool("no-input")) {
		return "", ui.Errf(ui.CodeUsage, fmt.Sprintf(
			c.UI.T("%q 下已存着另一把 Key（%s）", "%q already holds a different key (%s)"),
			target, config.Mask(existing.Key))).
			WithHint(fmt.Sprintf("tf login %s   |   tf login --force", suggestion))
	}

	idx, err := c.UI.Select(fmt.Sprintf(
		c.UI.T("%q 下已存着另一把 Key（%s）", "%q already holds a different key (%s)"),
		target, config.Mask(existing.Key)), []ui.Item{
		{Label: fmt.Sprintf(c.UI.T("另存为 %q", "save as %q"), suggestion),
			Detail: c.UI.T("保留原有的", "keeps the existing one")},
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
	return askKeyName(c, creds, suggestion, key)
}

// askKeyName 让用户输入 Key 名称，并当场校验。
//
// 已存在的名字不直接拒绝 —— 用户可能就是想覆盖那一个，
// 但必须把影响说清楚。
func askKeyName(c *Context, creds *config.Credentials, suggestion, key string) (string, error) {
	for {
		name, err := c.UI.ReadLine(fmt.Sprintf(
			c.UI.T("Key 名称 [%s]：", "key name [%s]:"), suggestion))
		if err != nil {
			return "", err
		}
		if name == "" {
			return suggestion, nil
		}
		if !validKeyName(name) {
			c.UI.Warnf("%s", c.UI.T("名称只能用字母、数字、下划线和连字符，最长 32 位",
				"names may only contain letters, digits, underscores and hyphens, max 32"))
			continue
		}
		return name, confirmKeyReplacement(c, creds, name, key)
	}
}

func confirmKeyReplacement(c *Context, creds *config.Credentials, name, key string) error {
	old, exists := creds.Get(name)
	if !exists || old.Key == key || c.Flags.Bool("force") {
		return nil
	}
	if !c.UI.Interactive(c.Flags.Bool("no-input")) {
		return ui.Errf(ui.CodeUsage, fmt.Sprintf(c.UI.T("%q 已存在，覆盖需要确认", "%q already exists; replacement requires confirmation"), name)).
			WithHint(fmt.Sprintf("tf login %s --force", name))
	}
	pick, err := c.UI.Select(fmt.Sprintf(c.UI.T("覆盖 Key %q？", "Replace key %q?"), name), []ui.Item{
		{Label: c.UI.T("取消", "cancel")},
		{Label: c.UI.T("确认覆盖", "replace"), Detail: config.Mask(old.Key) + " → " + config.Mask(key)},
	})
	if err != nil {
		return err
	}
	if pick == 0 {
		return ui.Errf(ui.CodeCancelled, c.UI.T("已取消", "cancelled"))
	}
	return nil
}

func validKeyName(s string) bool {
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

	if !c.UI.Interactive(c.Flags.Bool("no-input")) {
		msg := c.UI.T("非交互模式请通过管道传入 Key", "pipe the key in when non-interactive")
		return "", ui.Errf(ui.CodeUsage, msg).WithHint("echo $KEY | tf login")
	}

	key, err := c.UI.ReadSecret(c.UI.T("粘贴 API Key（输入不回显）：", "Paste API key (hidden):"))
	if err != nil {
		if ui.AsError(err).Code == ui.CodeCancelled {
			return "", err
		}
		// ui 层的哨兵错误只有英文，是底层措辞；本地化只在命令层做，
		// 直接抛上去会让中文界面顶着一句英文。
		return "", ui.Errf(ui.CodeUsage,
			c.UI.T("隐藏输入需要终端支持", "hidden input needs a terminal")).
			WithHint("echo $KEY | tf login")
	}
	return key, nil
}

func loginGatewayItems(u *ui.UI, current string) ([]ui.Item, int) {
	items := []ui.Item{
		{Label: u.T("默认网关", "Default gateway"), Detail: config.DefaultHost},
		{Label: u.T("自定义网关", "Custom gateway"), Detail: u.T("输入网关地址", "Enter a gateway URL")},
	}
	defaultIndex := 0
	if current != "" && normalizeHost(current) != normalizeHost(config.DefaultHost) {
		items[1].Detail = current
		items[0], items[1] = items[1], items[0]
		defaultIndex = 1
	}
	return items, defaultIndex
}

func selectLoginHost(c *Context, current string) (string, error) {
	items, defaultIndex := loginGatewayItems(c.UI, current)
	idx, err := c.UI.Select(c.UI.T("选择网关", "Choose a gateway"), items)
	if err != nil {
		return "", err
	}
	if idx == defaultIndex {
		return normalizeHost(config.DefaultHost), nil
	}
	fallback := ""
	prompt := c.UI.T("网关地址：", "Gateway URL:")
	if current != "" && normalizeHost(current) != normalizeHost(config.DefaultHost) {
		fallback = normalizeHost(current)
		prompt = fmt.Sprintf(c.UI.T("网关地址 [%s]：", "Gateway URL [%s]:"), fallback)
	}
	for attempt := 0; attempt < 3; attempt++ {
		value, err := c.UI.ReadLine(prompt)
		if err != nil {
			return "", err
		}
		if value == "" {
			value = fallback
		}
		host := normalizeHost(value)
		if _, err := webOrigin(host); err == nil {
			return host, nil
		}
		c.UI.Warnf("%s", c.UI.T("请输入有效的 HTTP(S) 网关地址，不含用户名、密码、查询参数或片段", "Enter a valid HTTP(S) gateway URL without credentials, query or fragment"))
	}
	return "", ui.Errf(ui.CodeUsage, c.UI.T("网关地址无效", "invalid gateway address"))
}

// normalizeHost 归一化用户输入的 host。
//
// 用户会填 `gw.example.com`、带尾斜杠、或误带 `/v1`。协议前缀由 tf
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
