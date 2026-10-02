package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Package-manager batch shims are resolved through their package's bin metadata.
// Execute the registered entry directly, without interpreting argv in a shell.
func CommandContext(ctx context.Context, name string, args, env []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	ext := strings.ToLower(filepath.Ext(cmd.Path))
	if cmd.Err != nil || (ext != ".cmd" && ext != ".bat") {
		return cmd
	}
	entry, err := resolvePackageBin(cmd.Path)
	if err != nil {
		cmd.Err = err
		return cmd
	}
	runtime, flags, err := binRuntime(entry)
	if err != nil {
		cmd.Err = err
		return cmd
	}
	if runtime == "" {
		cmd = exec.CommandContext(ctx, entry, args...)
	} else {
		executable := filepath.Join(filepath.Dir(cmd.Path), runtime+".exe")
		if info, err := os.Stat(executable); err != nil || info.IsDir() {
			executable, err = exec.LookPath(runtime + ".exe")
			if err != nil {
				cmd.Err = fmt.Errorf("%s requires %s on PATH: %w", name, runtime, err)
				return cmd
			}
		}
		binName := strings.ToLower(strings.TrimSuffix(filepath.Base(cmd.Path), filepath.Ext(cmd.Path)))
		if runtime == "node" && (binName == "npm" || binName == "npx") {
			entry, err = npmRedirect(ctx, executable, entry, env)
			if err != nil {
				cmd.Err = err
				return cmd
			}
		}
		argv := append(append(flags, entry), args...)
		cmd = exec.CommandContext(ctx, executable, argv...)
	}
	cmd.Env = env
	return cmd
}
