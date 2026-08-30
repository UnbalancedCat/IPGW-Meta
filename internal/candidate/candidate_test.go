package candidate

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type assemblyFixture struct {
	repository string
	commit     string
	build      string
	parent     string
}

func TestAssembleRejectsWrongPackagerHost(t *testing.T) {
	if validPackagerHost() {
		t.Skip("running on the frozen linux-amd64 Go toolchain")
	}
	if _, err := Assemble(context.Background(), AssembleOptions{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Assemble() error = %v, want ErrInvalidInput", err)
	}
}

func TestAssembleVerifyAndDeterminism(t *testing.T) {
	fixture := newAssemblyFixture(t)
	first := filepath.Join(fixture.parent, "candidate-one")
	second := filepath.Join(fixture.parent, "candidate-two")
	resultOne := assembleFixture(t, fixture, first, 42, 3)
	resultTwo := assembleFixture(t, fixture, second, 42, 3)
	if resultOne.CandidateSetSHA256 != resultTwo.CandidateSetSHA256 ||
		resultOne.BuildInputSHA256 != resultTwo.BuildInputSHA256 ||
		resultOne.SourceTree != resultTwo.SourceTree {
		t.Fatal("identical assembly inputs produced different identities")
	}
	compareTrees(t, first, second)
	verified, err := verifyCandidate(first, false)
	if err != nil || verified != resultOne {
		t.Fatalf("Verify() = %#v, %v; want %#v", verified, err, resultOne)
	}
	if count := countRegularFiles(t, first); count != 14 {
		t.Fatalf("candidate regular file count = %d, want 14", count)
	}
	assertLineCount(t, filepath.Join(first, "release", "SHA256SUMS"), 8)
	assertLineCount(t, filepath.Join(first, "SHA256SUMS"), 13)

	third := filepath.Join(fixture.parent, "candidate-three")
	assembleFixture(t, fixture, third, 43, 1)
	for _, expected := range expectedReleasePayloads() {
		left := readTestFile(t, filepath.Join(first, "release", expected.name))
		right := readTestFile(t, filepath.Join(third, "release", expected.name))
		if !bytes.Equal(left, right) {
			t.Fatalf("public payload %s changed with run identity", expected.name)
		}
	}
	if !bytes.Equal(readTestFile(t, filepath.Join(first, "release", "SHA256SUMS")), readTestFile(t, filepath.Join(third, "release", "SHA256SUMS"))) {
		t.Fatal("release SHA256SUMS changed with run identity")
	}
	if bytes.Equal(readTestFile(t, filepath.Join(first, "release", "release-manifest.json")), readTestFile(t, filepath.Join(third, "release", "release-manifest.json"))) {
		t.Fatal("release manifest did not bind run identity")
	}

	before := readTestFile(t, filepath.Join(first, "candidate-manifest.json"))
	_, err = assembleCandidate(context.Background(), fixture.options(first, 42, 3), false)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("no-clobber Assemble() error = %v", err)
	}
	if after := readTestFile(t, filepath.Join(first, "candidate-manifest.json")); !bytes.Equal(before, after) {
		t.Fatal("no-clobber failure changed existing candidate")
	}
}

func TestVerifyRejectsTamperingAndExtraMembers(t *testing.T) {
	fixture := newAssemblyFixture(t)
	base := filepath.Join(fixture.parent, "base")
	assembleFixture(t, fixture, base, 91, 2)
	tests := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"helper bytes", func(t *testing.T, root string) {
			path := filepath.Join(root, "test-tools", "ipgw-live-gate-linux-amd64")
			if err := os.WriteFile(path, append(readTestFile(t, path), 'x'), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"manifest whitespace", func(t *testing.T, root string) {
			path := filepath.Join(root, "candidate-manifest.json")
			if err := os.WriteFile(path, append(readTestFile(t, path), ' '), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"release archive", func(t *testing.T, root string) {
			path := filepath.Join(root, "release", "ipgw-meta-linux-amd64.tar.gz")
			raw := readTestFile(t, path)
			raw[len(raw)/2] ^= 1
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"extra member", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "extra"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing member", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "release", "install.ps1")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := filepath.Join(fixture.parent, "tamper-"+strings.ReplaceAll(test.name, " ", "-"))
			copyTree(t, base, root)
			test.mutate(t, root)
			if _, err := verifyCandidate(root, false); !errors.Is(err, ErrVerify) {
				t.Fatalf("Verify() error = %v, want ErrVerify", err)
			}
		})
	}
}

func TestCanonicalManifestIsClosedAndBindsRunnerProjection(t *testing.T) {
	manifest := validTestManifest()
	raw, err := EncodeManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > int(MaxManifestBytes) || raw[len(raw)-1] != '\n' || bytes.Contains(raw, []byte("\r")) {
		t.Fatal("manifest encoding is not bounded canonical LF JSON")
	}
	decoded, err := DecodeManifest(raw)
	if err != nil || decoded.CandidateID != manifest.CandidateID {
		t.Fatalf("DecodeManifest() = %#v, %v", decoded, err)
	}
	mutations := map[string][]byte{
		"leading whitespace": append([]byte(" "), raw...),
		"second LF":          append(append([]byte(nil), raw...), '\n'),
		"trailing value":     append(append([]byte(nil), raw...), []byte("{}")...),
		"duplicate top":      bytes.Replace(raw, []byte("{\"schema_version\":1"), []byte("{\"schema_version\":1,\"schema_version\":1"), 1),
		"duplicate nested":   bytes.Replace(raw, []byte("\"go_version\":\"go1.25.0\""), []byte("\"go_version\":\"go1.25.0\",\"go_version\":\"go1.25.0\""), 1),
		"unknown":            bytes.Replace(raw, []byte("\"revision\":"), []byte("\"unknown\":true,\"revision\":"), 1),
		"invalid utf8":       append(append([]byte(nil), raw[:len(raw)-2]...), 0xff, '}', '\n'),
	}
	for name, mutated := range mutations {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeManifest(mutated); !errors.Is(err, ErrInvalidManifest) {
				t.Fatalf("DecodeManifest() error = %v", err)
			}
		})
	}
	reversed := validTestManifest()
	reversed.LiveGateTargets[0], reversed.LiveGateTargets[1] = reversed.LiveGateTargets[1], reversed.LiveGateTargets[0]
	if _, err := EncodeManifest(reversed); !errors.Is(err, ErrInvalidManifest) {
		t.Fatalf("EncodeManifest(reversed targets) error = %v", err)
	}
}

func validTestManifest() Manifest {
	assets := make([]Asset, 0, 10)
	for _, expected := range expectedReleaseAssets() {
		assets = append(assets, Asset{Name: expected.name, Platform: expected.platform, Size: 1, SHA256: strings.Repeat("a", 64)})
	}
	tools := make([]Asset, 0, 2)
	for _, expected := range expectedTestTools() {
		tools = append(tools, Asset{Name: expected.name, Platform: expected.platform, Size: 1, SHA256: strings.Repeat("b", 64)})
	}
	return Manifest{
		SchemaVersion: 1, PlanID: PlanID, Revision: Revision, Version: Version,
		CandidateID: "v1.0.0-0123456789ab-42.3", SourceCommit: "0123456789abcdef0123456789abcdef01234567",
		SourceTree: strings.Repeat("c", 40), WorkflowRunID: 42, WorkflowRunAttempt: 3,
		Toolchain:        Toolchain{GoVersion: GoVersion, GoToolchain: GoToolchain, HostPlatform: HostPlatform, GOAMD64: GOAMD64, GOARM64: GOARM64, SourceDateEpoch: 1788048000, BuildRecipe: BuildRecipe},
		BuildInputSHA256: strings.Repeat("d", 64), ReleaseAssets: assets, TestTools: tools,
		LiveGateTargets: []LiveGateTarget{
			{Platform: "linux-amd64", Name: "ipgw-meta", Size: 1, SHA256: strings.Repeat("e", 64)},
			{Platform: "windows-amd64", Name: "ipgw-meta.exe", Size: 1, SHA256: strings.Repeat("f", 64)},
		},
	}
}

func newAssemblyFixture(t *testing.T) assemblyFixture {
	t.Helper()
	repository := t.TempDir()
	runTestCommand(t, repository, nil, "git", "init", "-q")
	runTestCommand(t, repository, nil, "git", "config", "user.name", "Candidate Test")
	runTestCommand(t, repository, nil, "git", "config", "user.email", "candidate@example.invalid")
	runTestCommand(t, repository, nil, "git", "config", "commit.gpgsign", "false")
	writeTestFile(t, repository, "LICENSE", []byte("synthetic license\n"), 0o644)
	writeTestFile(t, repository, "install.sh", []byte("#!/usr/bin/env bash\nset -eu\n"), 0o755)
	writeTestFile(t, repository, "install.ps1", []byte("param(\n)\nWrite-Output ok\n"), 0o644)
	writeTestFile(t, repository, "source.txt", []byte("source\n"), 0o644)
	runTestCommand(t, repository, nil, "git", "add", "--", ".")
	environment := []string{"GIT_AUTHOR_DATE=2026-08-30T00:00:00Z", "GIT_COMMITTER_DATE=2026-08-30T00:00:00Z"}
	runTestCommand(t, repository, environment, "git", "commit", "-q", "-m", "fixture")
	commit := strings.TrimSpace(runTestCommand(t, repository, nil, "git", "rev-parse", "HEAD"))
	build := filepath.Join(t.TempDir(), "build")
	for _, target := range targetOrder {
		for _, product := range productOrder {
			name := product
			if strings.HasPrefix(target, "windows-") {
				name += ".exe"
			}
			writeTestFile(t, build, filepath.Join(target, name), []byte(target+"/"+name+"\n"), 0o755)
		}
	}
	for _, expected := range expectedTestTools() {
		writeTestFile(t, build, filepath.Join("test-tools", filepath.Base(expected.name)), []byte(expected.name+"\n"), 0o755)
	}
	return assemblyFixture{repository: repository, commit: commit, build: build, parent: t.TempDir()}
}

func (fixture assemblyFixture) options(output string, runID, attempt int64) AssembleOptions {
	return AssembleOptions{
		RepositoryRoot: fixture.repository, SourceCommit: fixture.commit,
		CandidateID:   "v1.0.0-" + fixture.commit[:12] + "-" + strconv.FormatInt(runID, 10) + "." + strconv.FormatInt(attempt, 10),
		WorkflowRunID: runID, WorkflowRunAttempt: attempt, BuildDir: fixture.build, OutputDir: output,
	}
}

func assembleFixture(t *testing.T, fixture assemblyFixture, output string, runID, attempt int64) Result {
	t.Helper()
	result, err := assembleCandidate(context.Background(), fixture.options(output, runID, attempt), false)
	if err != nil {
		t.Fatalf("Assemble() error = %v", err)
	}
	return result
}

func writeTestFile(t *testing.T, root, name string, content []byte, mode os.FileMode) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func runTestCommand(t *testing.T, directory string, extraEnv []string, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = directory
	command.Env = append(os.Environ(), extraEnv...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", name, err, output)
	}
	return string(output)
}

func compareTrees(t *testing.T, left, right string) {
	t.Helper()
	leftFiles := treeFiles(t, left)
	rightFiles := treeFiles(t, right)
	if strings.Join(leftFiles, "\n") != strings.Join(rightFiles, "\n") {
		t.Fatal("candidate trees differ")
	}
	for _, name := range leftFiles {
		if !bytes.Equal(readTestFile(t, filepath.Join(left, name)), readTestFile(t, filepath.Join(right, name))) {
			t.Fatalf("candidate file %s differs", name)
		}
	}
}

func treeFiles(t *testing.T, root string) []string {
	t.Helper()
	var names []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			relative, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			names = append(names, relative)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(names)
	return names
}

func countRegularFiles(t *testing.T, root string) int { t.Helper(); return len(treeFiles(t, root)) }

func assertLineCount(t *testing.T, name string, count int) {
	t.Helper()
	raw := readTestFile(t, name)
	if got := len(strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")); got != count {
		t.Fatalf("%s lines = %d, want %d", name, got, count)
	}
}

func copyTree(t *testing.T, source, destination string) {
	t.Helper()
	err := filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, name)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, readTestFile(t, name), info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}
