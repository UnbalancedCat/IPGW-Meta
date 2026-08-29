//go:build !linux && !windows

package livegate

import (
	"errors"
	"os"
)

var errPrivateCandidateFile = errors.New("livegate: private candidate files are unsupported")

func openPrivateRegularFile(string) (*os.File, privateFileSnapshot, error) {
	return nil, privateFileSnapshot{}, errPrivateCandidateFile
}

func openPrivateExecutableFile(string) (*os.File, privateFileSnapshot, error) {
	return nil, privateFileSnapshot{}, errPrivateCandidateFile
}

func candidatePathsEqual(left, right string) bool {
	return left == right
}
