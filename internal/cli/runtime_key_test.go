package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/harness"
)

func TestStatusDoesNotBroadcastEnvironmentCredential(t *testing.T) {
	t.Setenv("TF_API_KEY", "sk-environment-private")
	cfg, creds := fixture(t, map[string][]string{"a": nil, "b": nil})
	received := make(chan string, 2)
	for _, name := range []string{"a", "b"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			received <- r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(srv.Close)
		cfg.KeyMetaOf(name).Host = srv.URL
	}
	fetchUsage(cfg, creds)
	for range 2 {
		if got := <-received; got != "Bearer sk-a" && got != "Bearer sk-b" {
			t.Fatalf("unexpected credential: %q", got)
		}
	}
}

func TestEnvironmentLaunchIsIsolatedAndDoesNotPersist(t *testing.T) {
	t.Setenv("TF_API_KEY", "sk-environment-private")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-environment-private" {
			t.Error("incorrect credential")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-test"}]}`))
	}))
	defer server.Close()
	c := testCtx()
	c.Flags.set["host"] = server.URL
	c.Flags.set["model"] = "gpt-test"
	c.Flags.present["model"] = true
	st, err := launchState(c)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := harness.Lookup("codex")
	key, slots, err := resolveTarget(c, st.cfg, st.creds, h)
	if err != nil || key != "env" || slots["default"] != "gpt-test" {
		t.Fatalf("%s %v %v", key, slots, err)
	}
	if err := st.cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := st.creds.Save(); err != nil {
		t.Fatal(err)
	}
	paths, _ := config.DefaultPaths()
	for _, path := range []string{paths.ConfigFile(), paths.CredentialsFile()} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("runtime account persisted: %s %v", path, err)
		}
	}
}

func TestExplicitStoredKeyWinsOverEnvironment(t *testing.T) {
	t.Setenv("TF_API_KEY", "sk-environment-private")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	paths, _ := config.DefaultPaths()
	creds, _, _ := config.LoadCredentials(paths)
	creds.Set("work", &config.Credential{Key: "sk-stored"})
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}
	c := testCtx()
	c.Flags.set["key"] = "work"
	st, err := launchState(c)
	if err != nil {
		t.Fatal(err)
	}
	cred, ok := st.creds.Get("work")
	if !ok || cred.Key != "sk-stored" {
		t.Fatalf("%v", cred)
	}
}
