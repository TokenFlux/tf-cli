package harness

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tokenflux/tf-cli/internal/process"
)

// A global install may update user PATH without changing this running process.
// Discover the manager's actual bin directory; never modify the registry or profile.
func (h *Harness) DetectAfterInstall(opt InstallOption) (Status, error) {
	status := h.Detect()
	if status.Installed {
		cmd := process.CommandContext(context.Background(), status.Path, nil, nil)
		if cmd.Err != nil {
			return status, cmd.Err
		}
		return checkedInstallStatus(status)
	}
	if len(opt.Args) == 0 {
		return status, nil
	}
	manager := strings.ToLower(strings.TrimSuffix(filepath.Base(opt.Args[0]), filepath.Ext(opt.Args[0])))
	var args []string
	switch manager {
	case "npm":
		args = []string{"prefix", "--global"}
	case "pnpm":
		args = []string{"bin", "--global"}
	case "bun":
		args = []string{"pm", "bin", "--global"}
	default:
		return status, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := process.CommandContext(ctx, opt.Args[0], args, nil).Output()
	if err != nil {
		return status, fmt.Errorf("cannot locate %s global executables: %w", manager, err)
	}
	dir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, "\r\n") {
		return status, fmt.Errorf("%s did not return an absolute global executable directory", manager)
	}
	candidate, err := exec.LookPath(filepath.Join(dir, h.Bin))
	if err != nil {
		return status, fmt.Errorf("%s was not found in %s after installation", h.Bin, dir)
	}
	if cmd := process.CommandContext(ctx, candidate, nil, nil); cmd.Err != nil {
		return status, cmd.Err
	}
	// Only append a verified installation location, preserving existing PATH priority.
	current := os.Getenv("PATH")
	for _, item := range filepath.SplitList(current) {
		if strings.EqualFold(filepath.Clean(item), filepath.Clean(dir)) {
			return checkedInstallStatus(h.Detect())
		}
	}
	if current != "" {
		current += string(os.PathListSeparator)
	}
	if err := os.Setenv("PATH", current+dir); err != nil {
		return status, err
	}
	return checkedInstallStatus(h.Detect())
}

func checkedInstallStatus(status Status) (Status, error) {
	if status.Installed && status.Version == "" {
		return status, fmt.Errorf("%s --version did not return a usable version; check the client's runtime and dependencies", status.Path)
	}
	return status, nil
}
