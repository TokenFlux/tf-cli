package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UserExistsError/conpty"
	"github.com/charmbracelet/x/ansi"
	"github.com/tokenflux/tf-cli/internal/config"
	"golang.org/x/sys/windows"
)

func TestWindowsLoginHelper(t *testing.T) {
	host := os.Getenv("TF_TEST_LOGIN_HOST")
	if host == "" {
		return
	}
	mode := os.Getenv("TF_TEST_GATEWAY")
	args := []string{"login", "fixture", "--with-key", "--host", host}
	switch mode {
	case "default":
		config.DefaultHost = host
		args = []string{"login", "fixture", "--with-key"}
	case "existing":
		// 同名 Key 已存的 host 由配置文件提供，命令行不带 --host。
		args = []string{"login", "fixture", "--with-key"}
	case "invalid":
		args = []string{"login", "fixture", "--with-key", "--host", "ftp://invalid"}
	case "pipe":
		args = []string{"login", "fixture"}
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.WriteString("sk-fixture-only\n"); err != nil {
			t.Fatal(err)
		}
		w.Close()
		os.Stdin = r
	}
	os.Exit(Main(args))
}

type loginOutput struct {
	mu   sync.Mutex
	text strings.Builder
}

func (o *loginOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.text.Write(p)
}
func (o *loginOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.ReplaceAll(strings.ReplaceAll(ansi.Strip(o.text.String()), "\r", ""), "\n", "")
}

// Windows 登录走与 Unix 一致的默认路径：方式与网关都不再询问，
// --with-key 直达隐藏输入，host 的优先级为 --host > 同名已存 > 默认值。
func TestWindowsLoginInteraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "gpt-test"}}})
			return
		}
		http.Error(w, "fixture", 400)
	}))
	defer srv.Close()
	for _, tc := range []struct {
		name, gateway string
		cancel        bool
		wantCode      uint32
	}{
		{"save", "", false, 0}, {"cancel", "", true, 130},
		{"default-gateway", "default", false, 0},
		{"existing-gateway", "existing", false, 0},
		{"invalid-gateway", "invalid", false, 1},
		{"piped-key-existing-gateway", "pipe", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cancelInput := tc.cancel
			dir := t.TempDir()
			paths := config.Paths{ConfigDir: filepath.Join(dir, "tf")}
			cfg, err := config.Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			if tc.gateway == "existing" || tc.gateway == "pipe" {
				cfg.KeyMetaOf("fixture").Host = srv.URL
			}
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			env := append(os.Environ(), "TF_TEST_GATEWAY="+tc.gateway, "TF_TEST_LOGIN_HOST="+srv.URL, "TF_LANG=en", "TF_API_KEY=", "XDG_CONFIG_HOME="+dir, "HOME="+dir, "USERPROFILE="+dir, "SHELL=")
			pty, err := conpty.Start(windows.EscapeArg(exe)+" -test.run=^TestWindowsLoginHelper$", conpty.ConPtyDimensions(100, 30), conpty.ConPtyEnv(env))
			if err != nil {
				t.Fatal(err)
			}
			defer pty.Close()
			output := &loginOutput{}
			go func() { _, _ = io.Copy(output, pty) }()
			waitFor := func(text string) {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for !strings.Contains(output.String(), text) {
					if time.Now().After(deadline) {
						t.Fatalf("waiting for %q: %s", text, output.String())
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			send := func(text string) {
				t.Helper()
				if _, err := pty.Write([]byte(text)); err != nil {
					t.Fatal(err)
				}
			}
			switch tc.gateway {
			case "pipe":
				// 管道 stdin 本身就是明确选择，不该有任何终端交互。
			case "invalid":
				waitFor("invalid gateway address")
			default:
				waitFor("Paste API key (hidden):")
				if cancelInput {
					send("sk-fixture-only\x03")
				} else {
					send("sk-fixture-only\r")
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			code, err := pty.Wait(ctx)
			if err != nil || code != tc.wantCode {
				t.Fatalf("exit=%d err=%v output=%s", code, err, output.String())
			}
			if strings.Contains(output.String(), "sk-fixture-only") {
				t.Fatal("secret was echoed")
			}
			creds, _, err := config.LoadCredentials(paths)
			if err != nil {
				t.Fatal(err)
			}
			cred, exists := creds.Get("fixture")
			if cancelInput || tc.gateway == "invalid" {
				if exists {
					t.Fatal("a rejected login persisted the key")
				}
				return
			}
			if !exists || cred.Key != "sk-fixture-only" {
				t.Fatal("login did not save the fixture key")
			}
			stored, err := config.Load(paths)
			if err != nil || stored.Keys["fixture"].Host != srv.URL {
				t.Fatalf("wrong gateway saved: %v", err)
			}
		})
	}
}
