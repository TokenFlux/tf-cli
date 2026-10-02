package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tokenflux/tf-cli/internal/harness"
	"github.com/tokenflux/tf-cli/internal/ui"
)

func TestUnexpectedInstallHelper(t *testing.T) {
	marker := os.Getenv("TF_TEST_INSTALL_MARKER")
	if marker == "" {
		return
	}
	_ = os.WriteFile(marker, []byte("ran"), 0600)
	os.Exit(23)
}

func TestHarnessInstallNeverRunsWithoutInteraction(t *testing.T) {
	for _, flag := range []string{"--no-input", "--json"} {
		t.Run(flag, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "installed")
			t.Setenv("TF_TEST_INSTALL_MARKER", marker)
			h := &harness.Harness{Name: "fixture", Bin: "tf-missing-install-fixture", Installs: []harness.InstallOption{{Args: []string{exe, "-test.run=^TestUnexpectedInstallHelper$"}}}}
			c, err := parse(newHarnessCommand(), []string{"install", "fixture", flag})
			if err != nil {
				t.Fatal(err)
			}
			c.UI = ui.New(flag == "--json")
			err = EnsureInstalled(c, h)
			if err == nil || ui.AsError(err).Code != ui.CodeHarnessNotInstalled {
				t.Fatalf("wrong error: %v", err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("installer ran without confirmation")
			}
		})
	}
}
