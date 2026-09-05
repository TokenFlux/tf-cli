package config

import (
	"golang.org/x/sys/windows"
	"os"
	"testing"
)

func assertPermissions(t *testing.T, path string, _ os.FileMode) {
	t.Helper()
	want, err := privateSecurity(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := got.SetControl(windows.SE_DACL_AUTO_INHERITED, 0); err != nil {
		t.Fatal(err)
	}
	if got.String() != want.String() {
		t.Errorf("%s ACL=%s, want %s", path, got.String(), want.String())
	}
}

func loosenPermissions(t *testing.T, path string) {
	t.Helper()
	want, err := privateSecurity(path)
	if err != nil {
		t.Fatal(err)
	}
	loose, err := windows.SecurityDescriptorFromString(want.String() + "(A;;FR;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := loose.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}
