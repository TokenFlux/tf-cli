//go:build pty

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// uxMetrics records user-visible work rather than terminal redraws. A selector
// is one decision screen; controls count navigation and confirmation keys;
// submissions count completed text prompts such as a key or gateway URL.
type uxMetrics struct {
	name        string
	selectors   int
	prompts     int
	controls    int
	submissions int
	elapsed     time.Duration
}

func (m uxMetrics) log(t *testing.T) {
	t.Helper()
	t.Logf("UX %s: selectors=%d prompts=%d controls=%d submissions=%d elapsed_ms=%d",
		m.name, m.selectors, m.prompts, m.controls, m.submissions,
		m.elapsed.Milliseconds())
}

func (m *uxMetrics) selector(p *pty, title string) {
	m.selectors++
	p.waitFor(title)
}

func (m *uxMetrics) control(p *pty, keys string, logical int) {
	m.controls += logical
	p.send(keys)
}

func (m *uxMetrics) submit(p *pty, text string) {
	m.submissions++
	p.send(text)
}

// postImport 向正在监听的 web-import 端口发送一把 Key，
// 并在终端确认后断言浏览器拿到 202 accepted。
func postImport(t *testing.T, listen, origin, key string) <-chan error {
	t.Helper()
	payload := []byte(fmt.Sprintf(`{"version":1,"key":%q,"host":%q}`, key, origin))
	resultCh := make(chan error, 1)
	go func() {
		req, err := http.NewRequest(http.MethodPost, listen+"/import", strings.NewReader(string(payload)))
		if err != nil {
			resultCh <- err
			return
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 12 * time.Second}).Do(req)
		if err != nil {
			resultCh <- err
			return
		}
		defer resp.Body.Close()
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			resultCh <- readErr
			return
		}
		if resp.StatusCode != http.StatusAccepted || !strings.Contains(string(body), `"accepted"`) {
			resultCh <- fmt.Errorf("import response = %d %s", resp.StatusCode, body)
			return
		}
		resultCh <- nil
	}()
	return resultCh
}

// stubBrowser 放一个什么都不做的浏览器占位，避免测试去开真浏览器。
func stubBrowser(t *testing.T, f fixture) {
	t.Helper()
	for _, name := range []string{"open", "xdg-open"} {
		if err := os.WriteFile(filepath.Join(f.dir, "bin", name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func listenAddr(t *testing.T, p *pty) string {
	t.Helper()
	listen := regexp.MustCompile(`http://127.0.0.1:4311[0-9]`).FindString(p.screen())
	if listen == "" {
		t.Fatalf("web-import listener not found\n%s", p.tail())
	}
	return listen
}

// TestUXBaselineDefaultLoginIsWebImport measures the default tf login:
// it goes straight to web import — no method or gateway selector —
// and the only remaining decision is the terminal write confirmation.
func TestUXBaselineDefaultLoginIsWebImport(t *testing.T) {
	models := []string{"gpt-5.4", "gpt-5.5"}
	server := fakeGateway(t, models)
	f := writeConfig(t, server.URL, models)
	stubBrowser(t, f)

	started := time.Now()
	env := append(f.env(), "TF_API_KEY=", "SHELL=/bin/sh")
	p := start(t, env, "login", "--host", server.URL)
	m := uxMetrics{name: "web-import-default"}

	p.waitFor("等待网页导入")
	resultCh := postImport(t, listenAddr(t, p), server.URL, "sk-web-import-test")

	p.waitFor("收到网页导入请求")
	m.selector(p, "写入")
	m.control(p, keyEnter, 1)
	if err := <-resultCh; err != nil {
		t.Fatal(err)
	}
	// 导入后不再选择名称：本地名由模型目录自动给出。
	p.waitFor(`已保存为 Key "gpt"`)

	if code := p.waitExit(); code != 0 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
	m.elapsed = time.Since(started)
	m.log(t)
	for _, gone := range []string{"选择登录方式", "选择网关", "选择本地 Key 名称", "是否安装"} {
		if strings.Contains(p.screen(), gone) {
			t.Fatalf("removed selector %q still shown\n%s", gone, p.tail())
		}
	}
}

// TestUXBaselineWithKey measures the explicit paste path: --with-key
// goes directly to the hidden prompt without any selector.
func TestUXBaselineWithKey(t *testing.T) {
	server := fakeGateway(t, []string{"gpt-5.4"})
	f := writeConfig(t, server.URL, []string{"gpt-5.4"})

	started := time.Now()
	p := start(t, append(f.env(), "TF_API_KEY=", "SHELL=/bin/sh"), "login", "paste", "--with-key", "--host", server.URL)
	m := uxMetrics{name: "explicit-with-key"}
	p.waitFor("粘贴 API Key")
	m.submit(p, "sk-explicit-paste\n")
	p.waitFor(`已保存为 Key "paste"`)
	if code := p.waitExit(); code != 0 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
	m.elapsed = time.Since(started)
	m.log(t)
	for _, gone := range []string{"选择登录方式", "选择网关", "是否安装"} {
		if strings.Contains(p.screen(), gone) {
			t.Fatalf("removed selector %q still shown\n%s", gone, p.tail())
		}
	}
}

// TestUXBaselineLoginDoesNotOfferCompletions locks in that a successful
// login exits right after printing the result: installing completions is
// an explicit tf completions <shell> --install, not a login follow-up.
func TestUXBaselineLoginDoesNotOfferCompletions(t *testing.T) {
	server := fakeGateway(t, []string{"gpt-5.4"})
	f := writeConfig(t, server.URL, []string{"gpt-5.4"})
	home := filepath.Join(f.dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	env := append(f.env(), "TF_API_KEY=", "HOME="+home, "SHELL=/bin/bash")
	p := start(t, env, "login", "paste", "--with-key", "--host", server.URL)
	m := uxMetrics{name: "post-login-no-completion-offer"}
	p.waitFor("粘贴 API Key")
	m.submit(p, "sk-completion-paste\n")
	p.waitFor(`已保存为 Key "paste"`)
	if code := p.waitExit(); code != 0 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
	m.elapsed = time.Since(started)
	m.log(t)
	if strings.Contains(p.screen(), "是否安装") {
		t.Fatalf("login still offered completions\n%s", p.tail())
	}
}

// TestUXBaselineWebImportWithExplicitTarget measures the web-import shortcut
// with both the gateway and local name already supplied. It isolates the
// terminal confirmation that cannot be removed without weakening the import
// authorization boundary.
func TestUXBaselineWebImportWithExplicitTarget(t *testing.T) {
	server := fakeGateway(t, []string{"gpt-5.4"})
	f := writeConfig(t, server.URL, []string{"gpt-5.4"})
	stubBrowser(t, f)

	started := time.Now()
	p := start(t, append(f.env(), "TF_API_KEY=", "SHELL=/bin/sh"), "login", "named", "--from-web", "--host", server.URL)
	m := uxMetrics{name: "explicit-web-import-target"}
	p.waitFor("等待网页导入")
	resultCh := postImport(t, listenAddr(t, p), server.URL, "sk-web-import-explicit")

	p.waitFor("收到网页导入请求")
	m.selector(p, "写入")
	m.control(p, keyEnter, 1)
	if err := <-resultCh; err != nil {
		t.Fatal(err)
	}
	p.waitFor(`已保存为 Key "named"`)
	if code := p.waitExit(); code != 0 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
	m.elapsed = time.Since(started)
	m.log(t)
}

// TestUXBaselineFirstLaunchAndSteadyState measures the model setup
// cost and the saved fast path in a single isolated configuration.
func TestUXBaselineFirstLaunchAndSteadyState(t *testing.T) {
	models := []string{"claude-sonnet-5", "claude-haiku-4-5", "claude-opus-5"}
	server := fakeGateway(t, models)
	f := writeConfig(t, server.URL, models)
	env := append(f.env(), "TF_API_KEY=")

	first := uxMetrics{name: "first-launch-with-slot-setup"}
	started := time.Now()
	p := start(t, env, "claude")
	first.selector(p, "选择主模型")
	first.control(p, keyEnter, 1)
	// 辅助槽自动填充，不再逐个询问。
	p.waitFor("FAKE-claude")
	if code := p.waitExit(); code != 0 {
		t.Fatalf("first launch code=%d\n%s", code, p.tail())
	}
	first.elapsed = time.Since(started)
	first.log(t)

	steady := uxMetrics{name: "steady-state-launch"}
	started = time.Now()
	p = start(t, env, "claude")
	p.waitFor("FAKE-claude")
	if code := p.waitExit(); code != 0 {
		t.Fatalf("steady launch code=%d\n%s", code, p.tail())
	}
	steady.elapsed = time.Since(started)
	steady.log(t)

	if first.selectors != 1 {
		t.Fatalf("first launch selectors=%d, want 1", first.selectors)
	}
	if steady.selectors != 0 {
		t.Fatalf("steady launch selectors=%d, want 0", steady.selectors)
	}
}

// TestUXBaselineOneShotModel measures the existing explicit one-shot path.
func TestUXBaselineOneShotModel(t *testing.T) {
	models := []string{"claude-sonnet-5", "claude-haiku-4-5", "claude-opus-5"}
	server := fakeGateway(t, models)
	f := writeConfig(t, server.URL, models)

	started := time.Now()
	p := start(t, append(f.env(), "TF_API_KEY="), "claude", "-m", "claude-sonnet-5", "--", "-p", "hello")
	m := uxMetrics{name: "one-shot-model"}
	p.waitFor("FAKE-claude")
	if code := p.waitExit(); code != 0 {
		t.Fatalf("code=%d\n%s", code, p.tail())
	}
	m.elapsed = time.Since(started)
	m.log(t)
}
