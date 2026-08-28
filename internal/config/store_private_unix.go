//go:build linux || darwin

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func openVerifiedStoreConfig(path string) (*os.File, error) {
	dir, err := openVerifiedUnixStoreDirectory(path, false)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = dir.Close()
	}()

	fd, err := unix.Openat(
		int(dir.Fd()),
		filepath.Base(path),
		unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("open config without following links: %w", err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open config from verified handle")
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened config: %w", err)
	}
	if err := validateUnixStoreFileMetadata(info, uint64(os.Geteuid())); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func openVerifiedUnixStoreDirectory(configPath string, create bool) (*os.File, error) {
	dirPath := filepath.Dir(configPath)
	if create {
		created, err := ensureUnixStoreDirectory(dirPath)
		if err != nil {
			return nil, err
		}
		if created {
			if err := restrictPrivateDirectory(dirPath); err != nil {
				return nil, fmt.Errorf("protect new config directory: %w", err)
			}
		}
	}

	fd, err := unix.Open(dirPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, fmt.Errorf("open config directory without following links: %w", err)
	}
	dir := os.NewFile(uintptr(fd), dirPath)
	if dir == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open config directory from verified handle")
	}
	info, err := dir.Stat()
	if err != nil {
		_ = dir.Close()
		return nil, fmt.Errorf("inspect opened config directory: %w", err)
	}
	if err := validateUnixStoreDirectoryMetadata(info, uint64(os.Geteuid())); err != nil {
		_ = dir.Close()
		return nil, err
	}
	return dir, nil
}

func ensureUnixStoreDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("config directory is a symbolic link")
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect config directory: %w", err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return false, fmt.Errorf("create config directory: %w", err)
	}
	return true, nil
}

func validateUnixStoreDirectoryMetadata(info os.FileInfo, effectiveUID uint64) error {
	if !info.IsDir() {
		return fmt.Errorf("config base path is not a directory")
	}
	owner, err := unixFileOwner(info)
	if err != nil {
		return fmt.Errorf("config directory owner is unavailable")
	}
	if owner != effectiveUID {
		return fmt.Errorf("config directory is not owned by the current user")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("config directory permissions %o allow group or other users to write", info.Mode().Perm())
	}
	return nil
}

func validateUnixStoreFileMetadata(info os.FileInfo, effectiveUID uint64) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config path is not a regular file")
	}
	owner, err := unixFileOwner(info)
	if err != nil {
		return fmt.Errorf("config file owner is unavailable")
	}
	if owner != effectiveUID {
		return fmt.Errorf("config file is not owned by the current user")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("config file permissions %o allow group or other users to write", info.Mode().Perm())
	}
	return nil
}
