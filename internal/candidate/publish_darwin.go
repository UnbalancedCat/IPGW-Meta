//go:build darwin

package candidate

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func publishCandidateDirectory(stage, final string, expected os.FileInfo, validate func(string) bool, _ func(), validateAfter func(string) bool) error {
	if filepath.Dir(stage) != filepath.Dir(final) || !publishSourceMatches(stage, expected) || !publishSealMatches(validate, stage) {
		return ErrAssemble
	}
	parent, err := unix.Open(filepath.Dir(stage), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return ErrAssemble
	}
	defer unix.Close(parent)
	if err := unix.RenameatxNp(
		parent, filepath.Base(stage), parent, filepath.Base(final), unix.RENAME_EXCL,
	); err != nil {
		if errors.Is(err, unix.EEXIST) || errors.Is(err, unix.ENOTEMPTY) {
			return ErrInvalidInput
		}
		return ErrAssemble
	}
	if !publishedIdentityMatches(final, expected) || !publishSealMatches(validate, final) || !publishSealMatches(validateAfter, final) {
		return ErrAssemble
	}
	if err := unix.Fsync(parent); err != nil {
		return ErrAssemble
	}
	return nil
}
