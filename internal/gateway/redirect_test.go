package gateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestAuthenticatedRequestsRejectCrossOriginRedirects(t *testing.T) {
	var hits atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client := New(source.URL, "sk-private")
	if _, err := client.Models(context.Background()); err == nil {
		t.Fatal("models followed cross-origin redirect")
	}
	if got := client.probeOne(context.Background(), "/v1/messages", ""); got != verdictUnknown {
		t.Fatalf("probe verdict=%v", got)
	}
	if hits.Load() != 0 {
		t.Fatal("destination received an authenticated request")
	}
}

func TestRedirectOriginPolicy(t *testing.T) {
	original, _ := http.NewRequest("GET", "https://gateway.example/v1/models", nil)
	for _, tc := range []struct {
		url     string
		allowed bool
	}{
		{"https://gateway.example/other", true},
		{"https://GATEWAY.example:443/other", true},
		{"http://gateway.example/other", false},
		{"https://gateway.example:8443/other", false},
		{"https://sub.gateway.example/other", false},
		{"https://user:pass@gateway.example/other", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			req, _ := http.NewRequest("GET", tc.url, nil)
			if err := sameOriginRedirect(req, []*http.Request{original}); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
	if err := sameOriginRedirect(original, make([]*http.Request, 10)); err == nil {
		t.Fatal("redirect limit missing")
	}
}

func TestSameOriginRedirectRetainsAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			http.Redirect(w, r, "/models", http.StatusTemporaryRedirect)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-private" {
			t.Error("missing authentication")
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
	}))
	defer server.Close()
	ids, err := New(server.URL, "sk-private").Models(context.Background())
	if err != nil || len(ids) != 1 {
		t.Fatalf("%v %v", ids, err)
	}
}
