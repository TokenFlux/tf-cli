package config

import (
	"os"
	"path/filepath"
)

// 目录布局（见 docs/design/open-decisions.md C 项）：
//
//	<config>/config.json        0644  可分享、可贴进工单
//	<config>/credentials.json   0600  仅凭据，独立文件
//	<cache>/                    0700  目录数据与探测结果
//
// 默认 <config> = ~/.tf，<cache> = ~/.tf/cache；
// 设置了 XDG_CONFIG_HOME / XDG_CACHE_HOME 时优先遵循 XDG。
const (
	dirName        = "tf"
	configFileName = "config.json"
	credsFileName  = "credentials.json"
	dirPerm        = 0o700
	configFilePerm = 0o644
	credsFilePerm  = 0o600
)

// Paths 汇总所有落盘位置，便于测试时整体替换。
type Paths struct {
	ConfigDir string
	CacheDir  string
}

// DefaultPaths 依据环境变量解析路径。
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}

	configDir := filepath.Join(home, "."+dirName)
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		configDir = filepath.Join(x, dirName)
	}

	cacheDir := filepath.Join(configDir, "cache")
	if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
		cacheDir = filepath.Join(x, dirName)
	}

	return Paths{ConfigDir: configDir, CacheDir: cacheDir}, nil
}

// ConfigFile 返回配置文件路径。
func (p Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, configFileName) }

// CredentialsFile 返回凭据文件路径。
func (p Paths) CredentialsFile() string { return filepath.Join(p.ConfigDir, credsFileName) }

// ensureDir 创建目录并收紧权限。
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	// MkdirAll 受 umask 影响，显式收紧一次。
	return os.Chmod(dir, dirPerm)
}
