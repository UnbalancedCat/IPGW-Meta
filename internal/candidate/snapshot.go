package candidate

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// regularSnapshot keeps verification bound to one already-open file object.
// Paths are never reopened after the initial no-follow open.
type regularSnapshot struct {
	file     *os.File
	info     os.FileInfo
	metadata fileMetadata
	maximum  int64
}

func openRegularSnapshot(name string, maximum int64) (*regularSnapshot, error) {
	file, err := openRegularNoFollow(name)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return snapshotOpenRegular(file, maximum)
}

func openRegularSnapshotAt(directory *os.File, name string, maximum int64) (*regularSnapshot, error) {
	file, err := openRegularNoFollowAt(directory, name)
	if err != nil {
		return nil, ErrInvalidInput
	}
	return snapshotOpenRegular(file, maximum)
}

func snapshotOpenRegular(file *os.File, maximum int64) (*regularSnapshot, error) {
	if maximum < 1 {
		_ = file.Close()
		return nil, ErrInvalidInput
	}
	fail := func() (*regularSnapshot, error) {
		_ = file.Close()
		return nil, ErrInvalidInput
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum || hasMultipleLinks(file, info) {
		return fail()
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, maximum+1))
	if err != nil || written != info.Size() {
		return fail()
	}
	after, err := file.Stat()
	if err != nil || !stableFileInfo(info, after) {
		return fail()
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fail()
	}
	return &regularSnapshot{
		file: file, info: info, maximum: maximum,
		metadata: fileMetadata{size: written, sha256: hex.EncodeToString(hash.Sum(nil))},
	}, nil
}

func (snapshot *regularSnapshot) readAll() ([]byte, error) {
	if snapshot == nil || snapshot.file == nil {
		return nil, ErrInvalidInput
	}
	if _, err := snapshot.file.Seek(0, io.SeekStart); err != nil {
		return nil, ErrInvalidInput
	}
	content, err := io.ReadAll(io.LimitReader(snapshot.file, snapshot.maximum+1))
	if err != nil || int64(len(content)) != snapshot.info.Size() || metadataBytes(content) != snapshot.metadata {
		return nil, ErrInvalidInput
	}
	after, err := snapshot.file.Stat()
	if err != nil || !stableOpenedFileInfo(snapshot.info, after) || hasMultipleLinks(snapshot.file, after) {
		return nil, ErrInvalidInput
	}
	return content, nil
}

func (snapshot *regularSnapshot) unchanged() bool {
	if snapshot == nil || snapshot.file == nil {
		return false
	}
	if _, err := snapshot.file.Seek(0, io.SeekStart); err != nil {
		return false
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(snapshot.file, snapshot.maximum+1))
	if err != nil || written != snapshot.info.Size() ||
		(fileMetadata{size: written, sha256: hex.EncodeToString(hash.Sum(nil))}) != snapshot.metadata {
		return false
	}
	after, err := snapshot.file.Stat()
	if err != nil || !stableFileInfo(snapshot.info, after) || hasMultipleLinks(snapshot.file, after) {
		return false
	}
	_, err = snapshot.file.Seek(0, io.SeekStart)
	return err == nil
}

func (snapshot *regularSnapshot) close() {
	if snapshot != nil && snapshot.file != nil {
		_ = snapshot.file.Close()
		snapshot.file = nil
	}
}

func stableFileInfo(before, after os.FileInfo) bool {
	return before != nil && after != nil && os.SameFile(before, after) &&
		before.Mode() == after.Mode() && before.Size() == after.Size() && before.ModTime() == after.ModTime() &&
		sameFileFingerprint(before, after)
}

func stableOpenedFileInfo(before, after os.FileInfo) bool {
	return before != nil && after != nil && os.SameFile(before, after) &&
		before.Mode() == after.Mode() && before.Size() == after.Size() && before.ModTime() == after.ModTime()
}
