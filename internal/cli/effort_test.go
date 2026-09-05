package cli

import (
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/harness"
	"github.com/tokenflux/tf-cli/internal/ui"
)

func TestEffortPreservesGroupPrefix(t *testing.T) {
	cfg, _ := fixture(t, map[string][]string{"combo": nil})
	cfg.KeyMetaOf("combo").Models = []string{"GPT/gemini-pro-low", "GPT/gemini-pro-high", "Other/gemini-pro-xhigh"}
	c := testCtx()
	c.Flags.set["effort"] = "high"
	slots := config.ModelSlots{"default": "GPT/gemini-pro-low"}
	h, _ := harness.Lookup("codex")
	effort, err := applyEffort(c, cfg, h, slots, "combo")
	if err != nil || effort != "" || slots["default"] != "GPT/gemini-pro-high" {
		t.Fatalf("%s %v %v", effort, slots, err)
	}
	c.Flags.set["effort"] = "xhigh"
	_, err = applyEffort(c, cfg, h, slots, "combo")
	if err == nil {
		t.Fatal("matched a variant from another group")
	}
	if hint := ui.AsError(err).Hint; hint != "low | high" {
		t.Fatalf("cross-group hint: %q", hint)
	}
}
