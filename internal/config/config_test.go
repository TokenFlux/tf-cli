package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPaths(t *testing.T) Paths {
	t.Helper()
	dir := t.TempDir()
	return Paths{ConfigDir: filepath.Join(dir, "cfg"), CacheDir: filepath.Join(dir, "cache")}
}

// 首次读取应给出可用的默认配置，而不是报错。
func TestLoadMissingConfigYieldsDefaults(t *testing.T) {
	p := testPaths(t)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prof, ok := cfg.Profile("")
	if !ok {
		t.Fatal("default profile missing")
	}
	if prof.Host != DefaultHost {
		t.Errorf("host = %q, want %q", prof.Host, DefaultHost)
	}
}

// 配置文件 0644、目录 0700，且能往返。
func TestSaveConfigPermissionsAndRoundTrip(t *testing.T) {
	p := testPaths(t)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	prof, _ := cfg.Profile("")
	prof.SetSlots("claude", ModelSlots{Default: "a", Fast: "b", Heavy: "c"})

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(p.ConfigFile())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != configFilePerm {
		t.Errorf("config perm = %o, want %o", got, configFilePerm)
	}
	dirInfo, err := os.Stat(p.ConfigDir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != dirPerm {
		t.Errorf("dir perm = %o, want %o", got, dirPerm)
	}

	reloaded, err := Load(p)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	rp, _ := reloaded.Profile("")
	if got := rp.Slots("claude"); got.Default != "a" || got.Fast != "b" || got.Heavy != "c" {
		t.Errorf("slots = %+v, want a/b/c", got)
	}
}

// 模型槽必须按 harness 分开存，互不影响。
func TestSlotsAreIsolatedPerHarness(t *testing.T) {
	p := &Profile{Host: DefaultHost}
	p.SetSlots("claude", ModelSlots{Default: "claude-model"})
	p.SetSlots("codex", ModelSlots{Default: "codex-model"})

	if got := p.Slots("claude").Default; got != "claude-model" {
		t.Errorf("claude slot = %q", got)
	}
	if got := p.Slots("codex").Default; got != "codex-model" {
		t.Errorf("codex slot = %q", got)
	}
	if got := p.Slots("opencode").Default; got != "" {
		t.Errorf("unset harness should be empty, got %q", got)
	}
}

// 凭据文件必须是 0600。
func TestCredentialsWrittenWith0600(t *testing.T) {
	p := testPaths(t)
	creds, _, err := LoadCredentials(p)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	creds.Set(DefaultProfile, &Credential{Key: "sk-secret", Source: SourcePaste})
	if err := creds.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(p.CredentialsFile())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != credsFilePerm {
		t.Errorf("credentials perm = %o, want %o", got, credsFilePerm)
	}
}

// 权限过宽的凭据文件应被自动收紧并汇报。
func TestLoadCredentialsRepairsLoosePermissions(t *testing.T) {
	p := testPaths(t)
	if err := ensureDir(p.ConfigDir); err != nil {
		t.Fatalf("ensureDir: %v", err)
	}
	if err := os.WriteFile(p.CredentialsFile(), []byte(`{"version":1,"credentials":{}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, repaired, err := LoadCredentials(p)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if !repaired {
		t.Error("expected the loose permission to be reported")
	}
	info, _ := os.Stat(p.CredentialsFile())
	if got := info.Mode().Perm(); got != credsFilePerm {
		t.Errorf("perm after repair = %o, want %o", got, credsFilePerm)
	}
}

// 环境变量优先于落盘凭据，且不写盘。
func TestEnvKeyTakesPrecedence(t *testing.T) {
	p := testPaths(t)
	creds, _, err := LoadCredentials(p)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	creds.Set(DefaultProfile, &Credential{Key: "sk-from-disk"})

	t.Setenv("TKR_API_KEY", "sk-from-env")
	got, ok := creds.Get(DefaultProfile)
	if !ok {
		t.Fatal("expected a credential")
	}
	if got.Key != "sk-from-env" || got.Source != SourceEnv {
		t.Errorf("got %+v, want the env key", got)
	}
}

// 任何展示都必须经过 Mask，且不能泄露中间部分。
func TestMask(t *testing.T) {
	const key = "sk-d616520b0071d4df6ccc3ad743362bd4"
	masked := Mask(key)
	if strings.Contains(masked, "0071d4df") {
		t.Errorf("mask leaked the middle: %q", masked)
	}
	if got := Mask("short"); got != "****" {
		t.Errorf("short key mask = %q, want ****", got)
	}
}
