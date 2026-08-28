//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

func openRestrictedPasswordFile(path string) (*os.File, error) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return nil, fmt.Errorf("secure credential files are unsupported on %s", runtime.GOOS)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve credential path: %w", err)
	}
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(absPath), string(os.PathSeparator)), string(os.PathSeparator))
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return nil, fmt.Errorf("credential path does not name a file")
	}

	root, err := os.OpenRoot(string(os.PathSeparator))
	if err != nil {
		return nil, fmt.Errorf("open credential path root: %w", err)
	}
	defer func() {
		_ = root.Close()
	}()

	for _, part := range parts[:len(parts)-1] {
		before, err := root.Lstat(part)
		if err != nil {
			return nil, fmt.Errorf("inspect credential path component: %w", err)
		}
		if before.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("credential path contains a symbolic link")
		}
		if !before.IsDir() {
			return nil, fmt.Errorf("credential path component is not a directory")
		}

		next, err := root.OpenRoot(part)
		if err != nil {
			return nil, fmt.Errorf("open credential path component: %w", err)
		}
		after, err := next.Stat(".")
		if err != nil {
			_ = next.Close()
			return nil, fmt.Errorf("inspect opened credential path component: %w", err)
		}
		if !os.SameFile(before, after) {
			_ = next.Close()
			return nil, fmt.Errorf("credential path component changed while opening")
		}
		if err := root.Close(); err != nil {
			_ = next.Close()
			return nil, fmt.Errorf("close credential path component: %w", err)
		}
		root = next
	}

	name := parts[len(parts)-1]
	before, err := root.Lstat(name)
	if err != nil {
		return nil, fmt.Errorf("inspect credential file: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("credential file is a symbolic link")
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("credential path is not a regular file")
	}

	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened credential file: %w", err)
	}
	if !os.SameFile(before, after) {
		_ = file.Close()
		return nil, fmt.Errorf("credential file changed while opening")
	}
	owner, err := unixFileOwner(after)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := validateUnixCredentialMetadata(after.Mode(), owner, uint64(os.Geteuid())); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateUnixCredentialMetadata(mode os.FileMode, owner, effectiveUID uint64) error {
	if !mode.IsRegular() {
		return fmt.Errorf("credential path is not a regular file")
	}
	if mode.Perm()&^os.FileMode(0o600) != 0 {
		return fmt.Errorf("credential file permissions %o are too broad; require 0600 or stricter", mode.Perm())
	}
	if owner != effectiveUID {
		return fmt.Errorf("credential file is not owned by the current user")
	}
	return nil
}

func unixFileOwner(info os.FileInfo) (uint64, error) {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return 0, fmt.Errorf("credential file owner is unavailable")
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, fmt.Errorf("credential file owner is unavailable")
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return 0, fmt.Errorf("credential file owner is unavailable")
	}
	uid := value.FieldByName("Uid")
	if !uid.IsValid() || !uid.CanUint() {
		return 0, fmt.Errorf("credential file owner is unavailable")
	}
	return uid.Uint(), nil
}

func restrictPrivateDirectory(path string) error {
	return os.Chmod(path, 0o700)
}

func restrictPrivateFile(path string, mode os.FileMode) error {
	return os.Chmod(path, mode.Perm())
}

func syncParentDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}
