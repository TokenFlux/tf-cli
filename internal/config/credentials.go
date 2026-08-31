package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Credential 是一把 Key 及其来源信息。
//
// Source 记录它是怎么进来的（粘贴 / 网页导入），
// 便于 doctor 解释「这把 Key 是哪来的」。
type Credential struct {
	Key       string    `json:"key"`
	Source    string    `json:"source,omitempty"`
	Origin    string    `json:"origin,omitempty"`
	KeyName   string    `json:"key_name,omitempty"`
	GroupID   int64     `json:"group_id,omitempty"`
	GroupName string    `json:"group_name,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

const (
	SourcePaste  = "paste"  // tf login --with-key
	SourceImport = "import" // 网页「导入 tf」按钮
	SourceEnv    = "env"    // 环境变量，不落盘
)

// Credentials 是 credentials.json 的根结构。
type Credentials struct {
	Version int                    `json:"version"`
	Items   map[string]*Credential `json:"credentials"`

	paths Paths
}

// LoadCredentials 读取凭据文件；不存在时返回空集合。
//
// 同时校验权限：文件对 group/other 可读时自动收紧回 0600 并汇报，
// 因为这是一个装着 API Key 的文件。
func LoadCredentials(p Paths) (*Credentials, bool, error) {
	c := &Credentials{Version: 1, Items: map[string]*Credential{}, paths: p}

	path := p.CredentialsFile()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return c, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	repaired := false
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, credsFilePerm); err != nil {
			return nil, false, fmt.Errorf("tighten %s: %w", path, err)
		}
		repaired = true
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, repaired, err
	}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, repaired, fmt.Errorf("parse %s: %w", path, err)
	}
	c.paths = p
	if c.Items == nil {
		c.Items = map[string]*Credential{}
	}
	return c, repaired, nil
}

// Save 原子写回凭据，权限固定 0600。
func (c *Credentials) Save() error {
	if err := ensureDir(c.paths.ConfigDir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(c.paths.CredentialsFile(), append(data, '\n'), credsFilePerm)
}

// Get 返回某 profile 的凭据。
//
// 环境变量 TF_API_KEY 优先于落盘凭据，且不写盘 —— 容器与 CI 场景。
func (c *Credentials) Get(name string) (*Credential, bool) {
	if k := os.Getenv("TF_API_KEY"); k != "" {
		return &Credential{Key: k, Source: SourceEnv}, true
	}
	cred, ok := c.Items[name]
	return cred, ok && cred != nil && cred.Key != ""
}

// Set 写入某 profile 的凭据。
func (c *Credentials) Set(name string, cred *Credential) {
	if c.Items == nil {
		c.Items = map[string]*Credential{}
	}
	if cred.CreatedAt.IsZero() {
		cred.CreatedAt = time.Now()
	}
	c.Items[name] = cred
}

// Mask 把 Key 截断成可安全展示的形式。
// 任何面向用户的输出都必须经过它。
func Mask(key string) string {
	const head, tail = 6, 4
	// 空值不是秘密，拿 **** 去表示“没有 Key”只会误导。
	if key == "" {
		return ""
	}
	if len(key) <= head+tail {
		return "****"
	}
	return key[:head] + "…" + key[len(key)-tail:]
}

// Names 返回所有已保存凭据的标签。
func (c *Credentials) Names() []string {
	out := make([]string, 0, len(c.Items))
	for name := range c.Items {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Remove 删除某个 profile 的凭据。
func (c *Credentials) Remove(name string) {
	delete(c.Items, name)
}

// Clear 删除全部凭据。
func (c *Credentials) Clear() {
	c.Items = map[string]*Credential{}
}
