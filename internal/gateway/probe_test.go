package gateway

import "testing"

// 网关的四种真实应答形状（全部实测自 tokenflux.dev）。
//
// 判据必须按 (状态码, 文案) 一起看：403 至少有三种互不相同的含义。
func TestProbeVerdicts(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool // isClaudeCodeOnly
		miss bool // isModelMiss
	}{
		{
			name: "claude_code_only 在 messages 入口",
			body: `{"error":{"message":"this group only allows Claude Code clients","type":"permission_error"}}`,
			want: true,
		},
		{
			name: "claude_code_only 在 responses 入口用了另一种措辞",
			body: `{"error":{"code":"permission_error","message":"This group is restricted to Claude Code clients (/v1/messages only)"}}`,
			want: true,
		},
		{
			name: "模型不在分组：协议其实是通的",
			body: `{"error":{"message":"The current group does not support the requested model \"__tkr_probe__\". Available models: gpt-5.4"}}`,
			miss: true,
		},
		{
			name: "responses 入口的模型缺失形状不同",
			body: `{"error":{"message":"Model \"__tkr_probe__\" is not supported by any configured account in this group","type":"model_not_found"}}`,
			miss: true,
		},
		{
			name: "协议不准入",
			body: `{"error":{"message":"This group does not allow openai_responses requests"}}`,
		},
	}
	for _, c := range cases {
		if got := isClaudeCodeOnly(c.body); got != c.want {
			t.Errorf("%s: isClaudeCodeOnly = %v, want %v", c.name, got, c.want)
		}
		if got := isModelMiss(c.body); got != c.miss {
			t.Errorf("%s: isModelMiss = %v, want %v", c.name, got, c.miss)
		}
	}
}

// 只接受 Claude Code 的分组不能被记成「空协议集」——
// 那会被读成「什么都不支持」，而它其实支持 Claude Code 的全部功能。
func TestClaudeCodeOnlyIsNotEmptyProtocols(t *testing.T) {
	a := Admission{ClaudeCodeOnly: true}
	if len(a.Protocols) != 0 {
		t.Error("protocols are unknowable for such a group")
	}
	if !a.ClaudeCodeOnly {
		t.Error("the lock itself must be recorded")
	}
}
