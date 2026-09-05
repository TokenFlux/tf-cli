package cli

import (
	"testing"

	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/ui"
)

func TestLoginGatewayChoices(t *testing.T) {
	t.Setenv("TF_LANG", "en")
	u := ui.New(false)
	for _, tc := range []struct {
		name, current string
		defaultIndex  int
	}{
		{"new", config.DefaultHost, 0},
		{"normalized-default", config.DefaultHost + "/v1/", 0},
		{"existing-custom", "https://router.example.test", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, index := loginGatewayItems(u, tc.current)
			if len(items) != 2 || index != tc.defaultIndex {
				t.Fatalf("items=%v defaultIndex=%d", items, index)
			}
			if items[index].Detail != config.DefaultHost {
				t.Fatal("default gateway does not follow build configuration")
			}
			if index == 1 && items[0].Detail != tc.current {
				t.Fatal("existing gateway should be the first choice")
			}
		})
	}
}
