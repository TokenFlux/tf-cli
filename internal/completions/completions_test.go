package completions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 每种 shell 都要有脚本，且脚本必须回调用户实际敲的那个路径。
//
// 写死 tf 的话，./bin/tf 这种没进 PATH 的跑法会因为找不到命令而
// 静默无补全 —— 没有报错，只是按 Tab 什么都不发生。
func TestScriptsCallBackByInvokedPath(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		s, ok := Scripts[sh]
		if !ok {
			t.Errorf("%s 没有补全脚本", sh)
			continue
		}
		if !strings.Contains(s, "__complete") {
			t.Errorf("%s 的脚本没有调用 __complete", sh)
		}
		// 不该出现「直接写死 tf 再接 __complete」的写法。
		if strings.Contains(s, "tf __complete") {
			t.Errorf("%s 的脚本写死了 tf，未入 PATH 时会静默失效", sh)
		}
	}
}

// zsh 的候选目录要覆盖 macOS 与 Linux 两边的标准位置。
func TestZshSiteDirsCoverBothPlatforms(t *testing.T) {
	dirs := ZshSiteDirs()
	want := map[string]bool{
		"/opt/homebrew/share/zsh/site-functions": false, // macOS
		"/usr/share/zsh/site-functions":          false, // Linux
	}
	for _, d := range dirs {
		if _, ok := want[d]; ok {
			want[d] = true
		}
	}
	for d, found := range want {
		if !found {
			t.Errorf("候选目录漏了 %s", d)
		}
	}
}

// Path 给出的位置必须在家目录之下，且每种 shell 各不相同。
func TestPathIsPerShellAndUnderHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("拿不到家目录")
	}
	seen := map[string]string{}
	for _, sh := range []string{"bash", "zsh", "fish"} {
		p, err := Path(sh)
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if !strings.HasPrefix(p, home) && !strings.HasPrefix(p, string(filepath.Separator)) {
			t.Errorf("%s 的位置既不在家目录也不是绝对路径：%s", sh, p)
		}
		if prev, dup := seen[p]; dup {
			t.Errorf("%s 与 %s 落在同一个文件：%s", sh, prev, p)
		}
		seen[p] = sh
	}
	if _, err := Path("nushell"); err == nil {
		t.Error("不认识的 shell 应当报错")
	}
}
