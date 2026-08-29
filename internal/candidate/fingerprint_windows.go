//go:build windows

package candidate

import "os"

func sameFileFingerprint(os.FileInfo, os.FileInfo) bool {
	// Candidate files are opened without FILE_SHARE_WRITE or FILE_SHARE_DELETE.
	return true
}
