//go:build windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

const windowsStoreReadSharing = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

func openVerifiedStoreConfig(path string) (*os.File, error) {
	prefixes, err := restrictedWindowsPathPrefixes(path)
	if err != nil {
		return nil, fmt.Errorf("validate config path: %w", err)
	}
	if err := validateRestrictedWindowsParents(prefixes[:len(prefixes)-1]); err != nil {
		return nil, fmt.Errorf("validate config path: %w", err)
	}
	dir, err := openVerifiedWindowsStoreDirectory(path, windowsStoreReadSharing)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(dir)

	handle, err := openRestrictedWindowsPath(
		path,
		uint32(windows.GENERIC_READ|windows.READ_CONTROL),
		windowsStoreReadSharing,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
	)
	if err != nil {
		return nil, fmt.Errorf("open config without following reparse points: %w", err)
	}
	closeHandle := true
	defer func() {
		if closeHandle {
			_ = windows.CloseHandle(handle)
		}
	}()
	if err := validateWindowsStoreHandle(handle, false, "config file"); err != nil {
		return nil, err
	}
	if err := validateRestrictedPasswordDACL(handle); err != nil {
		return nil, fmt.Errorf("validate config file DACL: %w", err)
	}
	if err := validateRestrictedWindowsParents(prefixes[:len(prefixes)-1]); err != nil {
		return nil, fmt.Errorf("revalidate config path: %w", err)
	}

	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		return nil, fmt.Errorf("open config from verified handle")
	}
	closeHandle = false
	return file, nil
}

func ensureWindowsStoreDirectory(configPath string) error {
	dirPath := filepath.Dir(configPath)
	_, err := os.Lstat(dirPath)
	created := false
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dirPath, 0o700); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
		created = true
	} else if err != nil {
		return fmt.Errorf("inspect config directory: %w", err)
	}
	if created {
		if err := restrictPrivateDirectory(dirPath); err != nil {
			return fmt.Errorf("protect new config directory: %w", err)
		}
	}
	handle, err := openVerifiedWindowsStoreDirectory(configPath, windowsStoreReadSharing)
	if err != nil {
		return err
	}
	return windows.CloseHandle(handle)
}

func openVerifiedWindowsStoreDirectory(configPath string, share uint32) (windows.Handle, error) {
	prefixes, err := restrictedWindowsPathPrefixes(configPath)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("validate config path: %w", err)
	}
	if len(prefixes) < 2 {
		return windows.InvalidHandle, fmt.Errorf("config path does not have a base directory")
	}
	if err := validateRestrictedWindowsParents(prefixes[:len(prefixes)-1]); err != nil {
		return windows.InvalidHandle, fmt.Errorf("validate config directory: %w", err)
	}
	dirPath := filepath.Dir(configPath)
	handle, err := openRestrictedWindowsPath(
		dirPath,
		uint32(windows.GENERIC_READ|windows.READ_CONTROL),
		share,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
	)
	if err != nil {
		return windows.InvalidHandle, fmt.Errorf("open config directory without following reparse points: %w", err)
	}
	if err := validateWindowsStoreHandle(handle, true, "config directory"); err != nil {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, err
	}
	if err := validateWindowsStoreDirectoryDACL(handle); err != nil {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, err
	}
	return handle, nil
}

func validateWindowsStoreHandle(handle windows.Handle, wantDirectory bool, label string) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return fmt.Errorf("inspect %s: %w", label, err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("%s is a reparse point", label)
	}
	isDirectory := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if isDirectory != wantDirectory {
		if wantDirectory {
			return fmt.Errorf("config base path is not a directory")
		}
		return fmt.Errorf("config path is not a regular file")
	}
	fileType, err := windows.GetFileType(handle)
	if err != nil {
		return fmt.Errorf("inspect %s type: %w", label, err)
	}
	if fileType != windows.FILE_TYPE_DISK {
		return fmt.Errorf("%s is not a disk file", label)
	}
	return nil
}

func validateWindowsStoreDirectoryDACL(handle windows.Handle) error {
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read config directory DACL: %w", err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return fmt.Errorf("read config directory DACL control: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("config directory DACL is inherited; require a protected DACL")
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read config directory DACL: %w", err)
	}
	if dacl == nil || defaulted || dacl.AceCount == 0 {
		return fmt.Errorf("config directory DACL is absent, defaulted, or empty")
	}

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read current Windows user: %w", err)
	}
	currentSID := user.User.Sid
	var currentMask windows.ACCESS_MASK
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			return fmt.Errorf("read config directory DACL entry: %w", err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("config directory DACL contains a non-allow or unknown ACE")
		}
		allowedFlags := uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE | windows.INHERIT_ONLY_ACE)
		if ace.Header.AceFlags&windows.INHERITED_ACE != 0 || ace.Header.AceFlags & ^allowedFlags != 0 {
			return fmt.Errorf("config directory DACL contains an inherited or unexpected ACE flags %#x", ace.Header.AceFlags)
		}
		if uintptr(ace.Header.AceSize) < unsafe.Offsetof(ace.SidStart)+unsafe.Sizeof(ace.SidStart) {
			return fmt.Errorf("config directory DACL contains a malformed ACE")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return fmt.Errorf("config directory DACL contains an invalid SID")
		}
		isCurrentUser := sid.Equals(currentSID)
		if !isCurrentUser && !sid.IsWellKnown(windows.WinLocalSystemSid) && !sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			return fmt.Errorf("config directory DACL grants access to an untrusted principal")
		}
		if isCurrentUser && ace.Header.AceFlags&windows.INHERIT_ONLY_ACE == 0 {
			currentMask |= ace.Mask
		}
	}

	required := windows.ACCESS_MASK(windows.FILE_LIST_DIRECTORY | windows.READ_CONTROL)
	if currentMask&windows.GENERIC_ALL == 0 && currentMask&windows.GENERIC_READ == 0 && currentMask&required != required {
		return fmt.Errorf("config directory DACL does not grant the current user read access")
	}
	return nil
}
