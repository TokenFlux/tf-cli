package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tokenflux/tkr/internal/gateway"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/model"
	"github.com/tokenflux/tkr/internal/ui"
)

// newStatusCommand 回答「现在敲 tf claude 会发生什么」。
//
// 启动横幅写着用了哪把 Key、哪个模型，但 Claude Code 与 codex 一进
// alternate screen 那行就没了 —— 出问题回头找，找不到。
//
// 只发一次 /v1/usage：那是唯一会改变「下一次请求能不能成」的东西。
// 额度用完时 harness 只会报一个 429，不说为什么 —— 这条命令要能答上。
// 取不到就不显示，本机状态照常出。
func newStatusCommand() *Command {
	return &Command{
		Name:  "status",
		Usage: "tf status",
		Summary: func(u *ui.UI) string {
			return u.T("显示当前会用哪把 Key、哪个模型", "Show which key and models are in effect")
		},
		Run: runStatus,
	}
}

type statusHarness struct {
	Name      string            `json:"harness"`
	Installed bool              `json:"installed"`
	Version   string            `json:"version,omitempty"`
	Key       string            `json:"key,omitempty"`
	Slots     map[string]string `json:"slots,omitempty"`
}

type statusOut struct {
	ConfigDir string                    `json:"config_dir"`
	Keys      []string                  `json:"keys"`
	Harnesses []statusHarness           `json:"harnesses"`
	Usage     map[string]*gateway.Usage `json:"usage,omitempty"`
	Problems  []string                  `json:"problems,omitempty"`
}

func runStatus(c *Context) error {
	st, err := loadState(c)
	if err != nil {
		return err
	}
	cfg, creds := st.cfg, st.creds

	paths, err := config.DefaultPaths()
	if err != nil {
		return ui.Errf(ui.CodeConfigRead,
			c.UI.T("无法定位配置目录", "cannot locate the config directory")).WithCause(err)
	}

	out := statusOut{ConfigDir: paths.ConfigDir, Keys: creds.Names()}
	for _, h := range harness.All {
		d := h.Detect()
		hc := cfg.Harness(h.Name)
		out.Harnesses = append(out.Harnesses, statusHarness{
			Name: h.Name, Installed: d.Installed, Version: d.Version,
			Key: hc.Key, Slots: hc.Slots,
		})
	}
	out.Usage = fetchUsage(cfg, creds)
	out.Problems = checkEnvironment(c, paths)

	c.UI.Emit("status", out, func() { printStatus(c, cfg, creds, out) })
	return nil
}

func printStatus(c *Context, cfg *config.Config, creds *config.Credentials, out statusOut) {
	c.UI.Printf("%s\n", c.UI.Dim(out.ConfigDir))

	if len(out.Keys) == 0 {
		c.UI.Printf("\n%s\n", c.UI.T("还没有 Key", "no keys yet"))
	}
	for _, name := range out.Keys {
		cred, ok := creds.Get(name)
		if !ok {
			continue
		}
		meta := cfg.KeyMetaOf(name)
		c.UI.Printf("\n%s  %s  %s\n", name, c.UI.Dim(config.Mask(cred.Key)),
			c.UI.Dim(fmt.Sprintf(c.UI.T("%d 个模型", "%d models"), len(meta.Models))))
		printUsage(c, out.Usage[name])
	}

	// 列宽按本屏内容量出来：Key 名长短不一，写死会错位。
	c.UI.Printf("\n")
	nameW, keyW := 0, 0
	for _, h := range out.Harnesses {
		if w := ui.Width(h.Name); w > nameW {
			nameW = w
		}
		if w := ui.Width(h.Key); w > keyW {
			keyW = w
		}
	}
	for _, h := range out.Harnesses {
		line := "  " + ui.Pad(h.Name, nameW)
		if !h.Installed {
			c.UI.Printf("%s  %s\n", line, c.UI.Dim(c.UI.T("未安装", "not installed")))
			continue
		}
		key := h.Key
		if key == "" {
			// 没绑定不是错，主模型决定用哪把 Key。留白即可。
			key = c.UI.Dim("—")
		}
		line += "  " + ui.Pad(key, keyW)
		if m := h.Slots[config.SlotDefault]; m != "" {
			line += "  " + model.Parse(m).Display()
		} else {
			line += "  " + c.UI.Dim(c.UI.T("启动时询问", "asked at launch"))
		}
		c.UI.Printf("%s\n", line)
	}

	for _, p := range out.Problems {
		c.UI.Warnf("%s", p)
	}
}

// fetchUsage 并发取各把 Key 的额度。取不到就没有，不报错。
func fetchUsage(cfg *config.Config, creds *config.Credentials) map[string]*gateway.Usage {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out = map[string]*gateway.Usage{}
	)
	for _, name := range creds.Names() {
		cred, ok := creds.Get(name)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(name, key string) {
			defer wg.Done()
			u, err := gateway.New(cfg.HostOf(name), key).Usage(ctx)
			if err != nil {
				return
			}
			mu.Lock()
			out[name] = u
			mu.Unlock()
		}(name, cred.Key)
	}
	wg.Wait()
	return out
}

// printUsage 只讲会影响下一次请求的两件事：还剩多少、今天用了多少。
//
// 用标签加数据的排法，不要把数值塞进句子。单位是网关给的字符串
// （实测是「推理积分」），塞进英文句子会得到 "0/10 推理积分 left" ——
// 服务端的原文没有语言可言，只能当数据摆着，不能当词用。
func printUsage(c *Context, u *gateway.Usage) {
	if u == nil {
		return
	}

	w := ui.Width(c.UI.T("额度", "quota"))
	if t := ui.Width(c.UI.T("今天", "today")); t > w {
		w = t
	}

	quota := fmt.Sprintf("%s/%s", trimNum(u.Quota.Remaining), trimNum(u.Quota.Limit))
	if u.Quota.Unit != "" {
		quota += " " + u.Quota.Unit
	}
	line := "  " + c.UI.Dim(ui.Pad(c.UI.T("额度", "quota"), w)) + "  " + quota
	if u.Exhausted() {
		// 额度用完时 harness 只报一个 429。这句话是那个 429 的翻译。
		line += "  " + c.UI.T("已用完，请求会被拒", "exhausted, requests will fail")
	} else {
		line = c.UI.Dim(line)
	}
	c.UI.Printf("%s\n", line)

	if t := u.Usage.Today; t.Requests > 0 {
		c.UI.Printf("%s\n", c.UI.Dim(fmt.Sprintf("  %s  %s",
			ui.Pad(c.UI.T("今天", "today"), w),
			fmt.Sprintf(c.UI.T("%d 次请求，%d tokens", "%d requests, %d tokens"),
				t.Requests, t.TotalTokens))))
	}
}

// trimNum 去掉无意义的小数位：额度是给人看的，10.000000 只会碍眼。
func trimNum(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}

// checkEnvironment 找出会让 tf 的注入落空、或让凭据流经别处的东西。
//
// 只报确定的事实，不猜。每一条都说清「是什么」和「后果是什么」，
// 说不出后果的检查不值得做 —— 用户没法据以行动。
func checkEnvironment(c *Context, paths config.Paths) []string {
	var out []string

	// settings.json 的 env 段会赢过 tf 注入的环境变量。
	//
	// 实测过：把 ANTHROPIC_BASE_URL 设成死地址，tf claude 连的是那个
	// 死地址而不是网关。CC-Switch 这类工具正是往这里写东西的。
	for _, p := range claudeSettingsFiles() {
		for _, k := range settingsEnvKeys(p) {
			if strings.HasPrefix(k, "ANTHROPIC_") {
				out = append(out, fmt.Sprintf(c.UI.T(
					"%s 设了 env.%s，它会盖掉 tf 注入的值",
					"%s sets env.%s, which overrides what tf injects"), tildify(p), k))
			}
		}
	}

	// harness 自己也存着凭据：不经 tf 启动时它用的是那一份。
	for _, f := range []struct{ path, note string }{
		{filepath.Join(home(), ".codex", "auth.json"), "codex"},
		{filepath.Join(home(), ".claude", ".credentials.json"), "claude"},
	} {
		if _, err := os.Stat(f.path); err == nil {
			out = append(out, fmt.Sprintf(c.UI.T(
				"%s 自己也存着凭据；直接跑 %s（不经 tf）用的是那一份",
				"%s has its own stored credentials; running %s directly uses those"),
				tildify(f.path), f.note))
		}
	}

	// 代理会经手 Key。
	for _, v := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY"} {
		if p := os.Getenv(v); p != "" {
			out = append(out, fmt.Sprintf(c.UI.T(
				"%s=%s，Key 与请求都会经过它", "%s=%s; keys and requests go through it"), v, p))
			break
		}
	}

	return out
}

// claudeSettingsFiles 列出 Claude Code 会读的 settings.json。
//
// 项目级的也算：它跟着仓库走，换个目录跑就变了，最容易被忘掉。
func claudeSettingsFiles() []string {
	out := []string{filepath.Join(home(), ".claude", "settings.json")}
	if wd, err := os.Getwd(); err == nil {
		out = append(out, filepath.Join(wd, ".claude", "settings.json"))
	}
	return out
}

// settingsEnvKeys 读出 settings.json 里 env 段的键名。读不出就当没有。
func settingsEnvKeys(path string) []string {
	blob, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal(blob, &doc) != nil {
		return nil
	}
	keys := make([]string, 0, len(doc.Env))
	for k := range doc.Env {
		keys = append(keys, k)
	}
	return keys
}

func home() string {
	h, _ := os.UserHomeDir()
	return h
}

// tildify 把家目录换成 ~，路径才不会撑爆一行。
func tildify(p string) string {
	if h := home(); h != "" && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}
