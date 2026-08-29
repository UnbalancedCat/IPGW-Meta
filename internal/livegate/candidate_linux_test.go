//go:build linux

package livegate

import (
	"os"
	"testing"
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

func removePrivateTestExecutePermission(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("remove owner execute permission: %v", err)
	}
}
