//go:build !windows

package harness

func (h *Harness) DetectAfterInstall(_ InstallOption) (Status, error) {
	return h.Detect(), nil
}
