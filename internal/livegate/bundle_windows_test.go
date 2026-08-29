//go:build windows

package livegate

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestSetBundleCurrentUserOnlyACLAssignsCurrentOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner-check")
	if err := os.WriteFile(path, []byte("owner"), 0o600); err != nil {
		t.Fatalf("create owner fixture: %v", err)
	}
	if err := setBundleCurrentUserOnlyACL(path, windows.NO_INHERITANCE); err != nil {
		t.Fatalf("set bundle security: %v", err)
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("read bundle owner: %v", err)
	}
	owner, defaulted, err := descriptor.Owner()
	if err != nil || owner == nil || defaulted {
		t.Fatalf("read explicit bundle owner: owner=%v defaulted=%v err=%v", owner, defaulted, err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current user: %v", err)
	}
	if !owner.Equals(user.User.Sid) {
		t.Fatal("bundle setter did not assign the current user as owner")
	}
}

func TestEnsureBundlePrivateDirectoryConcurrentExistingPreservesACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-private")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create shared directory: %v", err)
	}
	setPrivateTestACL(
		t,
		path,
		windows.ACCESS_MASK(windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC),
		windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
	)
	before := bundleWindowsTestSDDL(t, path)
	bundleWindowsTestConcurrentEnsure(t, path, 64)
	after := bundleWindowsTestSDDL(t, path)
	if after != before {
		t.Fatal("concurrent ensure rewrote an existing private directory ACL")
	}
	if err := verifyBundlePrivateDirectory(path); err != nil {
		t.Fatalf("verify shared private directory: %v", err)
	}
}

func TestEnsureBundlePrivateDirectoryExistingNonPrivateFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shared-non-private")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("create shared directory: %v", err)
	}
	bundleWindowsTestSetNonPrivateACL(t, path)
	before := bundleWindowsTestSDDL(t, path)
	if err := ensureBundlePrivateDirectory(path); err != ErrEvidenceDurability {
		t.Fatalf("ensure non-private directory error = %v, want exact ErrEvidenceDurability", err)
	}
	after := bundleWindowsTestSDDL(t, path)
	if after != before {
		t.Fatal("failed ensure rewrote a non-private directory ACL")
	}
}

func TestEnsureBundlePrivateDirectoryConcurrentCreateIsAtomicAndPreservesOuterACL(t *testing.T) {
	outer := filepath.Join(t.TempDir(), "build")
	if err := os.Mkdir(outer, 0o700); err != nil {
		t.Fatalf("create outer directory: %v", err)
	}
	beforeOuter := bundleWindowsTestSDDL(t, outer)
	target := filepath.Join(outer, "live-evidence")
	bundleWindowsTestConcurrentEnsure(t, target, 64)
	if got := bundleWindowsTestSDDL(t, outer); got != beforeOuter {
		t.Fatal("ensure rewrote the existing outer build ACL")
	}
	bundleWindowsTestRequireAtomicPrivateDirectory(t, target)
}

func TestCreateBundleStagingDirectoryConcurrentAtomicAndPreservesParentACL(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "build", "live-evidence")
	if err := ensureBundlePrivateDirectory(parent); err != nil {
		t.Fatalf("ensure parent: %v", err)
	}
	setPrivateTestACL(
		t,
		parent,
		windows.ACCESS_MASK(
			windows.GENERIC_READ|
				windows.GENERIC_WRITE|
				windows.GENERIC_EXECUTE|
				windows.READ_CONTROL|
				windows.WRITE_DAC,
		),
		windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
	)
	before := bundleWindowsTestSDDL(t, parent)

	const workers = 32
	paths := make([]string, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := range paths {
		go func(index int) {
			defer wait.Done()
			<-start
			paths[index], errs[index] = createBundleStagingDirectory(parent)
		}(index)
	}
	close(start)
	wait.Wait()

	seen := make(map[string]struct{}, workers)
	for index, stage := range paths {
		if errs[index] != nil {
			t.Fatalf("create staging[%d]: %v", index, errs[index])
		}
		if filepath.Dir(stage) != parent ||
			!strings.HasPrefix(filepath.Base(stage), ".livegate-stage-") {
			t.Fatal("invalid staging path shape")
		}
		if _, duplicate := seen[stage]; duplicate {
			t.Fatal("duplicate staging directory")
		}
		seen[stage] = struct{}{}
		bundleWindowsTestRequireAtomicPrivateDirectory(t, stage)
		if err := os.Remove(stage); err != nil {
			t.Fatalf("remove empty staging directory: %v", err)
		}
	}
	if got := bundleWindowsTestSDDL(t, parent); got != before {
		t.Fatal("staging creation rewrote the shared parent ACL")
	}
}

func bundleWindowsTestConcurrentEnsure(t *testing.T, path string, workers int) {
	t.Helper()
	errs := make([]error, workers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := range errs {
		go func(index int) {
			defer wait.Done()
			<-start
			errs[index] = ensureBundlePrivateDirectory(path)
		}(index)
	}
	close(start)
	wait.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("ensure[%d]: %v", index, err)
		}
	}
}

func bundleWindowsTestSDDL(t *testing.T, path string) string {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|
			windows.GROUP_SECURITY_INFORMATION|
			windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("read security descriptor: %v", err)
	}
	value := descriptor.String()
	if value == "" {
		t.Fatal("empty security descriptor string")
	}
	return value
}

func bundleWindowsTestRequireAtomicPrivateDirectory(t *testing.T, path string) {
	t.Helper()
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("read private directory descriptor: %v", err)
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil {
		t.Fatalf("read private directory owner: %v", err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || !owner.Equals(user.User.Sid) {
		t.Fatal("private directory owner is not current user")
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("private directory DACL is not protected")
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil || dacl == nil || defaulted || dacl.AceCount != 1 {
		t.Fatal("private directory DACL is not one explicit ACE")
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil ||
		ace == nil ||
		ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
		uintptr(ace.Header.AceSize) < unsafe.Offsetof(ace.SidStart)+unsafe.Sizeof(ace.SidStart) {
		t.Fatal("private directory ACE is invalid")
	}
	wantFlags := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
	if ace.Header.AceFlags != wantFlags {
		t.Fatalf(
			"private directory ACE flags = %#x, want OICI %#x",
			ace.Header.AceFlags,
			wantFlags,
		)
	}
	sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	if !sid.IsValid() || !sid.Equals(user.User.Sid) {
		t.Fatal("private directory ACE is not current user")
	}
	if err := verifyBundlePrivateDirectory(path); err != nil {
		t.Fatalf("verify private directory: %v", err)
	}
}

func bundleWindowsTestSetNonPrivateACL(t *testing.T, path string) {
	t.Helper()
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current user: %v", err)
	}
	world, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatalf("create world SID: %v", err)
	}
	entries := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
			},
		},
		{
			AccessPermissions: windows.GENERIC_READ,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(world),
			},
		},
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatalf("build non-private ACL: %v", err)
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
		t.Fatalf("apply non-private ACL: %v", err)
	}
}
