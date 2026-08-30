package candidate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type fileMetadata struct {
	size   int64
	sha256 string
}

func metadataBytes(content []byte) fileMetadata {
	digest := sha256.Sum256(content)
	return fileMetadata{size: int64(len(content)), sha256: hex.EncodeToString(digest[:])}
}

func metadataFile(name string, maximum int64) (fileMetadata, error) {
	snapshot, err := openRegularSnapshot(name, maximum)
	if err != nil {
		return fileMetadata{}, ErrVerify
	}
	defer snapshot.close()
	return snapshot.metadata, nil
}

func readRegular(name string, maximum int64) ([]byte, os.FileInfo, error) {
	snapshot, err := openRegularSnapshot(name, maximum)
	if err != nil {
		return nil, nil, ErrInvalidInput
	}
	defer snapshot.close()
	content, err := snapshot.readAll()
	if err != nil {
		return nil, nil, ErrInvalidInput
	}
	return content, snapshot.info, nil
}

func writeFileExclusive(name string, content []byte, mode os.FileMode) error {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return ErrAssemble
	}
	written, writeErr := file.Write(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(name)
		return ErrAssemble
	}
	return nil
}

func checksumBytes(files map[string]fileMetadata) ([]byte, error) {
	if len(files) == 0 {
		return nil, ErrInvalidInput
	}
	names := make([]string, 0, len(files))
	for name, metadata := range files {
		if !validAssetName(name) || metadata.size < 1 || !lowerHex64.MatchString(metadata.sha256) {
			return nil, ErrInvalidInput
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var output bytes.Buffer
	for _, name := range names {
		output.WriteString(files[name].sha256)
		output.WriteString("  ")
		output.WriteString(name)
		output.WriteByte('\n')
	}
	return output.Bytes(), nil
}

func parseChecksums(raw []byte, expected []string) (map[string]string, error) {
	if len(raw) == 0 || raw[len(raw)-1] != '\n' || !utf8.Valid(raw) {
		return nil, ErrVerify
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != len(expected) {
		return nil, ErrVerify
	}
	result := make(map[string]string, len(lines))
	for index, line := range lines {
		if len(line) < 67 || line[64:66] != "  " || !lowerHex64.MatchString(line[:64]) ||
			line[66:] != expected[index] {
			return nil, ErrVerify
		}
		result[line[66:]] = line[:64]
	}
	return result, nil
}

func normalizeText(content []byte) ([]byte, error) {
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		return nil, ErrInvalidInput
	}
	normalized := bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
	if bytes.IndexByte(normalized, '\r') >= 0 {
		return nil, ErrInvalidInput
	}
	return normalized, nil
}

func assetFromMetadata(name, platform string, metadata fileMetadata) Asset {
	return Asset{Name: name, Platform: platform, Size: metadata.size, SHA256: metadata.sha256}
}

func decimalPositive(value string) (int64, bool) {
	if value == "" || value[0] == '0' || strings.HasPrefix(value, "+") {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return parsed, err == nil && parsed > 0
}

func absoluteClean(name string) bool {
	return name != "" && filepath.IsAbs(name) && filepath.Clean(name) == name
}
