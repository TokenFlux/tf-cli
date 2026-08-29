// Package config 负责 profile 与凭据的读写。
//
// 两个文件刻意分开（见 docs/design/open-decisions.md C 项）：
// config.json 要能贴进 issue，credentials.json 不能。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DefaultHost 是托管实例。自托管用 --host 或 profile 覆盖。
const DefaultHost = "https://tokenflux.dev"

// DefaultProfile 是 v0 唯一会用到的 profile 名。
const DefaultProfile = "default"

// 通用槽名。各 harness 只使用其中一部分，另有自己特有的槽。
const (
	SlotDefault = "default" // 主模型
	SlotSmall   = "small"   // 标题、摘要等廉价调用
	SlotFast    = "fast"    // claude 的 haiku 档
	SlotHeavy   = "heavy"   // claude 的 opus 档
	SlotReview  = "review"  // codex 的 review_model
)

// ModelSlots 是一个 harness 的模型槽：槽名 → 模型 ID。
//
// 用映射而非固定字段，因为各 harness 的槽位互不相同：
// claude 是 fast/default/heavy，codex 是 default/review，
// opencode 是 default/small。适配表负责声明自己有哪些槽。
//
// 必须按 harness 分开存：claude 走 anthropic_messages、codex 走
// openai_responses，复合 Key 下可能落在完全不同的分组。
type ModelSlots map[string]string

// Get 返回槽位模型，未设置时为空串。
func (s ModelSlots) Get(slot string) string { return s[slot] }

// Clone 返回副本，避免调用方改到配置内部状态。
func (s ModelSlots) Clone() ModelSlots {
	out := make(ModelSlots, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// Profile 是一组「指向哪个网关 + 用什么模型」的设定。
type Profile struct {
	Host      string                `json:"host"`
	Harnesses map[string]ModelSlots `json:"harnesses,omitempty"`
}

// Config 是 config.json 的根结构。
type Config struct {
	Version  int                 `json:"version"`
	Current  string              `json:"current"`
	Profiles map[string]*Profile `json:"profiles"`

	paths Paths
}

// Load 读取配置；文件不存在时返回带默认 profile 的空配置。
func Load(p Paths) (*Config, error) {
	c := &Config{
		Version:  1,
		Current:  DefaultProfile,
		Profiles: map[string]*Profile{},
		paths:    p,
	}

	data, err := os.ReadFile(p.ConfigFile())
	if os.IsNotExist(err) {
		c.Profiles[DefaultProfile] = &Profile{Host: DefaultHost}
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p.ConfigFile(), err)
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", p.ConfigFile(), err)
	}
	c.paths = p

	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	if c.Current == "" {
		c.Current = DefaultProfile
	}
	if _, ok := c.Profiles[c.Current]; !ok {
		c.Profiles[c.Current] = &Profile{Host: DefaultHost}
	}
	return c, nil
}

// Save 原子写回配置。
func (c *Config) Save() error {
	if err := ensureDir(c.paths.ConfigDir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(c.paths.ConfigFile(), append(data, '\n'), configFilePerm)
}

// Profile 返回指定 profile；name 为空时返回当前 profile。
func (c *Config) Profile(name string) (*Profile, bool) {
	if name == "" {
		name = c.Current
	}
	p, ok := c.Profiles[name]
	return p, ok
}

// Slots 返回某 harness 的模型槽副本，不存在时返回空槽。
func (p *Profile) Slots(harness string) ModelSlots {
	if s, ok := p.Harnesses[harness]; ok && s != nil {
		return s.Clone()
	}
	return ModelSlots{}
}

// SetSlots 整体写入某 harness 的模型槽。
func (p *Profile) SetSlots(harness string, s ModelSlots) {
	if p.Harnesses == nil {
		p.Harnesses = map[string]ModelSlots{}
	}
	p.Harnesses[harness] = s.Clone()
}

// SetSlot 只改一个槽，其余保持不变。供 `tkr model set` 使用。
func (p *Profile) SetSlot(harness, slot, model string) {
	if p.Harnesses == nil {
		p.Harnesses = map[string]ModelSlots{}
	}
	if p.Harnesses[harness] == nil {
		p.Harnesses[harness] = ModelSlots{}
	}
	if model == "" {
		delete(p.Harnesses[harness], slot)
		return
	}
	p.Harnesses[harness][slot] = model
}

// ClearSlots 清空某 harness 的槽位，使下次启动重新引导选择。
func (p *Profile) ClearSlots(harness string) {
	delete(p.Harnesses, harness)
}

// writeAtomic 先写同目录临时文件再 rename，避免中断产生半个文件。
// 权限在 rename 之前设置，防止出现短暂的宽权限窗口。
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
