package gateway

import (
	"context"
	"os"
	"testing"
)

// 需要真实网关，默认跳过：TKR_LIVE_KEY=<key> go test ./internal/gateway/ -run Live -v
func TestLiveProbeProtocols(t *testing.T) {
	key := os.Getenv("TKR_LIVE_KEY")
	if key == "" {
		t.Skip("set TKR_LIVE_KEY to run")
	}
	got, err := New("https://tokenflux.dev", key).ProbeProtocols(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("allowed protocols: %v", got)
	if len(got) == 0 {
		t.Error("expected at least one protocol")
	}
}
