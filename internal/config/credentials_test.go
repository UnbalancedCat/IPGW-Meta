//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

const windowsCredentialPlaceholder = "test-credential-placeholder\n"

func TestReadRestrictedPasswordAcceptsProtectedCurrentUserFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.txt")
	writeWindowsCredentialTestFile(t, path)
	if err := setCurrentUserOnlyACL(path, windows.NO_INHERITANCE); err != nil {
		t.Fatalf("protect fixed test credential: %v", err)
	}
	got, err := readRestrictedPassword(path)
	if err != nil {
		t.Fatalf("readRestrictedPassword: %v", err)
	}
	if got != "test-credential-placeholder" {
		t.Fatalf("readRestrictedPassword() = %q, want fixed test placeholder", got)
	}
}

func TestReadRestrictedPasswordRejectsDefaultInheritedACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.txt")
	writeWindowsCredentialTestFile(t, path)

	if value, err := readRestrictedPassword(path); err == nil {
		t.Fatalf("readRestrictedPassword accepted a default inherited DACL and returned %q", value)
	}
}

func TestReadRestrictedPasswordRejectsFinalReparsePoint(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeWindowsCredentialTestFile(t, target)
	if err := setCurrentUserOnlyACL(target, windows.NO_INHERITANCE); err != nil {
		t.Fatalf("protect fixed test credential: %v", err)
	}

	link := filepath.Join(dir, "credential-link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("Windows symbolic-link creation is unavailable: %v", err)
	}
	if value, err := readRestrictedPassword(link); err == nil {
		t.Fatalf("readRestrictedPassword accepted a final reparse point and returned %q", value)
	}
}

func writeWindowsCredentialTestFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(windowsCredentialPlaceholder), 0o600); err != nil {
		t.Fatalf("write fixed test credential: %v", err)
	}
}
