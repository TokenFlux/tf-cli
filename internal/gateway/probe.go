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
// 探测只关心「是否越过协议准入这一关」，所以模型不存在正是我们要的：
// 请求会在调度前就被拒，不产生任何 token 消耗。
const probeModel = "__tkr_probe__"

// ProbeProtocols 探测各分组前缀允许哪些协议。
//
// prefixes 为空表示普通 Key（只绑一个分组），用空串作为唯一作用域键。
// 复合 Key 必须逐前缀探测：一把 Key 横跨多个分组，每个分组的准入集合
// 各不相同，而分组是由模型 ID 的前缀决定的。
//
// 零 token 成本：协议准入发生在调度与计费之前。
func (c *Client) ProbeProtocols(ctx context.Context, prefixes []string) (map[string][]Protocol, error) {
	if len(prefixes) == 0 {
		prefixes = []string{""}
	}

	type job struct {
		prefix string
		proto  Protocol
		ok     bool
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out []job
	)
	for _, prefix := range prefixes {
		for proto, path := range probeTargets {
			wg.Add(1)
			go func(prefix string, proto Protocol, path string) {
				defer wg.Done()
				ok := c.probeOne(ctx, path, prefix)
				mu.Lock()
				out = append(out, job{prefix, proto, ok})
				mu.Unlock()
			}(prefix, proto, path)
		}
	}
	wg.Wait()

	result := map[string][]Protocol{}
	for _, prefix := range prefixes {
		for _, want := range AllProtocols {
			for _, j := range out {
				if j.prefix == prefix && j.proto == want && j.ok {
					result[prefix] = append(result[prefix], want)
				}
			}
		}
	}
	return result, nil
}

// probeOne 报告某个协议入口对某个分组前缀是否准入。
func (c *Client) probeOne(ctx context.Context, path, prefix string) bool {
	model := probeModel
	if prefix != "" {
		model = prefix + "/" + probeModel
	}
	body := `{"model":"` + model + `"}`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+path, strings.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)
	// Anthropic 入口另外认这两个头。
	req.Header.Set("x-api-key", c.Key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// 403 = 协议不准入。模型不存在会返回 403/404 但文案不同，
	// 所以不能只看状态码，必须看是不是「协议」这一层拒的。
	if resp.StatusCode == http.StatusUnauthorized {
		return false
	}
	if resp.StatusCode != http.StatusForbidden {
		return true
	}
	return !isProtocolDenial(readBodySnippet(resp))
}

// isProtocolDenial 区分「协议不准入」与「模型不在分组」。
//
// 两者都可能是 403，但语义完全不同：前者说明这个 harness 根本不能用
// 这个分组，后者只是模型选错了。
func isProtocolDenial(body string) bool {
	low := strings.ToLower(body)
	return strings.Contains(low, "does not allow")
}
