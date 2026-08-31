// Package access 判断「哪把 Key 的哪个分组能跑哪个 harness，走哪种协议」。
//
// 从 internal/cli 拆出来的理由：这是整个产品最核心的判断，却混在一个
// 既要读配置、又要打印、又要弹选择器的包里，覆盖率长期上不去。
//
// 这里全是纯函数 —— 给定配置与 harness 就能算出答案，不碰终端、
// 不发请求、不写盘。
package access

import (
	"strings"

	"github.com/tokenflux/tkr/internal/config"
	"github.com/tokenflux/tkr/internal/harness"
	"github.com/tokenflux/tkr/internal/model"
)

// CanRun 报告某个分组能否跑这个 harness。
//
// claude_code_only 分组拦的是客户端指纹而不是协议：只有 Claude Code
// 本身过得去，其它 harness 无论用什么协议都会被拒。
func CanRun(meta *config.KeyMeta, prefix string, h *harness.Harness) bool {
	_, ok := Pick(meta, prefix, h)
	return ok
}

// Pick 选出这次该走哪种协议。
//
// harness 会的协议按偏好排序，取第一个该分组也允许的。
// 多数 harness 不止会一种：opencode 两个 provider 都内置，
// 因此它在只开 anthropic_messages 的分组上照样能跑。
func Pick(meta *config.KeyMeta, prefix string, h *harness.Harness) (harness.Protocol, bool) {
	return PickFor(meta, prefix, "", h)
}

// PickFor 在知道具体模型时按模型的原生协议优先。
//
// 网关两种协议都能翻译（实测 /v1/responses 打 claude-opus-4-6 确实能用），
// 但翻译只会丢信息不会补信息 —— 思考块、缓存标记、工具调用的细节都要
// 过一道映射。而且 opencode 会照实显示 provider：用 openai 跑 claude 模型
// 界面上写着「OpenAI」，看起来像配错了。
//
// 猜错也无害：网关照样翻译。所以这只是偏好排序，不是准入判断。
func PickFor(meta *config.KeyMeta, prefix, modelID string, h *harness.Harness) (harness.Protocol, bool) {
	if meta.LockedToClaudeCode(prefix) {
		if h.IsClaudeCode {
			return harness.ProtoAnthropicMessages, true
		}
		return "", false
	}
	if native := Native(modelID); native != "" &&
		h.Speaks(string(native)) && meta.SupportsIn(prefix, string(native)) {
		return native, true
	}
	for _, p := range h.Protocols {
		if meta.SupportsIn(prefix, string(p)) {
			return p, true
		}
	}
	return "", false
}

// Native 报告该模型「原生」说哪种协议。认不出来就返回空。
//
// 只按模型名判断，不去猜分组的 platform：认错的代价仅仅是多一道翻译，
// 而为此在客户端建一套推断才是真的得不偿失。
func Native(modelID string) harness.Protocol {
	if modelID == "" {
		return ""
	}
	if strings.HasPrefix(model.Parse(modelID).Base, "claude-") {
		return harness.ProtoAnthropicMessages
	}
	return ""
}

// ProtocolList 列出该 harness 会的协议，用于解释为什么没有 Key 合格。
func ProtocolList(h *harness.Harness) string {
	out := make([]string, 0, len(h.Protocols))
	for _, p := range h.Protocols {
		out = append(out, string(p))
	}
	return strings.Join(out, " / ")
}

// Fitting 返回至少有一个分组能跑该 harness 的 Key。未探测过的视为可用。
func Fitting(cfg *config.Config, names []string, h *harness.Harness) []string {
	var out []string
	for _, n := range names {
		meta := cfg.Keys[n]
		if !meta.Probed() {
			out = append(out, n)
			continue
		}
		for _, prefix := range meta.Scopes() {
			if CanRun(meta, prefix, h) {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// Runnable 列出这把 Key 能跑的 harness。
func Runnable(cfg *config.Config, name string) []string {
	return RunnableIn(cfg.Keys[name], config.GroupScope)
}

// RunnableIn 列出某个分组能跑的 harness。
func RunnableIn(meta *config.KeyMeta, prefix string) []string {
	var out []string
	for _, h := range harness.All {
		if CanRun(meta, prefix, h) {
			out = append(out, h.Name)
		}
	}
	return out
}
