package ui

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// 前缀必须跟着 locale 走，否则中文正文顶着英文标签。
// 分隔符写进译文：中文全角冒号自带间距，再补空格会散开。
func TestPrefixesFollowLocale(t *testing.T) {
	for _, tc := range []struct {
		lang       Lang
		warn, fail string
	}{
		{LangZH, "警告：正文\n", "错误：正文\n"},
		{LangEN, "warning: body\n", "error: body\n"},
	} {
		var buf bytes.Buffer
		u := &UI{Err: &buf, Out: &buf, Lang: tc.lang}

		body := "body"
		if tc.lang == LangZH {
			body = "正文"
		}
		u.Warnf("%s", body)
		if got := buf.String(); got != tc.warn {
			t.Errorf("%v Warnf = %q, want %q", tc.lang, got, tc.warn)
		}

		buf.Reset()
		u.Fail("x", Errf(CodeUsage, body))
		if got := buf.String(); !strings.HasPrefix(got, tc.fail) {
			t.Errorf("%v Fail = %q, want prefix %q", tc.lang, got, tc.fail)
		}
	}
}

// JSON 模式下的警告不能凭空消失：给机器看的输出反而比给人看的少，
// 「凭据权限已收紧」「模型列表沿用旧的」这类信息就无从得知。
func TestJSONEnvelopeCarriesWarnings(t *testing.T) {
	var out bytes.Buffer
	u := &UI{Out: &out, Err: io.Discard, Lang: LangEN, JSON: true}

	u.Warnf("could not persist the binding: %v", "disk full")
	u.Logf("hiding 2 models from work")
	u.Emit("keys", map[string]string{"key": "work"}, nil)

	var env struct {
		OK       bool     `json:"ok"`
		Warnings []string `json:"warnings"`
		Notes    []string `json:"notes"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("not valid json: %v\n%s", err, out.String())
	}
	if len(env.Warnings) != 1 || env.Warnings[0] != "could not persist the binding: disk full" {
		t.Errorf("warnings = %#v", env.Warnings)
	}
	if len(env.Notes) != 1 || env.Notes[0] != "hiding 2 models from work" {
		t.Errorf("notes = %#v", env.Notes)
	}
}

// 命令没发信封时，攒下的警告也要有出口，否则照样是丢。
func TestFlushEmitsOrphanWarnings(t *testing.T) {
	var out bytes.Buffer
	u := &UI{Out: &out, Err: io.Discard, Lang: LangEN, JSON: true}

	u.Warnf("credentials were 0644, tightened to 0600")
	u.Flush("claude")
	if !strings.Contains(out.String(), "tightened to 0600") {
		t.Errorf("flush lost the warning: %s", out.String())
	}

	// 已经发过信封就不再补发第二份，否则输出不是单个 JSON 文档。
	before := out.Len()
	u.Flush("claude")
	if out.Len() != before {
		t.Error("flush emitted a second envelope")
	}
}
