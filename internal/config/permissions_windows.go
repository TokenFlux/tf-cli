package config

import (
	"fmt"
	"golang.org/x/sys/windows"
	"os"
)

// Windows mode bits do not restrict access. Protect the DACL instead, allowing
// only the current account, Administrators and SYSTEM, with directory inheritance.
func privateSecurity(path string) (*windows.SECURITY_DESCRIPTOR, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	flags := ""
	if info.IsDir() {
		flags = "OICI"
	}
	return windows.SecurityDescriptorFromString(fmt.Sprintf("D:P(A;%s;FA;;;SY)(A;%s;FA;;;BA)(A;%s;FA;;;%s)", flags, flags, flags, user.User.Sid.String()))
}

func ensurePermissions(path string, _ os.FileMode) (bool, error) {
	want, err := privateSecurity(path)
	if err != nil {
		return false, err
	}
	got, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	// AUTO_INHERITED records ACL processing history, not additional access.
	if err := got.SetControl(windows.SE_DACL_AUTO_INHERITED, 0); err != nil {
		return false, err
	}
	if got.String() == want.String() {
		return false, nil
	}
	acl, _, err := want.DACL()
	if err != nil {
		return false, err
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
	return err == nil, err
}
