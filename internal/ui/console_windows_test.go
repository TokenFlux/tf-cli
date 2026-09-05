package ui

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/UserExistsError/conpty"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sys/windows"
)

func TestConsoleHelper(t *testing.T) {
	mode := os.Getenv("TF_TEST_CONSOLE")
	if mode == "" {
		return
	}
	input, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	var beforeOut, afterOut uint32
	if err := windows.GetConsoleMode(windows.Handle(output.Fd()), &beforeOut); err != nil {
		t.Fatal(err)
	}
	var before, after uint32
	if err := windows.GetConsoleMode(windows.Handle(input.Fd()), &before); err != nil {
		t.Fatal(err)
	}
	originalOut := os.Stdout
	if mode == "redirect" {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		w.Close()
		os.Stdin = r
		f, err := os.CreateTemp(t.TempDir(), "stdout")
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = f
		defer f.Close()
	}
	u := New(false)
	if !u.Interactive(false) {
		t.Fatal("console not detected")
	}
	if mode != "redirect" {
		u.Printf("%s\n", u.Bold("COLOR"))
	}
	var result string
	switch mode {
	case "secret":
		value, err := u.ReadSecret("Secret:")
		result = fmt.Sprintf("hash=%x error=%v interrupted=%v", sha256.Sum256([]byte(value)), err, err != nil && AsError(err).Cause == ErrInterrupted)
	case "line":
		value, err := u.ReadLine("Name:")
		result = fmt.Sprintf("value=%s error=%v", value, err)
	case "choose":
		index, err := u.Choose("Choose client", []string{"one", "two"})
		result = fmt.Sprintf("index=%d error=%v", index, err)
	default:
		index, err := u.Select("Select client", []Item{{Label: "one"}, {Label: "中文"}, {Label: "third"}, {Label: "😀"}})
		result = fmt.Sprintf("index=%d error=%v interrupted=%v", index, err, err != nil && AsError(err).Cause == ErrInterrupted)
	}
	if mode == "resize" {
		tty, err := openRawTTY()
		if err != nil {
			t.Fatal(err)
		}
		rows, cols := tty.Size()
		tty.Restore()
		result += fmt.Sprintf(" rows=%d cols=%d", rows, cols)
	}
	if err := windows.GetConsoleMode(windows.Handle(input.Fd()), &after); err != nil {
		t.Fatal(err)
	}
	if err := windows.GetConsoleMode(windows.Handle(output.Fd()), &afterOut); err != nil {
		t.Fatal(err)
	}
	if beforeOut != afterOut {
		t.Fatalf("output mode not restored: %x -> %x", beforeOut, afterOut)
	}
	if before != after {
		t.Fatalf("input mode not restored: %x -> %x", before, after)
	}
	if mode == "redirect" {
		stat, _ := os.Stdout.Stat()
		if stat.Size() != 0 {
			t.Fatal("UI contaminated redirected stdout")
		}
	}
	fmt.Fprintf(originalOut, "\r\nRESULT\r\n%s\r\nRESTORED\r\n", result)
}

type consoleOutput struct {
	mu   sync.Mutex
	text strings.Builder
}

func (o *consoleOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.text.Write(p)
}
func (o *consoleOutput) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.ReplaceAll(strings.ReplaceAll(ansi.Strip(o.text.String()), "\r", ""), "\n", "")
}

func TestWindowsConsoleInteraction(t *testing.T) {
	cases := []struct{ name, mode, prompt, input, want, hidden string }{
		{"resize", "resize", "Select client", "\x1b[B\r", "rows=6 cols=24", ""},
		{"clear-filter", "select", "Select client", "third\x1b", "index=0 error=<nil>", ""},
		{"ctrl-u", "select", "Select client", "wrong\x15中文\r", "index=1 error=<nil>", ""},
		{"arrows", "select", "Select client", "\x1b[B\r", "index=1 error=<nil>", ""},
		{"up", "select", "Select client", "\x1b[B\x1b[B\x1b[A\r", "index=1 error=<nil>", ""},
		{"unicode-backspace", "select", "Select client", "中X\x7f文\r", "index=1 error=<nil>", ""},
		{"surrogate-pair", "select", "Select client", "😀\r", "index=3 error=<nil>", ""},
		{"escape", "select", "Select client", "\x1b", "interrupted=false", ""},
		{"ctrl-c", "select", "Select client", "one\x03", "interrupted=true", ""},
		{"ctrl-d", "select", "Select client", "\x04", "interrupted=true", ""},
		{"redirect", "redirect", "Select client", "\x0e\r", "index=1 error=<nil>", ""},
		{"secret", "secret", "Secret:", "sk-fixture-中文\r", fmt.Sprintf("hash=%x", sha256.Sum256([]byte("sk-fixture-中文"))), "sk-fixture"},
		{"secret-cancel", "secret", "Secret:", "sk-fixture\x03", "interrupted=true", "sk-fixture"},
		{"line-edit", "line", "Name:", "中X文\x1b[D\x7f\r", "value=中文 error=<nil>", ""},
		{"line-home-delete", "line", "Name:", "X中文\x1b[H\x1b[3~\x1b[F\r", "value=中文 error=<nil>", ""},
		{"numbered", "choose", "Choose client", "2\r", "index=1 error=<nil>", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			pty, err := conpty.Start(windows.EscapeArg(exe)+" -test.run=^TestConsoleHelper$", conpty.ConPtyDimensions(48, 12), conpty.ConPtyEnv(append(os.Environ(), "TF_TEST_CONSOLE="+tc.mode, "TF_LANG=en")))
			if err != nil {
				t.Fatal(err)
			}
			defer pty.Close()
			output := &consoleOutput{}
			done := make(chan struct{})
			go func() { _, _ = io.Copy(output, pty); close(done) }()
			deadline := time.Now().Add(10 * time.Second)
			for !strings.Contains(output.String(), tc.prompt) {
				if time.Now().After(deadline) {
					t.Fatalf("prompt timeout: %s", output.String())
				}
				time.Sleep(10 * time.Millisecond)
			}
			// Idle beyond the escape timeout must not be mistaken for EOF.
			time.Sleep(150 * time.Millisecond)
			if tc.name == "resize" {
				if err := pty.Resize(24, 6); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pty.Write([]byte(tc.input)); err != nil {
				t.Fatal(err)
			}
			if tc.name == "clear-filter" {
				time.Sleep(200 * time.Millisecond)
				if _, err := pty.Write([]byte("\r")); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			code, err := pty.Wait(ctx)
			if err != nil || code != 0 {
				t.Fatalf("exit=%d err=%v output=%s", code, err, output.String())
			}
			for !strings.Contains(output.String(), "RESTORED") && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			got := output.String()
			if !strings.Contains(got, tc.want) || !strings.Contains(got, "RESTORED") {
				t.Fatalf("want %q, got %s", tc.want, got)
			}
			if tc.hidden != "" && strings.Contains(got, tc.hidden) {
				t.Fatal("secret was echoed")
			}
		})
	}
}
