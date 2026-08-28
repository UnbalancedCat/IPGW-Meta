//go:build windows

package config

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

type windowsStoreMutationLock struct {
	handle    windows.Handle
	directory windows.Handle
}

func acquireStoreMutationLock(configPath string) (*windowsStoreMutationLock, error) {
	if err := ensureWindowsStoreDirectory(configPath); err != nil {
		return nil, err
	}
	directory, err := openVerifiedWindowsStoreDirectory(
		configPath,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
	)
	if err != nil {
		return nil, err
	}
	closeDirectory := true
	defer func() {
		if closeDirectory {
			_ = windows.CloseHandle(directory)
		}
	}()

	lockPath := configPath + ".lock"
	var handle windows.Handle
	for {
		name, encodeErr := windows.UTF16PtrFromString(lockPath)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode fixed config lock path: %w", encodeErr)
		}
		handle, err = windows.CreateFile(
			name,
			windows.GENERIC_READ|windows.GENERIC_WRITE|windows.READ_CONTROL|windows.WRITE_DAC,
			0,
			nil,
			windows.OPEN_ALWAYS,
			windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
			0,
		)
		if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		return nil, fmt.Errorf("open fixed config lock exclusively: %w", err)
	}
	closeHandle := true
	defer func() {
		if closeHandle {
			_ = windows.CloseHandle(handle)
		}
	}()
	if err := validateWindowsStoreHandle(handle, false, "config lock"); err != nil {
		return nil, err
	}
	if err := restrictPrivateFile(lockPath, 0o600); err != nil {
		return nil, fmt.Errorf("protect fixed config lock: %w", err)
	}
	if err := validateRestrictedPasswordDACL(handle); err != nil {
		return nil, fmt.Errorf("validate config lock DACL: %w", err)
	}

	closeHandle = false
	closeDirectory = false
	return &windowsStoreMutationLock{handle: handle, directory: directory}, nil
}

func (lock *windowsStoreMutationLock) Close() error {
	if lock == nil {
		return nil
	}
	var first error
	if lock.handle != 0 && lock.handle != windows.InvalidHandle {
		first = windows.CloseHandle(lock.handle)
		lock.handle = windows.InvalidHandle
	}
	if lock.directory != 0 && lock.directory != windows.InvalidHandle {
		if err := windows.CloseHandle(lock.directory); first == nil {
			first = err
		}
		lock.directory = windows.InvalidHandle
	}
	return first
}
