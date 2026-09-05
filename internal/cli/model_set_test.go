package cli

import (
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
)

func TestMultipleSlotAssignmentsAreSavedTogether(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	c, err := parse(newModelCommand(), []string{"codex", "--set", "default=a", "--set=review=b"})
	if err != nil {
		t.Fatal(err)
	}
	c.UI = testCtx().UI
	if err := runModel(c); err != nil {
		t.Fatal(err)
	}
	paths, _ := config.DefaultPaths()
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	slots := cfg.Harness("codex").Slots
	if slots["default"] != "a" || slots["review"] != "b" {
		t.Fatalf("%v", slots)
	}
}

func TestInvalidSlotBatchDoesNotPartiallySave(t *testing.T) {
	for _, invalid := range []string{"bogus=b", "review=", "default=b"} {
		t.Run(invalid, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			c, err := parse(newModelCommand(), []string{"codex", "--set", "default=a", "--set", invalid})
			if err != nil {
				t.Fatal(err)
			}
			c.UI = testCtx().UI
			if err := runModel(c); err == nil {
				t.Fatal("invalid batch accepted")
			}
			paths, _ := config.DefaultPaths()
			cfg, err := config.Load(paths)
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Harness("codex").Slots) != 0 {
				t.Fatal("batch was partially saved")
			}
		})
	}
}
