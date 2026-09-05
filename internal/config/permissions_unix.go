//go:build !windows

package config

import "os"

func ensurePermissions(path string, perm os.FileMode) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.Mode().Perm() == perm {
		return false, nil
	}
	err = os.Chmod(path, perm)
	return err == nil, err
}
