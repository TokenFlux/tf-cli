package ui

import (
	"bytes"
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
