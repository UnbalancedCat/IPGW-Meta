//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRestrictedPasswordAcceptsPrivateCurrentUserFile(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o400} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "credential.txt")
			writeUnixCredentialTestFile(t, path, mode)
			got, err := readRestrictedPassword(path)
			if err != nil {
				t.Fatalf("readRestrictedPassword: %v", err)
			}
			if got != "test-credential-placeholder" {
				t.Fatalf("readRestrictedPassword() = %q, want fixed placeholder", got)
			}
		})
	}
}

func TestReadRestrictedPasswordRejectsBroadUnixModes(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o660} {
		t.Run(mode.String(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "credential.txt")
			writeUnixCredentialTestFile(t, path, mode)
			if value, err := readRestrictedPassword(path); err == nil {
				t.Fatalf("readRestrictedPassword accepted mode %#o and returned %q", mode, value)
			}
		})
	}
}

func TestReadRestrictedPasswordRejectsFinalUnixSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	writeUnixCredentialTestFile(t, target, 0o600)
	link := filepath.Join(dir, "credential-link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create credential symlink: %v", err)
	}
	if value, err := readRestrictedPassword(link); err == nil {
		t.Fatalf("readRestrictedPassword accepted a final symlink and returned %q", value)
	}
}

func TestReadRestrictedPasswordRejectsUnixParentSymlink(t *testing.T) {
	base := t.TempDir()
	targetDir := filepath.Join(base, "target")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatalf("create target directory: %v", err)
	}
	writeUnixCredentialTestFile(t, filepath.Join(targetDir, "credential.txt"), 0o600)
	linkDir := filepath.Join(base, "linked-directory")
	if err := os.Symlink(targetDir, linkDir); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}
	if value, err := readRestrictedPassword(filepath.Join(linkDir, "credential.txt")); err == nil {
		t.Fatalf("readRestrictedPassword accepted a parent symlink and returned %q", value)
	}
}

func TestReadRestrictedPasswordRejectsUnixDirectory(t *testing.T) {
	if value, err := readRestrictedPassword(t.TempDir()); err == nil {
		t.Fatalf("readRestrictedPassword accepted a directory and returned %q", value)
	}
}

func TestValidateUnixCredentialMetadata(t *testing.T) {
	tests := []struct {
		name         string
		mode         os.FileMode
		owner        uint64
		effectiveUID uint64
		wantErr      bool
	}{
		{name: "0600", mode: 0o600, owner: 1000, effectiveUID: 1000},
		{name: "0400", mode: 0o400, owner: 1000, effectiveUID: 1000},
		{name: "group readable", mode: 0o640, owner: 1000, effectiveUID: 1000, wantErr: true},
		{name: "owner executable", mode: 0o700, owner: 1000, effectiveUID: 1000, wantErr: true},
		{name: "directory", mode: os.ModeDir | 0o600, owner: 1000, effectiveUID: 1000, wantErr: true},
		{name: "different owner", mode: 0o600, owner: 1001, effectiveUID: 1000, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateUnixCredentialMetadata(test.mode, test.owner, test.effectiveUID)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateUnixCredentialMetadata() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestUnixFileOwnerReportsCurrentUser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.txt")
	writeUnixCredentialTestFile(t, path, 0o600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credential: %v", err)
	}
	owner, err := unixFileOwner(info)
	if err != nil {
		t.Fatalf("unixFileOwner: %v", err)
	}
	if want := uint64(os.Geteuid()); owner != want {
		t.Fatalf("unixFileOwner() = %d, want %d", owner, want)
	}
}

func writeUnixCredentialTestFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("test-credential-placeholder\n"), mode); err != nil {
		t.Fatalf("write fixed test credential: %v", err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("set credential mode: %v", err)
	}
}
