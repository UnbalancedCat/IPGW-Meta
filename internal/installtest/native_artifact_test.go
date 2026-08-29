package installtest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/UnbalancedCat/ipgw-meta/internal/candidate"
	"go.yaml.in/yaml/v3"
)

const (
	nativeArtifactRootEnv     = "IPGW_NATIVE_INSTALL_ARTIFACT_ROOT"
	nativeArtifactVersionEnv  = "IPGW_NATIVE_INSTALL_VERSION"
	nativeArtifactCommitEnv   = "IPGW_NATIVE_INSTALL_SOURCE_COMMIT"
	nativeArtifactTreeEnv     = "IPGW_NATIVE_INSTALL_SOURCE_TREE"
	nativeArtifactRequiredEnv = "IPGW_NATIVE_INSTALL_REQUIRED"
	nativeArtifactManifest    = "native-install-manifest.json"
)

type nativeInstallManifest struct {
	SchemaVersion           int    `json:"schema_version"`
	ArtifactKind            string `json:"artifact_kind"`
	SourceCommit            string `json:"source_commit"`
	SourceTree              string `json:"source_tree"`
	Version                 string `json:"version"`
	ReleaseSHA256SUMSSHA256 string `json:"release_sha256sums_sha256"`
}

type nativeInstallAsset struct {
	bundlePath    string
	bundleSHA256  string
	installerPath string
	version       string
}

type installedLauncherState struct {
	SchemaVersion int       `yaml:"schema_version"`
	Mode          string    `yaml:"mode"`
	Cohort        string    `yaml:"cohort"`
	ChosenAt      time.Time `yaml:"chosen_at"`
}

func assertFreshLauncherState(t *testing.T, content []byte) {
	t.Helper()
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	var state installedLauncherState
	if err := decoder.Decode(&state); err != nil {
		t.Fatalf("decode fresh launcher state: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("fresh launcher state has trailing YAML: %v", err)
	}
	if state.SchemaVersion != 1 || state.Mode != "meta" || state.Cohort != "new-install" || state.ChosenAt.IsZero() {
		t.Fatalf("fresh launcher state mismatch: %#v", state)
	}
}

func nativeInstallArtifactConfigured() bool {
	return strings.TrimSpace(os.Getenv(nativeArtifactRootEnv)) != ""
}

func nativeInstallArtifactRequired() bool {
	return strings.TrimSpace(os.Getenv(nativeArtifactRequiredEnv)) == "1"
}

func prepareNativeInstallAsset(t *testing.T, privateRoot string) nativeInstallAsset {
	t.Helper()
	artifactRoot := requireNativeArtifactEnv(t, nativeArtifactRootEnv)
	version := requireNativeArtifactEnv(t, nativeArtifactVersionEnv)
	sourceCommit := requireNativeArtifactEnv(t, nativeArtifactCommitEnv)
	sourceTree := requireNativeArtifactEnv(t, nativeArtifactTreeEnv)
	if !filepath.IsAbs(artifactRoot) {
		t.Fatalf("%s must be absolute", nativeArtifactRootEnv)
	}
	assertPlainDirectory(t, artifactRoot, "native install artifact root")
	candidateManifestPath := filepath.Join(artifactRoot, "candidate-manifest.json")
	_, candidateManifestErr := os.Lstat(candidateManifestPath)
	candidateMode := candidateManifestErr == nil
	if candidateManifestErr != nil && !os.IsNotExist(candidateManifestErr) {
		t.Fatalf("inspect candidate manifest: %v", candidateManifestErr)
	}
	releaseSHA256SUMSSHA256 := ""
	if candidateMode {
		result, err := candidate.Verify(artifactRoot)
		if err != nil {
			t.Fatalf("verify candidate set: %v", err)
		}
		if version != candidate.Version || result.SourceCommit != sourceCommit || result.SourceTree != sourceTree {
			t.Fatal("candidate set source or version mismatch")
		}
	} else {
		assertExactDirectoryEntries(t, artifactRoot, []string{nativeArtifactManifest, "release"})
		manifestPath := filepath.Join(artifactRoot, nativeArtifactManifest)
		manifestBytes := readBoundedRegularFile(t, manifestPath, 64*1024, "native install artifact manifest")
		var manifest nativeInstallManifest
		decoder := json.NewDecoder(strings.NewReader(string(manifestBytes)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&manifest); err != nil {
			t.Fatalf("decode native install artifact manifest: %v", err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			t.Fatalf("native install artifact manifest has trailing JSON: %v", err)
		}
		if manifest.SchemaVersion != 1 || manifest.ArtifactKind != "ipgw-native-install-v1" {
			t.Fatal("native install artifact manifest identity mismatch")
		}
		if manifest.SourceCommit != sourceCommit || manifest.SourceTree != sourceTree || manifest.Version != version {
			t.Fatal("native install artifact source or version mismatch")
		}
		for name, value := range map[string]string{
			"source commit":           manifest.SourceCommit,
			"source tree":             manifest.SourceTree,
			"release checksum digest": manifest.ReleaseSHA256SUMSSHA256,
		} {
			if !isLowerHex(value, 64) && name == "release checksum digest" {
				t.Fatalf("%s is not lowercase SHA-256", name)
			}
			if name != "release checksum digest" && !isLowerHex(value, 40) {
				t.Fatalf("%s is not a full lowercase Git object ID", name)
			}
		}
		releaseSHA256SUMSSHA256 = manifest.ReleaseSHA256SUMSSHA256
	}
	if version == "" || strings.ContainsAny(version, "\r\n") {
		t.Fatal("native install artifact version is empty or multiline")
	}

	releaseDir := filepath.Join(artifactRoot, "release")
	assertPlainDirectory(t, releaseDir, "native install release directory")
	expectedChecksummed := []string{
		"install.ps1",
		"install.sh",
		"ipgw-meta-darwin-amd64.tar.gz",
		"ipgw-meta-darwin-arm64.tar.gz",
		"ipgw-meta-linux-amd64.tar.gz",
		"ipgw-meta-linux-arm64.tar.gz",
		"ipgw-meta-windows-amd64.zip",
		"ipgw-meta-windows-arm64.zip",
	}
	expectedRelease := append([]string{"SHA256SUMS"}, expectedChecksummed...)
	if candidateMode {
		expectedRelease = append(expectedRelease, "release-manifest.json")
	}
	assertExactDirectoryEntries(t, releaseDir, expectedRelease)

	checksumPath := filepath.Join(releaseDir, "SHA256SUMS")
	checksumBytes := readBoundedRegularFile(t, checksumPath, 64*1024, "release SHA256SUMS")
	if !candidateMode && hashNativeFile(t, checksumPath) != releaseSHA256SUMSSHA256 {
		t.Fatal("release SHA256SUMS digest does not match artifact manifest")
	}
	checksums := parseNativeChecksums(t, checksumBytes, expectedChecksummed)
	for _, name := range expectedChecksummed {
		path := filepath.Join(releaseDir, name)
		info := assertPlainRegularFile(t, path, "checksummed release file")
		if info.Size() <= 0 {
			t.Fatalf("release file %s is empty", name)
		}
		if strings.HasPrefix(name, "ipgw-meta-") && info.Size() > 100*1024*1024 {
			t.Fatalf("release bundle %s exceeds 100 MiB", name)
		}
		if actual := hashNativeFile(t, path); actual != checksums[name] {
			t.Fatalf("release file %s does not match SHA256SUMS", name)
		}
	}

	target := runtime.GOOS + "-" + runtime.GOARCH
	bundleName := "ipgw-meta-" + target + ".tar.gz"
	installerName := "install.sh"
	if runtime.GOOS == "windows" {
		bundleName = "ipgw-meta-" + target + ".zip"
		installerName = "install.ps1"
	}
	if _, ok := checksums[bundleName]; !ok {
		t.Fatalf("native install artifact does not support runner target %s", target)
	}
	assertPlainDirectory(t, privateRoot, "native install private test root")
	bundleDestination := filepath.Join(privateRoot, "native-candidate"+filepath.Ext(bundleName))
	if strings.HasSuffix(bundleName, ".tar.gz") {
		bundleDestination = filepath.Join(privateRoot, "native-candidate.tar.gz")
	}
	installerDestination := filepath.Join(privateRoot, "native-"+installerName)
	copyNativeFile(t, filepath.Join(releaseDir, bundleName), bundleDestination, 0o600)
	copyNativeFile(t, filepath.Join(releaseDir, installerName), installerDestination, 0o700)
	if hashNativeFile(t, bundleDestination) != checksums[bundleName] {
		t.Fatal("private candidate bundle copy changed bytes")
	}
	if hashNativeFile(t, installerDestination) != checksums[installerName] {
		t.Fatal("private installer copy changed bytes")
	}
	return nativeInstallAsset{
		bundlePath:    bundleDestination,
		bundleSHA256:  checksums[bundleName],
		installerPath: installerDestination,
		version:       version,
	}
}

func requireNativeArtifactEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s is required when native artifact testing is enabled", name)
	}
	return value
}

func parseNativeChecksums(t *testing.T, content []byte, expected []string) map[string]string {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(lines) != len(expected) {
		t.Fatalf("release SHA256SUMS has %d lines, want %d", len(lines), len(expected))
	}
	checksums := make(map[string]string, len(lines))
	for _, line := range lines {
		if len(line) < 67 {
			t.Fatal("release SHA256SUMS contains a malformed entry")
		}
		digest := line[:64]
		marker := line[64:66]
		name := line[66:]
		if (marker != "  " && marker != " *") || !isLowerHex(digest, 64) || name == "" || strings.ContainsAny(name, "/\\\r\n") {
			t.Fatal("release SHA256SUMS contains a malformed entry")
		}
		if _, duplicate := checksums[name]; duplicate {
			t.Fatalf("release SHA256SUMS contains duplicate %s", name)
		}
		checksums[name] = digest
	}
	want := append([]string(nil), expected...)
	got := make([]string, 0, len(checksums))
	for name := range checksums {
		got = append(got, name)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatal("release SHA256SUMS file set mismatch")
	}
	return checksums
}

func assertExactDirectoryEntries(t *testing.T, path string, expected []string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatalf("read directory %s: %v", path, err)
	}
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	want := append([]string(nil), expected...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("directory %s entries mismatch: got %v, want %v", path, got, want)
	}
}

func assertPlainDirectory(t *testing.T, path, label string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("inspect %s: %v", label, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("%s must be a plain directory", label)
	}
	return info
}

func assertPlainRegularFile(t *testing.T, path, label string) os.FileInfo {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("inspect %s %s: %v", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		t.Fatalf("%s %s must be a plain regular file", label, path)
	}
	return info
}

func readBoundedRegularFile(t *testing.T, path string, limit int64, label string) []byte {
	t.Helper()
	info := assertPlainRegularFile(t, path, label)
	if info.Size() <= 0 || info.Size() > limit {
		t.Fatalf("%s size %d is outside 1..%d", label, info.Size(), limit)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", label, err)
	}
	return content
}

func hashNativeFile(t *testing.T, path string) string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open file for SHA-256 %s: %v", path, err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		t.Fatalf("hash file %s: %v", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func copyNativeFile(t *testing.T, source, destination string, mode os.FileMode) {
	t.Helper()
	sourceInfo := assertPlainRegularFile(t, source, "native artifact source")
	input, err := os.Open(source)
	if err != nil {
		t.Fatalf("open native artifact source: %v", err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		t.Fatalf("create private native artifact copy: %v", err)
	}
	written, copyErr := io.Copy(output, input)
	if copyErr == nil {
		copyErr = output.Sync()
	}
	closeErr := output.Close()
	if copyErr != nil {
		t.Fatalf("copy native artifact source: %v", copyErr)
	}
	if closeErr != nil {
		t.Fatalf("close private native artifact copy: %v", closeErr)
	}
	if written != sourceInfo.Size() {
		t.Fatalf("private native artifact copy size %d, want %d", written, sourceInfo.Size())
	}
}

func isLowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func nativeAssetDescription(asset nativeInstallAsset) string {
	return fmt.Sprintf("version=%s sha256=%s", asset.version, asset.bundleSHA256)
}
