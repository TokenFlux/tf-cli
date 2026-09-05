package cli

import (
	"strings"
	"testing"
)

func TestProxyDiagnosticRedactsSecrets(t *testing.T) {
	for _, raw := range []string{
		"http://alice:private-password@proxy.example:8080/path?token=private-token#private-fragment",
		"http://alice:bad%password@proxy.example",
		"alice:private-password@proxy.example",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("HTTPS_PROXY", raw)
			out := strings.Join(checkEnvironment(testCtx()), "\n")
			for _, secret := range []string{"alice", "private", "bad%password"} {
				if strings.Contains(out, secret) {
					t.Fatalf("diagnostic exposed %q: %s", secret, out)
				}
			}
		})
	}
	if got := proxyAddress("socks5://alice:secret@127.0.0.1:1080"); got != "socks5://127.0.0.1:1080" {
		t.Fatal(got)
	}
}
