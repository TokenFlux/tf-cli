// Package cli 实现命令注册与参数解析。
//
// 解析规则刻意手写而非使用 cobra —— 透传语义（见 docs/PLAN.md B 项）
// 与通用 flag 库的假设冲突，手写反而更短也更可控：
//
//  1. tkr 只认自己的一小组 flag，且必须紧跟在子命令之后。
//  2. 对透传型命令，遇到第一个不认识的参数即停止解析，其后全部原样交给 harness。
//  3. "--" 之后无条件全部透传，用于传递与 tkr 同名的 flag。
package cli

import (
	"fmt"
	"strings"

	"github.com/tokenflux/tkr/internal/ui"
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
}

// Values 保存解析结果。
type Values struct {
	set     map[string]string
	present map[string]bool
}

func newValues() *Values {
	return &Values{set: map[string]string{}, present: map[string]bool{}}
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
		{Name: "yes", Short: "y", Kind: KindBool, Desc: "非交互，全部接受默认||Non-interactive, accept defaults"},
	}
}

// parse 按上述规则解析一个命令的参数。
func parse(cmd *Command, args []string) (*Context, error) {
	flags := append(globalFlags(), cmd.Flags...)
	byName := map[string]*Flag{}
	for i := range flags {
		f := &flags[i]
		byName[f.Name] = f
		if f.Short != "" {
			byName[f.Short] = f
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
				WithHint(fmt.Sprintf("tkr %s --help", cmd.Name))
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
