package cli

import (
	"github.com/tokenflux/tkr/internal/config"
	"reflect"
	"strings"
	"testing"
)

// 透传规则是 tkr 最容易写错的地方（见 docs/PLAN.md B 项），
// 这里逐条锁定行为。
func TestParsePassthrough(t *testing.T) {
	harness := &Command{
		Name:        "claude",
		Passthrough: true,
		Flags: []Flag{
			{Name: "model", Short: "m", Kind: KindOptString},
			{Name: "reasoning-effort", Kind: KindString},
		},
	}

	tests := []struct {
		name        string
		args        []string
		wantModel   string
		wantPresent bool
		wantPassthr []string
	}{
		{
			name:        "tf 的 flag 紧跟子命令时被吃掉",
			args:        []string{"-m", "gpt-5.4"},
			wantModel:   "gpt-5.4",
			wantPresent: true,
		},
		{
			name:        "第一个陌生 flag 起全部透传",
			args:        []string{"-m", "gpt-5.4", "--resume", "abc", "-p"},
			wantModel:   "gpt-5.4",
			wantPresent: true,
			wantPassthr: []string{"--resume", "abc", "-p"},
		},
		{
			name:        "位置参数同样触发透传",
			args:        []string{"写个测试"},
			wantPassthr: []string{"写个测试"},
		},
		{
			name:        "-- 之后无条件透传，即使与 tkr 同名",
			args:        []string{"--", "-m", "claude-opus-4"},
			wantPassthr: []string{"-m", "claude-opus-4"},
		},
		{
			name:        "陌生 flag 之后的同名 flag 不再被 tkr 解析",
			args:        []string{"--resume", "-m", "opus"},
			wantModel:   "",
			wantPresent: false,
			wantPassthr: []string{"--resume", "-m", "opus"},
		},
		{
			name:        "-m 不带值表示进选择器：出现但为空",
			args:        []string{"-m"},
			wantModel:   "",
			wantPresent: true,
		},
		{
			name:        "-m 后面跟 flag 时也视为无值",
			args:        []string{"-m", "--json"},
			wantModel:   "",
			wantPresent: true,
		},
		{
			name:        "--name=value 形式",
			args:        []string{"--model=gpt-5.5"},
			wantModel:   "gpt-5.5",
			wantPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, err := parse(harness, tt.args)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := ctx.Flags.String("model"); got != tt.wantModel {
				t.Errorf("model = %q, want %q", got, tt.wantModel)
			}
			if got := ctx.Flags.Present("model"); got != tt.wantPresent {
				t.Errorf("model present = %v, want %v", got, tt.wantPresent)
			}
			if !reflect.DeepEqual(ctx.Passthr, tt.wantPassthr) {
				t.Errorf("passthrough = %#v, want %#v", ctx.Passthr, tt.wantPassthr)
			}
		})
	}
}

// 非透传命令遇到陌生 flag 必须报错，而不是悄悄忽略。
func TestParseNonPassthroughRejectsUnknownFlag(t *testing.T) {
	cmd := &Command{Name: "models"}

	if _, err := parse(cmd, []string{"--nope"}); err == nil {
		t.Fatal("expected an error for an unknown flag")
	}

	ctx, err := parse(cmd, []string{"gpt-5.4"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(ctx.Args, []string{"gpt-5.4"}) {
		t.Errorf("args = %#v, want [gpt-5.4]", ctx.Args)
	}
}

// 缺值的字符串 flag 要给出明确错误。
func TestParseMissingValue(t *testing.T) {
	cmd := &Command{Name: "login", Flags: []Flag{{Name: "with-key", Kind: KindString}}}
	if _, err := parse(cmd, []string{"--with-key"}); err == nil {
		t.Fatal("expected an error for a missing flag value")
	}
}

// 全局 flag 在任何命令上都可用。
func TestGlobalFlagsAvailableEverywhere(t *testing.T) {
	cmd := &Command{Name: "models"}
	ctx, err := parse(cmd, []string{"--json", "--key", "work"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !ctx.Flags.Bool("json") {
		t.Error("--json not parsed")
	}
	if got := ctx.Flags.String("key"); got != "work" {
		t.Errorf("key = %q, want work", got)
	}
}

// 建议的名字取自分组前缀或模型名首词元，并要避开已占用的名字。
func TestSuggestKeyName(t *testing.T) {
	cases := []struct {
		ids   []string
		taken []string
		want  string
	}{
		{[]string{"gpt-5.4", "gpt-5.5", "gpt-5.6-sol", "codex-auto-review"}, nil, "gpt"},
		{[]string{"claude-opus-5", "claude-sonnet-5"}, nil, "claude"},
		{[]string{"gemini-3.1-pro-high"}, nil, "gemini"},
		{[]string{"gpt-5.4"}, []string{"gpt"}, "gpt-2"},
		{[]string{"gpt-5.4"}, []string{"gpt", "gpt-2"}, "gpt-3"},
		{nil, nil, "key"},
		// 复合 Key：分组前缀本身就是最好的名字。
		// 横跨两个分组时两个都要出现 —— 这里原本断言的是「模型多的那个赢」，
		// 那会把一把 gpt+ccmax 的 Key 叫成 ccmax，用户会当成纯 Claude Max。
		{[]string{"GPT/gpt-5.4", "GPT/gpt-5.5", "Claude/claude-opus-5"}, nil, "claude+gpt"},
	}
	for _, c := range cases {
		if got := suggestKeyName(c.ids, c.taken); got != c.want {
			t.Errorf("suggestKeyName(%v, taken=%v) = %q, want %q", c.ids, c.taken, got, c.want)
		}
	}
}

// 任何位置都不能返回空候选。
//
// 空结果在 zsh 里不等于「什么都不补」：菜单补全会沿用上一次的候选，
// 于是 tf codex <TAB> 会把 codex 再插一遍 —— 用户看到的是补出了个
// tf codex codex。没得补时也得给出 tf 自己的 flag。
func TestCompletionNeverReturnsNothing(t *testing.T) {
	for _, words := range [][]string{
		{""},                    // tf <TAB>
		{"codex", ""},           // 启动命令的参数位
		{"claude", ""},          //
		{"keys", ""},            //
		{"config", ""},          // 不带参数的命令
		{"version", ""},         //
		{"login", ""},           //
		{"model", ""},           //
		{"harness", "list", ""}, //
	} {
		if got := complete(words); len(got) == 0 {
			t.Errorf("complete(%q) returned nothing", words)
		}
	}

	// 唯一该返回空的地方：越过透传边界之后，那些参数属于 harness。
	if got := complete([]string{"codex", "exec", ""}); len(got) != 0 {
		t.Errorf("past the passthrough boundary should yield nothing, got %v", got)
	}
}

// 候选里不能有重复项：--key 在启动命令和全局 flag 里各有一份，
// 出现两次会让人以为是两个不同的东西。
func TestCompletionHasNoDuplicates(t *testing.T) {
	got := complete([]string{"codex", ""})
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c] {
			t.Errorf("duplicate candidate %q in %v", c, got)
		}
		seen[c] = true
	}
}

// zsh 脚本里的数组展开必须加引号并用 (@)。
//
// 不加的话 zsh 会丢掉末尾的空词：用户按下 tf codex <TAB> 时，
// __complete 收到的是「codex」而不是「codex ""」，于是以为对方还在敲
// 命令名，把 codex 又补了一遍 —— 屏幕上出现 tf codex codex。
//
// bash 那份用的是 "${COMP_WORDS[@]:...}"，本来就带引号，所以只有 zsh 中招。
func TestZshScriptQuotesWordArray(t *testing.T) {
	zsh := completionScripts["zsh"]
	if !strings.Contains(zsh, `"${(@)words[2,$CURRENT]}"`) {
		t.Error("zsh script must quote the word array with (@), or the trailing empty word is dropped")
	}
	if strings.Contains(zsh, `__complete ${words[`) {
		t.Error("unquoted array expansion found; zsh drops empty elements")
	}
	if !strings.Contains(completionScripts["bash"], `"${COMP_WORDS[@]:1:COMP_CWORD}"`) {
		t.Error("bash script must quote COMP_WORDS too")
	}
}

// 问过一次就不再问。
//
// 答过「不要」的人不该在每次 login 时被重新打扰，所以无论答什么
// 都要落盘记住。
func TestCompletionsAskedOnlyOnce(t *testing.T) {
	c := testCtx()
	cfg := &config.Config{CompletionsAsked: true}

	// 已问过：不该再走到选择器（testCtx 是非交互，走到就会挂）。
	offerCompletions(c, cfg)

	if !cfg.CompletionsAsked {
		t.Error("the flag must survive")
	}
}

// 非交互环境绝不弹选择器。
func TestCompletionsNotOfferedNonInteractive(t *testing.T) {
	cfg := &config.Config{}
	offerCompletions(testCtx(), cfg)
	if cfg.CompletionsAsked {
		t.Error("must not mark as asked when it never asked")
	}
}
