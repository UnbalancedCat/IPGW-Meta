//go:build windows

package candidate

import (
	"os"
	"syscall"
)

func hasMultipleLinks(file *os.File, _ os.FileInfo) bool {
	var information syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &information); err != nil {
		return true
	}
	return information.NumberOfLinks != 1
}
