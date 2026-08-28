//go:build linux || darwin

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

const trustedDarwinVarTarget = "private/var"

const (
	credentialDirectoryOpenFlags = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_DIRECTORY | unix.O_NOFOLLOW
	credentialFileOpenFlags      = unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
)

type trustedSystemAliasSnapshot struct {
	stat   unix.Stat_t
	target string
}

func openRestrictedPasswordFile(path string) (*os.File, error) {
	absPath, parts, err := absoluteCredentialPathParts(path)
	if err != nil {
		return nil, err
	}

	dirFD, firstPart, err := openCredentialTraversalRoot(parts)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = unix.Close(dirFD)
	}()

	for _, part := range parts[firstPart : len(parts)-1] {
		nextFD, openErr := openatRetry(dirFD, part, credentialDirectoryOpenFlags, 0)
		if openErr != nil {
			return nil, classifyCredentialDirectoryOpenError(dirFD, part, openErr)
		}
		if err := unix.Close(dirFD); err != nil {
			_ = unix.Close(nextFD)
			return nil, fmt.Errorf("close credential path component: %w", err)
		}
		dirFD = nextFD
	}

	name := parts[len(parts)-1]
	fileFD, openErr := openatRetry(dirFD, name, credentialFileOpenFlags, 0)
	if openErr != nil {
		return nil, classifyCredentialFileOpenError(dirFD, name, openErr)
	}
	file := os.NewFile(uintptr(fileFD), absPath)
	if file == nil {
		_ = unix.Close(fileFD)
		return nil, fmt.Errorf("open credential file without following links")
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect opened credential file: %w", err)
	}
	owner, err := unixFileOwner(info)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := validateUnixCredentialMetadata(info.Mode(), owner, uint64(os.Geteuid())); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func absoluteCredentialPathParts(path string) (string, []string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", nil, fmt.Errorf("resolve credential path: %w", err)
	}
	absPath = filepath.Clean(absPath)
	parts := strings.Split(strings.TrimPrefix(absPath, string(os.PathSeparator)), string(os.PathSeparator))
	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		return "", nil, fmt.Errorf("credential path does not name a file")
	}
	return absPath, parts, nil
}

func openCredentialTraversalRoot(parts []string) (int, int, error) {
	rootFD, err := openRetry(string(os.PathSeparator), credentialDirectoryOpenFlags, 0)
	if err != nil {
		return -1, 0, fmt.Errorf("open credential path root without following links: %w", err)
	}
	if !selectTrustedDarwinVarAlias(runtime.GOOS, parts) {
		return rootFD, 0, nil
	}

	anchorFD, err := openTrustedDarwinVarAnchor(rootFD)
	if err != nil {
		_ = unix.Close(rootFD)
		return -1, 0, err
	}
	if err := unix.Close(rootFD); err != nil {
		_ = unix.Close(anchorFD)
		return -1, 0, fmt.Errorf("close credential path root: %w", err)
	}
	return anchorFD, 1, nil
}

func selectTrustedDarwinVarAlias(goos string, parts []string) bool {
	return goos == "darwin" && len(parts) > 1 && parts[0] == "var"
}

func openTrustedDarwinVarAnchor(rootFD int) (int, error) {
	before, err := inspectTrustedDarwinVarAlias(rootFD)
	if err != nil {
		return -1, fmt.Errorf("inspect trusted macOS /var alias: %w", err)
	}
	if !trustedDarwinVarAlias(runtime.GOOS, 0, "var", before, before) {
		return -1, fmt.Errorf("credential path contains an untrusted symbolic link")
	}

	privateFD, err := openatRetry(rootFD, "private", credentialDirectoryOpenFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("open trusted macOS credential anchor component: %w", err)
	}
	anchorFD, err := openatRetry(privateFD, "var", credentialDirectoryOpenFlags, 0)
	if err != nil {
		_ = unix.Close(privateFD)
		return -1, fmt.Errorf("open trusted macOS credential anchor: %w", err)
	}
	if err := unix.Close(privateFD); err != nil {
		_ = unix.Close(anchorFD)
		return -1, fmt.Errorf("close trusted macOS credential anchor component: %w", err)
	}

	after, err := inspectTrustedDarwinVarAlias(rootFD)
	if err != nil {
		_ = unix.Close(anchorFD)
		return -1, fmt.Errorf("reinspect trusted macOS /var alias: %w", err)
	}
	if !trustedDarwinVarAlias(runtime.GOOS, 0, "var", before, after) {
		_ = unix.Close(anchorFD)
		return -1, fmt.Errorf("trusted macOS /var alias changed while opening")
	}
	return anchorFD, nil
}

func inspectTrustedDarwinVarAlias(rootFD int) (trustedSystemAliasSnapshot, error) {
	var snapshot trustedSystemAliasSnapshot
	if err := fstatatRetry(rootFD, "var", &snapshot.stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return snapshot, err
	}
	if snapshot.stat.Mode&unix.S_IFMT != unix.S_IFLNK {
		return snapshot, nil
	}

	buffer := make([]byte, len(trustedDarwinVarTarget)+1)
	n, err := readlinkatRetry(rootFD, "var", buffer)
	if err != nil {
		return snapshot, err
	}
	snapshot.target = string(buffer[:n])
	return snapshot, nil
}

func trustedDarwinVarAlias(goos string, componentIndex int, component string, before, after trustedSystemAliasSnapshot) bool {
	if goos != "darwin" || componentIndex != 0 || component != "var" {
		return false
	}
	if before.stat.Mode&unix.S_IFMT != unix.S_IFLNK || after.stat.Mode&unix.S_IFMT != unix.S_IFLNK {
		return false
	}
	if before.stat.Uid != 0 || after.stat.Uid != 0 {
		return false
	}
	if before.target != trustedDarwinVarTarget || after.target != trustedDarwinVarTarget {
		return false
	}
	return before.stat.Dev == after.stat.Dev &&
		before.stat.Ino == after.stat.Ino &&
		before.stat.Mode == after.stat.Mode &&
		before.stat.Uid == after.stat.Uid
}

func classifyCredentialDirectoryOpenError(dirFD int, name string, openErr error) error {
	var stat unix.Stat_t
	if err := fstatatRetry(dirFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		switch stat.Mode & unix.S_IFMT {
		case unix.S_IFLNK:
			return fmt.Errorf("credential path contains a symbolic link")
		case unix.S_IFDIR:
			// Preserve the atomic open error for directories that could not be opened.
		default:
			return fmt.Errorf("credential path component is not a directory")
		}
	}
	return fmt.Errorf("open credential path component without following links: %w", openErr)
}

func classifyCredentialFileOpenError(dirFD int, name string, openErr error) error {
	var stat unix.Stat_t
	if err := fstatatRetry(dirFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err == nil && stat.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("credential file is a symbolic link")
	}
	return fmt.Errorf("open credential file without following links: %w", openErr)
}

func openRetry(path string, flags int, mode uint32) (int, error) {
	for {
		fd, err := unix.Open(path, flags, mode)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}

func openatRetry(dirFD int, path string, flags int, mode uint32) (int, error) {
	for {
		fd, err := unix.Openat(dirFD, path, flags, mode)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}

func fstatatRetry(dirFD int, path string, stat *unix.Stat_t, flags int) error {
	for {
		err := unix.Fstatat(dirFD, path, stat, flags)
		if !errors.Is(err, unix.EINTR) {
			return err
		}
	}
}

func readlinkatRetry(dirFD int, path string, buffer []byte) (int, error) {
	for {
		n, err := unix.Readlinkat(dirFD, path, buffer)
		if !errors.Is(err, unix.EINTR) {
			return n, err
		}
	}
}
