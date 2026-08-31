package gateway

import "testing"

// 判断额度用尽要看 remaining，不要看 status 字符串。
//
// status 的取值集合没有文档（实测见过 quota_exhausted），而
// remaining <= 0 的含义不会变。判据挑不会变的那个。
func TestUsageExhausted(t *testing.T) {
	var u Usage
	u.Quota.Limit, u.Quota.Remaining = 10, 0
	if !u.Exhausted() {
		t.Error("remaining 0 of 10 must count as exhausted")
	}

	u.Quota.Remaining = 2.5
	if u.Exhausted() {
		t.Error("remaining 2.5 must not count as exhausted")
	}

	// 没有配额上限的 Key 不该被判成用尽。
	u.Quota.Limit, u.Quota.Remaining = 0, 0
	if u.Exhausted() {
		t.Error("no quota limit must not read as exhausted")
	}
}
