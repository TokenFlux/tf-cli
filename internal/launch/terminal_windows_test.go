package launch

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/UserExistsError/conpty"
	"golang.org/x/sys/windows"
)

func TestWindowsLaunchHelper(t *testing.T) {
	mode := os.Getenv("TF_TEST_LAUNCH")
	if mode == "" {
		return
	}
	term := captureTerm()
	if term.tty == nil {
		t.Fatal("no console")
	}
	if mode == "child" {
		if err := windows.SetConsoleMode(windows.Handle(term.input.Fd()), term.inputMode&^(windows.ENABLE_ECHO_INPUT|windows.ENABLE_LINE_INPUT)); err != nil {
			t.Fatal(err)
		}
		if err := windows.SetConsoleMode(windows.Handle(term.tty.Fd()), term.outputMode^windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("TF_TEST_INTERRUPT") == "1" {
			windows.NewLazySystemDLL("kernel32.dll").NewProc("ExitProcess").Call(0xc000013a)
		}
		os.Exit(7)
	}
	defer term.restore(false)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	result, err := Run(exe, []string{"-test.run=^TestWindowsLaunchHelper$"}, append(os.Environ(), "TF_TEST_LAUNCH=child"))
	if err != nil {
		t.Fatal(err)
	}
	want := 7
	if os.Getenv("TF_TEST_INTERRUPT") == "1" {
		want = 130
	}
	if result.ExitCode != want {
		t.Fatalf("exit=%d, want %d", result.ExitCode, want)
	}
	var in, out uint32
	if err := windows.GetConsoleMode(windows.Handle(term.input.Fd()), &in); err != nil {
		t.Fatal(err)
	}
	if err := windows.GetConsoleMode(windows.Handle(term.tty.Fd()), &out); err != nil {
		t.Fatal(err)
	}
	if in != term.inputMode || out != term.outputMode {
		t.Fatalf("console modes not restored: in=%x/%x out=%x/%x", in, term.inputMode, out, term.outputMode)
	}
	fmt.Println("RESTORED")
}

func TestWindowsLaunchRestoresConsole(t *testing.T) {
	for _, interrupted := range []string{"0", "1"} {
		t.Run("interrupt="+interrupted, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			pty, err := conpty.Start(windows.EscapeArg(exe)+" -test.run=^TestWindowsLaunchHelper$", conpty.ConPtyEnv(append(os.Environ(), "TF_TEST_LAUNCH=parent", "TF_TEST_INTERRUPT="+interrupted)))
			if err != nil {
				t.Fatal(err)
			}
			output := make(chan string, 1)
			go func() { b, _ := io.ReadAll(pty); output <- string(b) }()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			code, err := pty.Wait(ctx)
			_ = pty.Close()
			text := <-output
			if err != nil || code != 0 || !strings.Contains(text, "RESTORED") {
				t.Fatalf("code=%d err=%v output=%s", code, err, text)
			}
		})
	}
}

func TestWindowsRestoreWithoutConsole(t *testing.T) {
	term := &termState{}
	term.homeColumn()
	term.restore(true)
	term.restore(false)
}
