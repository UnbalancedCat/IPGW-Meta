//go:build linux || darwin

package candidate

import (
	"os"
	"syscall"
)

func hasMultipleLinks(_ *os.File, info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return stat.Nlink != 1
}
