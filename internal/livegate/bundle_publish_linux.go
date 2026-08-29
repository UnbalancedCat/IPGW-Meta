//go:build linux

package livegate

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	bundleLinuxDirectoryFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_DIRECTORY | unix.O_NOFOLLOW
	bundleLinuxFileFlags      = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
)

func ensureBundlePrivateDirectory(path string) error {
	directoryFD, err := ensureBundleLinuxDirectory(path)
	if err != nil {
		return err
	}
	return unix.Close(directoryFD)
}

func protectBundleDirectory(path string) error {
	directoryFD, err := openBundleLinuxDirectory(path)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)

	var metadata unix.Stat_t
	if err := unix.Fstat(directoryFD, &metadata); err != nil ||
		metadata.Uid != uint32(os.Geteuid()) {
		return ErrEvidenceDurability
	}
	if err := unix.Fchmod(directoryFD, 0o700); err != nil {
		return err
	}
	if err := unix.Fsync(directoryFD); err != nil {
		return err
	}
	return verifyBundleLinuxDirectoryFD(directoryFD)
}

func verifyBundlePrivateDirectory(path string) error {
	directoryFD, err := openBundleLinuxDirectory(path)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	return verifyBundleLinuxDirectoryFD(directoryFD)
}

func createBundleStagingDirectory(parent string) (string, error) {
	parentFD, err := openBundleLinuxDirectory(parent)
	if err != nil {
		return "", err
	}
	defer unix.Close(parentFD)
	if err := verifyBundleLinuxDirectoryFD(parentFD); err != nil {
		return "", err
	}

	var randomBytes [12]byte
	for attempt := 0; attempt < 128; attempt++ {
		if _, err := rand.Read(randomBytes[:]); err != nil {
			return "", err
		}
		name := ".livegate-stage-" + hex.EncodeToString(randomBytes[:])
		if err := unix.Mkdirat(parentFD, name, 0o700); err != nil {
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			return "", err
		}
		cleanup := func() {
			_ = unix.Unlinkat(parentFD, name, unix.AT_REMOVEDIR)
			_ = unix.Fsync(parentFD)
		}
		if err := unix.Fsync(parentFD); err != nil {
			cleanup()
			return "", err
		}
		stageFD, err := unix.Openat(parentFD, name, bundleLinuxDirectoryFlags, 0)
		if err != nil {
			cleanup()
			return "", err
		}
		if err := unix.Fchmod(stageFD, 0o700); err != nil {
			_ = unix.Close(stageFD)
			cleanup()
			return "", err
		}
		if err := unix.Fsync(stageFD); err != nil {
			_ = unix.Close(stageFD)
			cleanup()
			return "", err
		}
		if err := verifyBundleLinuxDirectoryFD(stageFD); err != nil {
			_ = unix.Close(stageFD)
			cleanup()
			return "", err
		}
		if err := unix.Close(stageFD); err != nil {
			cleanup()
			return "", err
		}
		return filepath.Join(parent, name), nil
	}
	return "", ErrEvidenceDurability
}

func protectBundleFile(path string) error {
	fileFD, err := openBundleLinuxFileFD(path)
	if err != nil {
		return err
	}
	defer unix.Close(fileFD)

	var metadata unix.Stat_t
	if err := unix.Fstat(fileFD, &metadata); err != nil ||
		metadata.Mode&unix.S_IFMT != unix.S_IFREG ||
		metadata.Uid != uint32(os.Geteuid()) {
		return ErrEvidenceDurability
	}
	if err := unix.Fchmod(fileFD, 0o600); err != nil {
		return err
	}
	if err := unix.Fsync(fileFD); err != nil {
		return err
	}
	return verifyBundleLinuxFileFD(fileFD)
}

func verifyBundlePrivateFile(path string) error {
	fileFD, err := openBundleLinuxFileFD(path)
	if err != nil {
		return err
	}
	defer unix.Close(fileFD)
	return verifyBundleLinuxFileFD(fileFD)
}

func openBundlePrivateFile(path string) (*os.File, error) {
	fileFD, err := openBundleLinuxFileFD(path)
	if err != nil {
		return nil, err
	}
	if err := verifyBundleLinuxFileFD(fileFD); err != nil {
		_ = unix.Close(fileFD)
		return nil, err
	}
	file := os.NewFile(uintptr(fileFD), path)
	if file == nil {
		_ = unix.Close(fileFD)
		return nil, ErrEvidenceDurability
	}
	return file, nil
}

func syncBundleDirectory(path string) error {
	directoryFD, err := openBundleLinuxDirectory(path)
	if err != nil {
		return err
	}
	defer unix.Close(directoryFD)
	return unix.Fsync(directoryFD)
}

func publishBundleDirectory(stageDir, finalDir string) error {
	parent := filepath.Dir(stageDir)
	if parent != filepath.Dir(finalDir) {
		return ErrEvidenceDurability
	}
	stageName := filepath.Base(stageDir)
	finalName := filepath.Base(finalDir)
	if !validBundleLinuxLeaf(stageName) || !validBundleLinuxLeaf(finalName) {
		return ErrEvidenceDurability
	}

	parentFD, err := openBundleLinuxDirectory(parent)
	if err != nil {
		return err
	}
	defer unix.Close(parentFD)
	if err := verifyBundleLinuxDirectoryFD(parentFD); err != nil {
		return err
	}

	err = unix.Renameat2(
		parentFD,
		stageName,
		parentFD,
		finalName,
		unix.RENAME_NOREPLACE,
	)
	if errors.Is(err, unix.EEXIST) || errors.Is(err, unix.ENOTEMPTY) {
		return errBundleCollision
	}
	// ENOSYS, EINVAL, unsupported filesystems, and all other failures are
	// deliberately returned without an exists-check/os.Rename fallback.
	return err
}

func ensureBundleLinuxDirectory(path string) (int, error) {
	parts, err := bundleLinuxPathParts(path)
	if err != nil {
		return -1, err
	}
	currentFD, err := unix.Open(string(os.PathSeparator), bundleLinuxDirectoryFlags, 0)
	if err != nil {
		return -1, err
	}
	for index, part := range parts {
		nextFD, openErr := unix.Openat(currentFD, part, bundleLinuxDirectoryFlags, 0)
		created := false
		if errors.Is(openErr, unix.ENOENT) {
			mkdirErr := unix.Mkdirat(currentFD, part, 0o700)
			if mkdirErr == nil {
				created = true
			} else if !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(currentFD)
				return -1, mkdirErr
			}
			if err := unix.Fsync(currentFD); err != nil {
				if created {
					_ = unix.Unlinkat(currentFD, part, unix.AT_REMOVEDIR)
				}
				_ = unix.Close(currentFD)
				return -1, err
			}
			nextFD, openErr = unix.Openat(currentFD, part, bundleLinuxDirectoryFlags, 0)
		}
		if openErr != nil {
			_ = unix.Close(currentFD)
			return -1, openErr
		}

		if created {
			var metadata unix.Stat_t
			if err := unix.Fstat(nextFD, &metadata); err != nil ||
				metadata.Uid != uint32(os.Geteuid()) {
				_ = unix.Close(nextFD)
				_ = unix.Close(currentFD)
				return -1, ErrEvidenceDurability
			}
			if err := unix.Fchmod(nextFD, 0o700); err != nil {
				_ = unix.Close(nextFD)
				_ = unix.Close(currentFD)
				return -1, err
			}
			if err := unix.Fsync(nextFD); err != nil {
				_ = unix.Close(nextFD)
				_ = unix.Close(currentFD)
				return -1, err
			}
		}
		if created || index == len(parts)-1 {
			if err := verifyBundleLinuxDirectoryFD(nextFD); err != nil {
				_ = unix.Close(nextFD)
				_ = unix.Close(currentFD)
				return -1, err
			}
		}
		if err := unix.Close(currentFD); err != nil {
			_ = unix.Close(nextFD)
			return -1, err
		}
		currentFD = nextFD
	}
	return currentFD, nil
}

func openBundleLinuxDirectory(path string) (int, error) {
	parts, err := bundleLinuxPathParts(path)
	if err != nil {
		return -1, err
	}
	currentFD, err := unix.Open(string(os.PathSeparator), bundleLinuxDirectoryFlags, 0)
	if err != nil {
		return -1, err
	}
	for _, part := range parts {
		nextFD, err := unix.Openat(currentFD, part, bundleLinuxDirectoryFlags, 0)
		if err != nil {
			_ = unix.Close(currentFD)
			return -1, err
		}
		if err := unix.Close(currentFD); err != nil {
			_ = unix.Close(nextFD)
			return -1, err
		}
		currentFD = nextFD
	}
	return currentFD, nil
}

func openBundleLinuxFileFD(path string) (int, error) {
	parent := filepath.Dir(path)
	name := filepath.Base(path)
	if !validBundleLinuxLeaf(name) {
		return -1, ErrEvidenceDurability
	}
	parentFD, err := openBundleLinuxDirectory(parent)
	if err != nil {
		return -1, err
	}
	if err := verifyBundleLinuxDirectoryFD(parentFD); err != nil {
		_ = unix.Close(parentFD)
		return -1, err
	}
	fileFD, err := unix.Openat(parentFD, name, bundleLinuxFileFlags, 0)
	closeErr := unix.Close(parentFD)
	if err != nil {
		return -1, err
	}
	if closeErr != nil {
		_ = unix.Close(fileFD)
		return -1, closeErr
	}
	return fileFD, nil
}

func verifyBundleLinuxDirectoryFD(directoryFD int) error {
	var metadata unix.Stat_t
	if err := unix.Fstat(directoryFD, &metadata); err != nil ||
		metadata.Mode&unix.S_IFMT != unix.S_IFDIR ||
		metadata.Uid != uint32(os.Geteuid()) ||
		metadata.Mode&0o777 != 0o700 ||
		metadata.Mode&(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX) != 0 {
		return ErrEvidenceDurability
	}
	return nil
}

func verifyBundleLinuxFileFD(fileFD int) error {
	var metadata unix.Stat_t
	if err := unix.Fstat(fileFD, &metadata); err != nil ||
		metadata.Mode&unix.S_IFMT != unix.S_IFREG ||
		metadata.Uid != uint32(os.Geteuid()) ||
		metadata.Mode&0o777 != 0o600 ||
		metadata.Mode&(unix.S_ISUID|unix.S_ISGID|unix.S_ISVTX) != 0 {
		return ErrEvidenceDurability
	}
	return nil
}

func bundleLinuxPathParts(path string) ([]string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrEvidenceDurability
	}
	trimmed := strings.TrimPrefix(path, string(os.PathSeparator))
	if trimmed == "" {
		return nil, ErrEvidenceDurability
	}
	parts := strings.Split(trimmed, string(os.PathSeparator))
	for _, part := range parts {
		if !validBundleLinuxLeaf(part) {
			return nil, ErrEvidenceDurability
		}
	}
	return parts, nil
}

func validBundleLinuxLeaf(name string) bool {
	return name != "" &&
		name != "." &&
		name != ".." &&
		!strings.ContainsRune(name, os.PathSeparator)
}
