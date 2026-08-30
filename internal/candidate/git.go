package candidate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// GitSource is the immutable identity and build-input digest derived directly
// from a commit, never from the checkout worktree.
type GitSource struct {
	Commit           string
	Tree             string
	CommitterEpoch   int64
	BuildInputSHA256 string
}

type gitTreeEntry struct {
	mode string
	oid  string
	path []byte
}

// InspectGitSource verifies a full commit ID, resolves its tree and committer
// time, and hashes every non-whitelisted tracked regular file.
func InspectGitSource(ctx context.Context, repository, commit string) (GitSource, error) {
	if ctx == nil || repository == "" || !lowerHex40.MatchString(commit) {
		return GitSource{}, ErrInvalidSource
	}
	resolved, err := runGit(ctx, repository, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil || strings.TrimSpace(string(resolved)) != commit {
		return GitSource{}, ErrInvalidSource
	}
	treeRaw, err := runGit(ctx, repository, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return GitSource{}, ErrInvalidSource
	}
	tree := strings.TrimSpace(string(treeRaw))
	if !lowerHex40.MatchString(tree) {
		return GitSource{}, ErrInvalidSource
	}
	epochRaw, err := runGit(ctx, repository, "show", "-s", "--format=%ct", commit)
	if err != nil {
		return GitSource{}, ErrInvalidSource
	}
	epochText := strings.TrimSpace(string(epochRaw))
	if epochText == "" || strings.HasPrefix(epochText, "+") {
		return GitSource{}, ErrInvalidSource
	}
	epoch, err := strconv.ParseInt(epochText, 10, 64)
	if err != nil || epoch < 1 {
		return GitSource{}, ErrInvalidSource
	}

	buildInputSHA256, err := hashBuildInput(ctx, repository, commit)
	if err != nil {
		return GitSource{}, ErrInvalidSource
	}
	return GitSource{
		Commit:           commit,
		Tree:             tree,
		CommitterEpoch:   epoch,
		BuildInputSHA256: buildInputSHA256,
	}, nil
}

const maxGitTreeRecordBytes = MaxBuildInputPathBytes + 128

func hashBuildInput(ctx context.Context, repository, commit string) (string, error) {
	command, stdout, err := startGitCommand(ctx, repository, "ls-tree", "-rz", "--full-tree", "-r", commit)
	if err != nil {
		return "", ErrInvalidSource
	}
	waited := false
	defer func() {
		if !waited {
			abortGitCommand(command, stdout)
		}
	}()

	reader := bufio.NewReaderSize(stdout, maxGitTreeRecordBytes)
	hash := sha256.New()
	var previous []byte
	seen := false
	for {
		record, readErr := reader.ReadSlice(0)
		if readErr == io.EOF && len(record) == 0 {
			break
		}
		if readErr != nil || len(record) < 2 || record[len(record)-1] != 0 {
			return "", ErrInvalidSource
		}
		entry, err := parseGitTreeRecord(record[:len(record)-1])
		if err != nil {
			return "", ErrInvalidSource
		}
		if previous != nil && bytes.Compare(previous, entry.path) >= 0 {
			return "", ErrInvalidSource
		}
		previous = append(previous[:0], entry.path...)
		seen = true
		if promotionWhitelistPath(string(entry.path)) {
			continue
		}
		size, digest, err := hashGitBlob(ctx, repository, entry.oid)
		if err != nil {
			return "", ErrInvalidSource
		}
		_, _ = hash.Write(entry.path)
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(entry.mode))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strconv.FormatInt(size, 10)))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(digest))
		_, _ = hash.Write([]byte{'\n'})
	}
	if !seen {
		return "", ErrInvalidSource
	}
	waited = true
	if err := command.Wait(); err != nil {
		return "", ErrInvalidSource
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func parseGitTree(raw []byte) ([]gitTreeEntry, error) {
	if len(raw) == 0 || raw[len(raw)-1] != 0 {
		return nil, ErrInvalidSource
	}
	records := bytes.Split(raw[:len(raw)-1], []byte{0})
	entries := make([]gitTreeEntry, 0, len(records))
	for _, record := range records {
		entry, err := parseGitTreeRecord(record)
		if err != nil {
			return nil, ErrInvalidSource
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return bytes.Compare(entries[i].path, entries[j].path) < 0 })
	for index := 1; index < len(entries); index++ {
		if bytes.Equal(entries[index-1].path, entries[index].path) {
			return nil, ErrInvalidSource
		}
	}
	return entries, nil
}

func parseGitTreeRecord(record []byte) (gitTreeEntry, error) {
	tab := bytes.IndexByte(record, '\t')
	if tab < 0 {
		return gitTreeEntry{}, ErrInvalidSource
	}
	metadata := bytes.Fields(record[:tab])
	name := append([]byte(nil), record[tab+1:]...)
	if len(metadata) != 3 || (string(metadata[0]) != "100644" && string(metadata[0]) != "100755") ||
		string(metadata[1]) != "blob" || !lowerHex40.Match(metadata[2]) ||
		len(name) == 0 || len(name) > MaxBuildInputPathBytes || !utf8.Valid(name) {
		return gitTreeEntry{}, ErrInvalidSource
	}
	return gitTreeEntry{
		mode: string(metadata[0]),
		oid:  string(metadata[2]),
		path: name,
	}, nil
}

func promotionWhitelistPath(name string) bool {
	return name == "docs/upgrade/status.md" ||
		name == "docs/compatibility/auth-capabilities.md" ||
		strings.HasPrefix(name, "docs/evidence/releases/v1.0.0/")
}

const maxGitScalarOutputBytes int64 = 4 * 1024

func runGit(ctx context.Context, repository string, args ...string) ([]byte, error) {
	return runGitBounded(ctx, repository, maxGitScalarOutputBytes, args...)
}

func runGitBounded(ctx context.Context, repository string, maximum int64, args ...string) ([]byte, error) {
	if ctx == nil || repository == "" || maximum < 1 {
		return nil, ErrInvalidSource
	}
	command, stdout, err := startGitCommand(ctx, repository, args...)
	if err != nil {
		return nil, ErrInvalidSource
	}
	output, readErr := io.ReadAll(io.LimitReader(stdout, maximum+1))
	if readErr != nil || int64(len(output)) > maximum {
		abortGitCommand(command, stdout)
		return nil, ErrInvalidSource
	}
	if err := command.Wait(); err != nil {
		return nil, ErrInvalidSource
	}
	return output, nil
}

func startGitCommand(ctx context.Context, repository string, args ...string) (*exec.Cmd, io.ReadCloser, error) {
	commandArgs := append([]string{"-C", repository}, args...)
	command := exec.CommandContext(ctx, "git", commandArgs...)
	command.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1", "LC_ALL=C")
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, ErrInvalidSource
	}
	if err := command.Start(); err != nil {
		_ = stdout.Close()
		return nil, nil, ErrInvalidSource
	}
	return command, stdout, nil
}

func abortGitCommand(command *exec.Cmd, stdout io.ReadCloser) {
	if stdout != nil {
		_ = stdout.Close()
	}
	if command == nil || command.Process == nil || command.ProcessState != nil {
		return
	}
	_ = command.Process.Kill()
	_ = command.Wait()
}

func hashGitBlob(ctx context.Context, repository, oid string) (int64, string, error) {
	if !lowerHex40.MatchString(oid) {
		return 0, "", ErrInvalidSource
	}
	sizeRaw, err := runGitBounded(ctx, repository, 64, "cat-file", "-s", oid)
	if err != nil {
		return 0, "", ErrInvalidSource
	}
	sizeText := strings.TrimSpace(string(sizeRaw))
	size, err := strconv.ParseInt(sizeText, 10, 64)
	if err != nil || size < 0 || strconv.FormatInt(size, 10) != sizeText {
		return 0, "", ErrInvalidSource
	}

	command, stdout, err := startGitCommand(ctx, repository, "cat-file", "blob", oid)
	if err != nil {
		return 0, "", ErrInvalidSource
	}
	hash := sha256.New()
	written, readErr := io.CopyN(hash, stdout, size)
	if readErr != nil || written != size {
		abortGitCommand(command, stdout)
		return 0, "", ErrInvalidSource
	}
	extra, extraErr := io.CopyN(io.Discard, stdout, 1)
	if extra != 0 || extraErr != io.EOF {
		abortGitCommand(command, stdout)
		return 0, "", ErrInvalidSource
	}
	if err := command.Wait(); err != nil {
		return 0, "", ErrInvalidSource
	}
	return size, hex.EncodeToString(hash.Sum(nil)), nil
}

func readGitBlob(ctx context.Context, repository, commit, name string, maximum int64) ([]byte, error) {
	if !validAssetName(name) || maximum < 1 {
		return nil, ErrInvalidSource
	}
	content, err := runGitBounded(ctx, repository, maximum, "show", commit+":"+name)
	if err != nil || len(content) == 0 {
		return nil, ErrInvalidSource
	}
	return content, nil
}
