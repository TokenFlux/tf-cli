package cli

import (
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/ui"
)

// 网关不再用选择器决定：--host 优先，同名 Key 的已存 host 其次，
// 其余一律落到编译时默认网关。
func TestLoginHostPriority(t *testing.T) {
	for _, tc := range []struct {
		name    string
		flag    string
		keyName string
		saved   map[string]string // key name -> saved host
		want    string
	}{
		{"default", "", "work", nil, config.DefaultHost},
		{"explicit host wins over saved", "https://flag.example", "work",
			map[string]string{"work": "https://saved.example"}, "https://flag.example"},
		{"same-name key normalizes saved host", "", "work",
			map[string]string{"work": "https://router.example/v1/"}, "https://router.example"},
		{"other key's host is not inherited", "", "personal",
			map[string]string{"work": "https://router.example"}, config.DefaultHost},
		{"bare domain gets https", "router.example", "work", nil, "https://router.example"},
		{"trailing slash trimmed", "https://router.example/", "work", nil, "https://router.example"},
		{"stray /v1 trimmed", "https://router.example/v1/", "work", nil, "https://router.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Keys: map[string]*config.KeyMeta{}}
			for name, host := range tc.saved {
				cfg.KeyMetaOf(name).Host = host
			}
			c := testCtx()
			if tc.flag != "" {
				c.Flags.set["host"] = tc.flag
			}
			if got := loginHost(c, cfg, tc.keyName); got != tc.want {
				t.Errorf("loginHost = %q, want %q", got, tc.want)
			}
		})
	}
}

// 显式 --host 不合法时要在发任何请求之前报用法错误。
func TestLoginRejectsInvalidHostBeforeDialing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	for _, bad := range []string{
		"ftp://router.example",
		"https://user:pw@router.example",
		"https://router.example?x=1",
		"https://router.example#frag",
	} {
		c, err := parse(newLoginCommand(), []string{"fixture", "--with-key", "--host", bad})
		if err != nil {
			t.Fatal(err)
		}
		c.UI = testCtx().UI
	}
}

// 已保存的 host 也必须在读取 Key 前校验，不能把坏配置变成一次网络请求。
func TestLoginRejectsSavedInvalidHostBeforeDialing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg, creds, _, err := config.LoadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.KeyMetaOf("fixture").Host = "ftp://router.example"
	creds.Set("fixture", &config.Credential{Key: "sk-test", Source: config.SourcePaste})
	if err := config.SaveState(cfg, creds); err != nil {
		t.Fatal(err)
	}

	c, err := parse(newLoginCommand(), []string{"fixture", "--with-key"})
	if err != nil {
		t.Fatal(err)
	}
	c.UI = testCtx().UI
	if err := runLogin(c); ui.AsError(err).Code != ui.CodeUsage {
		t.Fatalf("saved invalid host error = %v, want usage", err)
	}
}
