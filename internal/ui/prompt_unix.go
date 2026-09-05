//go:build !windows

package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func (u *UI) readPrompt(prompt string, secret bool) (string, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", ErrNotInteractive
	}
	saved, err := sttyCapture(f, "-g")
	if err != nil {
		f.Close()
		return "", ErrNotInteractive
	}
	tty := &rawTTY{f: f, saved: saved}
	defer guardTerminal(tty)()
	echo := "echo"
	if secret {
		echo = "-echo"
	}
	// Establish line editing even if the previous program left the terminal raw.
	if err := sttyRun(f, echo, "icanon", "icrnl", "isig"); err != nil {
		return "", ErrNotInteractive
	}
	if secret {
		defer fmt.Fprint(f, "\r\n")
	}
	fmt.Fprintf(f, "%s ", prompt)
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && line == "" {
		return "", ErrNotInteractive
	}
	return strings.TrimRight(line, "\r\n"), nil
}
