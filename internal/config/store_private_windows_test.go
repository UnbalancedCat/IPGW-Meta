//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestStoreWindowsLockUsesProtectedCurrentUserDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	if err := (&Store{Path: path}).Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	assertCurrentUserOnlyACL(t, filepath.Dir(path), true)
	assertCurrentUserOnlyACL(t, path, false)
	assertCurrentUserOnlyACL(t, path+".lock", false)
}

func TestStoreLoadWindowsRejectsConfigReparsePoint(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config")
	targetPath := filepath.Join(base, "target.yaml")
	if err := (&Store{Path: targetPath}).Save(Default()); err != nil {
		t.Fatalf("save target config: %v", err)
	}
	linkPath := filepath.Join(base, "linked.yaml")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("Windows file symlink creation is unavailable: %v", err)
	}
	if _, _, err := (&Store{Path: linkPath}).Load(); err == nil {
		t.Fatal("Load() accepted a config reparse point")
	}
}

func TestStoreLoadWindowsRejectsDirectoryReparsePoint(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	path := filepath.Join(target, "config.yaml")
	if err := (&Store{Path: path}).Save(Default()); err != nil {
		t.Fatalf("save target config: %v", err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("Windows directory symlink creation is unavailable: %v", err)
	}
	if _, _, err := (&Store{Path: filepath.Join(link, "config.yaml")}).Load(); err == nil {
		t.Fatal("Load() accepted a directory reparse point")
	}
}

func TestStoreLoadWindowsRejectsDirectoryAsConfig(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config")
	path := filepath.Join(base, "initial.yaml")
	if err := (&Store{Path: path}).Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	directory := filepath.Join(base, "directory.yaml")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if _, _, err := (&Store{Path: directory}).Load(); err == nil {
		t.Fatal("Load() accepted a directory as config")
	}
}

func TestStoreLoadWindowsRejectsUntrustedDirectoryDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	store := &Store{Path: path}
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	setWindowsTestDACLWithWorld(t, filepath.Dir(path))
	if _, _, err := store.Load(); err == nil {
		t.Fatal("Load() accepted an untrusted config directory DACL")
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("Save() accepted an untrusted config directory DACL")
	}
}

func TestStoreLoadWindowsRejectsUntrustedConfigDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	store := &Store{Path: path}
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	setWindowsTestDACLWithWorld(t, path)
	if _, _, err := store.Load(); err == nil {
		t.Fatal("Load() accepted an untrusted config file DACL")
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("Save() replaced a config with an untrusted DACL")
	}
}

func TestStoreSaveWindowsRejectsLockReparsePoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	store := &Store{Path: path}
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	lockPath := path + ".lock"
	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove persistent lock: %v", err)
	}
	target := filepath.Join(filepath.Dir(path), "lock-target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("write lock target: %v", err)
	}
	if err := restrictPrivateFile(target, 0o600); err != nil {
		t.Fatalf("protect lock target: %v", err)
	}
	if err := os.Symlink(target, lockPath); err != nil {
		t.Skipf("Windows file symlink creation is unavailable: %v", err)
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("Save() accepted a lock reparse point")
	}
}

func setWindowsTestDACLWithWorld(t *testing.T, path string) {
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
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
			},
		},
		{
			AccessPermissions: windows.GENERIC_READ,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(world),
			},
		},
	}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		t.Fatalf("build test ACL: %v", err)
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
		t.Fatalf("set untrusted test DACL: %v", err)
	}
}
