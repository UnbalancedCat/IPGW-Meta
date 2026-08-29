//go:build !windows && !linux

package livegate

import (
	"os"
	"syscall"
)

func ensureBundlePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return protectBundleDirectory(path)
}

func protectBundleDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrEvidenceDurability
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return verifyBundlePrivateDirectory(path)
}

func verifyBundlePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil ||
		info.Mode()&os.ModeSymlink != 0 ||
		!info.IsDir() ||
		info.Mode().Perm() != 0o700 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 ||
		!bundleOwnedByCurrentUser(info) {
		return ErrEvidenceDurability
	}
	return nil
}

func protectBundleFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return ErrEvidenceDurability
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return verifyBundlePrivateFile(path)
}

func verifyBundlePrivateFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil ||
		info.Mode()&os.ModeSymlink != 0 ||
		!info.Mode().IsRegular() ||
		info.Mode().Perm() != 0o600 ||
		info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 ||
		!bundleOwnedByCurrentUser(info) {
		return ErrEvidenceDurability
	}
	return nil
}

func openBundlePrivateFile(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil ||
		!info.Mode().IsRegular() ||
		info.Mode().Perm() != 0o600 ||
		!bundleOwnedByCurrentUser(info) {
		_ = file.Close()
		return nil, ErrEvidenceDurability
	}
	pathInfo, err := os.Lstat(path)
	if err != nil ||
		pathInfo.Mode()&os.ModeSymlink != 0 ||
		!os.SameFile(info, pathInfo) {
		_ = file.Close()
		return nil, ErrEvidenceDurability
	}
	return file, nil
}

func bundleOwnedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func syncBundleDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func createBundleStagingDirectory(parent string) (string, error) {
	stage, err := os.MkdirTemp(parent, ".livegate-stage-*")
	if err != nil {
		return "", err
	}
	if err := protectBundleDirectory(stage); err != nil {
		_ = os.RemoveAll(stage)
		return "", err
	}
	return stage, nil
}

func publishBundleDirectory(string, string) error {
	// The required no-replace directory primitive is currently implemented only
	// for Linux and Windows. Other targets fail closed.
	return ErrEvidenceDurability
}
