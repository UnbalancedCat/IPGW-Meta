//go:build windows

package livegate

import (
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestValidatePrivateWindowsSecurityDescriptorOwner(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current Windows user: %v", err)
	}
	currentSID := user.User.Sid
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatalf("create SYSTEM SID: %v", err)
	}
	administratorsSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatalf("create Administrators SID: %v", err)
	}

	dacl := fmt.Sprintf(
		"D:P(A;;GA;;;%s)(A;;GR;;;%s)(A;;GR;;;%s)",
		currentSID.String(),
		systemSID.String(),
		administratorsSID.String(),
	)
	currentOwner, err := windows.SecurityDescriptorFromString("O:" + currentSID.String() + dacl)
	if err != nil {
		t.Fatalf("create current-owner security descriptor: %v", err)
	}
	if _, err := validatePrivateWindowsSecurityDescriptor(currentOwner, currentSID, false, true); err != nil {
		t.Fatalf("validate current-owner security descriptor: %v", err)
	}

	for _, test := range []struct {
		name  string
		owner *windows.SID
	}{
		{name: "system", owner: systemSID},
		{name: "administrators", owner: administratorsSID},
	} {
		t.Run(test.name, func(t *testing.T) {
			descriptor, err := windows.SecurityDescriptorFromString("O:" + test.owner.String() + dacl)
			if err != nil {
				t.Fatalf("create foreign-owner security descriptor: %v", err)
			}
			if _, err := validatePrivateWindowsSecurityDescriptor(descriptor, currentSID, false, true); err != errPrivateCandidateFile {
				t.Fatalf("validate foreign-owner security descriptor error = %v, want exact errPrivateCandidateFile", err)
			}
		})
	}
}

func TestValidatePrivateWindowsSecurityDescriptorRejectsDefaultedOwner(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current Windows user: %v", err)
	}
	currentSID := user.User.Sid
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(currentSID),
		},
	}}, nil)
	if err != nil {
		t.Fatalf("create current-user ACL: %v", err)
	}
	descriptor, err := windows.NewSecurityDescriptor()
	if err != nil {
		t.Fatalf("create absolute security descriptor: %v", err)
	}
	if err := descriptor.SetOwner(currentSID, true); err != nil {
		t.Fatalf("set defaulted owner: %v", err)
	}
	if err := descriptor.SetDACL(acl, true, false); err != nil {
		t.Fatalf("set DACL: %v", err)
	}
	if err := descriptor.SetControl(windows.SE_DACL_PROTECTED, windows.SE_DACL_PROTECTED); err != nil {
		t.Fatalf("protect DACL: %v", err)
	}
	if _, err := validatePrivateWindowsSecurityDescriptor(descriptor, currentSID, false, true); err != errPrivateCandidateFile {
		t.Fatalf("validate defaulted-owner security descriptor error = %v, want exact errPrivateCandidateFile", err)
	}
}

func preparePrivateTestDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create private test directory: %v", err)
	}
	setPrivateTestACL(
		t,
		path,
		windows.ACCESS_MASK(windows.GENERIC_ALL),
		windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
	)
}

func writePrivateTestReadableFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write private test file: %v", err)
	}
	setPrivateTestACL(
		t,
		path,
		windows.ACCESS_MASK(windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC),
		windows.NO_INHERITANCE,
	)
}
func writePrivateTestExecutableFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write private executable test file: %v", err)
	}
	setPrivateTestACL(
		t,
		path,
		windows.ACCESS_MASK(windows.GENERIC_ALL),
		windows.NO_INHERITANCE,
	)
}

func removePrivateTestExecutePermission(t *testing.T, path string) {
	t.Helper()
	setPrivateTestACL(
		t,
		path,
		windows.ACCESS_MASK(windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC),
		windows.NO_INHERITANCE,
	)
}

func setPrivateTestACL(
	t *testing.T,
	path string,
	access windows.ACCESS_MASK,
	inheritance uint32,
) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current Windows user: %v", err)
	}
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: access,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatalf("create private test ACL: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		t.Fatalf("apply private test ACL: %v", err)
	}
}
