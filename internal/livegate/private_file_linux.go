//go:build linux

package livegate

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

var errPrivateCandidateFile = errors.New("livegate: private candidate file check failed")

const (
	privateLinuxDirectoryFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_DIRECTORY | unix.O_NOFOLLOW
	privateLinuxFileFlags      = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
)

func openPrivateRegularFile(path string) (*os.File, privateFileSnapshot, error) {
	return openPrivateLinuxFile(path, false)
}

func openPrivateExecutableFile(path string) (*os.File, privateFileSnapshot, error) {
	return openPrivateLinuxFile(path, true)
}

func openPrivateLinuxFile(path string, executable bool) (*os.File, privateFileSnapshot, error) {
	var empty privateFileSnapshot
	parts := strings.Split(strings.TrimPrefix(path, string(os.PathSeparator)), string(os.PathSeparator))
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return nil, empty, errPrivateCandidateFile
	}

	dirFD, err := unix.Open(string(os.PathSeparator), privateLinuxDirectoryFlags, 0)
	if err != nil {
		return nil, empty, errPrivateCandidateFile
	}
	defer func() {
		if dirFD >= 0 {
			_ = unix.Close(dirFD)
		}
	}()

	if len(parts) == 1 {
		var parent unix.Stat_t
		if err := unix.Fstat(dirFD, &parent); err != nil || !validPrivateLinuxMetadata(parent, true) {
			return nil, empty, errPrivateCandidateFile
		}
	}
	for index, part := range parts[:len(parts)-1] {
		nextFD, err := unix.Openat(dirFD, part, privateLinuxDirectoryFlags, 0)
		if err != nil {
			return nil, empty, errPrivateCandidateFile
		}
		var directory unix.Stat_t
		if err := unix.Fstat(nextFD, &directory); err != nil ||
			directory.Mode&unix.S_IFMT != unix.S_IFDIR {
			_ = unix.Close(nextFD)
			return nil, empty, errPrivateCandidateFile
		}
		if index == len(parts)-2 && !validPrivateLinuxMetadata(directory, true) {
			_ = unix.Close(nextFD)
			return nil, empty, errPrivateCandidateFile
		}
		if err := unix.Close(dirFD); err != nil {
			_ = unix.Close(nextFD)
			return nil, empty, errPrivateCandidateFile
		}
		dirFD = nextFD
	}

	fileFD, err := unix.Openat(dirFD, parts[len(parts)-1], privateLinuxFileFlags, 0)
	if err != nil {
		return nil, empty, errPrivateCandidateFile
	}
	closeFD := true
	defer func() {
		if closeFD {
			_ = unix.Close(fileFD)
		}
	}()

	var stat unix.Stat_t
	if err := unix.Fstat(fileFD, &stat); err != nil ||
		!validPrivateLinuxMetadata(stat, false) ||
		executable && stat.Mode&0o100 == 0 {
		return nil, empty, errPrivateCandidateFile
	}
	file := os.NewFile(uintptr(fileFD), path)
	if file == nil {
		return nil, empty, errPrivateCandidateFile
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, empty, errPrivateCandidateFile
	}
	closeFD = false
	return file, privateFileSnapshot{
		info:                info,
		securityFingerprint: linuxSecurityFingerprint(stat),
	}, nil
}

func validPrivateLinuxMetadata(stat unix.Stat_t, directory bool) bool {
	wantType := uint32(unix.S_IFREG)
	if directory {
		wantType = unix.S_IFDIR
	}
	if stat.Mode&unix.S_IFMT != wantType ||
		stat.Uid != uint32(os.Geteuid()) ||
		stat.Mode&0o077 != 0 ||
		stat.Mode&(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX) != 0 {
		return false
	}
	return true
}

func linuxSecurityFingerprint(stat unix.Stat_t) [sha256.Size]byte {
	var encoded [12]byte
	binary.LittleEndian.PutUint32(encoded[0:4], stat.Uid)
	binary.LittleEndian.PutUint32(encoded[4:8], stat.Gid)
	binary.LittleEndian.PutUint32(encoded[8:12], stat.Mode)
	return sha256.Sum256(encoded[:])
}

func candidatePathsEqual(left, right string) bool {
	return left == right
}
