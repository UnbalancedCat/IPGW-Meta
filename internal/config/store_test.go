package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteRotatesLastKnownGood(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private-state")
	path := filepath.Join(dir, "state.yaml")

	writeAtomicTestValue(t, path, "state-v1\n")
	assertFileValue(t, path, "state-v1\n")
	if _, err := os.Stat(path + ".lkg"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first write unexpectedly created last-known-good file: %v", err)
	}

	writeAtomicTestValue(t, path, "state-v2\n")
	assertFileValue(t, path, "state-v2\n")
	assertFileValue(t, path+".lkg", "state-v1\n")

	writeAtomicTestValue(t, path, "state-v3\n")
	assertFileValue(t, path, "state-v3\n")
	assertFileValue(t, path+".lkg", "state-v2\n")
	assertNoAtomicTemps(t, dir)
}

func TestAtomicWriteBackupFailurePreservesDestination(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private-state")
	path := filepath.Join(dir, "state.yaml")
	writeAtomicTestValue(t, path, "stable-state\n")

	// A directory at the fixed backup path makes its atomic replacement fail
	// after the new primary temp file has been prepared and synced.
	if err := os.Mkdir(path+".lkg", 0o700); err != nil {
		t.Fatalf("create blocking backup directory: %v", err)
	}
	if err := atomicWrite(path, []byte("uncommitted-state\n"), 0o600); err == nil {
		t.Fatal("atomicWrite succeeded despite an unusable backup destination")
	}

	assertFileValue(t, path, "stable-state\n")
	info, err := os.Stat(path + ".lkg")
	if err != nil {
		t.Fatalf("stat blocking backup directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("failed write replaced the blocking backup directory")
	}
	assertNoAtomicTemps(t, dir)
}

func TestConfigWriteClassificationDistinguishesPrimaryFromBackupPublication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cause := errors.New("synthetic directory sync failure")
	primary := classifyConfigWriteError(path, &publishedWriteError{path: path, cause: cause})
	if !errors.Is(primary, ErrConfigPublishedDurabilityUnknown) {
		t.Fatalf("primary publication error = %v", primary)
	}
	backup := classifyConfigWriteError(path, &publishedWriteError{path: path + ".lkg", cause: cause})
	if errors.Is(backup, ErrConfigPublishedDurabilityUnknown) {
		t.Fatalf("backup publication was misclassified as primary config commit: %v", backup)
	}
	if !errors.Is(backup, cause) {
		t.Fatalf("backup publication lost its underlying error: %v", backup)
	}
}

func writeAtomicTestValue(t *testing.T, path, value string) {
	t.Helper()
	if err := atomicWrite(path, []byte(value), 0o600); err != nil {
		t.Fatalf("atomicWrite(%q): %v", filepath.Base(path), err)
	}
}

func assertFileValue(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", filepath.Base(path), err)
	}
	if got := string(data); got != want {
		t.Fatalf("%q content = %q, want %q", filepath.Base(path), got, want)
	}
}

func assertNoAtomicTemps(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".ipgw-meta-*.tmp"))
	if err != nil {
		t.Fatalf("glob temporary files: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("atomic write left temporary files behind: %v", matches)
	}
}
