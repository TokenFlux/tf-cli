//go:build !windows

package ui

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type rawTTY struct {
	f     *os.File
	saved string
	once  sync.Once
}

func openRawTTY() (*rawTTY, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	saved, err := sttyCapture(f, "-g")
	if err != nil {
		f.Close()
		return nil, err
	}
	if err := sttyRun(f, "raw", "-echo", "min", "0", "time", "1"); err != nil {
		f.Close()
		return nil, err
	}
	return &rawTTY{f: f, saved: saved}, nil
}

func (t *rawTTY) Restore() {
	if t == nil || t.f == nil {
		return
	}
	t.once.Do(func() {
		if t.saved != "" {
			_ = sttyRun(t.f, t.saved)
		}
		_ = t.f.Close()
	})
}

func (t *rawTTY) Size() (rows, cols int) {
	rows, cols = 24, 80
	out, err := sttyCapture(t.f, "size")
	if err != nil {
		return
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return
	}
	if r, err := strconv.Atoi(parts[0]); err == nil && r > 0 {
		rows = r
	}
	if c, err := strconv.Atoi(parts[1]); err == nil && c > 0 {
		cols = c
	}
	return
}

func sttyRun(f *os.File, args ...string) error {
	cmd := exec.Command("stty", args...)
	cmd.Stdin, cmd.Stdout = f, os.Stderr
	return cmd.Run()
}

func sttyCapture(f *os.File, args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = f
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// VTIME expiry is a zero-byte read, not EOF. os.File.Read loses that distinction.
func (t *rawTTY) rawRead(buf []byte) (n int, ok bool) {
	n, err := syscall.Read(int(t.f.Fd()), buf)
	if err == syscall.EINTR || err == syscall.EAGAIN {
		return 0, true
	}
	return n, err == nil
}
