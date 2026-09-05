//go:build pty

package e2e

import (
	"strings"
	"testing"
)

func TestCtrlCAtAuxiliarySlotAbortsLaunch(t *testing.T) {
	ids := []string{"claude-sonnet-5", "claude-haiku-4-5"}
	srv := fakeGateway(t, ids)
	f := writeConfig(t, srv.URL, ids)
	p := start(t, append(f.env(), "TF_API_KEY="), "claude")
	p.waitFor("选择主模型")
	p.send(keyEnter)
	p.waitFor("配置 claude 的 fast")
	p.send("\x03")
	if code := p.waitExit(); code != 130 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
	if strings.Contains(p.screen(), "FAKE-claude") {
		t.Fatal("Ctrl-C launched the harness")
	}
}

func TestEscapeAtAuxiliarySlotUsesSuggestions(t *testing.T) {
	ids := []string{"claude-sonnet-5", "claude-haiku-4-5"}
	srv := fakeGateway(t, ids)
	f := writeConfig(t, srv.URL, ids)
	p := start(t, append(f.env(), "TF_API_KEY="), "claude")
	p.waitFor("选择主模型")
	p.send(keyEnter)
	p.waitFor("配置 claude 的 fast")
	p.send(keyEsc)
	p.waitFor("FAKE-claude")
	if code := p.waitExit(); code != 0 {
		t.Fatalf("code=%d", code)
	}
}

func TestCtrlCInSlotEditorAbortsCommand(t *testing.T) {
	ids := []string{"gpt-5.4"}
	srv := fakeGateway(t, ids)
	f := writeConfig(t, srv.URL, ids)
	p := start(t, append(f.env(), "TF_API_KEY="), "model", "codex", "--edit")
	p.waitFor("codex 的模型槽")
	p.send(keyEnter)
	p.waitFor("codex.default 用哪个模型")
	p.send("\x03")
	if code := p.waitExit(); code != 130 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
}
