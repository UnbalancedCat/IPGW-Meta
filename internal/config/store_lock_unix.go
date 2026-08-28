//go:build linux || darwin

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type unixStoreMutationLock struct {
	file *os.File
}

func acquireStoreMutationLock(configPath string) (*unixStoreMutationLock, error) {
	dir, err := openVerifiedUnixStoreDirectory(configPath, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = dir.Close()
	}()

	lockName := filepath.Base(configPath) + ".lock"
	fd, err := unix.Openat(
		int(dir.Fd()),
		lockName,
		unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK,
		0o600,
	)
	if err != nil {
		return nil, fmt.Errorf("open fixed config lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), filepath.Join(filepath.Dir(configPath), lockName))
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("open config lock from verified handle")
	}
	closeOnError := true
	defer func() {
		if closeOnError {
			_ = file.Close()
		}
	}()

	if err := validateUnixStoreLock(file); err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(fd, unix.LOCK_EX)
		if err != unix.EINTR {
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("lock fixed config lock: %w", err)
	}
	if err := validateUnixStoreLock(file); err != nil {
		_ = unix.Flock(fd, unix.LOCK_UN)
		return nil, err
	}
	closeOnError = false
	return &unixStoreMutationLock{file: file}, nil
}

func validateUnixStoreLock(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect config lock: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config lock is not a regular file")
	}
	owner, err := unixFileOwner(info)
	if err != nil {
		return fmt.Errorf("config lock owner is unavailable")
	}
	if owner != uint64(os.Geteuid()) {
		return fmt.Errorf("config lock is not owned by the current user")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("config lock permissions %o are not private", info.Mode().Perm())
	}
	return nil
}

func (lock *unixStoreMutationLock) Close() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	fd := int(lock.file.Fd())
	unlockErr := unix.Flock(fd, unix.LOCK_UN)
	closeErr := lock.file.Close()
	lock.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
