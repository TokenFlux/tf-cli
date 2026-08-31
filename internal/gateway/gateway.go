// Package gateway 是 TokenFlux / TokenRouter 的 HTTP 客户端。
//
// 只用于 tf 自身的查询（模型目录、Key 校验）。harness 的流量绝不经过这里
// —— tf 不代理、不 MITM。
package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tokenflux/tkr/internal/buildinfo"
)

// Client 是一个网关客户端。
type Client struct {
	Host string
	Key  string
	HTTP *http.Client
}

// New 构造客户端。超时刻意短：这些调用都在用户等待启动的路径上。
func New(host, key string) *Client {
	return &Client{
		Host: strings.TrimRight(host, "/"),
		Key:  key,
		HTTP: &http.Client{Timeout: 15 * time.Second},
	}
}

// Model 是 /v1/models 返回的一项。
type Model struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// Models 返回当前 Key 可见的模型列表。
//
// 这是唯一一个用 API Key 就能读到的目录接口：分组能力、用量、Key 详情
// 都需要用户 JWT。见 docs/research/tokenflux-api-probe.md。
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	var out struct {
		Data []Model `json:"data"`
	}
	if err := c.getJSON(ctx, "/v1/models", &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// Usage 是 /v1/usage 的应答里 tf 用得上的部分。
//
// 只取会影响「下一次请求能不能成」的字段。接口还返回按模型、按日的
// 明细和账单细节 —— 那些属于网页控制台，塞进命令行只会挤掉重点。
type Usage struct {
	Status string `json:"status"` // quota_exhausted 等
	Quota  struct {
		Limit     float64 `json:"limit"`
		Used      float64 `json:"used"`
		Remaining float64 `json:"remaining"`
		Unit      string  `json:"unit"`
	} `json:"quota"`
	Billing struct {
		Available bool    `json:"available"`
		PlanName  string  `json:"plan_name"`
		Remaining float64 `json:"remaining"`
		Unit      string  `json:"unit"`
	} `json:"billing"`
	Usage struct {
		Today struct {
			Requests    int     `json:"requests"`
			TotalTokens int     `json:"total_tokens"`
			Cost        float64 `json:"cost"`
		} `json:"today"`
	} `json:"usage"`
}

// Exhausted 报告配额是否已经用尽。
//
// 判据用 remaining 而不是 status 字符串：status 的取值集合没有文档，
// 而 remaining <= 0 的含义不会变。
func (u Usage) Exhausted() bool { return u.Quota.Limit > 0 && u.Quota.Remaining <= 0 }

// Usage 取这把 Key 的用量与剩余额度。
func (c *Client) Usage(ctx context.Context) (*Usage, error) {
	var out Usage
	if err := c.getJSON(ctx, "/v1/usage", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Host+path, nil)
	if err != nil {
		return err
	}
	// 只有 tf 自己的请求带这个 UA。注入给 harness 的环境绝不改 UA。
	req.Header.Set("User-Agent", buildinfo.UserAgent())
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return classify(resp.StatusCode, body)
	}
	return json.Unmarshal(body, out)
}

// APIError 是网关返回的结构化错误。
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("http %d", e.Status)
}

// InvalidKey 报告是否是「Key 不可用」。
//
// 网关用两个不同的码区分凭据类型：API Key 用 INVALID_API_KEY，
// 用户 JWT 用 INVALID_TOKEN。实测见 research/tokenflux-api-probe.md。
func (e *APIError) InvalidKey() bool {
	return e.Status == http.StatusUnauthorized
}

// classify 把网关错误归一化。
//
// 注意不能只看状态码：同一语义在不同协议入口下形状不同
// （chat 用 403 带可用模型列表，responses 用 404）。
func classify(status int, body []byte) error {
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)

	e := &APIError{Status: status, Code: envelope.Code, Message: envelope.Message}
	if e.Message == "" {
		e.Message = envelope.Error.Message
	}
	if e.Code == "" {
		if s, ok := envelope.Error.Code.(string); ok {
			e.Code = s
		} else if envelope.Error.Type != "" {
			e.Code = envelope.Error.Type
		}
	}
	if e.Message == "" {
		e.Message = strings.TrimSpace(string(body))
	}
	return e
}

// readBodySnippet 读一小段响应体用于错误分类。
func readBodySnippet(resp *http.Response) string {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	return string(b)
}
