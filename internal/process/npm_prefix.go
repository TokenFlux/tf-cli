package process

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// npm's bundled Windows launcher prefers a globally upgraded npm. Query its own
// read-only prefix helper so npmrc and environment precedence remain npm's job.
func npmRedirect(ctx context.Context, node, entry string, env []string) (string, error) {
	name := filepath.Base(entry)
	if (name != "npm-cli.js" && name != "npx-cli.js") || filepath.Base(filepath.Dir(filepath.Dir(entry))) != "npm" {
		return entry, nil
	}
	helper := filepath.Join(filepath.Dir(entry), "npm-prefix.js")
	if _, err := os.Stat(helper); os.IsNotExist(err) {
		return entry, nil
	} else if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, helper)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cannot resolve npm's configured prefix: %w", err)
	}
	prefix := strings.TrimSpace(string(out))
	if !filepath.IsAbs(prefix) || strings.ContainsAny(prefix, "\r\n") {
		return "", fmt.Errorf("npm returned an invalid global prefix")
	}
	candidate := filepath.Join(prefix, "node_modules", "npm", "bin", name)
	info, err := os.Stat(candidate)
	if os.IsNotExist(err) {
		return entry, nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("npm's global CLI is not a regular file")
	}
	return candidate, nil
}
