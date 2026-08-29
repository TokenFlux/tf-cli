package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/gateway"
	"github.com/tokenflux/tkr/internal/ui"
)

func newLoginCommand() *Command {
	return &Command{
		Name:  "login",
		Usage: "tkr login [<profile>]",
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
	paths, err := config.DefaultPaths()
	if err != nil {
		return ui.Errf(ui.CodeConfigRead, c.UI.T("无法定位配置目录", "cannot locate the config directory")).WithCause(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return ui.Errf(ui.CodeConfigRead, c.UI.T("配置文件无法读取", "cannot read the config file")).WithCause(err)
	}

	// profile 来源：位置参数 > --profile > 当前 profile。
	// 前两者算“用户明确指定”，不会再因冲突而追问。
	profileName := c.Flags.String("profile")
	if len(c.Args) > 0 {
		profileName = c.Args[0]
	}
	explicit := profileName != ""
	if !explicit {
		profileName = cfg.Current
	}
	profile, ok := cfg.Profile(profileName)
	if !ok {
		profile = &config.Profile{Host: config.DefaultHost}
		cfg.Profiles[profileName] = profile
	}
	if h := c.Flags.String("host"); h != "" {
		profile.Host = normalizeHost(h)
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
	client := gateway.New(profile.Host, key)
	models, err := client.Models(context.Background())
	if err != nil {
		var apiErr *gateway.APIError
		if ok := asAPIError(err, &apiErr); ok && apiErr.InvalidKey() {
			return ui.Errf(ui.CodeNotLoggedIn,
				c.UI.T("这把 Key 不被网关接受", "the gateway rejected this key")).
				WithHint(profile.Host + "/keys").WithCause(err)
		}
		return ui.Errf(ui.CodeNotLoggedIn,
			fmt.Sprintf(c.UI.T("无法校验 Key：%v", "could not verify the key: %v"), err)).
			WithHint(profile.Host)
	}

	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}

	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		return ui.Errf(ui.CodeCredentialsRead, c.UI.T("凭据文件无法读取", "cannot read the credentials file")).WithCause(err)
	}
	profileName, err = resolveLoginProfile(c, creds, cfg, profileName, explicit, key, ids)
	if err != nil {
		return err
	}
	if _, ok := cfg.Profile(profileName); !ok {
		cfg.Profiles[profileName] = &config.Profile{Host: profile.Host}
	}

	creds.Set(profileName, &config.Credential{Key: key, Source: config.SourcePaste})
	if err := creds.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("凭据无法写入", "cannot write credentials")).WithCause(err)
	}
	if err := cfg.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("配置无法写入", "cannot write config")).WithCause(err)
	}

	// 顺手落一份模型缓存：补全必须零网络，这是它唯一的数据来源。
	if err := paths.WriteCache("models", ids); err != nil {
		c.UI.Warnf(c.UI.T("模型缓存写入失败：%v", "could not cache the model list: %v"), err)
	}

	c.UI.Emit("login", map[string]any{
		"profile": profileName, "host": profile.Host,
		"key": config.Mask(key), "models": ids,
	}, func() {
		c.UI.Printf("✓ %s\n", fmt.Sprintf(c.UI.T("已保存到 profile %q", "saved to profile %q"), profileName))
		c.UI.Printf("  %-8s %s\n", "host", profile.Host)
		c.UI.Printf("  %-8s %s\n", "key", config.Mask(key))
		c.UI.Printf("  %-8s %d %s\n", c.UI.T("模型", "models"), len(ids), c.UI.Dim(strings.Join(ids, ", ")))
	})
	return nil
}

// resolveLoginProfile 处理“这个 profile 已经有另一把 Key”的情况。
//
// 默认行为绝不能是静默覆盖：覆掉的 Key 本地无处可找，用户得重新
// 去网页拿。但也不能要求用户自己想名字 —— 直接根据这把 Key 看得到的
// 模型猫一个。
func resolveLoginProfile(c *Context, creds *config.Credentials, cfg *config.Config,
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

	suggestion := suggestProfileName(ids, creds.Names())

	if !c.UI.Interactive(c.Flags.Bool("yes")) {
		return "", ui.Errf(ui.CodeUsage, fmt.Sprintf(
			c.UI.T("profile %q 已保存另一把 Key（%s）", "profile %q already holds a different key (%s)"),
			target, config.Mask(existing.Key))).
			WithHint(fmt.Sprintf("tkr login %s   |   tkr login --force", suggestion))
	}

	idx, err := c.UI.Select(fmt.Sprintf(
		c.UI.T("profile %q 已有另一把 Key（%s）", "profile %q already holds a different key (%s)"),
		target, config.Mask(existing.Key)), []ui.Item{
		{Label: fmt.Sprintf(c.UI.T("另存为 %q", "save as %q"), suggestion),
			Detail: c.UI.T("保留原有凭据", "keeps the existing one")},
		{Label: fmt.Sprintf(c.UI.T("覆盖 %q", "replace %q"), target),
			Detail: config.Mask(existing.Key) + " → " + config.Mask(key)},
	})
	if err != nil {
		return "", err
	}
	if idx == 0 {
		return suggestion, nil
	}
	return target, nil
}

// suggestProfileName 从这把 Key 看得到的模型里猫一个名字。
//
// 模型名的首词元往往就是分组的性格（claude-opus-5 → claude），
// 比让用户现想一个名字要强。
func suggestProfileName(ids []string, taken []string) string {
	counts := map[string]int{}
	for _, id := range ids {
		if tok := leadingWord(id); tok != "" {
			counts[tok]++
		}
	}
	best, bestN := "", 0
	for tok, n := range counts {
		if n > bestN || (n == bestN && tok < best) {
			best, bestN = tok, n
		}
	}
	if best == "" {
		best = "profile"
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

	fmt.Fprintf(c.UI.Err, "%s ", c.UI.T("粘贴 API Key（输入不回显）：", "Paste your API key (input hidden):"))
	defer fmt.Fprintln(c.UI.Err)

	restore := disableEcho()
	defer restore()

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", ui.ErrNotInteractive
	}
	return strings.TrimSpace(line), nil
}

// disableEcho 借 stty 关闭回显，避免为此引入终端库。
// 不可用时降级为可见输入，而不是失败。
func disableEcho() func() {
	if os.Getenv("OS") == "Windows_NT" {
		return func() {}
	}
	cmd := exec.Command("stty", "-echo")
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return func() {}
	}
	return func() {
		restore := exec.Command("stty", "echo")
		restore.Stdin = os.Stdin
		_ = restore.Run()
	}
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
