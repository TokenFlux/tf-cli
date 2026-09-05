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
	os.Exit(Main([]string{"login", "fixture", "--host", host}))
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
	for _, cancelInput := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "cancel"}[cancelInput], func(t *testing.T) {
			dir := t.TempDir()
			paths := config.Paths{ConfigDir: filepath.Join(dir, "tf")}
			cfg, err := config.Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			cfg.CompletionsAsked = true
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			env := append(os.Environ(), "TF_TEST_LOGIN_HOST="+srv.URL, "TF_LANG=en", "TF_API_KEY=", "XDG_CONFIG_HOME="+dir, "HOME="+dir, "USERPROFILE="+dir, "SHELL=")
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
			waitFor("Choose a login method")
			send("\x1b[B\r")
			waitFor("Paste API key")
			if cancelInput {
				send("sk-fixture-only\x03")
			} else {
				send("sk-fixture-only\r")
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
		})
	}
}
