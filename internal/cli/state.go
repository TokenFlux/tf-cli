package cli

import (
	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/ui"
)

// state 是几乎每个命令都要的三样东西。
//
// 抽出来是因为「取路径 → 读配置 → 读凭据 → 各自包装错误」这段样板
// 曾在八个命令里各抄一遍，同一句错误文案出现过六次。
type state struct {
	paths config.Paths
	cfg   *config.Config
	creds *config.Credentials
}

// loadState 读取配置与凭据，并在凭据权限过宽时当场收紧并告知。
func loadState(c *Context) (*state, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return nil, ui.Errf(ui.CodeConfigRead,
			c.UI.T("无法定位配置目录", "cannot locate the config directory")).WithCause(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return nil, ui.Errf(ui.CodeConfigRead,
			c.UI.T("配置文件无法读取", "cannot read the config file")).WithCause(err)
	}
	creds, repaired, err := config.LoadCredentials(paths)
	if err != nil {
		return nil, ui.Errf(ui.CodeCredentialsRead,
			c.UI.T("凭据文件无法读取", "cannot read the credentials file")).WithCause(err)
	}
	if repaired {
		c.UI.Warnf("%s", c.UI.T("凭据文件权限过宽，已收紧为 0600",
			"credentials file was too permissive; tightened to 0600"))
	}
	return &state{paths: paths, cfg: cfg, creds: creds}, nil
}

// saveConfig 包装配置写入的错误。
func (s *state) saveConfig(c *Context) error {
	if err := s.cfg.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite,
			c.UI.T("配置无法写入", "cannot write config")).WithCause(err)
	}
	return nil
}

// saveCredentials 包装凭据写入的错误。
func (s *state) saveCredentials(c *Context) error {
	if err := s.creds.Save(); err != nil {
		return ui.Errf(ui.CodeConfigWrite,
			c.UI.T("凭据文件无法写入", "cannot write the credentials file")).WithCause(err)
	}
	return nil
}
