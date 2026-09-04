// Package model 收口模型 ID 的解析与变换。
//
// 模型 ID 在 tf 里要经历多重变换（分组前缀、思考强度后缀、harness 自己的
// provider 前缀），必须集中在一处，否则一定写乱。
package model

import "strings"

// 思考强度档位，由弱到强。
//
// 这些不是 tf 发明的：TokenFlux 的部分分组直接把强度编进模型 ID
// （gemini-3.1-pro-high / -low），另一些分组则靠 harness 自己的旋钮。
var efforts = []string{"minimal", "none", "low", "medium", "high", "xhigh"}

// tiered 是「由服务端自行决定」的特殊档，不参与强弱排序。
const effortTiered = "tiered"

func isEffort(s string) bool {
	if s == effortTiered {
		return true
	}
	for _, e := range efforts {
		if e == s {
			return true
		}
	}
	return false
}

// Ref 是拆开后的模型 ID。
type Ref struct {
	Prefix string // 复合 Key 的分组前缀；普通 Key 为空
	Base   string // 去掉前缀与强度后缀的基名
	Effort string // 空表示该 ID 未编码强度
}

// Parse 拆出分组前缀、基名与强度后缀。
//
// 前缀只按**第一个**斜杠拆 —— 模型 ID 自身可能含斜杠
// （GPT/vendor/model 的前缀是 GPT，模型是 vendor/model）。
//
// 强度只认最后一段且必须是已知档位，避免把 `gpt-5.6-sol` 这种
// 名字里本来就有连字符的模型误拆。
func Parse(id string) Ref {
	var r Ref
	if i := strings.Index(id, "/"); i > 0 {
		r.Prefix, id = id[:i], id[i+1:]
	}
	if i := strings.LastIndex(id, "-"); i > 0 {
		if suffix := id[i+1:]; isEffort(suffix) {
			r.Base, r.Effort = id[:i], suffix
			return r
		}
	}
	r.Base = id
	return r
}

// String 还原成完整模型 ID（含前缀）。
func (r Ref) String() string {
	if r.Prefix == "" {
		return r.Display()
	}
	return r.Prefix + "/" + r.Display()
}

// Display 返回不含分组前缀的模型名，用于展示。
func (r Ref) Display() string {
	if r.Effort == "" {
		return r.Base
	}
	return r.Base + "-" + r.Effort
}

// Prefixes 按出现顺序列出所有分组前缀。
func Prefixes(ids []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range ids {
		p := Parse(id).Prefix
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// Efforts 返回模型列表实际包含的强度，保持旧选择器的顺序：
// 每个族按强弱排序，族按首次出现顺序合并。
func Efforts(ids []string) []string {
	families := map[string][]string{}
	order := []string{}
	for _, id := range ids {
		r := Parse(id)
		key := r.Prefix + "\x00" + r.Base
		if _, ok := families[key]; !ok {
			order = append(order, key)
		}
		if r.Effort != "" {
			families[key] = append(families[key], r.Effort)
		}
	}

	seen := map[string]bool{}
	out := []string{}
	for _, key := range order {
		values := families[key]
		for i := 1; i < len(values); i++ {
			for j := i; j > 0 && effortRank(values[j]) < effortRank(values[j-1]); j-- {
				values[j], values[j-1] = values[j-1], values[j]
			}
		}
		for _, effort := range values {
			if !seen[effort] {
				seen[effort] = true
				out = append(out, effort)
			}
		}
	}
	return out
}

func effortRank(effort string) int {
	for i, known := range efforts {
		if known == effort {
			return i
		}
	}
	return len(efforts)
}

// 档位关键字，用于把模型按用途归位。
var (
	fastHints  = []string{"haiku", "flash", "mini", "nano", "small", "lite", "fast"}
	heavyHints = []string{"opus", "ultra", "max", "pro", "heavy"}
)

// GuessTier 猜测模型属于快速档还是重型档，返回 "fast" / "heavy" / ""。
//
// 仅用于首次运行时的预填：让 claude 的三个档位落到真正的
// haiku / sonnet / opus 上，而不是三个槽塞同一个模型 —— 后者会让
// Claude Code 的 /model 切换变成空操作。
//
// 按词元而非子串匹配：“gemini” 里含有 “mini”，子串匹配会把
// gemini-3.1-pro 归成快速档。
func GuessTier(id string) string {
	tokens := strings.FieldsFunc(strings.ToLower(Parse(id).Base), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
	for _, tok := range tokens {
		for _, h := range fastHints {
			if tok == h {
				return "fast"
			}
		}
		for _, h := range heavyHints {
			if tok == h {
				return "heavy"
			}
		}
	}
	return ""
}
