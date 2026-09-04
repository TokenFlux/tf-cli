package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/tokenflux/tf-cli/internal/completions"
	"github.com/tokenflux/tf-cli/internal/config"
	"github.com/tokenflux/tf-cli/internal/ui"
)

// 透传规则是 tf 最容易写错的地方（见 docs/PLAN.md B 项），
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
			name:        "-- 之后无条件透传，即使与 tf 同名",
			args:        []string{"--", "-m", "claude-opus-4"},
			wantPassthr: []string{"-m", "claude-opus-4"},
		},
		{
			name:        "harness 后的 help 属于底层工具",
			args:        []string{"--help"},
			wantPassthr: []string{"--help"},
		},
		{
			name:        "harness 后的短 help 属于底层工具",
			args:        []string{"-h"},
			wantPassthr: []string{"-h"},
		},
		{
			name:        "陌生 flag 之后的同名 flag 不再被 tf 解析",
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

func TestHarnessHelpBelongsToItsPosition(t *testing.T) {
	app := NewApp()
	var passed []string
	app.Register(&Command{
		Name: "tool", Passthrough: true, Usage: "tf tool [args...]",
		Summary: func(*ui.UI) string { return "tool summary" },
		Run: func(c *Context) error {
			passed = append([]string{}, c.Passthr...)
			return nil
		},
	})

	if code := app.Run([]string{"tool", "--help"}); code != 0 {
		t.Fatalf("post-command help exited %d", code)
	}
	if !reflect.DeepEqual(passed, []string{"--help"}) {
		t.Fatalf("post-command help passed %v, want [--help]", passed)
	}

	passed = nil
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w
	code := app.Run([]string{"--help", "tool"})
	os.Stdout = oldStdout
	_ = w.Close()
	out, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}
	if code != 0 {
		t.Fatalf("pre-command help exited %d", code)
	}
	if passed != nil {
		t.Fatalf("pre-command help reached tool with %v", passed)
	}
	if !strings.Contains(string(out), "tool summary") {
		t.Fatalf("pre-command help did not show tf wrapper help:\n%s", out)
	}
}

func TestLoginMethodItemsFollowLocale(t *testing.T) {
	cases := []struct {
		lang ui.Lang
		want []ui.Item
	}{
		{ui.LangZH, []ui.Item{
			{Label: "从网页导入", Detail: "自动打开 Keys 页面"},
			{Label: "粘贴 API Key", Detail: "终端隐藏输入"},
		}},
		{ui.LangEN, []ui.Item{
			{Label: "Import from web", Detail: "open the Keys page"},
			{Label: "Paste API key", Detail: "hidden terminal input"},
		}},
	}
	for _, tc := range cases {
		if got := loginMethodItems(&ui.UI{Lang: tc.lang}); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("loginMethodItems(%s) = %#v, want %#v", tc.lang, got, tc.want)
		}
	}
}

func TestImportedKeyNameItems(t *testing.T) {
	creds := &config.Credentials{Items: map[string]*config.Credential{
		"browser-key": {Key: "sk-other"},
	}}
	u := &ui.UI{Lang: ui.LangZH}

	items := importedKeyNameItems(u, creds, "gpt", "browser-key", "sk-new")
	if len(items) != 3 {
		t.Fatalf("items = %#v; want automatic, web, and custom choices", items)
	}
	if !strings.Contains(items[0].Label, `"gpt"`) || items[0].Disabled {
		t.Errorf("automatic item = %#v", items[0])
	}
	if !strings.Contains(items[1].Label, `"browser-key"`) ||
		!strings.Contains(items[1].Detail, "覆盖") || items[1].Disabled {
		t.Errorf("web item = %#v", items[1])
	}
	if !strings.Contains(items[2].Label, "自订") {
		t.Errorf("custom item = %#v", items[2])
	}

	invalid := importedKeyNameItems(u, creds, "gpt", "网页 Key", "sk-new")
	if len(invalid) != 3 || !invalid[1].Disabled || !strings.Contains(invalid[1].Detail, "不符合") {
		t.Errorf("invalid web name item = %#v", invalid)
	}

	duplicate := importedKeyNameItems(u, creds, "gpt", "gpt", "sk-new")
	if len(duplicate) != 2 {
		t.Errorf("a web name equal to the automatic name must be deduplicated: %#v", duplicate)
	}
}

func TestCommandUsageIsASCII(t *testing.T) {
	for _, cmd := range allCommands() {
		if strings.ContainsFunc(cmd.Usage, func(r rune) bool { return r > 0x7f }) {
			t.Errorf("%s usage must use ASCII placeholders: %q", cmd.Name, cmd.Usage)
		}
	}
}

func TestFormatModelCountFollowsLocale(t *testing.T) {
	cases := []struct {
		lang ui.Lang
		n    int
		want string
	}{
		{ui.LangZH, 1, "1 个模型"},
		{ui.LangEN, 1, "1 model"},
		{ui.LangEN, 2, "2 models"},
	}
	for _, tc := range cases {
		if got := formatModelCount(&ui.UI{Lang: tc.lang}, tc.n); got != tc.want {
			t.Errorf("formatModelCount(%s, %d) = %q, want %q", tc.lang, tc.n, got, tc.want)
		}
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

	// 越过透传边界之后，那些参数属于 harness。
	for _, words := range [][]string{{"codex", "exec", ""}, {"opencode", "--help", ""}} {
		if got := complete(words); len(got) != 0 {
			t.Errorf("past the passthrough boundary complete(%q) = %v, want nothing", words, got)
		}
	}

	if got := complete([]string{"login", "--f"}); !slices.Contains(got, "--from-web") {
		t.Errorf("login completion must include --from-web, got %v", got)
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
	zsh := completions.Scripts["zsh"]
	if !strings.Contains(zsh, `"${(@)words[2,$CURRENT]}"`) {
		t.Error("zsh script must quote the word array with (@), or the trailing empty word is dropped")
	}
	if strings.Contains(zsh, `__complete ${words[`) {
		t.Error("unquoted array expansion found; zsh drops empty elements")
	}
	if !strings.Contains(completions.Scripts["bash"], `"${COMP_WORDS[@]:1:COMP_CWORD}"`) {
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

// 全局 flag 写在子命令之前也要成立，且它的值不能被当成子命令。
// `tf --key work claude` 曾经报「未知命令：work」。
func TestGlobalFlagsBeforeCommand(t *testing.T) {
	cases := []struct {
		argv    []string
		leading []string
		cmdIdx  int
	}{
		{[]string{"--key", "work", "claude"}, []string{"--key", "work"}, 2},
		{[]string{"--key=work", "claude"}, []string{"--key=work"}, 1},
		{[]string{"--json", "keys"}, []string{"--json"}, 1},
		{[]string{"claude"}, nil, 0},
		{[]string{"--host", "https://x", "-y", "login"}, []string{"--host", "https://x", "-y"}, 3},
		{[]string{"--help"}, []string{"--help"}, -1},
	}
	for _, c := range cases {
		leading, idx, err := splitGlobals(c.argv)
		if err != nil {
			t.Fatalf("splitGlobals(%v): %v", c.argv, err)
		}
		if idx != c.cmdIdx {
			t.Errorf("splitGlobals(%v) cmdIdx = %d, want %d", c.argv, idx, c.cmdIdx)
		}
		if !reflect.DeepEqual(leading, c.leading) {
			t.Errorf("splitGlobals(%v) leading = %#v, want %#v", c.argv, leading, c.leading)
		}
	}
}

// --yes 是 --no-input 的旧名字，两种写法必须落到同一个值上。
func TestNoInputAcceptsOldName(t *testing.T) {
	cmd := &Command{Name: "logout"}
	for _, arg := range []string{"--no-input", "--yes"} {
		ctx, err := parse(cmd, []string{arg})
		if err != nil {
			t.Fatalf("parse(%s): %v", arg, err)
		}
		if !ctx.Flags.Bool("no-input") {
			t.Errorf("%s did not set no-input", arg)
		}
	}

	// -y 不是它的简写：业界 -y 一律是「替我答 yes」，这里语义恰好相反。
	// 留着等于给脚本作者埋一个语义翻转的坑 —— 同一条命令换个工具跑，
	// 一个是全部同意，一个是拒绝一切提问。
	if _, err := parse(cmd, []string{"-y"}); err == nil {
		t.Error("-y must not be an alias for --no-input")
	}
}

// 删 Key 不能静默执行：删完只能回网页重新拿。
// 问不了的时候要求 --force，而不是默认放行。删单把同样如此。
func TestLogoutRefusesToRemoveSilently(t *testing.T) {
	// JSON 模式即非交互，等价于管道 / CI 里的处境。
	c := &Context{UI: ui.New(true), Flags: newValues(), Command: "logout"}

	err := confirm(c, []string{"work", "personal"})
	if err == nil {
		t.Fatal("expected a refusal when confirmation is impossible")
	}
	if got := ui.AsError(err).Hint; got != "tf logout work personal --force" {
		t.Errorf("hint = %q, want the --force command", got)
	}

	c.Flags.set["force"] = "true"
	if err := confirm(c, []string{"work", "personal"}); err != nil {
		t.Errorf("--force should go through, got %v", err)
	}
}

// -m 分开写时，取值要留下标记，以便拿到候选集后再判定。
//
// tf codex -m exec "hi" 的 exec 是 codex 的子命令，
// tf claude -m "解释这段代码" 那句是 prompt。解析期分不出来。
func TestDetachedOptValueIsMarked(t *testing.T) {
	cmd := &Command{Name: "codex", Passthrough: true,
		Flags: []Flag{{Name: "model", Short: "m", Kind: KindOptString}}}

	ctx, err := parse(cmd, []string{"-m", "exec"})
	if err != nil {
		t.Fatal(err)
	}
	if !ctx.Flags.Detached("model") {
		t.Error("separate value must be marked detached")
	}
	if got := ctx.Flags.Detach("model"); got != "exec" {
		t.Errorf("Detach returned %q, want %q", got, "exec")
	}
	if ctx.Flags.String("model") != "" {
		t.Error("Detach must clear the value so -m reads as bare")
	}

	// 写在一起的不算：那明确就是取值。
	ctx, err = parse(cmd, []string{"-m=exec"})
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Flags.Detached("model") {
		t.Error("inline value must not be marked detached")
	}
}

// 拼错模型时要说「没有这个模型」，并给出最接近的候选。
func TestNearestSuggestsCloseModels(t *testing.T) {
	cands := []candidate{
		{Model: "gpt/gpt-5.6-sol"}, {Model: "gpt/gpt-5.6-terra"},
		{Model: "gpt/gpt-5.4"}, {Model: "ccmax/claude-opus-4-6"},
	}
	got := nearest("gpt-5.6-solar", cands)
	if len(got) == 0 || got[0] != "gpt-5.6-sol" {
		t.Errorf("nearest = %v, want gpt-5.6-sol first", got)
	}
	if n := nearest("zzz", cands); len(n) != 0 {
		t.Errorf("nothing close should yield nothing, got %v", n)
	}
}

// settings.json 的 env 段会赢过 tf 注入的环境变量。
//
// 实测过：把 ANTHROPIC_BASE_URL 设成死地址，tf claude 连的是那个死地址
// 而不是网关。CC-Switch 这类工具正是往这里写东西的，所以必须报出来。
func TestStatusFlagsSettingsEnvOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.json"),
		[]byte(`{"env":{"ANTHROPIC_BASE_URL":"http://x"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := settingsEnvKeys(filepath.Join(dir, ".claude", "settings.json"))
	if len(got) != 1 || got[0] != "ANTHROPIC_BASE_URL" {
		t.Errorf("settingsEnvKeys = %v, want [ANTHROPIC_BASE_URL]", got)
	}

	// 没有 env 段、文件不存在、内容不是 JSON —— 都当没有，不能报假警。
	for _, body := range []string{`{"theme":"dark"}`, `not json`} {
		p := filepath.Join(dir, "x.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := settingsEnvKeys(p); len(got) != 0 {
			t.Errorf("settingsEnvKeys(%s) = %v, want none", body, got)
		}
	}
	if got := settingsEnvKeys(filepath.Join(dir, "missing.json")); len(got) != 0 {
		t.Errorf("missing file should yield nothing, got %v", got)
	}
}

// 补全的命令名单必须与注册表一致。
//
// 之前补全里另有一份手写名单，加了 tf status 之后那份不知道 ——
// 两份长得一样，看代码时不会觉得有问题，只有敲 Tab 才发现少一个。
func TestCompletionNamesMatchRegistry(t *testing.T) {
	var want []string
	for _, c := range allCommands() {
		if !c.Hidden {
			want = append(want, c.Name)
		}
	}
	if got := commandNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("commandNames() = %v\nwant %v", got, want)
	}
}

// 服务端给的单位不能当词用，只能当数据摆着。
//
// 「推理积分」是网关返回的字符串，塞进英文句子会得到
// "0/10 推理积分 left"。改成标签加数据的排法之后，两种语言都读得通。
func TestUsageUnitIsNotInlinedIntoSentence(t *testing.T) {
	src, err := os.ReadFile("cmd_status.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func printUsage(")
	if start < 0 {
		t.Fatal("printUsage not found")
	}
	fn := body[start : start+strings.Index(body[start:], "\n}\n")]

	// 单位只该拼在数值后面，不该出现在带 left / 剩余 的格式串里。
	for _, bad := range []string{"%s left", "剩余 %s"} {
		if strings.Contains(fn, bad) {
			t.Errorf("printUsage must not build a sentence around the server unit: %q", bad)
		}
	}
}

// 启动前必须指出会盖掉本次注入的东西，而且只指出真正相撞的。
//
// 实测过：~/.claude/settings.json 的 env.ANTHROPIC_BASE_URL 会赢过
// tf 注入的进程环境。那种情况下横幅写着「模型 claude-sonnet-5」而请求
// 发去了别处 —— 误报比失败更坏，失败至少还会停下来。
func TestWarnOverridesOnlyFlagsRealCollisions(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) string {
		p := filepath.Join(dir, ".claude", "settings.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	managed := map[string]bool{"ANTHROPIC_BASE_URL": true, "ANTHROPIC_MODEL": true}
	collide := func(path string) []string {
		var hit []string
		for _, k := range settingsEnvKeys(path) {
			if managed[k] {
				hit = append(hit, k)
			}
		}
		return hit
	}

	if got := collide(write(`{"env":{"ANTHROPIC_BASE_URL":"http://x"}}`)); len(got) != 1 {
		t.Errorf("a managed key must be flagged, got %v", got)
	}
	// 不相干的键不能报：harness 的配置文件里有别的 env 是它自己的事。
	if got := collide(write(`{"env":{"EDITOR":"vim","FOO":"1"}}`)); len(got) != 0 {
		t.Errorf("unrelated keys must stay quiet, got %v", got)
	}
	if got := collide(write(`{"theme":"dark"}`)); len(got) != 0 {
		t.Errorf("no env section must stay quiet, got %v", got)
	}
}

// 建议里的命令必须在这台机器上真的存在。
//
// 实测在一台没有 npm、没有 brew 的 Ubuntu 上，tf 建议 npm install -g，
// 补全提示建议 brew install —— 照做只会得到 command not found。
func TestSuggestionsNameOnlyAvailableTools(t *testing.T) {
	c := testCtx()

	// zsh 的候选目录必须包含 Linux 的标准位置，否则 Linux 用户永远
	// 落到家目录，还得自己去 .zshrc 加一行 fpath。
	var found bool
	for _, d := range completions.ZshSiteDirs() {
		if d == "/usr/share/zsh/site-functions" {
			found = true
		}
	}
	if !found {
		t.Error("zsh 候选目录漏了 Linux 的标准位置")
	}

	// bash-completion 装好了就不该再提示。
	if completions.BashRuntimePresent() && bashCompletionNote(c) == "" {
		return // 本机已装，无从检验文案，跳过即可
	}
	note := bashCompletionNote(c)
	if note == "" {
		t.Fatal("未装 bash-completion 时应给出说明")
	}
	// 文案里若出现命令，那个命令的可执行文件必须存在。
	for _, bin := range []string{"brew", "apt", "dnf", "pacman"} {
		if strings.Contains(note, bin+" ") {
			if _, err := exec.LookPath(bin); err != nil {
				t.Errorf("建议了本机没有的 %s：%s", bin, note)
			}
		}
	}
}

// 装补全失败时不能记「已经问过」。
//
// 用户答了「装」，写盘失败了 —— 这时记上「问过了」等于把一件没办成
// 的事永久关掉，而且再也不会提起。只有结果已经确定才记：答了不用是
// 确定，装成了是确定，装失败不是。
func TestCompletionsAskedOnlyWhenSettled(t *testing.T) {
	src, err := os.ReadFile("cmd_completions.go")
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	start := strings.Index(body, "func offerCompletions(")
	if start < 0 {
		t.Fatal("offerCompletions not found")
	}
	fn := body[start : start+strings.Index(body[start:], "\n}\n")]

	// 赋值必须发生在 Select 之后：写在前面就等于「问了就算数」。
	set := strings.Index(fn, "CompletionsAsked = true")
	sel := strings.Index(fn, "c.UI.Select(")
	if set < 0 || sel < 0 {
		t.Fatal("expected both a Select call and the flag assignment")
	}
	if set < sel {
		t.Error("CompletionsAsked 在提问之前就置位了，装失败也会被记成问过")
	}
	if !strings.Contains(fn, "return // 下次再问") {
		t.Error("安装失败后必须直接返回，不记录")
	}
}

// 每个已发布的版本都必须在 CHANGELOG.md 里有一节。
//
// 发布流水线从那里取说明，取不到就拒发。把同样的检查放进单元测试，
// 是为了在本地就发现，而不是等 tag 推上去之后。
func TestChangelogCoversEveryTag(t *testing.T) {
	body, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)

	out, err := exec.Command("git", "tag", "--list", "v*").Output()
	if err != nil {
		t.Skip("拿不到 tag 列表")
	}
	for _, tag := range strings.Fields(string(out)) {
		want := "## [" + strings.TrimPrefix(tag, "v") + "]"
		if !strings.Contains(text, want) {
			t.Errorf("CHANGELOG.md 缺少 %s 那一节（找 %q）", tag, want)
		}
	}
	// 未发布的改动要有地方落脚，否则下次发版时无从取起。
	if !strings.Contains(text, "## [未发布]") {
		t.Error("CHANGELOG.md 应保留「未发布」一节")
	}
}

// 发布说明里给出的安装命令必须与 README 里的一致。
//
// 两处各写一份 URL，迟早会有一处指向不存在的路径 —— 而发布说明里的
// 那条是新用户见到的第一条命令，错了就是 404 开局。
func TestInstallURLMatchesReadme(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	notes, err := os.ReadFile("../../scripts/release-notes.sh")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`https://raw\.githubusercontent\.com/\S+install\.sh`)
	a, b := re.Find(readme), re.Find(notes)
	if a == nil {
		t.Fatal("README 里找不到安装 URL")
	}
	if b == nil {
		t.Fatal("发布说明脚本里找不到安装 URL")
	}
	if string(a) != string(b) {
		t.Errorf("两处不一致：\n  README: %s\n  发布说明: %s", a, b)
	}
}

// 发布说明由手写与自动生成两段拼成，所有 job 必须构建同一个 tag。
//
// workflow_dispatch 的 github.ref 是用户选择的分支，不是输入框里的 tag；
// checkout 若不显式指定输入 tag，就会把 main HEAD 装进旧 tag 的产物里。
func TestReleaseWorkflowNotes(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)

	const checkoutRef = "ref: ${{ inputs.tag || github.ref }}"
	checkoutJobs := make(map[string]int)
	currentJob := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			currentJob = strings.TrimSuffix(strings.TrimSpace(line), ":")
		}
		if strings.TrimSpace(line) == checkoutRef {
			checkoutJobs[currentJob]++
		}
	}
	for _, job := range []string{"check", "build", "npm", "release"} {
		if checkoutJobs[job] != 1 {
			t.Errorf("%s 必须且只能检出一次输入 tag：找到 %d 处", job, checkoutJobs[job])
		}
		delete(checkoutJobs, job)
	}
	if len(checkoutJobs) != 0 {
		t.Errorf("未知 job 检出了发布 tag：%v", checkoutJobs)
	}
	if !strings.Contains(text, "buildinfo.Commit=${{ steps.v.outputs.commit }}") {
		t.Error("构建提交必须来自实际 checkout，不能用 workflow_dispatch 的 GITHUB_SHA")
	}
	if !strings.Contains(text, "  build:\n    needs: check\n") {
		t.Error("构建矩阵必须等发版前检查通过")
	}
	if got := strings.Count(text, "bash scripts/release-notes.sh"); got != 2 {
		t.Errorf("变更记录应在前置检查和发布时各生成一次：找到 %d 处，期望 2", got)
	}

	// 只看非注释行：解释这些选项的注释里本来就有它们的名字，
	// 整份文本做子串匹配会被自己的说明撞上。
	var hand, gen bool
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if strings.Contains(line, "release-notes.sh") || strings.Contains(line, "--notes-file") {
			hand = true
		}
		if strings.Contains(line, "--generate-notes") {
			gen = true
		}
	}
	if !hand {
		t.Error("缺少手写那段：说明该从 CHANGELOG.md 取")
	}
	if !gen {
		t.Error("缺少 --generate-notes：GitHub 应追加 PR 列表与完整变更链接")
	}
}

// npm 发布只接受 workflow OIDC，不应退化成长效 registry token。
func TestReleaseWorkflowNPMTrustedPublishing(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)

	for _, required := range []string{
		"  npm:\n    needs: build\n",
		"id-token: write",
		"pattern: npm-*",
		"npm install --global npm@11.19.0 pnpm@9.15.9",
		"node scripts/publish-npm-packages.mjs dist/npm --provenance",
		"  release:\n    needs: [build, npm]\n",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("npm 发布流程缺少 %q", required)
		}
	}
	if strings.Contains(text, "NPM_TOKEN") || strings.Contains(text, "NODE_AUTH_TOKEN") {
		t.Error("npm 发布流程不得依赖长效 registry token")
	}
}
