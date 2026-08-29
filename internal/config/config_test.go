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
	prof.SetSlots("claude", ModelSlots{SlotDefault: "a", SlotFast: "b", SlotHeavy: "c"})

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
	got := rp.Slots("claude")
	if got.Get(SlotDefault) != "a" || got.Get(SlotFast) != "b" || got.Get(SlotHeavy) != "c" {
		t.Errorf("slots = %+v, want a/b/c", got)
	}
}

// 各 harness 的槽位集合不同，存储必须容纳任意槽名。
func TestSlotsAreHarnessSpecific(t *testing.T) {
	p := &Profile{Host: DefaultHost}
	p.SetSlots("claude", ModelSlots{SlotFast: "haiku", SlotDefault: "sonnet", SlotHeavy: "opus"})
	p.SetSlots("codex", ModelSlots{SlotDefault: "gpt", SlotReview: "gpt-review"})
	p.SetSlots("opencode", ModelSlots{SlotDefault: "gpt", SlotSmall: "gpt-small"})

	if got := p.Slots("codex").Get(SlotReview); got != "gpt-review" {
		t.Errorf("codex review slot = %q", got)
	}
	if got := p.Slots("opencode").Get(SlotSmall); got != "gpt-small" {
		t.Errorf("opencode small slot = %q", got)
	}
	if got := p.Slots("claude").Get(SlotSmall); got != "" {
		t.Errorf("claude should have no small slot, got %q", got)
	}
}

// 单槽修改不能波及同一 harness 的其它槽。
func TestSetSlotIsSurgical(t *testing.T) {
	p := &Profile{Host: DefaultHost}
	p.SetSlots("opencode", ModelSlots{SlotDefault: "main", SlotSmall: "small"})
	p.SetSlot("opencode", SlotSmall, "cheaper")

	s := p.Slots("opencode")
	if s.Get(SlotDefault) != "main" {
		t.Errorf("default slot was disturbed: %q", s.Get(SlotDefault))
	}
	if s.Get(SlotSmall) != "cheaper" {
		t.Errorf("small slot = %q, want cheaper", s.Get(SlotSmall))
	}

	// 置空即删除该槽，使其回到「未配置」。
	p.SetSlot("opencode", SlotSmall, "")
	if got := p.Slots("opencode").Get(SlotSmall); got != "" {
		t.Errorf("cleared slot = %q, want empty", got)
	}
}

// Slots 必须返回副本，调用方改动不能污染配置。
func TestSlotsReturnsCopy(t *testing.T) {
	p := &Profile{Host: DefaultHost}
	p.SetSlots("claude", ModelSlots{SlotDefault: "sonnet"})

	grabbed := p.Slots("claude")
	grabbed[SlotDefault] = "tampered"

	if got := p.Slots("claude").Get(SlotDefault); got != "sonnet" {
		t.Errorf("config was mutated through the returned map: %q", got)
	}
}

// 模型槽必须按 harness 分开存，互不影响。
func TestSlotsAreIsolatedPerHarness(t *testing.T) {
	p := &Profile{Host: DefaultHost}
	p.SetSlots("claude", ModelSlots{SlotDefault: "claude-model"})
	p.SetSlots("codex", ModelSlots{SlotDefault: "codex-model"})

	if got := p.Slots("claude").Get(SlotDefault); got != "claude-model" {
		t.Errorf("claude slot = %q", got)
	}
	if got := p.Slots("codex").Get(SlotDefault); got != "codex-model" {
		t.Errorf("codex slot = %q", got)
	}
	if got := p.Slots("opencode").Get(SlotDefault); got != "" {
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

// 凭据是按 profile 分开存的：删一把不能影响另一把。
func TestCredentialsRemoveIsPerProfile(t *testing.T) {
	dir := t.TempDir()
	paths := Paths{ConfigDir: dir, CacheDir: dir}

	creds, _, err := LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	creds.Set("default", &Credential{Key: "sk-aaa", Source: SourcePaste})
	creds.Set("work", &Credential{Key: "sk-bbb", Source: SourcePaste})
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}

	if got := creds.Names(); len(got) != 2 || got[0] != "default" || got[1] != "work" {
		t.Fatalf("Names() = %v, want sorted [default work]", got)
	}

	creds.Remove("work")
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}

	reloaded, _, err := LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get("work"); ok {
		t.Error("work credential survived removal")
	}
	cred, ok := reloaded.Get("default")
	if !ok || cred.Key != "sk-aaa" {
		t.Errorf("default credential was disturbed: %+v", cred)
	}

	reloaded.Clear()
	if got := len(reloaded.Names()); got != 0 {
		t.Errorf("Clear() left %d credentials", got)
	}
}
