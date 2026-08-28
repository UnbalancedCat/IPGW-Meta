//go:build darwin

package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRestrictedPasswordAcceptsDarwinSystemVarAlias(t *testing.T) {
	dir := filepath.Clean(t.TempDir())
	if !strings.HasPrefix(dir, "/var/folders/") {
		t.Fatalf("standard macOS t.TempDir() = %q, want /var/folders prefix; TMPDIR must not be overridden", dir)
	}

	aliasPath := filepath.Join(dir, "credential.txt")
	writeUnixCredentialTestFile(t, aliasPath, 0o600)
	if got, err := readRestrictedPassword(aliasPath); err != nil {
		t.Fatalf("read credential through /var alias: %v", err)
	} else if got != "test-credential-placeholder" {
		t.Fatalf("alias credential = %q", got)
	}

	canonicalPath := filepath.Join("/private", strings.TrimPrefix(aliasPath, "/"))
	if got, err := readRestrictedPassword(canonicalPath); err != nil {
		t.Fatalf("read credential through canonical /private/var path: %v", err)
	} else if got != "test-credential-placeholder" {
		t.Fatalf("canonical credential = %q", got)
	}
}
