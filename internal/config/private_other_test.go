//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteAppliesPrivateUnixModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private-state")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatalf("create test directory: %v", err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatalf("broaden test directory mode: %v", err)
	}
	path := filepath.Join(dir, "state.yaml")
	writeAtomicTestValue(t, path, "state-v1\n")
	writeAtomicTestValue(t, path, "state-v2\n")

	assertUnixMode(t, dir, 0o700)
	assertUnixMode(t, path, 0o600)
	assertUnixMode(t, path+".lkg", 0o600)
}

func TestSyncParentDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := syncParentDirectory(dir); err != nil {
		t.Fatalf("syncParentDirectory: %v", err)
	}
}

func assertUnixMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", filepath.Base(path), err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode for %q = %#o, want %#o", filepath.Base(path), got, want)
	}
}

func assertPrivateMigrationDirectory(t *testing.T, path string) {
	t.Helper()
	assertUnixMode(t, path, 0o700)
}
