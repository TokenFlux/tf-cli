// Package config 管理 tkr 的配置与凭据。
//
// 分两个文件：
//   - config.json      0644，Key 的元数据与 harness 绑定，可以贴进 issue
//   - credentials.json 0600，只放密钥本身
//
// 刻意没有「当前 profile」这类全局模式 —— 绑定属于 harness，
// 理由见 docs/design/no-global-mode.md。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DefaultHost 是默认网关，可在编译期覆盖：
//
//	go build -ldflags "-X github.com/tokenflux/tkr/internal/config.DefaultHost=https://router.acme.com"
//
// 自建 TokenRouter 的地址属于部署方的决定，不该问每一个登录的人 ——
// 部署方构建一次，团队里的人照常 tf login 就好。
// 拿到官方二进制又要指向自建网关的，用 --host。
var DefaultHost = "https://tokenflux.dev"

// 已知的模型槽名。各 harness 用到哪些由适配表声明。
const (
	SlotDefault = "default"
	SlotFast    = "fast"
	SlotHeavy   = "heavy"
	SlotSmall   = "small"
	SlotReview  = "review"
)

// GroupScope 是普通 Key 的唯一作用域键。
//
// 复合 Key 用分组前缀作键；普通 Key 只绑一个分组，用空串。
const GroupScope = ""

// KeyMeta 是一把 Key 的非机密元数据。
//
// 协议准入按**分组前缀**记录，而不是整把 Key 一个值：
// 复合 Key 一把横跨多个分组，每个分组的 allowed_client_protocols
// 各不相同 —— GPT/* 能跑 codex，Claude/* 只能跑 claude。
type KeyMeta struct {
	Host      string              `json:"host"`
	Protocols map[string][]string `json:"protocols,omitempty"`
	// ClaudeCodeOnly 标记只接受 Claude Code 客户端的分组。
	//
	// 这类分组拦的是客户端指纹而不是协议，tkr 自己去问一定被拒，
	// 所以它的协议集合永远是空的 —— 必须单独记，否则会被读成「什么都不支持」。
	ClaudeCodeOnly map[string]bool `json:"claude_code_only,omitempty"`
	Models         []string        `json:"models,omitempty"`
	ProbedAt       time.Time       `json:"probed_at,omitempty"`
}

// SupportsIn 报告某个分组前缀是否允许该协议。
//
// 未探测过时返回 true：没有证据就不拦，预检只能证伪。
func (m *KeyMeta) SupportsIn(prefix, proto string) bool {
	if m.LockedToClaudeCode(prefix) {
		// 协议问不出来，但 Claude Code 走的就是 messages。
		return proto == "anthropic_messages"
	}
	if m == nil || !m.Probed() {
		return true
	}
	allowed, ok := m.Protocols[prefix]
	if !ok {
		return true // 这个前缀没探到，同样不拦
	}
	for _, p := range allowed {
		if p == proto {
			return true
		}
	}
	return false
}

// Supports 报告这把 Key **至少有一个分组**允许该协议。
//
// 用于筛选 Key 候选；具体能用哪些模型要再用 SupportsIn 逐个判。
func (m *KeyMeta) Supports(proto string) bool {
	if m == nil || !m.Probed() {
		return true
	}
	for _, prefix := range m.Scopes() {
		if m.SupportsIn(prefix, proto) {
			return true
		}
	}
	return false
}

// Scopes 列出该 Key 的全部作用域：复合 Key 是各分组前缀，普通 Key 是单个空串。
func (m *KeyMeta) Scopes() []string {
	if m == nil {
		return []string{GroupScope}
	}
	seen := map[string]bool{}
	var out []string
	for _, set := range []map[string]bool{boolKeys(m.Protocols), m.ClaudeCodeOnly} {
		for p := range set {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	if len(out) == 0 {
		return []string{GroupScope}
	}
	sort.Strings(out)
	return out
}

func boolKeys(m map[string][]string) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// Probed 报告是否有可用的探测结果。
func (m *KeyMeta) Probed() bool {
	return m != nil && (len(m.Protocols) > 0 || len(m.ClaudeCodeOnly) > 0)
}

// LockedToClaudeCode 报告某个分组是否只接受 Claude Code 客户端。
func (m *KeyMeta) LockedToClaudeCode(prefix string) bool {
	return m != nil && m.ClaudeCodeOnly[prefix]
}

// ProtocolSummary 把探测结果扒平成可展示的行。
func (m *KeyMeta) ProtocolSummary() []string {
	if m == nil || !m.Probed() {
		return nil
	}
	out := make([]string, 0, len(m.Protocols))
	for _, p := range m.Scopes() {
		line := strings.Join(m.Protocols[p], " ")
		if m.LockedToClaudeCode(p) {
			line = "claude-code-only"
		}
		if p != GroupScope {
			line = p + ": " + line
		}
		out = append(out, line)
	}
	return out
}

// HarnessConfig 是某个 harness 的绑定与模型槽。
type HarnessConfig struct {
	Key   string     `json:"key,omitempty"`
	Slots ModelSlots `json:"slots,omitempty"`
}

// ModelSlots 是槽名到模型 ID 的映射。
//
// 用映射而非固定字段：各 harness 的槽位不同（claude 有 fast/default/heavy，
// codex 有 default/review），固定结构装不下。
type ModelSlots map[string]string

// Config 是 config.json 的根结构。
type Config struct {
	Version   int                       `json:"version"`
	Keys      map[string]*KeyMeta       `json:"keys"`
	Harnesses map[string]*HarnessConfig `json:"harnesses,omitempty"`
	Installs  map[string]InstallRecord  `json:"installs,omitempty"`

	paths Paths
}

// Load 读取配置；文件不存在时返回空配置。
func Load(paths Paths) (*Config, error) {
	cfg := &Config{Version: 1, Keys: map[string]*KeyMeta{}, paths: paths}

	data, err := os.ReadFile(paths.ConfigFile())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", paths.ConfigFile(), err)
	}
	if cfg.Keys == nil {
		cfg.Keys = map[string]*KeyMeta{}
	}
	cfg.paths = paths
	return cfg, nil
}

// Harness 返回某个 harness 的配置，必要时创建。
func (c *Config) Harness(name string) *HarnessConfig {
	if c.Harnesses == nil {
		c.Harnesses = map[string]*HarnessConfig{}
	}
	hc, ok := c.Harnesses[name]
	if !ok {
		hc = &HarnessConfig{Slots: ModelSlots{}}
		c.Harnesses[name] = hc
	}
	if hc.Slots == nil {
		hc.Slots = ModelSlots{}
	}
	return hc
}

// KeyNames 按名称排序列出所有已知的 Key 标签。
func (c *Config) KeyNames() []string {
	out := make([]string, 0, len(c.Keys))
	for name := range c.Keys {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// KeyMetaOf 返回某把 Key 的元数据，必要时创建。
func (c *Config) KeyMetaOf(name string) *KeyMeta {
	if c.Keys == nil {
		c.Keys = map[string]*KeyMeta{}
	}
	m, ok := c.Keys[name]
	if !ok {
		m = &KeyMeta{Host: DefaultHost}
		c.Keys[name] = m
	}
	return m
}

// HostOf 返回某把 Key 的网关地址。
func (c *Config) HostOf(name string) string {
	if m, ok := c.Keys[name]; ok && m.Host != "" {
		return m.Host
	}
	return DefaultHost
}

// Save 原子写回配置。
func (c *Config) Save() error {
	if err := os.MkdirAll(c.paths.ConfigDir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(c.paths.ConfigFile(), append(data, '\n'), 0o644)
}

// writeAtomic 先写临时文件再改名，避免中断留下半个文件。
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// InstallRecord 记录某个 harness 是被 tkr 用什么方式装上的。
type InstallRecord struct {
	Manager string    `json:"manager"`
	Command string    `json:"command"`
	Version string    `json:"version,omitempty"`
	At      time.Time `json:"at"`
}

// RecordInstall 记下一次安装，供 doctor 回溯与卸载参考。
func (c *Config) RecordInstall(name string, rec InstallRecord) {
	if c.Installs == nil {
		c.Installs = map[string]InstallRecord{}
	}
	rec.At = time.Now()
	c.Installs[name] = rec
}
