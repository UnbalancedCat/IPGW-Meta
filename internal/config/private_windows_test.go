//go:build windows

package config

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestValidateRestrictedWindowsPathSyntaxAcceptsLocalDrivePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "凭据 file.txt")
	got, err := validateRestrictedWindowsPathSyntax(path)
	if err != nil {
		t.Fatalf("validateRestrictedWindowsPathSyntax: %v", err)
	}
	if want := filepath.Clean(path); got != want {
		t.Fatalf("validated path = %q, want %q", got, want)
	}
}

func TestValidateRestrictedWindowsPathSyntaxRejectsUnsafeNames(t *testing.T) {
	tests := map[string]string{
		"relative":              `credential.txt`,
		"drive relative":        `C:credential.txt`,
		"UNC":                   `\\server\share\credential.txt`,
		"extended drive":        `\\?\C:\safe\credential.txt`,
		"local device drive":    `\\.\C:\safe\credential.txt`,
		"NT device drive":       `\??\C:\safe\credential.txt`,
		"alternate stream":      `C:\safe\credential.txt:secret`,
		"stream type":           `C:\safe\credential.txt::$DATA`,
		"control":               "C:\\safe\\bad\nname.txt",
		"wildcard star":         `C:\safe\bad*.txt`,
		"wildcard question":     `C:\safe\bad?.txt`,
		"reserved separator":    `C:\safe\bad|name.txt`,
		"reserved NUL":          `C:\safe\NUL`,
		"reserved with suffix":  `C:\safe\CON.txt`,
		"reserved serial":       `C:\safe\COM1.log`,
		"reserved printer":      `C:\safe\LPT9`,
		"reserved console":      `C:\safe\CONIN$`,
		"reserved superscript":  "C:\\safe\\COM\u00b2.txt",
		"trailing dot":          `C:\safe\credential.`,
		"trailing space":        `C:\safe\credential `,
		"non-letter DOS volume": `1:\safe\credential.txt`,
	}
	for name, path := range tests {
		t.Run(name, func(t *testing.T) {
			if got, err := validateRestrictedWindowsPathSyntax(path); err == nil {
				t.Fatalf("unsafe path was accepted as %q", got)
			}
		})
	}
}

func TestValidateRestrictedWindowsParentsRejectsDirectorySymlink(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create target directory: %v", err)
	}
	link := filepath.Join(base, "linked-directory")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("Windows directory symlink creation is unavailable: %v", err)
	}
	prefixes, err := restrictedWindowsPathPrefixes(filepath.Join(link, "credential.txt"))
	if err != nil {
		t.Fatalf("build restricted path prefixes: %v", err)
	}
	if err := validateRestrictedWindowsParents(prefixes[:len(prefixes)-1]); err == nil {
		t.Fatal("validateRestrictedWindowsParents accepted a directory symlink")
	}
}

func TestAtomicWriteAppliesProtectedCurrentUserACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private-state")
	path := filepath.Join(dir, "state.yaml")
	writeAtomicTestValue(t, path, "state-v1\n")
	writeAtomicTestValue(t, path, "state-v2\n")

	assertCurrentUserOnlyACL(t, dir, true)
	assertCurrentUserOnlyACL(t, path, false)
	assertCurrentUserOnlyACL(t, path+".lkg", false)
}

func TestAtomicWriteWindowsReplaceFailurePreservesDestination(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private-state")
	path := filepath.Join(dir, "state.yaml")
	writeAtomicTestValue(t, path, "stable-state\n")

	pathUTF16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("encode target path: %v", err)
	}
	// Permit the backup read but deliberately omit FILE_SHARE_DELETE so the
	// final MoveFileEx replacement fails with a sharing violation.
	handle, err := windows.CreateFile(
		pathUTF16,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("open target without delete sharing: %v", err)
	}
	defer windows.CloseHandle(handle)

	if err := atomicWrite(path, []byte("uncommitted-state\n"), 0o600); err == nil {
		t.Fatal("atomicWrite succeeded while target denied delete sharing")
	}
	assertFileValue(t, path, "stable-state\n")
	assertFileValue(t, path+".lkg", "stable-state\n")
	assertNoAtomicTemps(t, dir)
}

func assertCurrentUserOnlyACL(t *testing.T, path string, directory bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		t.Fatalf("read DACL for %q: %v", filepath.Base(path), err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatalf("read DACL control for %q: %v", filepath.Base(path), err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("DACL for %q is not protected", filepath.Base(path))
	}
	dacl, defaulted, err := sd.DACL()
	if err != nil {
		t.Fatalf("read DACL entries for %q: %v", filepath.Base(path), err)
	}
	if defaulted {
		t.Fatalf("DACL for %q is defaulted", filepath.Base(path))
	}
	if dacl == nil || dacl.AceCount == 0 {
		t.Fatalf("DACL for %q is empty", filepath.Base(path))
	}

	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("read current user SID: %v", err)
	}
	hasDirectoryInheritance := false
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil {
			t.Fatalf("read ACE %d for %q: %v", index, filepath.Base(path), err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			t.Fatalf("ACE %d for %q has type %d, want allow", index, filepath.Base(path), ace.Header.AceType)
		}
		if ace.Header.AceFlags&windows.INHERITED_ACE != 0 {
			t.Fatalf("ACE %d for %q is inherited", index, filepath.Base(path))
		}
		const mappedFileAllAccess windows.ACCESS_MASK = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
		hasGenericAll := ace.Mask&windows.GENERIC_ALL == windows.GENERIC_ALL
		hasMappedAll := ace.Mask&mappedFileAllAccess == mappedFileAllAccess
		if !hasGenericAll && !hasMappedAll {
			t.Fatalf("ACE %d for %q does not grant full control: %#x", index, filepath.Base(path), ace.Mask)
		}
		inheritance := ace.Header.AceFlags & (windows.OBJECT_INHERIT_ACE | windows.CONTAINER_INHERIT_ACE)
		if directory {
			hasDirectoryInheritance = hasDirectoryInheritance || inheritance != 0
		} else if inheritance != 0 {
			t.Fatalf("file ACE %d for %q unexpectedly has inheritance flags: %#x", index, filepath.Base(path), ace.Header.AceFlags)
		}

		aceSID := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !aceSID.Equals(user.User.Sid) {
			t.Fatalf(
				"ACE %d for %q belongs to %s, want %s",
				index, filepath.Base(path), aceSID.String(), user.User.Sid.String(),
			)
		}
	}
	if directory && !hasDirectoryInheritance {
		t.Fatalf("DACL for directory %q does not inherit to children", filepath.Base(path))
	}
}

func assertPrivateMigrationDirectory(t *testing.T, path string) {
	t.Helper()
	assertCurrentUserOnlyACL(t, path, true)
}
