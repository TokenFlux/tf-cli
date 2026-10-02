package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/tokenflux/tf-cli/internal/access"
	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/ui"
)

type authData struct {
	Configured          bool        `json:"configured"`
	Source              string      `json:"source,omitempty"`
	KeyName             string      `json:"key_name,omitempty"`
	Key                 string      `json:"key,omitempty"`
	Host                string      `json:"host,omitempty"`
	Models              int         `json:"models"`
	Scopes              []authScope `json:"scopes,omitempty"`
	EnvironmentOverride bool        `json:"environment_override"`
	StoredKeys          []string    `json:"stored_keys,omitempty"`
	Hint                string      `json:"hint,omitempty"`
}

type authScope struct {
	Prefix    string   `json:"prefix,omitempty"`
	Protocols []string `json:"protocols,omitempty"`
	Harnesses []string `json:"harnesses"`
}

func newAuthCommand() *Command {
	return &Command{
		Name:  "auth",
		Usage: "tf auth [--key <name>]",
		Summary: func(u *ui.UI) string {
			return u.T("解释当前实际使用的凭据", "Explain the credential tf would use")
		},
		Run: runAuth,
	}
}

func runAuth(c *Context) error {
	st, err := loadState(c)
	if err != nil {
		return err
	}

	stored := st.creds.Names()
	explicitName := c.Flags.String("key")
	name := explicitName
	if name == "" && os.Getenv("TF_API_KEY") == "" && len(stored) == 1 {
		name = stored[0]
	}
	if name == "" && len(stored) > 1 && os.Getenv("TF_API_KEY") == "" {
		return ui.Errf(ui.CodeUsage,
			c.UI.T("本机有多把 Key，请用 --key 指定；tf auth 不会静默猜测", "multiple keys are stored; specify --key; tf auth will not guess silently")).
			WithHint("tf auth --key " + stored[0])
	}

	data := authData{StoredKeys: stored, EnvironmentOverride: os.Getenv("TF_API_KEY") != "" && explicitName == ""}
	if data.EnvironmentOverride {
		data.Source = config.SourceEnv
		data.KeyName = name
		data.Key = config.Mask(os.Getenv("TF_API_KEY"))
		data.Host = config.DefaultHost
		if host := c.Flags.String("host"); host != "" {
			data.Host = normalizeHost(host)
		}
		data.Configured = true
	} else if name != "" {
		cred, ok := st.creds.Get(name)
		if !ok {
			return ui.Errf(ui.CodeKeyNotFound,
				fmt.Sprintf(c.UI.T("没有名为 %q 的 Key", "no key named %q"), name)).
				WithHint("tf auth --key " + strings.Join(stored, " | "))
		}
		meta := st.cfg.KeyMetaOf(name)
		data.Configured = true
		data.Source = cred.Source
		data.KeyName = name
		data.Key = config.Mask(cred.Key)
		data.Host = st.cfg.HostOf(name)
		if host := c.Flags.String("host"); host != "" {
			data.Host = normalizeHost(host)
		}
		data.Models = len(meta.Models)
		for _, prefix := range meta.Scopes() {
			scope := authScope{Prefix: prefix, Harnesses: access.RunnableIn(meta, prefix)}
			if meta.LockedToClaudeCode(prefix) {
				scope.Protocols = []string{"claude-code-only"}
			} else {
				scope.Protocols = meta.Protocols[prefix]
			}
			data.Scopes = append(data.Scopes, scope)
		}
	} else {
		data.Hint = "tf login"
	}

	c.UI.Emit("auth", data, func() {
		if !data.Configured {
			c.UI.Printf("%s\n", c.UI.T("认证状态：未配置", "auth: not configured"))
			c.UI.Printf("  %s\n", c.UI.Dim("tf login"))
			return
		}
		c.UI.Printf("%s\n", c.UI.T("认证状态：已配置", "auth: configured"))
		c.UI.Printf("  %-12s %s\n", c.UI.T("来源", "source"), data.Source)
		c.UI.Printf("  %-12s %s\n", c.UI.T("Key", "key"), data.Key)
		if data.KeyName != "" {
			c.UI.Printf("  %-12s %s\n", c.UI.T("名称", "name"), data.KeyName)
		}
		c.UI.Printf("  %-12s %s\n", c.UI.T("网关", "gateway"), data.Host)
		c.UI.Printf("  %-12s %d\n", c.UI.T("模型", "models"), data.Models)
		for _, scope := range data.Scopes {
			label := scope.Prefix
			if label == "" {
				label = c.UI.T("默认分组", "default scope")
			}
			c.UI.Printf("  %-12s %s", label, strings.Join(scope.Harnesses, " "))
			if len(scope.Protocols) > 0 {
				c.UI.Printf("  (%s)", strings.Join(scope.Protocols, ", "))
			}
			c.UI.Printf("\n")
		}
		if data.EnvironmentOverride {
			c.UI.Printf("  %s\n", c.UI.Dim(c.UI.T("TF_API_KEY 只对本次运行生效", "TF_API_KEY applies only to this run")))
		}
	})
	return nil
}
