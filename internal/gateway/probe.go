package gateway

import (
	"context"
	"net/http"
	"strings"
	"sync"

	"github.com/tokenflux/tkr/internal/buildinfo"
)

// Protocol 是网关的客户端文本协议，取值与 TokenRouter 的
// allowed_client_protocols 一致。
type Protocol string

const (
	ProtoAnthropicMessages Protocol = "anthropic_messages"
	ProtoOpenAIResponses   Protocol = "openai_responses"
	ProtoOpenAIChat        Protocol = "openai_chat_completions"
)

// AllProtocols 的顺序与网关返回集合一致，便于比对。
var AllProtocols = []Protocol{ProtoAnthropicMessages, ProtoOpenAIResponses, ProtoOpenAIChat}

var probeTargets = map[Protocol]string{
	ProtoAnthropicMessages: "/v1/messages",
	ProtoOpenAIResponses:   "/v1/responses",
	ProtoOpenAIChat:        "/v1/chat/completions",
}

// probeModel 是一个必然不存在的模型名。
//
// 探测只关心「是否越过协议准入这一关」，模型不存在正是我们要的：
// 请求会在调度前被拒，不产生任何 token 消耗。
const probeModel = "__tf_probe__"

// Admission 是一个分组对 tf 的准入情况。
type Admission struct {
	// Protocols 是探测下来准入的协议。
	Protocols []Protocol
	// ClaudeCodeOnly 表示该分组只接受 Claude Code 客户端。
	//
	// 这种分组连 /v1/messages 都会拒绝 tf 的探测 —— 它拦的是客户端指纹，
	// 不是协议。tf 永远问不出它到底允许哪些协议，因为 tf 不是
	// Claude Code，而伪装成 Claude Code 是明确不做的事。
	ClaudeCodeOnly bool
}

// ProbeProtocols 探测各分组前缀的准入情况。
//
// prefixes 为空表示普通 Key（只绑一个分组），用空串作为唯一作用域键。
// 复合 Key 必须逐前缀探测：一把 Key 横跨多个分组，每个分组的准入各不相同。
//
// 零 token 成本：准入判定发生在调度与计费之前。
func (c *Client) ProbeProtocols(ctx context.Context, prefixes []string) (map[string]Admission, error) {
	if len(prefixes) == 0 {
		prefixes = []string{""}
	}

	type result struct {
		prefix  string
		proto   Protocol
		verdict verdict
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out []result
	)
	for _, prefix := range prefixes {
		for proto, path := range probeTargets {
			wg.Add(1)
			go func(prefix string, proto Protocol, path string) {
				defer wg.Done()
				v := c.probeOne(ctx, path, prefix)
				mu.Lock()
				out = append(out, result{prefix, proto, v})
				mu.Unlock()
			}(prefix, proto, path)
		}
	}
	wg.Wait()

	admissions := map[string]Admission{}
	for _, prefix := range prefixes {
		var a Admission
		for _, want := range AllProtocols {
			for _, r := range out {
				if r.prefix != prefix || r.proto != want {
					continue
				}
				switch r.verdict {
				case verdictAllowed:
					a.Protocols = append(a.Protocols, want)
				case verdictClaudeCodeOnly:
					a.ClaudeCodeOnly = true
				}
			}
		}
		// 只接受 Claude Code 的分组，协议集合问不出来，也不该当成空集 ——
		// 空集会被读成「什么都不支持」，而它其实支持 Claude Code 的全部功能。
		if a.ClaudeCodeOnly {
			a.Protocols = nil
		}
		if len(a.Protocols) > 0 || a.ClaudeCodeOnly {
			admissions[prefix] = a
		}
	}
	return admissions, nil
}

type verdict int

const (
	// verdictUnknown 表示这次探测没有结论（网络故障等），不该写进配置。
	verdictUnknown verdict = iota
	verdictAllowed
	verdictDenied
	verdictClaudeCodeOnly
)

// probeOne 判断某个协议入口对某个分组前缀的准入情况。
//
// 判据必须按 (状态码, 文案) 一起看，实测的四种形状：
//
//	403 "...does not support the requested model \"X\"..."  → 准入通过，只是模型不存在
//	404 model_not_found                                     → 准入通过
//	403 "...only allows Claude Code clients..."             → 分组锁定 Claude Code
//	403 其它                                                 → 协议不准入
//
// 默认按「拒绝」处理：这一层的作用是把跑不通的组合挡在启动之前，
// 误判为通过会让用户在 harness 里撞一堵没有解释的墙。
func (c *Client) probeOne(ctx context.Context, path, prefix string) verdict {
	model := probeModel
	if prefix != "" {
		model = prefix + "/" + probeModel
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+path,
		strings.NewReader(`{"model":"`+model+`"}`))
	if err != nil {
		return verdictUnknown
	}
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)
	// Anthropic 入口另外认这两个头。
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return verdictUnknown
	}
	defer resp.Body.Close()

	return verdictOf(resp.StatusCode, readBodySnippet(resp))
}

// verdictOf 由 (状态码, 文案) 断出准入结论。
//
// 拆成纯函数是为了能用实测应答做固件测试：这段判断是整个项目里最微妙的
// 一处，而它此前只有活体探针测过 —— 要联网、要额度，额度用尽时还会
// 连测试本身一起失败。
//
// 判据以文案为主、状态码为辅：同一件事实测出现过 403 / 404 / 503 三种码。
//
// 分不清时一律 verdictUnknown，绝不猜「通过」。猜通过的代价是用户在
// harness 里撞一堵没有解释的墙；猜不通的代价只是少一个候选，
// 而调用方对未知的处理是「保留上一次的结论」，不会误删已知信息。
func verdictOf(status int, body string) verdict {
	switch {
	case status == http.StatusUnauthorized:
		return verdictUnknown // Key 的问题，与协议无关
	case isClaudeCodeOnly(body):
		return verdictClaudeCodeOnly
	case isModelMiss(body), status == http.StatusNotFound:
		return verdictAllowed
	case status == http.StatusForbidden:
		return verdictDenied

	// 400 是零成本探测的正例：探针只发 {"model":"X"}，没有 messages，
	// 能走到参数校验就说明协议与分组都放行了。
	case status == http.StatusBadRequest:
		return verdictAllowed
	}

	// 其余一律未知。429 是必须落在这里的那个：额度检查发生在准入检查
	// 之前，配额用尽时每个入口都返回 429 API_KEY_QUOTA_EXHAUSTED。
	// 这里原本兜底返回「通过」，于是额度一空，所有分组的所有协议都被
	// 记成可用 —— 包括 claude_code_only 的分组。
	return verdictUnknown
}

// isClaudeCodeOnly 识别「本分组只接受 Claude Code 客户端」。
//
// 同一个含义在不同入口有不同措辞，实测见过两种：
//
//	"this group only allows Claude Code clients"
//	"This group is restricted to Claude Code clients (/v1/messages only)"
//
// 所以只匹配共有的那部分。
func isClaudeCodeOnly(body string) bool {
	return strings.Contains(strings.ToLower(body), "claude code client")
}

// isModelMiss 识别「协议通过了，只是这个模型不在分组里」。
//
// 这句话是可以精确刻画的（它会点名模型），而拒绝的措辞五花八门，
// 所以把例外放在这一侧，默认按拒绝处理。
func isModelMiss(body string) bool {
	low := strings.ToLower(body)
	return strings.Contains(low, "requested model") ||
		strings.Contains(low, "model_not_found") ||
		strings.Contains(low, "is not supported by any configured account")
}
