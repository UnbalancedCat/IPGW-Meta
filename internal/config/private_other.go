//go:build !windows

package config

import (
	"fmt"
	"os"
	"reflect"
)

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
