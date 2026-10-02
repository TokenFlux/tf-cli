package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/ui"
)

func authTestContext(t *testing.T, args []string, jsonMode bool) (*Context, *bytes.Buffer) {
	t.Helper()
	c, err := parse(newAuthCommand(), args)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	c.UI = &ui.UI{Out: &out, Err: &out, Lang: ui.LangEN, JSON: jsonMode}
	return c, &out
}

func TestAuthReportsStoredCredentialWithoutNetwork(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	meta := cfg.KeyMetaOf("work")
	meta.Host = "https://router.example"
	meta.Models = []string{"gpt-5.5"}
	meta.Protocols = map[string][]string{config.GroupScope: {"openai_responses"}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	creds.Set("work", &config.Credential{Key: "sk-secret-value", Source: config.SourcePaste})
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}

	c, out := authTestContext(t, nil, false)
	if err := runAuth(c); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "sk-sec…alue") || !strings.Contains(text, "router.example") || strings.Contains(text, "sk-secret-value") {
		t.Fatalf("auth output = %q", text)
	}
}

func TestAuthEnvironmentKeyWinsUnlessExplicitKey(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("TF_API_KEY", "sk-environment-value")

	c, out := authTestContext(t, nil, true)
	if err := runAuth(c); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data := env["data"].(map[string]any)
	if data["source"] != config.SourceEnv || data["environment_override"] != true {
		t.Fatalf("environment auth = %v", data)
	}
}

func TestAuthDoesNotGuessBetweenStoredKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	creds, _, err := config.LoadCredentials(paths)
	if err != nil {
		t.Fatal(err)
	}
	creds.Set("a", &config.Credential{Key: "sk-a", Source: config.SourcePaste})
	creds.Set("b", &config.Credential{Key: "sk-b", Source: config.SourcePaste})
	if err := creds.Save(); err != nil {
		t.Fatal(err)
	}

	c, _ := authTestContext(t, nil, true)
	if err := runAuth(c); ui.AsError(err).Code != ui.CodeUsage {
		t.Fatalf("multiple keys error = %v, want usage", err)
	}
}

func TestAgentReadmeJSONIsMachineReadable(t *testing.T) {
	c, out := func() (*Context, *bytes.Buffer) {
		parsed, err := parse(newAgentReadmeCommand(), []string{"--json"})
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		parsed.UI = &ui.UI{Out: &buf, Err: &buf, Lang: ui.LangEN, JSON: true}
		return parsed, &buf
	}()
	if err := newAgentReadmeCommand().Run(c); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env["ok"] != true || env["command"] != "agent-readme" {
		t.Fatalf("agent-readme envelope = %v", env)
	}
	data := env["data"].(map[string]any)
	if !strings.Contains(data["text"].(string), "--no-tui") {
		t.Fatal("agent-readme does not document --no-tui")
	}
}
