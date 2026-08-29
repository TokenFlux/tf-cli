package harness

import "testing"

// 别名要能命中，未知名字要落空。
func TestLookup(t *testing.T) {
	for _, name := range []string{"claude", "cc", "claude-code", "codex", "cx", "opencode"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("Lookup(%q) missed", name)
		}
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup(nope) should miss")
	}
}

// 每个 harness 都必须声明主模型槽，否则启动时无从注入。
//
// 槽位漏声明是会静默坑用户的：harness 会回落到内置默认模型，
// 而那个模型通常不在用户的分组里（opencode 的 small_model 已实测坐实）。
func TestEveryHarnessDeclaresRequiredSlots(t *testing.T) {
	for _, h := range All {
		var hasDefault bool
		seen := map[string]bool{}
		for _, s := range h.Slots {
			if seen[s.Name] {
				t.Errorf("%s: duplicate slot %q", h.Name, s.Name)
			}
			seen[s.Name] = true
			if s.Name == "default" {
				hasDefault = true
				if !s.Required {
					t.Errorf("%s: the default slot must be required", h.Name)
				}
			}
			if s.Purpose == nil {
				t.Errorf("%s: slot %q has no purpose text", h.Name, s.Name)
			}
		}
		if !hasDefault {
			t.Errorf("%s: no default slot declared", h.Name)
		}
	}
}

// opencode 的 small 槽必须是必填：不注入会导致标题生成静默失败。
func TestOpencodeSmallSlotIsRequired(t *testing.T) {
	h, ok := Lookup("opencode")
	if !ok {
		t.Fatal("opencode missing")
	}
	for _, s := range h.Slots {
		if s.Name == "small" {
			if !s.Required {
				t.Error("opencode small slot must be required; see research/harness-probe.md")
			}
			return
		}
	}
	t.Error("opencode must declare a small slot")
}

// 安装命令必须自洽，且绝不含提权。
func TestInstallOptionsAreSane(t *testing.T) {
	for _, h := range All {
		if len(h.Installs) == 0 {
			t.Errorf("%s: no install options", h.Name)
		}
		for _, o := range h.Installs {
			if len(o.Args) == 0 {
				t.Errorf("%s: empty install argv", h.Name)
				continue
			}
			if o.Args[0] != o.Manager {
				t.Errorf("%s: argv[0]=%q does not match manager %q", h.Name, o.Args[0], o.Manager)
			}
			for _, a := range o.Args {
				if a == "sudo" {
					t.Errorf("%s: install command must never use sudo: %s", h.Name, o.Command())
				}
			}
		}
	}
}

// 探测不到的二进制不能报告为已安装。
func TestDetectMissingBinary(t *testing.T) {
	h := &Harness{Name: "ghost", Bin: "tkr-definitely-not-a-real-binary"}
	if st := h.Detect(); st.Installed {
		t.Errorf("phantom binary reported as installed: %+v", st)
	}
}
