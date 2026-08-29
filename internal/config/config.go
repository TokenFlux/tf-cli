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

// DefaultHost 是托管版的默认网关。
const DefaultHost = "https://tokenflux.dev"

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
	Models    []string            `json:"models,omitempty"`
	ProbedAt  time.Time           `json:"probed_at,omitempty"`
}

// UnmarshalJSON 兼容早期把 Protocols 写成数组的配置。
func (m *KeyMeta) UnmarshalJSON(data []byte) error {
	type alias KeyMeta
	var v struct {
		alias
		Protocols json.RawMessage `json:"protocols,omitempty"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*m = KeyMeta(v.alias)
	if len(v.Protocols) == 0 {
		return nil
	}
	if err := json.Unmarshal(v.Protocols, &m.Protocols); err == nil {
		return nil
	}
	var flat []string
	if err := json.Unmarshal(v.Protocols, &flat); err != nil {
		return err
	}
	m.Protocols = map[string][]string{GroupScope: flat}
	return nil
}

// SupportsIn 报告某个分组前缀是否允许该协议。
//
// 未探测过时返回 true：没有证据就不拦，预检只能证伪。
func (m *KeyMeta) SupportsIn(prefix, proto string) bool {
	if m == nil || len(m.Protocols) == 0 {
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
	if m == nil || len(m.Protocols) == 0 {
		return true
	}
	for prefix := range m.Protocols {
		if m.SupportsIn(prefix, proto) {
			return true
		}
	}
	return false
}

// Probed 报告是否有可用的探测结果。
func (m *KeyMeta) Probed() bool { return m != nil && len(m.Protocols) > 0 }

// ProtocolSummary 把探测结果扒平成可展示的行。
func (m *KeyMeta) ProtocolSummary() []string {
	if m == nil || len(m.Protocols) == 0 {
		return nil
	}
	prefixes := make([]string, 0, len(m.Protocols))
	for p := range m.Protocols {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)

	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		line := strings.Join(m.Protocols[p], " ")
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
	cfg.migrate(data)
	cfg.paths = paths
	return cfg, nil
}

// migrate 把旧的 profiles/current 结构搬到新的 keys/harnesses 上。
//
// 旧结构有一个全局 current，新结构没有：把它作为所有 harness 的初始绑定，
// 语义等价且不丢用户已选的模型。
func (c *Config) migrate(raw []byte) {
	var old struct {
		Current  string `json:"current"`
		Profiles map[string]struct {
			Host      string                `json:"host"`
			Harnesses map[string]ModelSlots `json:"harnesses"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(raw, &old); err != nil || len(old.Profiles) == 0 {
		return
	}
	for name, p := range old.Profiles {
		if _, exists := c.Keys[name]; !exists {
			host := p.Host
			if host == "" {
				host = DefaultHost
			}
			c.Keys[name] = &KeyMeta{Host: host}
		}
		if name != old.Current {
			continue
		}
		for hname, slots := range p.Harnesses {
			hc := c.Harness(hname)
			hc.Key = name
			for k, v := range slots {
				hc.Slots[k] = v
			}
		}
	}
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
