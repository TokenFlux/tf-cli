package cli

import (
	"reflect"
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
			name:        "tkr 的 flag 紧跟子命令时被吃掉",
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
	ctx, err := parse(cmd, []string{"--json", "--profile", "work"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !ctx.Flags.Bool("json") {
		t.Error("--json not parsed")
	}
	if got := ctx.Flags.String("profile"); got != "work" {
		t.Errorf("profile = %q, want work", got)
	}
}
