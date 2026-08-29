//go:build linux

package livegate

import (
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

func preparePrivateTestDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create private test directory: %v", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("protect private test directory: %v", err)
	}
}

func writePrivateTestReadableFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write private test file: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("protect private test file: %v", err)
	}
}
func writePrivateTestExecutableFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatalf("write private executable test file: %v", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("protect private executable test file: %v", err)
	}
}

func TestLinuxSecurityFingerprintIncludesChangeTime(t *testing.T) {
	original := unix.Stat_t{
		Uid:  1000,
		Gid:  1000,
		Mode: uint32(unix.S_IFREG | 0o600),
		Ctim: unix.Timespec{Sec: 123, Nsec: 456},
	}
	changed := original
	changed.Ctim.Nsec++

	if linuxSecurityFingerprint(original) == linuxSecurityFingerprint(changed) {
		t.Fatal("Linux private-file fingerprint ignored ctime drift")
	}
}

func removePrivateTestExecutePermission(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("remove owner execute permission: %v", err)
	}
}
