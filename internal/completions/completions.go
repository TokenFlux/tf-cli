// Package completions 管 shell 补全的脚本与落盘位置。
//
// 与 internal/cli 的界线：这里只回答「脚本长什么样、该写到哪、
// 本机装没装 bash-completion」，不组织候选、不产出给人看的话。
// 需要解释时由调用方措辞 —— 包不该管文案的语言。
package completions

import (
	"fmt"
	"os"
	"path/filepath"
)

// CurrentShell 从 $SHELL 认出当前 shell。认不出就返回空。
func CurrentShell() string {
	base := filepath.Base(os.Getenv("SHELL"))
	if _, ok := Scripts[base]; ok {
		return base
	}
	return ""
}

// Installed 报告补全文件是否已经就位。
func Installed(shell string) bool {
	path, err := Path(shell)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// 脚本里一律用「用户实际输入的那个路径」回调，而不是写死 `tf`。
// 否则 ./bin/tf 这种未入 PATH 的跑法会因找不到命令而静默无补全。
var Scripts = map[string]string{
	"bash": `# tf bash completion. eval "$(tf completions bash)"
_tf_complete() {
    local IFS=$'\n'
    COMPREPLY=($("${COMP_WORDS[0]}" __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null))
}
# 同时注册裸命令名与常见的相对路径写法。
complete -o default -F _tf_complete tf ./tf ./bin/tf bin/tf
`,
	"zsh": `# tf zsh completion. eval "$(tf completions zsh)"
_tf_complete() {
    local -a candidates
    # (@) 加引号：不加的话 zsh 会丢掉末尾的空词，__complete 收到的是
    # 「tf codex」而不是「tf codex ""」，于是以为你还在敲命令名，
    # 把 codex 又补了一遍。
    candidates=(${(f)"$(${words[1]} __complete "${(@)words[2,$CURRENT]}" 2>/dev/null)"})
    compadd -a candidates
}
compdef _tf_complete tf
# 模式注册：让 ./bin/tf、/usr/local/bin/tf 等写法也能补全。
compdef _tf_complete -p '*/tf'
`,
	"fish": `# tf fish completion. tf completions fish --install
function __tf_complete
    set -l tokens (commandline -opc) (commandline -ct)
    $tokens[1] __complete $tokens[2..-1] 2>/dev/null
end
complete -c tf -f -a '(__tf_complete)'
`,
}

// ZshSiteDirs 是 zsh 默认 fpath 里常见的可写候选，按优先级排。
func ZshSiteDirs() []string {
	return []string{
		// macOS 的 homebrew（ARM 与 Intel 两个前缀）
		"/opt/homebrew/share/zsh/site-functions",
		// Linux 发行版的标准位置；/usr/local 那个两边都可能有
		"/usr/share/zsh/site-functions",
		"/usr/local/share/zsh/site-functions",
	}
}

// ZshSiteDir 找一个已经在 zsh 默认 fpath 里、且可写的目录。
//
// 装到已在 fpath 里的目录，用户才不必自己动手改 .zshrc —— 而那行
// fpath 还必须排在 compinit 之前，等于留了份作业给用户。
//
// 找不到就返回空，调用方退回到家目录下的写法。
func ZshSiteDir() string {
	for _, d := range ZshSiteDirs() {
		info, err := os.Stat(d)
		if err != nil || !info.IsDir() {
			continue
		}
		// 可写才算数：只读目录写下去只会得到一个权限错误。
		probe := filepath.Join(d, ".tf-write-probe")
		if f, err := os.Create(probe); err == nil {
			f.Close()
			os.Remove(probe)
			return d
		}
	}
	return ""
}

// BashRuntimePresent 判断本机是否已经装了 bash-completion。
//
// 装好了就什么都不说 —— 提示一件已经成立的事只会让人怀疑是不是没装好。
func BashRuntimePresent() bool {
	for _, p := range []string{
		"/usr/share/bash-completion/bash_completion",     // 多数 Linux 发行版
		"/etc/bash_completion",                           // 较老的布局
		"/opt/homebrew/etc/profile.d/bash_completion.sh", // macOS homebrew（ARM）
		"/usr/local/etc/profile.d/bash_completion.sh",    // macOS homebrew（Intel）
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// Path 给出该 shell 的补全文件位置。
//
// 安装和「是否已装」的判断必须共用这一处，否则两边会慢慢长歪。
func Path(shell string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch shell {
	case "fish":
		return filepath.Join(home, ".config", "fish", "completions", "tf.fish"), nil
	case "zsh":
		// 优先装进已经在 fpath 里的目录，用户就不必再改 .zshrc。
		//
		// 装到 ~/.zsh/completions 是最常见的写法，但那个目录不在任何人的
		// 默认 fpath 里 —— 于是「装好了却不生效」，还要用户自己去补一行，
		// 而且那一行必须排在 compinit 之前，追加到文件末尾是没用的。
		if dir := ZshSiteDir(); dir != "" {
			return filepath.Join(dir, "_tf"), nil
		}
		return filepath.Join(home, ".zsh", "completions", "_tf"), nil
	case "bash":
		return filepath.Join(home, ".local", "share", "bash-completion", "completions", "tf"), nil
	}
	return "", fmt.Errorf("unsupported shell %q", shell)
}
