package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// 缓存的存在有两个理由：
//  1. 启动路径上不该等网络（见 PLAN 缓存策略：超时 2s 就用旧数据）。
//  2. shell 补全必须零网络 —— 按一次 Tab 等一次 HTTP 是不可接受的。
type cacheEnvelope struct {
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

// WriteCache 写入一份带时间戳的缓存。失败不应影响主流程，由调用方决定是否忽略。
func (p Paths) WriteCache(name string, v any) error {
	if err := ensureDir(p.CacheDir); err != nil {
		return err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	blob, err := json.Marshal(cacheEnvelope{At: time.Now(), Data: raw})
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(p.CacheDir, name+".json"), blob, 0o600)
}

// ReadCache 读取缓存并报告其年龄。
//
// 刻意不在这里判断是否过期：不同调用方对「多旧算旧」的容忍度不同，
// 补全宁可用很旧的数据也不能卡住。
func (p Paths) ReadCache(name string, v any) (age time.Duration, err error) {
	blob, err := os.ReadFile(filepath.Join(p.CacheDir, name+".json"))
	if err != nil {
		return 0, err
	}
	var env cacheEnvelope
	if err := json.Unmarshal(blob, &env); err != nil {
		return 0, err
	}
	if err := json.Unmarshal(env.Data, v); err != nil {
		return 0, err
	}
	return time.Since(env.At), nil
}

// ModelsCacheKey 返回某个 profile 的模型缓存名。
//
// 必须按 profile 分开：不同 profile 是不同的 Key、不同的分组，
// 共用一份缓存会让补全和降级读到另一把 Key 的模型列表。
func ModelsCacheKey(profile string) string { return "models-" + profile }

// RemoveCache 删除一份缓存。文件不存在不算错误。
func (p Paths) RemoveCache(name string) error {
	err := os.Remove(filepath.Join(p.CacheDir, name+".json"))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
