package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// npm/pnpm produce both .cmd and POSIX shims. Use the latter with Git Bash:
// CreateProcess cannot run .cmd files, and cmd /c would reinterpret user arguments.
func CommandContext(ctx context.Context, name string, args, env []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	ext := strings.ToLower(filepath.Ext(cmd.Path))
	if cmd.Err != nil || (ext != ".cmd" && ext != ".bat") {
		return cmd
	}
	shim := strings.TrimSuffix(cmd.Path, filepath.Ext(cmd.Path))
	f, err := os.Open(shim)
	if err != nil {
		cmd.Err = fmt.Errorf("%s needs a matching POSIX shim and Git Bash: %w", name, err)
		return cmd
	}
	header, _ := bufio.NewReader(io.LimitReader(f, 128)).ReadString('\n')
	f.Close()
	header = strings.TrimSpace(header)
	if header != "#!/bin/sh" && header != "#!/bin/bash" && header != "#!/usr/bin/env bash" && header != "#!/usr/bin/env sh" {
		cmd.Err = fmt.Errorf("%s has no supported POSIX shim; install the client with npm or pnpm", name)
		return cmd
	}
	bash, err := exec.LookPath("bash.exe")
	if err != nil {
		cmd.Err = fmt.Errorf("%s needs Git Bash: %w", name, err)
		return cmd
	}
	// System32's legacy bash.exe runs WSL, not the native Windows toolchain.
	root := strings.ToLower(filepath.Clean(os.Getenv("SystemRoot"))) + string(filepath.Separator)
	if strings.HasPrefix(strings.ToLower(bash), root) {
		cmd.Err = fmt.Errorf("%s needs Git Bash on PATH, not WSL bash.exe", name)
		return cmd
	}
	argv := append([]string{"--noprofile", "--norc", "--", shim}, args...)
	cmd = exec.CommandContext(ctx, bash, argv...)
	// MSYS parses backslash-escaped quotes only inside a quoted argument.
	// Go's default command line leaves quote-only arguments unquoted.
	quoted := make([]string, len(cmd.Args))
	for i, arg := range cmd.Args {
		q := syscall.EscapeArg(arg)
		if !strings.HasPrefix(q, `"`) {
			q = `"` + q + strings.Repeat(`\`, len(arg)-len(strings.TrimRight(arg, `\`))) + `"`
		}
		quoted[i] = q
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: strings.Join(quoted, " ")}
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = append(append([]string{}, env...), "MSYS2_ARG_CONV_EXCL=*")
	return cmd
}
