//go:build pty

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNarrowTerminalCanSelectTruncatedModels(t *testing.T) {
	ids := []string{strings.Repeat("long-model-", 12) + "one", strings.Repeat("long-model-", 12) + "two"}
	srv := fakeGateway(t, ids)
	f := writeConfig(t, srv.URL, ids)
	stty, err := exec.LookPath("stty")
	if err != nil {
		t.Skip(err)
	}
	wrapper := fmt.Sprintf("#!/bin/sh\n%q rows 12 cols 40\nexec %q \"$@\"\n", stty, stty)
	if err := os.WriteFile(filepath.Join(f.dir, "bin", "stty"), []byte(wrapper), 0755); err != nil {
		t.Fatal(err)
	}
	p := start(t, append(f.env(), "TF_API_KEY="), "claude", "-m")
	p.waitFor("选择主模型")
	p.waitFor("…")
	p.send(keyDown)
	p.send(keyEnter)
	p.waitFor("FAKE-claude")
	if code := p.waitExit(); code != 0 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
	if !strings.Contains(p.screen(), ids[1]) {
		t.Fatal("truncation changed the selected model ID")
	}
}
