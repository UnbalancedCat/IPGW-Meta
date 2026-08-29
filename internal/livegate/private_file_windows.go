//go:build windows

package livegate

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errPrivateCandidateFile = fmt.Errorf("livegate: private candidate file check failed")

const privateWindowsSharing = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

func openPrivateRegularFile(path string) (*os.File, privateFileSnapshot, error) {
	return openPrivateWindowsFile(path, false)
}

func openPrivateExecutableFile(path string) (*os.File, privateFileSnapshot, error) {
	return openPrivateWindowsFile(path, true)
}

func openPrivateWindowsFile(path string, executable bool) (*os.File, privateFileSnapshot, error) {
	var empty privateFileSnapshot
	prefixes, err := privateWindowsPathPrefixes(path)
	if err != nil || len(prefixes) < 2 {
		return nil, empty, errPrivateCandidateFile
	}
	if err := validatePrivateWindowsParents(prefixes[:len(prefixes)-1]); err != nil {
		return nil, empty, errPrivateCandidateFile
	}

	access := uint32(windows.GENERIC_READ | windows.READ_CONTROL)
	if executable {
		access |= uint32(windows.GENERIC_EXECUTE)
	}
	handle, err := openPrivateWindowsHandle(
		path,
		access,
		privateWindowsSharing,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
	)
	if err != nil {
		return nil, empty, errPrivateCandidateFile
	}
	closeHandle := true
	defer func() {
		if closeHandle {
			_ = windows.CloseHandle(handle)
		}
	}()

	if err := validatePrivateWindowsHandle(handle, false); err != nil {
		return nil, empty, errPrivateCandidateFile
	}
	fingerprint, err := validatePrivateWindowsDACL(handle, false, executable)
	if err != nil {
		return nil, empty, errPrivateCandidateFile
	}
	if err := validatePrivateWindowsParents(prefixes[:len(prefixes)-1]); err != nil {
		return nil, empty, errPrivateCandidateFile
	}

	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		return nil, empty, errPrivateCandidateFile
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, empty, errPrivateCandidateFile
	}
	closeHandle = false
	return file, privateFileSnapshot{
		info:                info,
		securityFingerprint: fingerprint,
	}, nil
}

func validatePrivateWindowsParents(prefixes []string) error {
	for index, prefix := range prefixes {
		handle, err := openPrivateWindowsHandle(
			prefix,
			uint32(windows.GENERIC_READ|windows.READ_CONTROL),
			privateWindowsSharing,
			windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		)
		if err != nil {
			return errPrivateCandidateFile
		}
		if err := validatePrivateWindowsHandle(handle, true); err != nil {
			_ = windows.CloseHandle(handle)
			return errPrivateCandidateFile
		}
		if index == len(prefixes)-1 {
			if _, err := validatePrivateWindowsDACL(handle, true, false); err != nil {
				_ = windows.CloseHandle(handle)
				return errPrivateCandidateFile
			}
		}
		if err := windows.CloseHandle(handle); err != nil {
			return errPrivateCandidateFile
		}
	}
	return nil
}

func validatePrivateWindowsHandle(handle windows.Handle, directory bool) error {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return errPrivateCandidateFile
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errPrivateCandidateFile
	}
	isDirectory := info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if isDirectory != directory {
		return errPrivateCandidateFile
	}
	fileType, err := windows.GetFileType(handle)
	if err != nil || fileType != windows.FILE_TYPE_DISK {
		return errPrivateCandidateFile
	}
	return nil
}

func validatePrivateWindowsDACL(handle windows.Handle, directory, executable bool) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	descriptor, err := windows.GetSecurityInfo(
		handle,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return empty, errPrivateCandidateFile
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return empty, errPrivateCandidateFile
	}
	return validatePrivateWindowsSecurityDescriptor(descriptor, user.User.Sid, directory, executable)
}

func validatePrivateWindowsSecurityDescriptor(
	descriptor *windows.SECURITY_DESCRIPTOR,
	currentSID *windows.SID,
	directory,
	executable bool,
) ([sha256.Size]byte, error) {
	var empty [sha256.Size]byte
	if descriptor == nil || currentSID == nil || !descriptor.IsValid() || !currentSID.IsValid() {
		return empty, errPrivateCandidateFile
	}
	owner, ownerDefaulted, err := descriptor.Owner()
	if err != nil || owner == nil || ownerDefaulted || !owner.IsValid() || !owner.Equals(currentSID) {
		return empty, errPrivateCandidateFile
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return empty, errPrivateCandidateFile
	}
	dacl, defaulted, err := descriptor.DACL()
	if err != nil || dacl == nil || defaulted || dacl.AceCount == 0 {
		return empty, errPrivateCandidateFile
	}
	var currentMask windows.ACCESS_MASK
	fingerprint := sha256.New()
	_, _ = fmt.Fprintf(fingerprint, "owner:%s;control:%d;", owner.String(), control)

	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil ||
			ace == nil ||
			ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE ||
			uintptr(ace.Header.AceSize) < unsafe.Offsetof(ace.SidStart)+unsafe.Sizeof(ace.SidStart) {
			return empty, errPrivateCandidateFile
		}
		allowedFlags := uint8(0)
		if directory {
			allowedFlags = uint8(windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE | windows.INHERIT_ONLY_ACE)
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE != 0 ||
			ace.Header.AceFlags & ^allowedFlags != 0 {
			return empty, errPrivateCandidateFile
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !sid.IsValid() {
			return empty, errPrivateCandidateFile
		}
		isCurrentUser := sid.Equals(currentSID)
		if !isCurrentUser &&
			!sid.IsWellKnown(windows.WinLocalSystemSid) &&
			!sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			return empty, errPrivateCandidateFile
		}
		if isCurrentUser && ace.Header.AceFlags&windows.INHERIT_ONLY_ACE == 0 {
			currentMask |= ace.Mask
		}
		_, _ = fmt.Fprintf(fingerprint, "%d:%d:%d:%s;", ace.Header.AceType, ace.Header.AceFlags, ace.Mask, sid.String())
	}

	required := windows.ACCESS_MASK(windows.FILE_READ_DATA | windows.READ_CONTROL)
	if directory {
		required = windows.ACCESS_MASK(windows.FILE_LIST_DIRECTORY | windows.READ_CONTROL)
	}
	if currentMask&windows.GENERIC_ALL == 0 &&
		currentMask&windows.GENERIC_READ == 0 &&
		currentMask&required != required {
		return empty, errPrivateCandidateFile
	}
	var result [sha256.Size]byte
	copy(result[:], fingerprint.Sum(nil))
	if executable &&
		currentMask&windows.GENERIC_ALL == 0 &&
		currentMask&windows.GENERIC_EXECUTE == 0 &&
		currentMask&windows.FILE_EXECUTE == 0 {
		return empty, errPrivateCandidateFile
	}
	return result, nil
}

func privateWindowsPathPrefixes(path string) ([]string, error) {
	volume := filepath.VolumeName(path)
	if len(volume) != 2 || volume[1] != ':' {
		return nil, errPrivateCandidateFile
	}
	remainder := strings.TrimPrefix(path, volume)
	if remainder == "" || !os.IsPathSeparator(remainder[0]) {
		return nil, errPrivateCandidateFile
	}
	parts := strings.FieldsFunc(remainder, func(r rune) bool {
		return r == '\\' || r == '/'
	})
	if len(parts) == 0 {
		return nil, errPrivateCandidateFile
	}
	for _, part := range parts {
		if !validPrivateWindowsPathPart(part) {
			return nil, errPrivateCandidateFile
		}
	}
	current := volume + string(os.PathSeparator)
	prefixes := []string{current}
	for _, part := range parts {
		current = filepath.Join(current, part)
		prefixes = append(prefixes, current)
	}
	return prefixes, nil
}

func validPrivateWindowsPathPart(part string) bool {
	if strings.HasSuffix(part, " ") || strings.HasSuffix(part, ".") {
		return false
	}
	for _, character := range part {
		if unicode.IsControl(character) || strings.ContainsRune("<>:\"|?*", character) {
			return false
		}
	}
	base := part
	if index := strings.IndexByte(base, '.'); index >= 0 {
		base = base[:index]
	}
	base = strings.ToUpper(strings.TrimRight(base, " "))
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	for _, prefix := range []string{"COM", "LPT"} {
		if strings.HasPrefix(base, prefix) {
			suffix := strings.TrimPrefix(base, prefix)
			if len(suffix) == 1 && suffix[0] >= '1' && suffix[0] <= '9' {
				return false
			}
		}
	}
	return true
}

func openPrivateWindowsHandle(path string, access, share, flags uint32) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, errPrivateCandidateFile
	}
	handle, err := windows.CreateFile(name, access, share, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return windows.InvalidHandle, errPrivateCandidateFile
	}
	return handle, nil
}

func candidatePathsEqual(left, right string) bool {
	return strings.EqualFold(left, right)
}
