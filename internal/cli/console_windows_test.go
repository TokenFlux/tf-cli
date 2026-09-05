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
	args := []string{"login", "fixture", "--host", host}
	if mode := os.Getenv("TF_TEST_GATEWAY"); mode != "" {
		args = []string{"login", "fixture"}
		if mode == "default" {
			config.DefaultHost = host
		}
		if mode == "pipe" {
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
	}{
		{"save", "", false}, {"cancel", "", true},
		{"default-gateway", "default", false}, {"custom-gateway", "custom", false},
		{"invalid-gateway-retry", "invalid", false}, {"existing-gateway", "existing", false},
		{"cancel-gateway", "cancel", true}, {"cancel-custom-url", "cancel-url", true},
		{"piped-key-existing-gateway", "pipe", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cancelInput := tc.cancel
			dir := t.TempDir()
			paths := config.Paths{ConfigDir: filepath.Join(dir, "tf")}
			cfg, err := config.Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			cfg.CompletionsAsked = true
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
			if tc.gateway != "pipe" {
				waitFor("Choose a login method")
				send("\x1b[B\r")
			}
			if tc.gateway != "" && tc.gateway != "pipe" {
				waitFor("Choose a gateway")
				switch tc.gateway {
				case "cancel":
					send("\x1b")
				case "default":
					send("\r")
				default:
					if tc.gateway == "existing" {
						send("\r")
					} else {
						send("\x1b[B\r")
					}
					waitFor("Gateway URL")
					if tc.gateway == "invalid" {
						send("ftp://invalid\r")
						waitFor("Enter a valid HTTP(S)")
					}
					if tc.gateway == "existing" {
						send("\r")
					} else if tc.gateway == "cancel-url" {
						send("\x03")
					} else {
						send(srv.URL + "/v1/\r")
					}
				}
			}
			if tc.gateway != "cancel" && tc.gateway != "cancel-url" && tc.gateway != "pipe" {
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
			want := uint32(0)
			if cancelInput {
				want = 130
			}
			if err != nil || code != want {
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
			if cancelInput {
				if exists {
					t.Fatal("cancel persisted the key")
				}
			} else if !exists || cred.Key != "sk-fixture-only" {
				t.Fatal("login did not save the fixture key")
			}
			if !cancelInput {
				stored, err := config.Load(paths)
				if err != nil || stored.Keys["fixture"].Host != srv.URL {
					t.Fatalf("wrong gateway saved: %v", err)
				}
			}
		})
	}
}
