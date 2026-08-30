// Package launch 负责起子进程并把终端语义交还给它。
//
// 用 fork + wait 而非 syscall.Exec（见 docs/design/open-decisions.md A 项）：
// 换来确定性的收尾（清理、用量摘要），代价是这段信号转发代码。
package launch

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// Result 是一次启动的结果。
type Result struct {
	ExitCode int
	Duration time.Duration
}

// Spec 描述要启动的进程。
type Spec struct {
	Bin  string
	Args []string
	Env  []string
}

// Run 启动子进程并等待它结束。
//
// 子进程完全继承当前终端（stdin/stdout/stderr 直连），因此 TUI、
// 颜色、窗口大小变化都与直接运行 harness 无异。
func Run(s Spec) (Result, error) {
	start := time.Now()

	cmd := exec.Command(s.Bin, s.Args...)
	cmd.Env = s.Env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// 必须在子进程动终端之前记下状态。
	term := captureTerm()

	if err := cmd.Start(); err != nil {
		term.restore(false)
		return Result{ExitCode: 127}, err
	}

	stop := forwardSignals(cmd)
	defer stop()

	err := cmd.Wait()
	res := Result{Duration: time.Since(start)}

	if err == nil {
		term.restore(false)
		return res, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// 被信号终止时按 shell 惯例返回 128+signal。
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			// 死于信号 = 没机会自己收尾，终端要由我们复位。
			term.restore(true)
			res.ExitCode = 128 + int(status.Signal())
			return res, nil
		}
		term.restore(false)
		res.ExitCode = exitErr.ExitCode()
		return res, nil
	}
	term.restore(true)
	res.ExitCode = 1
	return res, err
}

// forwardSignals 把父进程收到的信号转交子进程。
//
// 关键点：父进程收到 SIGINT 时**不能自己退出**。终端会把 Ctrl+C 投递给
// 整个前台进程组，子进程本来就会收到；父进程若跟着退出，就会在子进程
// 还在收尾时把终端抢回来。这里只做转发，然后继续等。
func forwardSignals(cmd *exec.Cmd) func() {
	ch := make(chan os.Signal, 8)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				if cmd.Process != nil {
					_ = cmd.Process.Signal(sig)
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(ch)
		close(done)
	}
}
