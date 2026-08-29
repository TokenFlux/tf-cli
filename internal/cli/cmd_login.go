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
		Usage: "tkr login [--with-key]",
		Summary: func(u *ui.UI) string {
			return u.T("保存 API Key", "Store an API key")
		},
		Flags: []Flag{
			{Name: "with-key", Kind: KindBool, Desc: "从 stdin 或隐藏输入读取 Key|Read the key from stdin or a hidden prompt"},
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

	profileName := c.Flags.String("profile")
	if profileName == "" {
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
			WithHint("echo $KEY | tkr login --with-key")
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

	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		return ui.Errf(ui.CodeCredentialsRead, c.UI.T("凭据文件无法读取", "cannot read the credentials file")).WithCause(err)
	}
	creds.Set(profileName, &config.Credential{Key: key, Source: config.SourcePaste})
	if err := creds.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("凭据无法写入", "cannot write credentials")).WithCause(err)
	}
	if err := cfg.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite, c.UI.T("配置无法写入", "cannot write config")).WithCause(err)
	}

	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
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
			WithHint("echo $KEY | tkr login --with-key")
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
