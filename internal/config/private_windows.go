//go:build windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openRestrictedPasswordFile(path string) (*os.File, error) {
	prefixes, err := restrictedWindowsPathPrefixes(path)
	if err != nil {
		return nil, err
	}
	if err := validateRestrictedWindowsParents(prefixes[:len(prefixes)-1]); err != nil {
		return nil, err
	}

	handle, err := openRestrictedWindowsPath(
		prefixes[len(prefixes)-1],
		uint32(windows.GENERIC_READ|windows.READ_CONTROL),
		windows.FILE_SHARE_READ,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
	)
	if err != nil {
		return nil, err
	}
	closeHandle := true
	defer func() {
		if closeHandle {
			_ = windows.CloseHandle(handle)
		}
	}()

	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return nil, fmt.Errorf("inspect credential file: %w", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nil, fmt.Errorf("credential file is a reparse point")
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return nil, fmt.Errorf("credential path is not a regular file")
	}
	fileType, err := windows.GetFileType(handle)
	if err != nil {
		return nil, fmt.Errorf("inspect credential file type: %w", err)
	}
	if fileType != windows.FILE_TYPE_DISK {
		return nil, fmt.Errorf("credential path is not a regular disk file")
	}
	if err := validateRestrictedPasswordDACL(handle); err != nil {
		return nil, err
	}
	// Re-check after the final handle is open. Even if a parent changed
	// between the two walks, data is read only from this non-reparse,
	// protected-DACL handle and never by reopening the path.
	if err := validateRestrictedWindowsParents(prefixes[:len(prefixes)-1]); err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		return nil, fmt.Errorf("create credential file handle")
	}
	closeHandle = false
	return file, nil
}

func validateRestrictedWindowsParents(prefixes []string) error {
	for _, prefix := range prefixes {
		name, err := windows.UTF16PtrFromString(prefix)
		if err != nil {
			return fmt.Errorf("encode credential path component: %w", err)
		}
		attributes, err := windows.GetFileAttributes(name)
		if err != nil {
			return fmt.Errorf("inspect credential path component: %w", err)
		}
		if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return fmt.Errorf("credential path contains a reparse point")
		}
		if attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			return fmt.Errorf("credential path component is not a directory")
		}
	}
	return nil
}

func restrictedWindowsPathPrefixes(path string) ([]string, error) {
	absPath, err := validateRestrictedWindowsPathSyntax(path)
	if err != nil {
		return nil, err
	}
	volume := filepath.VolumeName(absPath)

	parts := strings.FieldsFunc(strings.TrimPrefix(absPath, volume), func(r rune) bool {
		return r == '\\' || r == '/'
	})
	if len(parts) == 0 {
		return nil, fmt.Errorf("credential path does not name a file")
	}

	current := volume + string(os.PathSeparator)
	prefixes := make([]string, 0, len(parts)+1)
	prefixes = append(prefixes, current)
	for _, part := range parts {
		current = filepath.Join(current, part)
		prefixes = append(prefixes, current)
	}
	return prefixes, nil
}

func validateRestrictedWindowsPathSyntax(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("credential path must be an absolute local drive path")
	}
	cleanPath := filepath.Clean(path)
	volume := filepath.VolumeName(cleanPath)
	if !isLocalDOSDriveVolume(volume) {
		return "", fmt.Errorf("credential path must use a local drive-letter volume")
	}
	remainder := strings.TrimPrefix(cleanPath, volume)
	if remainder == "" || !os.IsPathSeparator(remainder[0]) {
		return "", fmt.Errorf("credential path must be rooted on a local drive")
	}

	parts := strings.FieldsFunc(remainder, func(r rune) bool {
		return r == '\\' || r == '/'
	})
	if len(parts) == 0 {
		return "", fmt.Errorf("credential path does not name a file")
	}
	for _, part := range parts {
		if err := validateRestrictedWindowsPathPart(part); err != nil {
			return "", err
		}
	}
	return cleanPath, nil
}

func isLocalDOSDriveVolume(volume string) bool {
	if len(volume) != 2 || volume[1] != ':' {
		return false
	}
	letter := volume[0]
	return letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z'
}

func validateRestrictedWindowsPathPart(part string) error {
	if strings.HasSuffix(part, " ") || strings.HasSuffix(part, ".") {
		return fmt.Errorf("credential path component has a trailing space or dot")
	}
	for _, r := range part {
		if unicode.IsControl(r) {
			return fmt.Errorf("credential path component contains a control character")
		}
		if strings.ContainsRune(`<>:"|?*`, r) {
			return fmt.Errorf("credential path component contains a reserved character")
		}
	}
	if isDangerousWindowsDeviceName(part) {
		return fmt.Errorf("credential path component is a reserved DOS device name")
	}
	return nil
}

func isDangerousWindowsDeviceName(part string) bool {
	base := part
	if index := strings.IndexAny(base, ".:"); index >= 0 {
		base = base[:index]
	}
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return true
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(base, prefix)
		if len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '9' {
			return true
		}
		switch suffix {
		case "\u00b9", "\u00b2", "\u00b3":
			return true
		}
	}
	return false
}

func openRestrictedWindowsPath(path string, access, share, flags uint32) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(name, access, share, nil, windows.OPEN_EXISTING, flags, 0)
}

func validateRestrictedPasswordDACL(handle windows.Handle) error {
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("read credential file DACL: %w", err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return fmt.Errorf("read credential file DACL control: %w", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return fmt.Errorf("credential file DACL is inherited; require a protected DACL")
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read credential file DACL: %w", err)
	}
	if dacl == nil || defaulted || dacl.AceCount == 0 {
		return fmt.Errorf("credential file DACL is absent, defaulted, or empty")
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
			return fmt.Errorf("read credential file DACL entry: %w", err)
		}
		if ace == nil || ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("credential file DACL contains a non-allow or unknown ACE")
		}
		if ace.Header.AceFlags != 0 {
			return fmt.Errorf("credential file DACL contains an inherited or inheritable ACE")
		}
		if uintptr(ace.Header.AceSize) < unsafe.Offsetof(ace.SidStart)+unsafe.Sizeof(ace.SidStart) {
			return fmt.Errorf("credential file DACL contains a malformed ACE")
		}

		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return fmt.Errorf("credential file DACL contains an invalid SID")
		}
		isCurrentUser := sid.Equals(currentSID)
		if !isCurrentUser && !sid.IsWellKnown(windows.WinLocalSystemSid) && !sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			return fmt.Errorf("credential file DACL grants access to an untrusted principal")
		}
		if isCurrentUser {
			currentMask |= ace.Mask
		}
	}

	required := windows.ACCESS_MASK(windows.FILE_READ_DATA | windows.READ_CONTROL)
	if currentMask&windows.GENERIC_ALL == 0 && currentMask&windows.GENERIC_READ == 0 && currentMask&required != required {
		return fmt.Errorf("credential file DACL does not grant the current user read access")
	}
	return nil
}

func restrictPrivateDirectory(path string) error {
	return setCurrentUserOnlyACL(path, windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT)
}

func restrictPrivateFile(path string, _ os.FileMode) error {
	return setCurrentUserOnlyACL(path, windows.NO_INHERITANCE)
}

func setCurrentUserOnlyACL(path string, inheritance uint32) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}
	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	)
}

func syncParentDirectory(string) error {
	// replaceFile uses MoveFileEx with MOVEFILE_WRITE_THROUGH for both the
	// last-known-good and primary replacements on Windows.
	return nil
}
