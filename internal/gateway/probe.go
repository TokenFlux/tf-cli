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

// probeTargets 是各协议的探测入口。
var probeTargets = map[Protocol]string{
	ProtoAnthropicMessages: "/v1/messages",
	ProtoOpenAIResponses:   "/v1/responses",
	ProtoOpenAIChat:        "/v1/chat/completions",
}

// ProbeProtocols 探测这把 Key 的分组允许哪些协议。
//
// 零 token 成本：协议准入发生在读取请求体之前，所以发一个空 body 就够了 ——
// 400（缺字段）说明准入通过，403（does not allow）说明不通过。
//
// 只能证伪：账号级能力可能更窄，通过不代表一定跑得起来。
func (c *Client) ProbeProtocols(ctx context.Context) ([]Protocol, error) {
	type result struct {
		proto Protocol
		ok    bool
	}

	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		out []result
	)
	for proto, path := range probeTargets {
		wg.Add(1)
		go func(proto Protocol, path string) {
			defer wg.Done()
			ok := c.probeOne(ctx, path)
			mu.Lock()
			out = append(out, result{proto, ok})
			mu.Unlock()
		}(proto, path)
	}
	wg.Wait()

	// 顺序固定，与网关返回集合的排列一致，便于比对与展示。
	order := []Protocol{ProtoAnthropicMessages, ProtoOpenAIResponses, ProtoOpenAIChat}
	var allowed []Protocol
	for _, want := range order {
		for _, r := range out {
			if r.proto == want && r.ok {
				allowed = append(allowed, want)
			}
		}
	}
	return allowed, nil
}

// probeOne 报告某个协议入口是否准入。
func (c *Client) probeOne(ctx context.Context, path string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Host+path, strings.NewReader("{}"))
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

	// 403 = 协议不准入。其余（含 400 缺字段）都说明请求已越过准入这一关。
	// 401 也算不通过，但那是 Key 的问题，调用方在此之前已经校验过。
	return resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusUnauthorized
}
