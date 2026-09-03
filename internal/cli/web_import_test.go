package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tokenflux/tkr/internal/ui"
)

const testWebOrigin = "https://tokenflux.dev"

func webRequest(method, path, origin, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func TestWebImportDecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		accepted bool
		err      error
		want     webImportDecision
	}{
		{name: "accepted", accepted: true, want: webImportDecision{Accepted: true}},
		{name: "rejected", want: webImportDecision{Error: "rejected"}},
		{name: "cancelled", accepted: true, err: io.ErrUnexpectedEOF, want: webImportDecision{Error: "cancelled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := makeWebImportDecision(tc.accepted, tc.err); got != tc.want {
				t.Fatalf("decision = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestWebImportPingAndPrivateNetworkPreflight(t *testing.T) {
	events := make(chan webImportEvent)
	h := &webImportHandler{
		host: testWebOrigin, origin: testWebOrigin, port: webImportPortFirst,
		sessionSecret: bytes.Repeat([]byte{0x42}, webImportSessionBytes), events: events,
	}

	ping := httptest.NewRecorder()
	h.ServeHTTP(ping, webRequest(http.MethodGet, "/ping", testWebOrigin, ""))
	if ping.Code != http.StatusOK {
		t.Fatalf("ping status = %d, body = %s", ping.Code, ping.Body.String())
	}
	if got := ping.Header().Get("Access-Control-Allow-Origin"); got != testWebOrigin {
		t.Errorf("allow origin = %q", got)
	}
	var data map[string]any
	if err := json.Unmarshal(ping.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["service"] != "tf" || data["protocol"] != float64(webImportProtocol) {
		t.Errorf("unexpected ping: %v", data)
	}
	if _, ok := data["proof"]; ok {
		t.Error("ordinary discovery ping must not expose a session proof")
	}

	preflight := httptest.NewRecorder()
	r := webRequest(http.MethodOptions, "/import", testWebOrigin, "")
	r.Header.Set("Access-Control-Request-Private-Network", "true")
	h.ServeHTTP(preflight, r)
	if preflight.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d", preflight.Code)
	}
	if got := preflight.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Errorf("allow private network = %q", got)
	}
	if got := preflight.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, webImportProofHeader) {
		t.Errorf("allow headers = %q", got)
	}
}

func TestWebImportSessionURLUsesFragment(t *testing.T) {
	secret := bytes.Repeat([]byte{0x42}, webImportSessionBytes)
	got := webImportSessionURL("https://router.example/base/", webImportPortFirst, secret)
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/base/keys" || parsed.RawQuery != "" {
		t.Fatalf("session URL path/query = %q %q", parsed.Path, parsed.RawQuery)
	}
	if parsed.Fragment != "tf=1.43110.QkJCQkJCQkJCQkJCQkJCQg" {
		t.Errorf("session fragment = %q", parsed.Fragment)
	}
}

func TestWebImportProofBindsPortAndChallenge(t *testing.T) {
	secret := make([]byte, webImportSessionBytes)
	for i := range secret {
		secret[i] = byte(i)
	}
	const challenge = "abcdefghijklmnop"
	const want = "OANoOu0fwo146i4Jsp5LVCCSuWp4wwh66qb8Z4NHoyA"
	if got := webImportProof(secret, webImportPortFirst, challenge); got != want {
		t.Fatalf("proof = %q, want %q", got, want)
	}
	if got := webImportProof(secret, webImportPortFirst+1, challenge); got == want {
		t.Error("proof did not bind the listener port")
	}
	if got := webImportProof(secret, webImportPortFirst, challenge+"x"); got == want {
		t.Error("proof did not bind the challenge")
	}
}

func TestWebImportPingCanProveSession(t *testing.T) {
	secret := bytes.Repeat([]byte{0x42}, webImportSessionBytes)
	h := &webImportHandler{
		host: testWebOrigin, origin: testWebOrigin, port: webImportPortFirst,
		sessionSecret: secret, events: make(chan webImportEvent),
	}
	const challenge = "abcdefghijklmnop"
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, webRequest(http.MethodGet, "/ping?challenge="+challenge, testWebOrigin, ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("proof ping status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var data map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if data["proof"] != webImportProof(secret, webImportPortFirst, challenge) {
		t.Errorf("proof ping = %v", data)
	}

	bad := httptest.NewRecorder()
	h.ServeHTTP(bad, webRequest(http.MethodGet, "/ping?challenge=short", testWebOrigin, ""))
	if bad.Code != http.StatusBadRequest || !strings.Contains(bad.Body.String(), "invalid_challenge") {
		t.Errorf("invalid challenge = %d %s", bad.Code, bad.Body.String())
	}
}

func TestWebImportRejectsOtherOrigins(t *testing.T) {
	events := make(chan webImportEvent)
	h := &webImportHandler{host: testWebOrigin, origin: testWebOrigin, events: events}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, webRequest(http.MethodGet, "/ping", "https://evil.example", ""))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("rejected origin must not receive CORS permission, got %q", got)
	}
}

func TestWebImportDeliversConfirmedRequestWithoutEchoingKey(t *testing.T) {
	events := make(chan webImportEvent)
	secret := bytes.Repeat([]byte{0x42}, webImportSessionBytes)
	h := &webImportHandler{
		host: testWebOrigin, origin: testWebOrigin, port: webImportPortFirst,
		sessionSecret: secret, events: events,
	}
	body := `{"version":1,"key":"sk-secret-value","host":"https://tokenflux.dev/","key_name":"laptop","group_id":7,"group_name":"GPT"}`
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		r := webRequest(http.MethodPost, "/import", testWebOrigin, body)
		r.Header.Set(webImportProofHeader, webImportBodyProof(secret, webImportPortFirst, []byte(body)))
		h.ServeHTTP(rr, r)
		close(done)
	}()

	select {
	case event := <-events:
		if event.Request.Key != "sk-secret-value" || event.Request.Host != testWebOrigin {
			t.Errorf("unexpected request: %+v", event.Request)
		}
		if event.Request.Origin != testWebOrigin || event.Request.GroupID != 7 {
			t.Errorf("metadata missing: %+v", event.Request)
		}
		if !event.Request.Verified {
			t.Error("valid import proof was not carried to terminal confirmation")
		}
		event.Reply <- webImportDecision{Accepted: true}
	case <-time.After(time.Second):
		t.Fatal("handler did not deliver the request")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish")
	}
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "sk-secret") {
		t.Error("HTTP response echoed the key")
	}
}

func TestWebImportValidatesRequestBeforeDelivery(t *testing.T) {
	valid := `{"version":1,"key":"sk-test","host":"https://tokenflux.dev"}`
	tests := []struct {
		name        string
		origin      string
		contentType string
		body        string
		status      int
		errorCode   string
	}{
		{"wrong protocol", testWebOrigin, "application/json", strings.Replace(valid, `"version":1`, `"version":2`, 1), 400, "unsupported_protocol"},
		{"host mismatch", testWebOrigin, "application/json", strings.Replace(valid, testWebOrigin, "https://other.example", 1), 400, "host_mismatch"},
		{"whitespace in key", testWebOrigin, "application/json", strings.Replace(valid, "sk-test", "sk bad", 1), 400, "invalid_key"},
		{"control key", testWebOrigin, "application/json", strings.TrimSuffix(valid, "}") + `,"key":"bad\u001b"}`, 400, "invalid_key"},
		{"unicode key", testWebOrigin, "application/json", strings.Replace(valid, "sk-test", "sk-密钥", 1), 400, "invalid_key"},
		{"control metadata", testWebOrigin, "application/json", strings.TrimSuffix(valid, "}") + `,"group_name":"bad\u001b"}`, 400, "invalid_metadata"},
		{"bidi metadata", testWebOrigin, "application/json", strings.TrimSuffix(valid, "}") + `,"group_name":"bad\u202e"}`, 400, "invalid_metadata"},
		{"unknown field", testWebOrigin, "application/json", strings.TrimSuffix(valid, "}") + `,"extra":true}`, 400, "invalid_json"},
		{"wrong content type", testWebOrigin, "text/plain", valid, 415, "content_type"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan webImportEvent)
			h := &webImportHandler{host: testWebOrigin, origin: testWebOrigin, events: events}
			r := webRequest(http.MethodPost, "/import", tc.origin, tc.body)
			r.Header.Set("Content-Type", tc.contentType)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, r)
			if rr.Code != tc.status || !strings.Contains(rr.Body.String(), tc.errorCode) {
				t.Errorf("status/body = %d %s", rr.Code, rr.Body.String())
			}
			select {
			case <-events:
				t.Error("invalid request was delivered")
			default:
			}
		})
	}
}

func TestWebImportReturnsRejectedDecision(t *testing.T) {
	events := make(chan webImportEvent)
	h := &webImportHandler{host: testWebOrigin, origin: testWebOrigin, events: events}
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, webRequest(http.MethodPost, "/import", testWebOrigin,
			`{"version":1,"key":"sk-rejected","host":"https://tokenflux.dev"}`))
		close(done)
	}()

	var event webImportEvent
	select {
	case event = <-events:
		if event.Request.Verified {
			t.Error("request without a session proof must remain unverified")
		}
	case <-time.After(time.Second):
		t.Fatal("handler did not deliver the rejected request")
	}
	event.Reply <- webImportDecision{Error: "rejected"}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not return the rejection")
	}
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "rejected") {
		t.Errorf("status/body = %d %s", rr.Code, rr.Body.String())
	}
}

func TestWebImportRejectsConcurrentRequest(t *testing.T) {
	h := &webImportHandler{host: testWebOrigin, origin: testWebOrigin, events: make(chan webImportEvent)}
	h.busy.Store(true)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, webRequest(http.MethodPost, "/import", testWebOrigin,
		`{"version":1,"key":"sk-test","host":"https://tokenflux.dev"}`))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "busy") {
		t.Errorf("status/body = %d %s", rr.Code, rr.Body.String())
	}
}

func TestWebOrigin(t *testing.T) {
	for _, tc := range []struct {
		host string
		want string
		ok   bool
	}{
		{"https://tokenflux.dev", "https://tokenflux.dev", true},
		{"HTTPS://TOKENFLUX.DEV:443/base", "https://tokenflux.dev", true},
		{"https://router.example/base", "https://router.example", true},
		{"http://127.0.0.1:80", "http://127.0.0.1", true},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080", true},
		{"http://[::1]:43110", "http://[::1]:43110", true},
		{"file:///tmp/key", "", false},
		{"https://user@example.com", "", false},
	} {
		got, err := webOrigin(tc.host)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("webOrigin(%q) = %q, %v", tc.host, got, err)
		}
	}
}

func TestWebImportAllPortsOccupied(t *testing.T) {
	var listeners []net.Listener
	for port := webImportPortFirst; port <= webImportPortLast; port++ {
		listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			listeners = append(listeners, listener)
		}
	}
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()

	listener, _, err := listenWebImport()
	if listener != nil {
		_ = listener.Close()
	}
	if err == nil {
		t.Fatal("listenWebImport succeeded while every port was occupied")
	}
}

func TestWebImportBodyLimit(t *testing.T) {
	events := make(chan webImportEvent)
	h := &webImportHandler{host: testWebOrigin, origin: testWebOrigin, events: events}
	body := `{"version":1,"key":"` + strings.Repeat("x", webImportMaxBody) + `","host":"https://tokenflux.dev"}`
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, webRequest(http.MethodPost, "/import", testWebOrigin, body))
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "invalid_json") {
		t.Errorf("status/body = %d %s", rr.Code, rr.Body.String())
	}
}

func TestWebImportRequiresInteractiveConfirmation(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cmd := newLoginCommand()
	ctx, err := parse(cmd, []string{"--from-web", "--no-input"})
	if err != nil {
		t.Fatal(err)
	}
	ctx.UI = &ui.UI{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, Lang: ui.LangZH, JSON: true}
	err = runLogin(ctx)
	got := ui.AsError(err)
	if got.Code != ui.CodeUsage || !strings.Contains(got.Message, "交互式终端") {
		t.Fatalf("error = %#v", got)
	}
}
