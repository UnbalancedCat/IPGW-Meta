package candidate

import (
	"io"
	"os"
	"sort"
)

type directorySnapshot struct {
	file *os.File
	info os.FileInfo
}

func openDirectorySnapshot(name string) (*directorySnapshot, error) {
	file, err := openDirectoryNoFollow(name)
	if err != nil {
		return nil, ErrVerify
	}
	return snapshotOpenDirectory(file)
}

func openDirectorySnapshotAt(parent *directorySnapshot, name string) (*directorySnapshot, error) {
	if parent == nil || parent.file == nil {
		return nil, ErrVerify
	}
	file, err := openDirectoryNoFollowAt(parent.file, name)
	if err != nil {
		return nil, ErrVerify
	}
	return snapshotOpenDirectory(file)
}

func snapshotOpenDirectory(file *os.File) (*directorySnapshot, error) {
	info, err := file.Stat()
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		_ = file.Close()
		return nil, ErrVerify
	}
	return &directorySnapshot{file: file, info: info}, nil
}

func (directory *directorySnapshot) close() {
	if directory != nil && directory.file != nil {
		_ = directory.file.Close()
		directory.file = nil
	}
}

func (directory *directorySnapshot) exact(expected []string) bool {
	if directory == nil || directory.file == nil {
		return false
	}
	entries, err := directory.file.ReadDir(len(expected) + 1)
	if err != nil && err != io.EOF {
		return false
	}
	after, err := directory.file.Stat()
	if err != nil || !stableFileInfo(directory.info, after) || len(entries) != len(expected) {
		return false
	}
	want := append([]string(nil), expected...)
	got := make([]string, len(entries))
	for index, entry := range entries {
		got[index] = entry.Name()
	}
	sort.Strings(want)
	sort.Strings(got)
	for index := range want {
		if want[index] != got[index] {
			return false
		}
	}
	return true
}

func directoryUnchanged(name string, original *directorySnapshot, expected []string) bool {
	reopened, err := openDirectorySnapshot(name)
	if err != nil {
		return false
	}
	defer reopened.close()
	return stableFileInfo(original.info, reopened.info) && reopened.exact(expected)
}

func directoryUnchangedAt(parent, original *directorySnapshot, name string, expected []string) bool {
	reopened, err := openDirectorySnapshotAt(parent, name)
	if err != nil {
		return false
	}
	defer reopened.close()
	return stableFileInfo(original.info, reopened.info) && reopened.exact(expected)
}
