//go:build pty

// Package e2e 用真的 pty 驱动真的二进制。
//
// 为什么必须这样测：这个项目里最难缠的几个 bug 全都只在真终端下出现，
// 单元测试一个都抓不到 ——
//
//   - j / k / q 被当成导航键吃掉，claude-haiku、qwen、kimi 过滤不了
//   - -m 吞掉 harness 的位置参数，把 prompt 当成了模型 ID
//   - zsh 未加引号的数组展开丢掉末尾空词，tf codex <TAB> 重复补 codex
//   - 补全在多处返回空，而空结果在 zsh 里会沿用上一次的菜单
//
// 它们都是「进程边界 + 终端行为」的产物。之前发现它们靠的是手动敲，
// 而手动敲不会在下一次改动时自动重来一遍。
//
// 默认不跑：CI 里没有 tty，而这类测试慢、依赖终端。用 make pty。
package e2e

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// pty 是一个跑在真伪终端里的 tf 进程。
type pty struct {
	t    *testing.T
	cmd  *exec.Cmd
	in   io.WriteCloser
	mu   sync.Mutex
	buf  bytes.Buffer
	seen int // waitFor 已经消费到哪里
	done chan struct{}
	code int
}

// start 在 pty 里启动 tf。
//
// 借 script(1) 造 pty 而不是自己 openpt：stdlib 没有 forkpty，
// 手写要碰 ioctl 和平台差异，而 script 在 macOS 与 Linux 上都有。
// 代价是两个平台的调用写法不同，就这一处差异。
func start(t *testing.T, env []string, args ...string) *pty {
	t.Helper()

	bin, err := filepath.Abs("../bin/tf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("先 make build：%v", err)
	}

	var cmd *exec.Cmd
	line := append([]string{bin}, args...)
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("script", append([]string{"-q", "/dev/null"}, line...)...)
	default:
		cmd = exec.Command("script", "-q", "-c", strings.Join(line, " "), "/dev/null")
	}
	cmd.Env = append(os.Environ(), env...)

	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout

	p := &pty{t: t, cmd: cmd, in: in, done: make(chan struct{})}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	go func() {
		chunk := make([]byte, 4096)
		for {
			n, err := out.Read(chunk)
			if n > 0 {
				p.mu.Lock()
				p.buf.Write(chunk[:n])
				p.mu.Unlock()
			}
			if err != nil {
				break
			}
		}
		_ = cmd.Wait()
		p.code = cmd.ProcessState.ExitCode()
		close(p.done)
	}()

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
	})
	return p
}

// screen 返回目前收到的全部输出，已剥掉转义序列。
func (p *pty) screen() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return clean(p.buf.String())
}

// send 往 pty 里写入按键。
func (p *pty) send(keys string) {
	p.t.Helper()
	if _, err := io.WriteString(p.in, keys); err != nil {
		p.t.Fatalf("写入按键失败：%v", err)
	}
}

// waitFor 等 want 出现在**新**输出里。
//
// 只看上次匹配之后的部分，这一点是必须的：选择器每次按键都重绘整屏，
// 而缓冲区留着全部历史。在整个历史里搜的话，「esc 取消」在第一屏就
// 出现过，等它等于不等 —— 测试会立刻通过然后在下一步莫名其妙地失败。
//
// 轮询而不是固定 sleep：固定 sleep 要么慢，要么在忙的机器上偶发失败，
// 而偶发失败的测试很快会被当成噪音忽略掉。
func (p *pty) waitFor(want string) {
	p.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		fresh := clean(p.buf.String())
		if p.seen > len(fresh) {
			p.seen = 0 // 剥转义后长度可能缩水
		}
		if i := strings.Index(fresh[p.seen:], want); i >= 0 {
			p.seen += i + len(want)
			p.mu.Unlock()
			return
		}
		p.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	p.t.Fatalf("等不到 %q\n--- 屏幕 ---\n%s", want, p.tail())
}

// tail 取屏幕末尾若干行，报错时用。整份缓冲太长，反而看不清。
func (p *pty) tail() string {
	lines := strings.Split(strings.TrimRight(p.screen(), "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}

// waitExit 等进程退出并返回退出码。
func (p *pty) waitExit() int {
	p.t.Helper()
	select {
	case <-p.done:
		return p.code
	case <-time.After(15 * time.Second):
		p.t.Fatalf("进程没有退出\n--- 屏幕 ---\n%s", p.tail())
		return -1
	}
}

const (
	keyEsc   = "\x1b"
	keyEnter = "\r"
	keyDown  = "\x1b[B"
	keyCtrlU = "\x15"
)

// fixture 是一份自带假网关的隔离配置。
//
// 不连真网关：真网关要网络、要额度、还会变。今天就因为配额用尽
// 收了一串 429 —— 那种测试失败与代码对错无关。
type fixture struct {
	dir    string
	models []string
}

// env 返回让 tf 使用这份配置的环境变量。
//
// PATH 里塞了假的 harness：不这样的话测试会真把 Claude Code 拉起来，
// 然后停在它的界面里等人 —— 而且会依赖本机装了什么。
func (f fixture) env() []string {
	return []string{
		"XDG_CONFIG_HOME=" + filepath.Join(f.dir, "cfg"),
		"XDG_CACHE_HOME=" + filepath.Join(f.dir, "cache"),
		"PATH=" + filepath.Join(f.dir, "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TF_LANG=zh",
	}
}

// writeConfig 落盘一份指向 host 的配置。
func writeConfig(t *testing.T, host string, models []string) fixture {
	t.Helper()
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "cfg", "tf")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}

	quoted := make([]string, len(models))
	for i, m := range models {
		quoted[i] = fmt.Sprintf("%q", m)
	}
	list := strings.Join(quoted, ",")

	cfg := fmt.Sprintf(`{"version":1,"keys":{"k":{"host":%q,
	  "protocols":{"":["anthropic_messages","openai_responses","openai_chat_completions"]},
	  "models":[%s]}}}`, host, list)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	creds := `{"version":1,"credentials":{"k":{"key":"sk-test","source":"paste"}}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "credentials.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}

	// 假 harness：打一行可断言的标记，把收到的参数原样吐出来，然后退出。
	// 参数也吐是有意的 —— 透传对不对是这套测试要看的东西之一。
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"claude", "codex", "opencode"} {
		body := "#!/bin/sh\necho \"FAKE-" + name + " args:$*\"\nexit 0\n"
		if err := os.WriteFile(filepath.Join(binDir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return fixture{dir: dir, models: models}
}
