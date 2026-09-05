//go:build !windows

package process

import (
	"context"
	"os/exec"
)

func CommandContext(ctx context.Context, name string, args, env []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	return cmd
}
