//go:build pty

package e2e

import (
	"strings"
	"testing"
)

func TestChineseFilteringAndBackspace(t *testing.T) {
	ids := []string{"中文/claude-sonnet-5", "other/claude-haiku-4-5"}
	srv := fakeGateway(t, ids)
	f := writeConfig(t, srv.URL, ids)
	p := start(t, append(f.env(), "TF_API_KEY="), "claude", "-m")
	p.waitFor("选择主模型")
	p.send("中文")
	p.waitFor("/中文")
	p.send("\x7f")
	p.waitFor("/中")
	p.send("\x7f")
	p.waitFor("esc 取消")
	p.send("\x03")
	if code := p.waitExit(); code != 130 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(p.screen(), "无匹配项") || strings.ContainsRune(p.screen(), '\ufffd') {
		t.Fatalf("broken Unicode filtering:\n%s", p.screen())
	}
}
