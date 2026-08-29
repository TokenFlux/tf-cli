// Package model 收口模型 ID 的解析与变换。
//
// 模型 ID 在 tkr 里要经历多重变换（分组前缀、思考强度后缀、harness 自己的
// provider 前缀），必须集中在一处，否则一定写乱。
package model

import (
	"sort"
	"strings"
)

// 思考强度档位，由弱到强。
//
// 这些不是 tkr 发明的：TokenFlux 的部分分组直接把强度编进模型 ID
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

// EffortRank 返回强度序号，用于排序；未知档位排在最后。
func EffortRank(e string) int {
	for i, v := range efforts {
		if v == e {
			return i
		}
	}
	return len(efforts)
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

// IsComposite 报告这份模型列表是否来自复合 Key。
//
// 复合 Key 的 /v1/models 会给每个 ID 加好分组前缀，所以出现斜杠
// 就是充分信号，不需要额外接口判断。
func IsComposite(ids []string) bool {
	for _, id := range ids {
		if strings.Contains(id, "/") {
			return true
		}
	}
	return false
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

// Family 是同一分组、同一基名下的一组强度变体。
//
// 分组必须参与分组键：同一个模型可能同时存在于多个分组，
// 且倒率差异巨大（claude-opus-5 在两个分组里相差 4 倍），
// 合并掉会让用户无法选便宜的那个。
type Family struct {
	Prefix  string
	Base    string
	Efforts []string          // 已按强度排序；无变体时为空
	byEfort map[string]string // 强度 → 完整模型 ID
	plain   string            // 无强度后缀的原始 ID
}

// ID 返回指定强度对应的完整模型 ID。effort 为空时返回默认 ID。
func (f Family) ID(effort string) (string, bool) {
	if effort == "" {
		if f.plain != "" {
			return f.plain, true
		}
		// 无裸 ID 时取中间档，避免默认落到最贵或最弱的一端。
		if len(f.Efforts) > 0 {
			return f.byEfort[f.Efforts[len(f.Efforts)/2]], true
		}
		return "", false
	}
	id, ok := f.byEfort[effort]
	return id, ok
}

// HasVariants 报告该族是否需要额外的强度选择。
func (f Family) HasVariants() bool { return len(f.Efforts) > 1 }

// Group 把模型列表折叠成族。
//
// 折叠的意义在选择器上很直接：Google 分组的 6 个模型其实只是 3 个模型
// 各带强度变体，摊平展示会让用户在重复基名里翻找。
func Group(ids []string) []Family {
	order := []string{}
	acc := map[string]*Family{}

	for _, id := range ids {
		r := Parse(id)
		key := r.Prefix + "\x00" + r.Base
		f, ok := acc[key]
		if !ok {
			f = &Family{Prefix: r.Prefix, Base: r.Base, byEfort: map[string]string{}}
			acc[key] = f
			order = append(order, key)
		}
		if r.Effort == "" {
			f.plain = id
			continue
		}
		f.byEfort[r.Effort] = id
		f.Efforts = append(f.Efforts, r.Effort)
	}

	out := make([]Family, 0, len(order))
	for _, key := range order {
		f := acc[key]
		sort.Slice(f.Efforts, func(i, j int) bool {
			return EffortRank(f.Efforts[i]) < EffortRank(f.Efforts[j])
		})
		out = append(out, *f)
	}
	return out
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
