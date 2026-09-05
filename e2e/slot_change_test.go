//go:build pty

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSlotKeyChangeConfirmsBeforeClearing(t *testing.T) {
	for _, accept := range []bool{false, true} {
		name := "cancel"
		if accept {
			name = "accept"
		}
		t.Run(name, func(t *testing.T) {
			ids := []string{"gpt-5.4"}
			srv := fakeGateway(t, ids)
			f := writeConfig(t, srv.URL, ids)
			dir := filepath.Join(f.dir, "cfg", "tf")
			configPath := filepath.Join(dir, "config.json")
			data, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var cfg map[string]any
			if err := json.Unmarshal(data, &cfg); err != nil {
				t.Fatal(err)
			}
			keys := cfg["keys"].(map[string]any)
			keys["other"] = keys["k"]
			cfg["harnesses"] = map[string]any{"codex": map[string]any{"key": "k", "slots": map[string]string{"default": "gpt-5.4", "review": "old-review"}}}
			data, _ = json.Marshal(cfg)
			if err := os.WriteFile(configPath, data, 0644); err != nil {
				t.Fatal(err)
			}
			credentials, _ := json.Marshal(map[string]any{"version": 1, "credentials": map[string]any{"k": map[string]string{"key": "sk-test"}, "other": map[string]string{"key": "sk-other"}}})
			if err := os.WriteFile(filepath.Join(dir, "credentials.json"), credentials, 0600); err != nil {
				t.Fatal(err)
			}
			p := start(t, append(f.env(), "TF_API_KEY="), "model", "codex", "--edit")
			p.waitFor("codex 的模型槽")
			p.send(keyEnter)
			p.waitFor("codex.default 用哪个模型")
			p.send("other")
			p.waitFor("/other")
			p.send(keyEnter)
			p.waitFor("切换到 Key")
			p.waitFor("old-review")
			if accept {
				p.send(keyDown)
			}
			p.send(keyEnter)
			p.waitFor("codex 的模型槽")
			p.send("\x03")
			if code := p.waitExit(); code != 130 {
				t.Fatalf("code=%d", code)
			}
			data, err = os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			var stored struct {
				Harnesses map[string]struct {
					Key   string            `json:"key"`
					Slots map[string]string `json:"slots"`
				} `json:"harnesses"`
			}
			if err := json.Unmarshal(data, &stored); err != nil {
				t.Fatal(err)
			}
			got := stored.Harnesses["codex"]
			if accept {
				if got.Key != "other" || got.Slots["review"] != "" {
					t.Fatalf("change not saved: %+v", got)
				}
			} else if got.Key != "k" || got.Slots["review"] != "old-review" {
				t.Fatalf("cancel changed config: %+v", got)
			}
		})
	}
}
