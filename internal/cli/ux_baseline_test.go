package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/ui"
)

// statusIsolation 搭一份指向假网关的隔离配置：一把 Key、假 harness。
//
// 返回已发出的请求计数 —— 「发了几次网络请求」就是这里的 UX 指标，
// 耗时不进断言，它太依赖环境。
func statusIsolation(t *testing.T, handler http.HandlerFunc) *atomic.Int32 {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	fakeBin := t.TempDir()
	for _, name := range []string{"claude", "codex", "opencode", "pi"} {
		path := filepath.Join(fakeBin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", fakeBin)
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	meta := cfg.KeyMetaOf("work")
	meta.Host = server.URL
	meta.Models = []string{"gpt-5.4"}
	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	creds.Set("work", &config.Credential{Key: "sk-test", Source: config.SourcePaste})
	if err := config.SaveState(cfg, creds); err != nil {
		t.Fatal(err)
	}
	return &requests
}

func runStatusJSON(t *testing.T, check bool) (out, errOut bytes.Buffer) {
	t.Helper()
	flags := newValues()
	if check {
		flags.set["check"] = "true"
	}
	ctx := &Context{
		UI:    &ui.UI{Out: &out, Err: &errOut, Lang: ui.LangEN, JSON: true},
		Flags: flags,
	}
	if err := runStatus(ctx); err != nil {
		t.Fatalf("runStatus: %v", err)
	}
	return out, errOut
}

type statusEnvelope struct {
	OK   bool `json:"ok"`
	Data struct {
		Checked     bool                       `json:"checked"`
		Usage       map[string]json.RawMessage `json:"usage"`
		CheckErrors map[string]string          `json:"check_errors"`
	} `json:"data"`
	Warnings []string `json:"warnings"`
}

// 默认 tf status 不联网：本地状态照常输出，一个请求都不发。
func TestStatusDefaultMakesNoNetworkRequests(t *testing.T) {
	requests := statusIsolation(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("default status must not dial the gateway, got %s", r.URL.Path)
	})

	out, _ := runStatusJSON(t, false)
	if got := requests.Load(); got != 0 {
		t.Fatalf("default status made %d requests, want 0", got)
	}

	var env statusEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("status output is not JSON: %v\n%s", err, out.String())
	}
	if !env.OK || env.Data.Checked {
		t.Fatalf("expected ok + checked=false, got %s", out.String())
	}
	if env.Data.Usage != nil || env.Data.CheckErrors != nil {
		t.Fatalf("default mode must omit usage and check_errors: %s", out.String())
	}
}

// tf status --check 对每把 Key 发一次 /v1/usage。
func TestStatusCheckFetchesUsagePerKey(t *testing.T) {
	requests := statusIsolation(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/usage" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"quota":{"limit":10,"remaining":8},"usage":{"today":{"requests":1,"total_tokens":12}}}`))
	})

	out, _ := runStatusJSON(t, true)
	if got := requests.Load(); got != 1 {
		t.Fatalf("--check made %d usage requests, want 1", got)
	}

	var env statusEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || !env.Data.Checked {
		t.Fatalf("expected ok + checked=true, got %s", out.String())
	}
	if env.Data.Usage["work"] == nil {
		t.Fatalf("usage for work missing: %s", out.String())
	}
}

// 一把 Key 检查失败不拖垮整个命令：本地状态照常返回，
// 失败落在 check_errors 和 warnings 里，退出码仍是 0。
func TestStatusCheckFailureKeepsLocalStatus(t *testing.T) {
	requests := statusIsolation(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":"BROKEN"}`, http.StatusInternalServerError)
	})

	out, _ := runStatusJSON(t, true)
	if requests.Load() != 1 {
		t.Fatalf("--check made %d requests, want 1", requests.Load())
	}

	var env statusEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || !env.Data.Checked {
		t.Fatalf("check failure must still be a successful command: %s", out.String())
	}
	if env.Data.CheckErrors["work"] != "http 500: BROKEN" {
		t.Fatalf("check_errors[work] = %q", env.Data.CheckErrors["work"])
	}
	if len(env.Warnings) == 0 {
		t.Fatal("a failed check must surface as a warning")
	}
}

// 多把 Key 的检查彼此隔离：一把失败时，另一把的 usage 仍然保留。
func TestStatusCheckKeepsSuccessfulKeysWhenOneFails(t *testing.T) {
	requests := statusIsolation(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer sk-broken" {
			http.Error(w, `{"code":"BROKEN"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"quota":{"limit":10,"remaining":8}}`))
	})

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg, creds, _, err := config.LoadState(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.KeyMetaOf("broken").Host = cfg.KeyMetaOf("work").Host
	cfg.KeyMetaOf("broken").Models = []string{"gpt-5.4"}
	creds.Set("broken", &config.Credential{Key: "sk-broken", Source: config.SourcePaste})
	if err := config.SaveState(cfg, creds); err != nil {
		t.Fatal(err)
	}

	out, _ := runStatusJSON(t, true)
	if got := requests.Load(); got != 2 {
		t.Fatalf("--check made %d requests, want 2", got)
	}
	var env statusEnvelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Usage["work"] == nil {
		t.Fatalf("successful usage was lost: %s", out.String())
	}
	if env.Data.CheckErrors["broken"] != "http 500: BROKEN" {
		t.Fatalf("check_errors[broken] = %q", env.Data.CheckErrors["broken"])
	}
}
