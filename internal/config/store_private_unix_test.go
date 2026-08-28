//go:build linux || darwin

package config

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStoreLoadUnixRejectsWritableConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	store := &Store{Path: path}
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := os.Chmod(path, 0o620); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("Load() accepted a group-writable config")
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("Save() replaced a group-writable config")
	}
}

func TestStoreLoadUnixAllowsReadOnlySharing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	store := &Store{Path: path}
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	if _, found, err := store.Load(); err != nil || !found {
		t.Fatalf("Load() = found %v, error %v", found, err)
	}
}

func TestStoreLoadUnixRejectsSymlinkAndNonRegularFiles(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatalf("create config base: %v", err)
	}
	target := filepath.Join(base, "target.yaml")
	if err := os.WriteFile(target, []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(base, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create config symlink: %v", err)
	}
	if _, _, err := (&Store{Path: link}).Load(); err == nil {
		t.Fatal("Load() accepted a config symlink")
	}

	directory := filepath.Join(base, "directory.yaml")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("create config directory entry: %v", err)
	}
	if _, _, err := (&Store{Path: directory}).Load(); err == nil {
		t.Fatal("Load() accepted a directory as config")
	}

	fifo := filepath.Join(base, "fifo.yaml")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("create config FIFO: %v", err)
	}
	if _, _, err := (&Store{Path: fifo}).Load(); err == nil {
		t.Fatal("Load() accepted a FIFO as config")
	}
}

func TestStoreUnixRejectsUnsafeBaseDirectory(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatalf("create config base: %v", err)
	}
	path := filepath.Join(base, "config.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(base, 0o770); err != nil {
		t.Fatalf("chmod config base: %v", err)
	}
	store := &Store{Path: path}
	if _, _, err := store.Load(); err == nil {
		t.Fatal("Load() accepted a group-writable config directory")
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("Save() accepted a group-writable config directory")
	}
}

func TestStoreUnixRejectsSymlinkBaseDirectory(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create target directory: %v", err)
	}
	path := filepath.Join(target, "config.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\n"), 0o600); err != nil {
		t.Fatalf("write target config: %v", err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create directory symlink: %v", err)
	}
	if _, _, err := (&Store{Path: filepath.Join(link, "config.yaml")}).Load(); err == nil {
		t.Fatal("Load() accepted a symlink config directory")
	}
}

func TestStoreUnixLockMustBePrivateAndNotSymlink(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config")
	path := filepath.Join(base, "config.yaml")
	store := &Store{Path: path}
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	lockPath := path + ".lock"
	info, err := os.Stat(lockPath)
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("lock mode = %o, want 600", got)
	}
	if err := os.Chmod(lockPath, 0o666); err != nil {
		t.Fatalf("broaden lock mode: %v", err)
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("Save() accepted a non-private lock file")
	}

	if err := os.Remove(lockPath); err != nil {
		t.Fatalf("remove lock: %v", err)
	}
	target := filepath.Join(base, "lock-target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("write lock target: %v", err)
	}
	if err := os.Symlink(target, lockPath); err != nil {
		t.Fatalf("create lock symlink: %v", err)
	}
	if err := store.Save(Default()); err == nil {
		t.Fatal("Save() accepted a symlink lock file")
	}
}

func TestValidateUnixStoreMetadataRejectsWrongOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if err := validateUnixStoreFileMetadata(info, uint64(os.Geteuid())+1); err == nil {
		t.Fatal("validateUnixStoreFileMetadata accepted the wrong owner")
	}
	if err := validateUnixStoreDirectoryMetadata(info, uint64(os.Geteuid())); err == nil {
		t.Fatal("validateUnixStoreDirectoryMetadata accepted a regular file")
	}
}
