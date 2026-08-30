//go:build !linux && !windows && !darwin

package candidate

import "os"

func publishCandidateDirectory(string, string, os.FileInfo, func(string) bool, func(), func(string) bool) error {
	return ErrAssemble
}
