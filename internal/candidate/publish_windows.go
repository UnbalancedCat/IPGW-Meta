//go:build windows

package candidate

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

func publishCandidateDirectory(stage, final string, expected os.FileInfo, validate func(string) bool, release func(), validateAfter func(string) bool) error {
	if filepath.Dir(stage) != filepath.Dir(final) {
		return ErrAssemble
	}
	sourceDirectory, err := openDirectorySnapshot(stage)
	if err != nil {
		return ErrAssemble
	}
	defer sourceDirectory.close()
	if !stableFileInfo(expected, sourceDirectory.info) || !publishSealMatches(validate, stage) {
		return ErrAssemble
	}
	if release != nil {
		release()
		validate = nil
	}
	parentDirectory, err := openDirectorySnapshot(filepath.Dir(stage))
	if err != nil {
		return ErrAssemble
	}
	defer parentDirectory.close()
	if err := renameDirectoryHandle(sourceDirectory.file, parentDirectory.file, filepath.Base(final)); err != nil {
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) ||
			errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return ErrInvalidInput
		}
		return ErrAssemble
	}
	if !publishedIdentityMatches(final, expected) || !publishSealMatches(validate, final) || !publishSealMatches(validateAfter, final) {
		return ErrAssemble
	}
	return nil
}

type fileRenameInformation struct {
	Flags          uint32
	RootDirectory  windows.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

func renameDirectoryHandle(source, targetParent *os.File, targetName string) error {
	name, err := windows.UTF16FromString(targetName)
	if err != nil || len(name) < 2 {
		return ErrAssemble
	}
	name = name[:len(name)-1]
	var layout fileRenameInformation
	bufferSize := int(unsafe.Offsetof(layout.FileName)) + len(name)*2
	buffer := make([]byte, bufferSize)
	information := (*fileRenameInformation)(unsafe.Pointer(&buffer[0]))
	information.Flags = windows.FILE_RENAME_POSIX_SEMANTICS // deliberately omit REPLACE_IF_EXISTS
	information.RootDirectory = windows.Handle(targetParent.Fd())
	information.FileNameLength = uint32(len(name) * 2)
	destination := unsafe.Slice(&information.FileName[0], len(name))
	copy(destination, name)
	var status windows.IO_STATUS_BLOCK
	return windows.NtSetInformationFile(windows.Handle(source.Fd()), &status, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation)
}
