package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tokenflux/tf-cli/internal/gateway"
	"github.com/tokenflux/tf-cli/internal/ui"
)

func TestTodayDisplaysActualCharge(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		lang             ui.Lang
	}{
		{"provided response", `{"billing":{"remaining":93.3826824,"unit":"推理积分"},"usage":{"today":{"requests":117,"total_tokens":3076385,"cost":0.81240735,"actual_cost":6.4992588}}}`, "117 次请求，3076385 tokens，6.4993 推理积分", ui.LangZH},
		{"English", `{"unit":"credits","usage":{"today":{"requests":2,"total_tokens":100,"actual_cost":1.25}}}`, "2 requests, 100 tokens, 1.25 credits", ui.LangEN},
		{"quota unit", `{"quota":{"limit":10,"unit":"credits"},"usage":{"today":{"requests":2,"actual_cost":1}}}`, "2 requests, 0 tokens, 1 credits", ui.LangEN},
		{"unlimited plan", `{"billing":{"remaining":-1,"source":"subscription","unit":"credits"},"usage":{"today":{"requests":2,"actual_cost":0.01}}}`, "2 requests, 0 tokens, 0.01 credits", ui.LangEN},
		{"free requests", `{"unit":"credits","usage":{"today":{"requests":2,"cost":10,"actual_cost":0}}}`, "2 requests, 0 tokens, 0 credits", ui.LangEN},
		{"tiny charge", `{"unit":"credits","usage":{"today":{"requests":2,"actual_cost":0.000001}}}`, "2 requests, 0 tokens, <0.0001 credits", ui.LangEN},
		{"missing actual cost", `{"unit":"credits","usage":{"today":{"requests":2,"cost":10}}}`, "2 requests, 0 tokens\n", ui.LangEN},
		{"null actual cost", `{"unit":"credits","usage":{"today":{"requests":2,"cost":10,"actual_cost":null}}}`, "2 requests, 0 tokens\n", ui.LangEN},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var usage gateway.Usage
			if err := json.Unmarshal([]byte(tc.body), &usage); err != nil {
				t.Fatal(err)
			}
			c := testCtx()
			var out bytes.Buffer
			c.UI.Out, c.UI.Lang, c.UI.JSON = &out, tc.lang, false
			printUsage(c, &usage)
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, out.String())
			}
		})
	}
}

func TestUsageDisplayUsesAccountFundsWithoutKeyLimit(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		lang             ui.Lang
		exhausted        bool
	}{
		{"limited", `{"quota":{"limit":10,"remaining":2.5,"unit":"credits"},"billing":{"available":true,"remaining":62.6,"unit":"credits"}}`, "2.5/10 credits", ui.LangEN, false},
		{"limited exhausted", `{"quota":{"limit":10,"remaining":0,"unit":"credits"},"billing":{"available":true,"remaining":62.6}}`, "0/10 credits", ui.LangEN, true},
		{"subscription", `{"mode":"unrestricted","remaining":62.6,"unit":"credits","billing":{"source":"subscription","available":true,"remaining":62.6,"unit":"credits"}}`, "available  62.6 credits", ui.LangEN, false},
		{"balance", `{"mode":"unrestricted","billing":{"source":"balance","available":true,"remaining":120.5,"unit":"credits"}}`, "available  120.5 credits", ui.LangEN, false},
		{"rate limit only", `{"mode":"quota_limited","billing":{"available":true,"remaining":42,"unit":"credits"}}`, "available  42 credits", ui.LangEN, false},
		{"legacy", `{"mode":"unrestricted","remaining":33.5,"unit":"credits"}`, "available  33.5 credits", ui.LangEN, false},
		{"zero wins over fallback", `{"remaining":100,"billing":{"source":"balance","available":false,"remaining":0,"unit":"credits"}}`, "available  0 credits", ui.LangEN, true},
		{"unlimited subscription", `{"billing":{"source":"subscription","available":true,"remaining":-1,"unit":"credits"}}`, "available  unlimited", ui.LangEN, false},
		{"negative balance", `{"billing":{"source":"balance","available":false,"remaining":-1,"unit":"credits"}}`, "available  -1 credits", ui.LangEN, true},
		{"missing", `{}`, "available  unknown", ui.LangEN, false},
		{"null", `{"remaining":null,"billing":{"remaining":null}}`, "available  unknown", ui.LangEN, false},
		{"Chinese", `{"billing":{"available":true,"remaining":62.612395,"unit":"推理积分"},"usage":{"today":{"requests":3,"total_tokens":100}}}`, "可用额度  62.6 推理积分", ui.LangZH, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/usage" {
					t.Errorf("path=%s", r.URL.Path)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			u, err := gateway.New(srv.URL, "sk-test").Usage(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			c := testCtx()
			var out bytes.Buffer
			c.UI.Out, c.UI.Lang, c.UI.JSON = &out, tc.lang, false
			printUsage(c, u)
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, out.String())
			}
			if got := strings.Contains(out.String(), "requests will fail"); tc.lang == ui.LangEN && got != tc.exhausted {
				t.Fatalf("incorrect exhaustion warning:\n%s", out.String())
			}
			if tc.lang == ui.LangZH && !strings.Contains(out.String(), "3 次请求，100 tokens") {
				t.Fatal("today's usage disappeared")
			}
		})
	}
}
