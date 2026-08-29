package candidate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
)

func TestInspectGitSourceUsesExactTreeEncodingAndWhitelist(t *testing.T) {
	repository := t.TempDir()
	runTestCommand(t, repository, nil, "git", "init", "-q")
	runTestCommand(t, repository, nil, "git", "config", "user.name", "Digest Test")
	runTestCommand(t, repository, nil, "git", "config", "user.email", "digest@example.invalid")
	runTestCommand(t, repository, nil, "git", "config", "commit.gpgsign", "false")
	a := []byte("alpha\n")
	b := []byte("#!/bin/sh\necho beta\n")
	writeTestFile(t, repository, "a.txt", a, 0o644)
	writeTestFile(t, repository, "bin.sh", b, 0o644)
	writeTestFile(t, repository, "docs/upgrade/status.md", []byte("status one\n"), 0o644)
	writeTestFile(t, repository, "docs/compatibility/auth-capabilities.md", []byte("auth one\n"), 0o644)
	writeTestFile(t, repository, "docs/evidence/releases/v1.0.0/note.md", []byte("evidence one\n"), 0o644)
	runTestCommand(t, repository, nil, "git", "add", "--", ".")
	runTestCommand(t, repository, nil, "git", "update-index", "--chmod=+x", "bin.sh")
	commitTestTree(t, repository, "base")
	baseCommit := strings.TrimSpace(runTestCommand(t, repository, nil, "git", "rev-parse", "HEAD"))
	base, err := InspectGitSource(context.Background(), repository, baseCommit)
	if err != nil {
		t.Fatal(err)
	}
	if base.Commit != baseCommit || base.Tree != strings.TrimSpace(runTestCommand(t, repository, nil, "git", "rev-parse", "HEAD^{tree}")) || base.CommitterEpoch != 1788048000 {
		t.Fatalf("source identity = %#v", base)
	}
	want := sha256.New()
	writeBuildInputRecord(want, "a.txt", "100644", a)
	writeBuildInputRecord(want, "bin.sh", "100755", b)
	if base.BuildInputSHA256 != hex.EncodeToString(want.Sum(nil)) {
		t.Fatalf("build input digest = %s, want %x", base.BuildInputSHA256, want.Sum(nil))
	}

	writeTestFile(t, repository, "docs/upgrade/status.md", []byte("status two\n"), 0o644)
	writeTestFile(t, repository, "docs/compatibility/auth-capabilities.md", []byte("auth two\n"), 0o644)
	writeTestFile(t, repository, "docs/evidence/releases/v1.0.0/note.md", []byte("evidence two\n"), 0o644)
	runTestCommand(t, repository, nil, "git", "add", "--", ".")
	commitTestTree(t, repository, "whitelist")
	whitelistCommit := strings.TrimSpace(runTestCommand(t, repository, nil, "git", "rev-parse", "HEAD"))
	whitelist, err := InspectGitSource(context.Background(), repository, whitelistCommit)
	if err != nil || whitelist.BuildInputSHA256 != base.BuildInputSHA256 || whitelist.Tree == base.Tree {
		t.Fatalf("whitelist-only source = %#v, %v", whitelist, err)
	}

	writeTestFile(t, repository, "new.txt", []byte("new\n"), 0o644)
	runTestCommand(t, repository, nil, "git", "add", "--", "new.txt")
	commitTestTree(t, repository, "new input")
	newCommit := strings.TrimSpace(runTestCommand(t, repository, nil, "git", "rev-parse", "HEAD"))
	changed, err := InspectGitSource(context.Background(), repository, newCommit)
	if err != nil || changed.BuildInputSHA256 == whitelist.BuildInputSHA256 {
		t.Fatalf("new tracked input was not bound: %#v, %v", changed, err)
	}

	runTestCommand(t, repository, nil, "git", "update-index", "--chmod=+x", "a.txt")
	commitTestTree(t, repository, "mode")
	modeCommit := strings.TrimSpace(runTestCommand(t, repository, nil, "git", "rev-parse", "HEAD"))
	modeChanged, err := InspectGitSource(context.Background(), repository, modeCommit)
	if err != nil || modeChanged.BuildInputSHA256 == changed.BuildInputSHA256 {
		t.Fatalf("mode change was not bound: %#v, %v", modeChanged, err)
	}
}

func TestGitSourceRejectsInvalidIdentityAndModes(t *testing.T) {
	if _, err := InspectGitSource(nil, t.TempDir(), strings.Repeat("a", 40)); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("nil context error = %v", err)
	}
	invalid := []byte("120000 blob " + strings.Repeat("a", 40) + "\tlink\x00")
	if _, err := parseGitTree(invalid); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("symlink tree error = %v", err)
	}
	invalid = []byte("160000 commit " + strings.Repeat("a", 40) + "\tsubmodule\x00")
	if _, err := parseGitTree(invalid); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("submodule tree error = %v", err)
	}
	invalid = append([]byte("100644 blob "+strings.Repeat("a", 40)+"\t"), 0xff, 0)
	if _, err := parseGitTree(invalid); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("invalid UTF-8 path error = %v", err)
	}
	invalid = []byte("100644 blob " + strings.Repeat("a", 40) + "\t" + strings.Repeat("p", MaxBuildInputPathBytes+1) + "\x00")
	if _, err := parseGitTree(invalid); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("oversize Git path error = %v", err)
	}

	if !promotionWhitelistPath("docs/evidence/releases/v1.0.0/nested/file.json") ||
		promotionWhitelistPath("docs/evidence/releases/v1.0.0-lookalike/file.json") ||
		promotionWhitelistPath("docs/evidence/other/file.json") {
		t.Fatal("promotion whitelist boundaries are not exact")
	}
}

func TestGitReadsAreBoundedAndBlobHashStreams(t *testing.T) {
	repository := t.TempDir()
	runTestCommand(t, repository, nil, "git", "init", "-q")
	runTestCommand(t, repository, nil, "git", "config", "user.name", "Bounded Test")
	runTestCommand(t, repository, nil, "git", "config", "user.email", "bounded@example.invalid")
	runTestCommand(t, repository, nil, "git", "config", "commit.gpgsign", "false")
	payload := []byte(strings.Repeat("streamed-", 8192))
	writeTestFile(t, repository, "payload.bin", payload, 0o644)
	runTestCommand(t, repository, nil, "git", "add", "--", "payload.bin")
	commitTestTree(t, repository, "bounded blob")
	commit := strings.TrimSpace(runTestCommand(t, repository, nil, "git", "rev-parse", "HEAD"))
	oid := strings.TrimSpace(runTestCommand(t, repository, nil, "git", "rev-parse", "HEAD:payload.bin"))

	size, digest, err := hashGitBlob(context.Background(), repository, oid)
	wantDigest := sha256.Sum256(payload)
	if err != nil || size != int64(len(payload)) || digest != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("streamed blob = size %d digest %q err %v", size, digest, err)
	}
	if _, err := readGitBlob(context.Background(), repository, commit, "payload.bin", int64(len(payload)-1)); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("bounded blob read error = %v", err)
	}
	content, err := readGitBlob(context.Background(), repository, commit, "payload.bin", int64(len(payload)))
	if err != nil || !bytes.Equal(content, payload) {
		t.Fatalf("bounded blob read mismatch: bytes=%d err=%v", len(content), err)
	}
	if _, err := runGitBounded(context.Background(), repository, 1, "rev-parse", "HEAD"); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("bounded scalar output error = %v", err)
	}
}

func writeBuildInputRecord(output io.Writer, name, mode string, content []byte) {
	digest := sha256.Sum256(content)
	_, _ = output.Write([]byte(name))
	_, _ = output.Write([]byte{0})
	_, _ = output.Write([]byte(mode))
	_, _ = output.Write([]byte{0})
	_, _ = output.Write([]byte(strconv.Itoa(len(content))))
	_, _ = output.Write([]byte{0})
	_, _ = output.Write([]byte(hex.EncodeToString(digest[:])))
	_, _ = output.Write([]byte{'\n'})
}

func commitTestTree(t *testing.T, repository, message string) {
	t.Helper()
	environment := []string{"GIT_AUTHOR_DATE=2026-08-30T00:00:00Z", "GIT_COMMITTER_DATE=2026-08-30T00:00:00Z"}
	runTestCommand(t, repository, environment, "git", "commit", "-q", "-m", message)
}
