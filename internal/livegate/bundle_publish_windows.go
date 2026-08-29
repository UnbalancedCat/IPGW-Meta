//go:build windows

package livegate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func ensureBundlePrivateDirectory(path string) error {
	prefixes, err := privateWindowsPathPrefixes(path)
	if err != nil || len(prefixes) < 2 {
		return ErrEvidenceDurability
	}

	firstMissing := len(prefixes)
	for index, prefix := range prefixes {
		attributes, attrErr := bundleWindowsPathAttributes(prefix)
		if isBundleWindowsPathMissing(attrErr) {
			firstMissing = index
			break
		}
		if attrErr != nil || !bundleWindowsAttributesAreDirectory(attributes) {
			return ErrEvidenceDurability
		}
	}
	if firstMissing == len(prefixes) {
		// Existing directories are shared state. A concurrent publisher may
		// only verify them; it must never rewrite a private ACL.
		return verifyBundlePrivateDirectory(path)
	}

	for index := firstMissing; index < len(prefixes); index++ {
		prefix := prefixes[index]
		leaf := index == len(prefixes)-1
		var createErr error
		if leaf {
			createErr = createBundleWindowsPrivateDirectory(prefix)
		} else {
			createErr = createBundleWindowsDirectory(prefix, nil)
		}
		if createErr != nil && !isBundleWindowsAlreadyExists(createErr) {
			return ErrEvidenceDurability
		}
		if err := validateBundleWindowsDirectoryPrefixes(prefixes[:index+1]); err != nil {
			return ErrEvidenceDurability
		}
		if leaf && isBundleWindowsAlreadyExists(createErr) {
			// The winner owns ACL construction; losers only verify.
			return verifyBundlePrivateDirectory(path)
		}
	}

	if err := validateBundleWindowsDirectoryPrefixes(prefixes); err != nil {
		return ErrEvidenceDurability
	}
	return verifyBundlePrivateDirectory(path)
}

func verifyBundlePrivateDirectory(path string) error {
	prefixes, err := privateWindowsPathPrefixes(path)
	if err != nil || len(prefixes) < 2 {
		return ErrEvidenceDurability
	}
	if err := validatePrivateWindowsParents(prefixes); err != nil {
		return ErrEvidenceDurability
	}
	return nil
}

func protectBundleFile(path string) error {
	if err := validateBundleWindowsPrefixes(path, false); err != nil {
		return ErrEvidenceDurability
	}
	if err := setBundleCurrentUserOnlyACL(path, windows.NO_INHERITANCE); err != nil {
		return ErrEvidenceDurability
	}
	return verifyBundlePrivateFile(path)
}

func verifyBundlePrivateFile(path string) error {
	file, _, err := openPrivateRegularFile(path)
	if err != nil {
		return ErrEvidenceDurability
	}
	if err := file.Close(); err != nil {
		return ErrEvidenceDurability
	}
	return nil
}

func openBundlePrivateFile(path string) (*os.File, error) {
	file, _, err := openPrivateRegularFile(path)
	if err != nil {
		return nil, ErrEvidenceDurability
	}
	return file, nil
}

func validateBundleWindowsPrefixes(path string, finalDirectory bool) error {
	prefixes, err := privateWindowsPathPrefixes(path)
	if err != nil || len(prefixes) < 2 {
		return ErrEvidenceDurability
	}
	for index, prefix := range prefixes {
		wantDirectory := index < len(prefixes)-1 || finalDirectory
		if err := validateBundleWindowsPathType(prefix, wantDirectory); err != nil {
			return err
		}
	}
	return nil
}

func bundleWindowsPathAttributes(path string) (uint32, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.GetFileAttributes(name)
}
func isBundleWindowsPathMissing(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) ||
		errors.Is(err, windows.ERROR_PATH_NOT_FOUND)
}

func isBundleWindowsAlreadyExists(err error) bool {
	return errors.Is(err, windows.ERROR_ALREADY_EXISTS) ||
		errors.Is(err, windows.ERROR_FILE_EXISTS)
}

func bundleWindowsAttributesAreDirectory(attributes uint32) bool {
	return attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 &&
		attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
}

func validateBundleWindowsDirectoryPrefixes(prefixes []string) error {
	for _, prefix := range prefixes {
		attributes, err := bundleWindowsPathAttributes(prefix)
		if err != nil || !bundleWindowsAttributesAreDirectory(attributes) {
			return ErrEvidenceDurability
		}
	}
	return nil
}
func validateBundleWindowsPathType(path string, directory bool) error {
	attributes, err := bundleWindowsPathAttributes(path)
	if err != nil || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return ErrEvidenceDurability
	}
	isDirectory := attributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0
	if isDirectory != directory {
		return ErrEvidenceDurability
	}
	return nil
}
func createBundleWindowsDirectory(path string, attributes *windows.SecurityAttributes) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ErrEvidenceDurability
	}
	return windows.CreateDirectory(name, attributes)
}

func createBundleWindowsPrivateDirectory(path string) error {
	attributes, err := bundleWindowsPrivateDirectorySecurityAttributes()
	if err != nil {
		return err
	}
	return createBundleWindowsDirectory(path, attributes)
}

func bundleWindowsPrivateDirectorySecurityAttributes() (*windows.SecurityAttributes, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || user == nil || user.User.Sid == nil || !user.User.Sid.IsValid() {
		return nil, ErrEvidenceDurability
	}
	sid := user.User.Sid.String()
	descriptor, err := windows.SecurityDescriptorFromString(
		"O:" + sid + "D:P(A;OICI;GA;;;" + sid + ")",
	)
	if err != nil {
		return nil, ErrEvidenceDurability
	}
	return &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}, nil
}

func setBundleCurrentUserOnlyACL(path string, inheritance uint32) error {
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
		windows.OWNER_SECURITY_INFORMATION|
			windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		user.User.Sid,
		nil,
		acl,
		nil,
	)
}

func createBundleStagingDirectory(parent string) (string, error) {
	if err := verifyBundlePrivateDirectory(parent); err != nil {
		return "", ErrEvidenceDurability
	}
	for attempts := 0; attempts < 128; attempts++ {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", ErrEvidenceDurability
		}
		stage := filepath.Join(parent, ".livegate-stage-"+hex.EncodeToString(random[:]))
		if err := createBundleWindowsPrivateDirectory(stage); err != nil {
			if isBundleWindowsAlreadyExists(err) {
				continue
			}
			return "", ErrEvidenceDurability
		}
		if err := validateBundleWindowsPrefixes(stage, true); err != nil {
			_ = os.Remove(stage)
			return "", ErrEvidenceDurability
		}
		if err := verifyBundlePrivateDirectory(stage); err != nil {
			_ = os.Remove(stage)
			return "", ErrEvidenceDurability
		}
		return stage, nil
	}
	return "", ErrEvidenceDurability
}
func syncBundleDirectory(string) error {
	// MoveFileEx with MOVEFILE_WRITE_THROUGH is the Windows directory publish
	// durability boundary.
	return nil
}

func publishBundleDirectory(stageDir, finalDir string) error {
	exists, err := bundlePathExists(finalDir)
	if err != nil {
		return err
	}
	if exists {
		return errBundleCollision
	}
	source, err := windows.UTF16PtrFromString(stageDir)
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(finalDir)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(source, target, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return classifyBundleWindowsPublishError(err)
	}
	return nil
}

func classifyBundleWindowsPublishError(publishErr error) error {
	if isBundleWindowsAlreadyExists(publishErr) {
		return errBundleCollision
	}
	return publishErr
}
