package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/harness"
)

func modelServer(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]map[string]string, len(ids))
		for i, id := range ids {
			data[i] = map[string]string{"id": id}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(s.Close)
	return s
}

func TestSwitchModelDiscardsForeignAuxiliarySlots(t *testing.T) {
	cfg, creds := fixture(t, map[string][]string{"a": {"openai_responses"}, "b": {"openai_responses"}})
	cfg.Harness("codex").Key = "a"
	cfg.Harness("codex").Slots = config.ModelSlots{"default": "a-main", "review": "a-review"}
	cfg.KeyMetaOf("a").Host = modelServer(t, "a-main", "a-review").URL
	cfg.KeyMetaOf("b").Host = modelServer(t, "b-main").URL
	c := testCtx()
	c.Flags.set["model"] = "b-main"
	h, _ := harness.Lookup("codex")
	key, slots, err := resolveTarget(c, cfg, creds, h)
	if err != nil || key != "b" || slots["review"] != "b-main" {
		t.Fatalf("%s %v %v", key, slots, err)
	}
}

func TestSwitchKeyCannotUseOldMainModel(t *testing.T) {
	cfg, creds := fixture(t, map[string][]string{"a": {"openai_responses"}, "b": {"openai_responses"}})
	cfg.Harness("codex").Key = "a"
	cfg.Harness("codex").Slots = config.ModelSlots{"default": "a-main", "review": "a-review"}
	cfg.KeyMetaOf("b").Host = modelServer(t, "b-main").URL
	c := testCtx()
	c.Flags.set["key"] = "b"
	h, _ := harness.Lookup("codex")
	if _, _, err := resolveTarget(c, cfg, creds, h); err == nil {
		t.Fatal("used foreign model instead of requiring a new selection")
	}
}

func TestModelOverridePrefersExistingOwner(t *testing.T) {
	cfg, creds := fixture(t, map[string][]string{"a": {"openai_responses"}, "b": {"openai_responses"}})
	cfg.Harness("codex").Key = "b"
	cfg.Harness("codex").Slots = config.ModelSlots{"default": "shared"}
	for _, key := range []string{"a", "b"} {
		cfg.KeyMetaOf(key).Host = modelServer(t, "shared").URL
	}
	c := testCtx()
	c.Flags.set["model"] = "shared"
	h, _ := harness.Lookup("codex")
	key, _, err := resolveTarget(c, cfg, creds, h)
	if err != nil || key != "b" {
		t.Fatalf("key=%s err=%v", key, err)
	}
}

func TestTemporaryKeyDoesNotPersistBindingOrFilledSlots(t *testing.T) {
	cfg, creds := fixture(t, map[string][]string{"a": {"openai_responses"}, "b": {"openai_responses"}})
	cfg.Harness("opencode").Key = "a"
	cfg.Harness("opencode").Slots = config.ModelSlots{"default": "shared"}
	cfg.KeyMetaOf("b").Host = modelServer(t, "shared").URL
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	c := testCtx()
	c.Flags.set["key"] = "b"
	h, _ := harness.Lookup("opencode")
	key, slots, err := resolveTarget(c, cfg, creds, h)
	if err != nil || key != "b" || slots["small"] != "shared" {
		t.Fatalf("%s %v %v", key, slots, err)
	}
	if hc := cfg.Harness("opencode"); hc.Key != "a" || hc.Slots["small"] != "" {
		t.Fatalf("temporary launch changed saved preferences: %+v", hc)
	}
}

func TestAuxiliarySlotsMustSpeakMainProtocol(t *testing.T) {
	h, _ := harness.Lookup("opencode")
	meta := &config.KeyMeta{Protocols: map[string][]string{"GPT": {"openai_responses"}, "Claude": {"anthropic_messages"}}}
	ids := compatibleSlotModels(meta, h, "GPT/gpt-main", []string{"GPT/gpt-main", "Claude/claude-mini"})
	if len(ids) != 1 || ids[0] != "GPT/gpt-main" {
		t.Fatalf("%v", ids)
	}
}
