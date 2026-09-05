//go:build pty

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReplacingStoredKeyDefaultsToCancel(t *testing.T) {
	ids := []string{"gpt-5.4"}
	srv := fakeGateway(t, ids)
	f := writeConfig(t, srv.URL, ids)
	p := start(t, append(f.env(), "TF_API_KEY="), "login", "k", "--with-key", "--host", srv.URL)
	p.waitFor("粘贴 API Key")
	p.send("sk-new-secret\n")
	p.waitFor("覆盖 Key")
	p.send(keyEnter)
	if code := p.waitExit(); code != 130 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
	data, err := os.ReadFile(filepath.Join(f.dir, "cfg", "tf", "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Items map[string]struct {
			Key string `json:"key"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Items["k"].Key != "sk-test" {
		t.Fatal("old key was overwritten despite cancellation")
	}
}
