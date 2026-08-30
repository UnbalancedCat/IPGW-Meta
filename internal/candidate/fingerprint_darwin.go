//go:build darwin

package candidate

import (
	"os"
	"syscall"
)

func sameFileFingerprint(before, after os.FileInfo) bool {
	left, leftOK := before.Sys().(*syscall.Stat_t)
	right, rightOK := after.Sys().(*syscall.Stat_t)
	return leftOK && rightOK &&
		left.Dev == right.Dev && left.Ino == right.Ino && left.Nlink == right.Nlink &&
		left.Ctimespec.Sec == right.Ctimespec.Sec && left.Ctimespec.Nsec == right.Ctimespec.Nsec
}
