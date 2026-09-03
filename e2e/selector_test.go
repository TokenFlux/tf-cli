//go:build pty

package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// fakeGateway 只提供 /v1/models。
//
// 选择器要的只有模型列表；真网关要网络、要额度、还会变，
// 今天就因为配额用尽收了一串 429 —— 那种失败与代码对错无关。
func fakeGateway(t *testing.T, models []string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		data := make([]map[string]string, len(models))
		for i, m := range models {
			data[i] = map[string]string{"id": m}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// 目录里带 j / k / q 的名字最需要过滤，而它们曾经恰好过滤不了。
//
// 那三个字母被当成了 vim 风格的导航键，于是敲 haiku 的 k 会让光标乱跳。
// 这是这套 pty 测试存在的首要理由：单元测试看不见按键。
func TestLettersFilterInsteadOfNavigating(t *testing.T) {
	models := []string{
		"claude-haiku-4-5-20251001", "claude-opus-5", "claude-sonnet-5",
		"qwen-max", "kimi-k2", "gpt-5.4",
	}
	srv := fakeGateway(t, models)
	f := writeConfig(t, srv.URL, models)

	for _, tc := range []struct{ typed, want string }{
		{"haiku", "claude-haiku-4-5-20251001"}, // k 在词中
		{"qwen", "qwen-max"},                   // q 打头
		{"kimi", "kimi-k2"},                    // k 打头
	} {
		p := start(t, f.env(), "claude", "-m")
		p.waitFor("选择主模型")
		p.send(tc.typed)
		p.waitFor("/" + tc.typed) // 标题上要显示当前过滤词

		screen := p.screen()
		if !strings.Contains(screen, tc.want) {
			t.Errorf("输入 %q 后没看到 %q\n--- 屏幕 ---\n%s", tc.typed, tc.want, screen)
		}
		if strings.Contains(screen, "无匹配项") {
			t.Errorf("输入 %q 得到无匹配项 —— 字母又被当成导航键了", tc.typed)
		}
		// 两个 ESC 不能连发：逐字节的状态机会把 \x1b\x1b 当成
		// 一个转义序列的开头。等第一个 ESC 的效果出现再发第二个。
		p.send(keyEsc)
		p.waitFor("esc 取消")
		p.send(keyEsc)
		p.waitExit()
	}
}

// ESC 先清过滤，过滤空了才退出。
//
// 打错一个字的唯一出路不该是退出整个选择器。
func TestEscapeClearsFilterBeforeExiting(t *testing.T) {
	models := []string{"claude-opus-5", "gpt-5.4"}
	srv := fakeGateway(t, models)
	f := writeConfig(t, srv.URL, models)

	p := start(t, f.env(), "claude", "-m")
	p.waitFor("选择主模型")

	p.send("opus")
	p.waitFor("/opus")

	// 第一次 ESC：过滤没了，选择器还在，提示行回到「取消」。
	p.send(keyEsc)
	p.waitFor("esc 取消")
	if s := p.screen(); !strings.Contains(s, "gpt-5.4") {
		t.Errorf("清掉过滤后应重新看到全部候选\n--- 屏幕 ---\n%s", s)
	}

	// 第二次 ESC：退出，且退出码是 130 而不是报错。
	p.send(keyEsc)
	if code := p.waitExit(); code != 130 {
		t.Errorf("取消的退出码 = %d，want 130", code)
	}
	if s := p.screen(); strings.Contains(s, "错误") {
		t.Errorf("取消不是错误，不该打错误行\n--- 屏幕 ---\n%s", s)
	}
}

// 列表被截断时必须说出来。
//
// 之前滚动是无声的：找不到想要的模型时，人会以为它不在，
// 而不是以为要往下翻。
func TestTruncationIsAnnounced(t *testing.T) {
	// 用远超任何终端高度的数量：可见行数来自 stty size，而 pty 的尺寸
	// 是从跑测试的终端继承来的，写死一个小数字会在大窗口上不截断。
	var models []string
	for i := 0; i < 100; i++ {
		models = append(models, fmt.Sprintf("model-%02d", i))
	}
	srv := fakeGateway(t, models)
	f := writeConfig(t, srv.URL, models)

	p := start(t, f.env(), "claude", "-m")
	p.waitFor("选择主模型")
	p.waitFor("共 100 个")

	p.send(keyEsc)
	p.waitExit()
}

// -m 后面跟的词不是模型时，要退还给 harness，而不是当成模型 ID。
//
// tf claude -m "解释这段代码" 曾经把那句 prompt 当成模型名，
// 然后报一句「现在没有 Key 能提供 解释这段代码」—— 两个错误叠在一起，
// 用户会得到一个完全误导的结论。
func TestDetachedModelValueFallsBackToPicker(t *testing.T) {
	models := []string{"claude-opus-5", "gpt-5.4"}
	srv := fakeGateway(t, models)
	f := writeConfig(t, srv.URL, models)

	p := start(t, f.env(), "claude", "-m", "解释这段代码")
	p.waitFor("选择主模型") // 没被当成模型 ID，而是进了选择器

	if s := p.screen(); strings.Contains(s, "解释这段代码") &&
		strings.Contains(s, "没有 Key 能提供") {
		t.Errorf("prompt 被当成了模型 ID\n--- 屏幕 ---\n%s", s)
	}
	// 那个词必须原样到达 harness，不能被吃掉。
	p.send(keyEnter)
	p.waitFor("FAKE-claude args:")
	if s := p.screen(); !strings.Contains(s, "解释这段代码") {
		t.Errorf("退还的词没有透传给 harness\n--- 屏幕 ---\n%s", p.tail())
	}
	p.waitExit()
}

// Ctrl-U 清空过滤，Ctrl-N 向下移动。
//
// j / k 让位之后，替代键必须真的可用，否则等于只是拿掉了功能。
func TestControlKeysStillNavigate(t *testing.T) {
	models := []string{"claude-opus-5", "claude-sonnet-5", "gpt-5.4"}
	srv := fakeGateway(t, models)
	f := writeConfig(t, srv.URL, models)

	p := start(t, f.env(), "claude", "-m")
	p.waitFor("选择主模型")

	p.send("claude")
	p.waitFor("/claude")
	p.send(keyCtrlU)
	p.waitFor("esc 取消") // 过滤清掉后提示行变回「取消」
	if s := p.screen(); !strings.Contains(s, "gpt-5.4") {
		t.Errorf("Ctrl-U 之后应重新看到全部候选\n--- 屏幕 ---\n%s", s)
	}

	p.send(keyDown)
	p.send(keyEnter)
	p.waitFor("tf → claude")
	p.waitFor("FAKE-claude")
	if code := p.waitExit(); code != 0 {
		t.Errorf("退出码 = %d，want 0", code)
	}
}

// 网页导入必须走完整的真实边界：HTTP 回环通道 → 终端确认 → 网关校验
// → 0600 凭据写盘。只测 handler 会漏掉控制终端和主登录流程之间的接线。
func TestWebImportRequiresTerminalConfirmationBeforeSaving(t *testing.T) {
	models := []string{"gpt-5.4", "gpt-5.5"}
	srv := fakeGateway(t, models)
	f := writeConfig(t, srv.URL, models)
	env := append(f.env(), "SHELL=/bin/sh") // 不在登录成功后追问 shell 补全

	p := start(t, env, "login", "web", "--host", srv.URL)
	p.waitFor("选择登录方式")
	p.waitFor("从网页导入")
	p.send(keyDown)
	p.send(keyEnter)
	p.waitFor("等待网页导入")
	match := regexp.MustCompile(`http://127\.0\.0\.1:4311[0-9]`).FindString(p.screen())
	if match == "" {
		t.Fatalf("没有从输出找到导入地址\n--- 屏幕 ---\n%s", p.tail())
	}

	session := regexp.MustCompile(`#tf=1\.(4311[0-9])\.([A-Za-z0-9_-]+)`).FindStringSubmatch(p.screen())
	if len(session) != 3 {
		t.Fatalf("没有从输出找到会话链接\n--- 屏幕 ---\n%s", p.tail())
	}
	if match != "http://127.0.0.1:"+session[1] {
		t.Fatalf("监听端口 %q 与会话端口 %q 不一致", match, session[1])
	}
	challenge := "abcdefghijklmnop"
	pingReq, err := http.NewRequest(http.MethodGet, match+"/ping?challenge="+challenge, nil)
	if err != nil {
		t.Fatal(err)
	}
	pingReq.Header.Set("Origin", srv.URL)
	pingResp, err := (&http.Client{Timeout: 10 * time.Second}).Do(pingReq)
	if err != nil {
		t.Fatal(err)
	}
	var ping struct {
		Proof string `json:"proof"`
	}
	if err := json.NewDecoder(pingResp.Body).Decode(&ping); err != nil {
		pingResp.Body.Close()
		t.Fatal(err)
	}
	pingResp.Body.Close()
	secret, err := base64.RawURLEncoding.DecodeString(session[2])
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 16 {
		t.Fatalf("会话 secret 长度 = %d; want 16", len(secret))
	}
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "tf-web-import-v1\n%s\n%s", session[1], challenge)
	wantProof := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if pingResp.StatusCode != http.StatusOK || ping.Proof != wantProof {
		t.Fatalf("会话证明 = %d %q; want %q", pingResp.StatusCode, ping.Proof, wantProof)
	}

	type result struct {
		status int
		body   string
		err    error
	}
	postImport := func(payload []byte, verified bool) <-chan result {
		ch := make(chan result, 1)
		go func() {
			req, err := http.NewRequest(http.MethodPost, match+"/import", bytes.NewReader(payload))
			if err != nil {
				ch <- result{err: err}
				return
			}
			req.Header.Set("Origin", srv.URL)
			req.Header.Set("Content-Type", "application/json")
			if verified {
				importMAC := hmac.New(sha256.New, secret)
				fmt.Fprintf(importMAC, "tf-web-import-v1\n%s\nimport\n", session[1])
				_, _ = importMAC.Write(payload)
				req.Header.Set("X-TF-Session-Proof", base64.RawURLEncoding.EncodeToString(importMAC.Sum(nil)))
			}
			resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
			if err != nil {
				ch <- result{err: err}
				return
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			ch <- result{status: resp.StatusCode, body: string(body), err: err}
		}()
		return ch
	}

	unverifiedPayload := []byte(fmt.Sprintf(
		`{"version":1,"key":"sk-unverified","host":%q}`, srv.URL))
	unverifiedCh := postImport(unverifiedPayload, false)
	p.waitFor("未验证本机 tf 会话")
	p.waitFor("写入")
	p.send(keyDown)
	p.send(keyEnter)
	select {
	case got := <-unverifiedCh:
		if got.err != nil || got.status != http.StatusConflict || !strings.Contains(got.body, `"rejected"`) {
			t.Fatalf("未验证请求响应 = %d %s %v", got.status, got.body, got.err)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("未验证请求没有收到拒绝响应")
	}
	p.waitFor("已拒绝该请求，继续等待")

	payload := []byte(fmt.Sprintf(`{
		"version": 1,
		"key": "sk-web-import-test",
		"host": %q,
		"key_name": "browser-key",
		"group_id": 7,
		"group_name": "GPT"
	}`, srv.URL))
	resultCh := postImport(payload, true)

	p.waitFor("收到网页导入请求")
	p.waitFor("已验证当前 tf 会话")
	p.waitFor("写入")
	p.send(keyEnter)

	select {
	case got := <-resultCh:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.status != http.StatusAccepted || !strings.Contains(got.body, `"accepted"`) {
			t.Errorf("浏览器响应 = %d %s", got.status, got.body)
		}
	case <-time.After(12 * time.Second):
		t.Fatal("浏览器没有收到导入响应")
	}

	p.waitFor(`已保存为 Key "web"`)
	if code := p.waitExit(); code != 0 {
		t.Fatalf("退出码 = %d，want 0\n--- 屏幕 ---\n%s", code, p.tail())
	}

	path := filepath.Join(f.dir, "cfg", "tf", "credentials.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored struct {
		Items map[string]struct {
			Key       string `json:"key"`
			Source    string `json:"source"`
			Origin    string `json:"origin"`
			KeyName   string `json:"key_name"`
			GroupID   int64  `json:"group_id"`
			GroupName string `json:"group_name"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	got := stored.Items["web"]
	if got.Key != "sk-web-import-test" || got.Source != "import" || got.Origin != srv.URL ||
		got.KeyName != "browser-key" || got.GroupID != 7 || got.GroupName != "GPT" {
		t.Errorf("落盘元数据不完整：%+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("凭据权限 = %v; want 0600", info.Mode().Perm())
	}
}

func TestInteractiveLoginCanChoosePaste(t *testing.T) {
	srv := fakeGateway(t, []string{"gpt-5.4"})
	f := writeConfig(t, srv.URL, []string{"gpt-5.4"})
	env := append(f.env(), "SHELL=/bin/sh")

	p := start(t, env, "login", "paste", "--host", srv.URL)
	p.waitFor("选择登录方式")
	p.waitFor("粘贴 API Key")
	p.send(keyEnter)
	p.waitFor("粘贴 API Key")
	p.send("sk-interactive-paste\n")
	p.waitFor(`已保存为 Key "paste"`)
	if code := p.waitExit(); code != 0 {
		t.Fatalf("退出码 = %d，want 0\n--- 屏幕 ---\n%s", code, p.tail())
	}
}

// 管道本身已经明确选择了粘贴方式；即使机器有控制终端，也不能再弹
// 登录方式选择器抢走 stdin。
func TestPipedLoginSkipsMethodPicker(t *testing.T) {
	srv := fakeGateway(t, []string{"gpt-5.4"})
	f := writeConfig(t, srv.URL, []string{"gpt-5.4"})
	bin, err := filepath.Abs("../bin/tf")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "login", "pipe", "--host", srv.URL)
	cmd.Env = append(os.Environ(), append(f.env(), "SHELL=/bin/sh")...)
	cmd.Stdin = strings.NewReader("sk-piped-login\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("管道登录失败：%v\n%s", err, out)
	}
	text := clean(string(out))
	if strings.Contains(text, "选择登录方式") {
		t.Fatalf("管道登录不应弹方式选择器：\n%s", text)
	}
	if !strings.Contains(text, `已保存为 Key "pipe"`) {
		t.Fatalf("管道登录没有保存：\n%s", text)
	}
}
