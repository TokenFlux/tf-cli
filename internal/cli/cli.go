// Package cli 实现命令注册与参数解析。
//
// 解析规则刻意手写而非使用 cobra —— 透传语义（见 docs/PLAN.md B 项）
// 与通用 flag 库的假设冲突，手写反而更短也更可控：
//
//  1. tf 只认自己的一小组 flag，且必须紧跟在子命令之后。
//  2. 对透传型命令，遇到第一个不认识的参数即停止解析，其后全部原样交给 harness。
//  3. "--" 之后无条件全部透传，用于传递与 tf 同名的 flag。
package cli

import (
	"fmt"
	"strings"

	"github.com/tokenflux/tf-cli/internal/ui"
)

// Kind 是 flag 的取值形态。
type Kind int

const (
	KindBool Kind = iota
	KindString
	// KindOptString 的值可省略：`-m` 不带值表示「进选择器」，
	// `-m gpt-5.4` 表示直接指定。
	KindOptString
)

// Flag 是一个命令行选项。
type Flag struct {
	Name  string
	Short string
	Kind  Kind
	Desc  string
	Def   string
	// Aliases 是兼容用的旧名字，不在帮助里列出。
	Aliases []string
}

// names 列出这个 flag 在命令行上的所有写法。
func (f Flag) names() []string {
	out := []string{f.Name}
	if f.Short != "" {
		out = append(out, f.Short)
	}
	return append(out, f.Aliases...)
}

// Values 保存解析结果。
type Values struct {
	set     map[string]string
	present map[string]bool

	// detached 记下哪些可选值 flag 的取值是分开写的（-m X 而非 -m=X）。
	//
	// 分开写时无法在解析期断定那个词是取值还是位置参数：
	// tf codex -m exec "hi" 里的 exec 是 codex 的子命令，
	// tf claude -m "解释这段代码" 里那句是 prompt。
	// 留个标记，等拿到候选集再判。
	detached map[string]bool
}

func newValues() *Values {
	return &Values{set: map[string]string{}, present: map[string]bool{}, detached: map[string]bool{}}
}

// Detached 报告这个 flag 的取值是否与 flag 分开写。
func (v *Values) Detached(name string) bool { return v.detached[name] }

// Detach 把分开写的取值退回去：清掉取值，返回那个词。
//
// 用于取值其实不是取值的时候 —— 那个词该还给位置参数。
func (v *Values) Detach(name string) string {
	val := v.set[name]
	v.set[name] = ""
	delete(v.detached, name)
	return val
}

// String 返回字符串值。
func (v *Values) String(name string) string { return v.set[name] }

// Bool 返回布尔值。
func (v *Values) Bool(name string) bool { return v.set[name] == "true" }

// Present 报告该 flag 是否出现过。
// 用于区分 `-m`（出现但无值）与完全没写 `-m`。
func (v *Values) Present(name string) bool { return v.present[name] }

// Set 让交互流程能补上没在命令行给出的值。
//
// 例如 login 里问出来的自建网关地址：问到之后写回 host，
// 后面的代码就不必区分「命令行给的」还是「问出来的」。
func (v *Values) Set(name, value string) {
	v.set[name] = value
	v.present[name] = true
}

// Context 传给命令的执行上下文。
type Context struct {
	UI      *ui.UI
	Flags   *Values
	Args    []string // 非透传命令的位置参数
	Passthr []string // 透传给 harness 的原始参数
	Command string
}

// Command 是一个子命令。
type Command struct {
	Name        string
	Aliases     []string
	Summary     func(u *ui.UI) string
	Usage       string
	Flags       []Flag
	Passthrough bool
	Hidden      bool
	Run         func(*Context) error
}

// globalFlags 在每个命令上都可用。
func globalFlags() []Flag {
	return []Flag{
		{Name: "help", Short: "h", Kind: KindBool, Desc: "显示帮助||Show help"},
		{Name: "json", Kind: KindBool, Desc: "以 JSON 输出||Emit JSON output"},
		{Name: "key", Short: "k", Kind: KindString, Desc: "本次使用哪把 Key||Which stored key to use for this run"},
		{Name: "host", Kind: KindString, Desc: "覆盖网关地址||Override the gateway host"},
		// 实现一直是「不提问」，不是「替你答 yes」：需要回答才能继续的
		// 地方全部报错。名字随实现改，--yes / -y 作为旧名保留。
		// -y 不再是它的简写：业界 -y 一律是「替我答 yes」，而这里的语义
		// 恰好相反 —— 需要回答就失败。留着这个简写等于给脚本作者埋一个
		// 语义翻转的坑。--yes 作为旧全名保留，因为它已经写进过文档。
		{Name: "no-input", Aliases: []string{"yes"}, Kind: KindBool,
			Desc: "不提问，需要输入时直接失败||Never prompt; fail instead of asking"},
	}
}

// parse 按上述规则解析一个命令的参数。
func parse(cmd *Command, args []string) (*Context, error) {
	flags := append(globalFlags(), cmd.Flags...)
	byName := map[string]*Flag{}
	for i := range flags {
		f := &flags[i]
		for _, n := range f.names() {
			byName[n] = f
		}
	}

	vals := newValues()
	for _, f := range flags {
		if f.Def != "" {
			vals.set[f.Name] = f.Def
		}
	}

	ctx := &Context{Flags: vals, Command: cmd.Name}

	i := 0
	for i < len(args) {
		a := args[i]

		// 规则 3：显式分隔符之后全部透传。
		if a == "--" {
			ctx.Passthr = append(ctx.Passthr, args[i+1:]...)
			return ctx, nil
		}

		if !strings.HasPrefix(a, "-") || a == "-" {
			// 位置参数。透传型命令从这里开始交给 harness。
			if cmd.Passthrough {
				ctx.Passthr = append(ctx.Passthr, args[i:]...)
				return ctx, nil
			}
			ctx.Args = append(ctx.Args, a)
			i++
			continue
		}

		name, inline, hasInline := splitFlag(a)
		f, known := byName[name]
		if !known {
			// 规则 2：不认识就整段透传。
			if cmd.Passthrough {
				ctx.Passthr = append(ctx.Passthr, args[i:]...)
				return ctx, nil
			}
			return nil, ui.Errf(ui.CodeUnknownFlag,
				fmt.Sprintf("unknown flag: %s", a)).
				WithHint(fmt.Sprintf("tf %s --help", cmd.Name))
		}

		vals.present[f.Name] = true

		switch f.Kind {
		case KindBool:
			if hasInline {
				vals.set[f.Name] = inline
			} else {
				vals.set[f.Name] = "true"
			}
			i++

		case KindString:
			if hasInline {
				vals.set[f.Name] = inline
				i++
				continue
			}
			if i+1 >= len(args) {
				return nil, ui.Errf(ui.CodeMissingValue,
					fmt.Sprintf("flag needs a value: %s", a))
			}
			vals.set[f.Name] = args[i+1]
			i += 2

		case KindOptString:
			if hasInline {
				vals.set[f.Name] = inline
				i++
				continue
			}
			// 后面紧跟另一个 flag 或已到末尾 → 视为「出现但无值」。
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				vals.set[f.Name] = ""
				i++
				continue
			}
			vals.set[f.Name] = args[i+1]
			vals.detached[f.Name] = true
			i += 2
		}
	}

	return ctx, nil
}

// splitFlag 拆出 --name=value 形式。
func splitFlag(a string) (name, value string, hasValue bool) {
	trimmed := strings.TrimLeft(a, "-")
	if eq := strings.IndexByte(trimmed, '='); eq >= 0 {
		return trimmed[:eq], trimmed[eq+1:], true
	}
	return trimmed, "", false
}
