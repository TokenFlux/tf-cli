package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// serve 用固件文件起一个假网关。
//
// 固件是真应答存下来的，只抹掉了账号标识。用固件而不是手写 JSON：
// 手写的会长成我以为的样子，而不是网关实际给的样子。
func serve(t *testing.T, routes map[string]string, status int) *Client {
	t.Helper()
	mux := http.NewServeMux()
	for path, file := range routes {
		body, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(body)
		})
	}
	s := httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return New(s.URL, "sk-test")
}

func TestModelsParsesRealResponse(t *testing.T) {
	c := serve(t, map[string]string{"/v1/models": "models.json"}, 200)

	got, err := c.Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("拿到 %d 个模型，want 5", len(got))
	}

	// 顺序必须原样保留。网关把 codex-auto-review 这类专用模型放在末尾，
	// 而选择器默认高亮第一项 —— 重排会把一个当主模型必定失败的模型
	// 顶到回车就能选中的位置。
	if got[0].ID != "gpt-5.4" || got[4].ID != "codex-auto-review" {
		t.Errorf("顺序被改了：%v", ids(got))
	}
}

func ids(ms []Model) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

func TestUsageParsesRealResponse(t *testing.T) {
	c := serve(t, map[string]string{"/v1/usage": "usage.json"}, 200)

	u, err := c.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 这份固件抓于额度用尽的时刻，正是最该被认出来的那个状态。
	if !u.Exhausted() {
		t.Errorf("quota %+v 应判为用尽", u.Quota)
	}
	if u.Quota.Unit == "" {
		t.Error("单位丢了 —— 界面要原样显示它，因为服务端的字符串没有语言")
	}
	if u.Usage.Today.Requests == 0 {
		t.Error("今天的请求数没解析出来")
	}
}

// 额度用尽时 /v1/models 也返回 429。
//
// 这一条必须是错误而不是空列表：空列表会被上层当成「这把 Key 一个模型
// 都没有」，然后清空存着的模型列表 —— 用户额度一恢复就得重新选模型。
func TestModelsFailsLoudlyOnQuotaExhausted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		// 2026-08-31 逐字
		_, _ = w.Write([]byte(`{"code":"API_KEY_QUOTA_EXHAUSTED","message":"API key 额度已用完"}`))
	})
	s := httptest.NewServer(mux)
	defer s.Close()

	got, err := New(s.URL, "sk-test").Models(context.Background())
	if err == nil {
		t.Fatalf("429 必须报错，却返回了 %d 个模型", len(got))
	}
}
