package gateway

import "testing"

// 固件全部是实测逐字抄下来的，不是照着文档编的。
//
// 这段判断是整个项目里最微妙的一处：它决定哪把 Key 的哪些模型会出现在
// 哪个 harness 的候选里，判错了用户会在 harness 里撞一堵没有解释的墙。
// 而它此前只有活体探针测过 —— 要联网、要额度，额度用尽时会连测试一起失败。
//
// 每条都注明了实测来源与日期。网关改了文案时，改这里比改代码先发生。
func TestVerdictOf(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   verdict
	}{
		{
			// 2026-08-30 ccmax 分组，/v1/messages
			name:   "claude_code_only 的 anthropic 文案",
			status: 403,
			body:   `{"error":{"message":"this group only allows Claude Code clients","type":"permission_error"}}`,
			want:   verdictClaudeCodeOnly,
		},
		{
			// 2026-08-30 ccmax 分组，/v1/chat/completions
			name:   "claude_code_only 的 openai 文案",
			status: 403,
			body:   `{"error":{"message":"This group is restricted to Claude Code clients (/v1/messages only)","type":"permission_error"}}`,
			want:   verdictClaudeCodeOnly,
		},
		{
			// 2026-08-30 ccmax 分组，/v1/responses。同一件事第三种措辞。
			name:   "claude_code_only 的 responses 文案",
			status: 403,
			body:   `{"error":{"code":"permission_error","message":"This group is restricted to Claude Code clients (/v1/messages only)"}}`,
			want:   verdictClaudeCodeOnly,
		},
		{
			name:   "模型不在分组：403 带 requested model",
			status: 403,
			body:   `{"error":{"message":"The current group does not support the requested model \"x\""}}`,
			want:   verdictAllowed, // 协议通过了，只是模型不存在
		},
		{
			name:   "模型不在分组：404 model_not_found",
			status: 404,
			body:   `{"error":{"code":"model_not_found","message":"The model does not exist"}}`,
			want:   verdictAllowed,
		},
		{
			// Kiro 分组实测出的第三种状态码。判据放文案才接得住。
			name:   "模型不在分组：503 No available accounts",
			status: 503,
			body:   `{"error":{"message":"No available accounts: The current group does not support the requested model \"x\""}}`,
			want:   verdictAllowed,
		},
		{
			name:   "责在协议：403 且没有模型相关文案",
			status: 403,
			body:   `{"error":{"message":"This group does not allow anthropic_messages requests"}}`,
			want:   verdictDenied,
		},
		{
			// 探针只发 {"model":"X"}，没有 messages。能走到参数校验
			// 就说明协议与分组都放行了 —— 这是零成本探测的正例。
			name:   "准入通过，倒在参数校验上",
			status: 400,
			body:   `{"error":{"message":"messages: field required"}}`,
			want:   verdictAllowed,
		},
		{
			// 2026-08-31 逐字。额度检查发生在准入检查之前，
			// 配额一空，每个入口都是这个。
			//
			// 这里曾经兜底返回「通过」，于是额度用尽时所有分组的所有协议
			// 都被记成可用，包括 claude_code_only 的分组。会话开头那次
			// 「ccmax 存成三协议全通」就是这么来的。
			name:   "额度用尽：不能当成准入通过",
			status: 429,
			body:   `{"code":"API_KEY_QUOTA_EXHAUSTED","message":"API key 额度已用完"}`,
			want:   verdictUnknown,
		},
		{
			// 2026-08-31 逐字，/v1/responses 的同一件事，形状不同。
			name:   "额度用尽：responses 形状",
			status: 429,
			body:   `{"error":{"code":"insufficient_quota","message":"API key 额度已用完","param":null,"type":"insufficient_quota"}}`,
			want:   verdictUnknown,
		},
		{
			// 2026-08-31 逐字
			name:   "Key 无效：与协议无关",
			status: 401,
			body:   `{"code":"INVALID_API_KEY","message":"Invalid API key"}`,
			want:   verdictUnknown,
		},
		{
			name:   "网关自己出错",
			status: 502,
			body:   `bad gateway`,
			want:   verdictUnknown,
		},
		{
			// 没见过的形状不猜「通过」：猜通过的代价是用户撞墙，
			// 猜未知的代价只是保留上一次的结论。
			name:   "没见过的形状",
			status: 418,
			body:   `{"error":{"message":"teapot"}}`,
			want:   verdictUnknown,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := verdictOf(tc.status, tc.body); got != tc.want {
				t.Errorf("verdictOf(%d, %s) = %v, want %v", tc.status, tc.body, got, tc.want)
			}
		})
	}
}

// claude_code_only 的判据是文案里的「claude code client」，
// 三个入口三种措辞，大小写与单复数都不一样。
func TestIsClaudeCodeOnlyMatchesAllThreeWordings(t *testing.T) {
	for _, body := range []string{
		"this group only allows Claude Code clients",
		"This group is restricted to Claude Code clients (/v1/messages only)",
		"THIS GROUP ONLY ALLOWS CLAUDE CODE CLIENT",
	} {
		if !isClaudeCodeOnly(body) {
			t.Errorf("没认出：%s", body)
		}
	}

	// 不能宽到把无关文案也算进去。
	for _, body := range []string{
		"the model claude-opus-5 is not available",
		"client error",
		"",
	} {
		if isClaudeCodeOnly(body) {
			t.Errorf("误判为 claude_code_only：%s", body)
		}
	}
}

// 「模型不存在」的三种说法。
//
// 这三条对应三个不同的入口，任何一条漏掉都会让整个分组被误判为
// 协议不准入，那个分组的模型就整体消失了。
func TestIsModelMissCoversAllWordings(t *testing.T) {
	for _, body := range []string{
		`The current group does not support the requested model "x"`,
		`{"code":"model_not_found"}`,
		`No available accounts: ... is not supported by any configured account`,
	} {
		if !isModelMiss(body) {
			t.Errorf("没认出：%s", body)
		}
	}

	for _, body := range []string{
		"This group does not allow anthropic_messages requests",
		"API key 额度已用完",
		"",
	} {
		if isModelMiss(body) {
			t.Errorf("误判为模型缺失：%s", body)
		}
	}
}
