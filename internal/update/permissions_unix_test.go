//go:build !windows

package update

import (
	"os"
	"testing"
)

func makeUnwritable(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
}
