package candidate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sort"
	"strings"
)

// Verify performs a closed-world verification of a downloaded candidate set.
func Verify(root string) (Result, error) {
	return verifyCandidate(root, true)
}

func verifyCandidate(root string, validateToolchain bool) (Result, error) {
	if !absoluteClean(root) {
		return Result{}, ErrVerify
	}
	rootEntryNames := []string{"SHA256SUMS", "candidate-manifest.json", "release", "test-tools"}
	releaseNames := make([]string, 0, 10)
	for _, item := range expectedReleaseAssets() {
		releaseNames = append(releaseNames, strings.TrimPrefix(item.name, "release/"))
	}
	toolNames := make([]string, 0, 2)
	for _, item := range expectedTestTools() {
		toolNames = append(toolNames, strings.TrimPrefix(item.name, "test-tools/"))
	}
	rootDirectory, err := openDirectorySnapshot(root)
	if err != nil {
		return Result{}, ErrVerify
	}
	defer rootDirectory.close()
	if !rootDirectory.exact(rootEntryNames) {
		return Result{}, ErrVerify
	}
	releaseDirectory, err := openDirectorySnapshotAt(rootDirectory, "release")
	if err != nil {
		return Result{}, ErrVerify
	}
	defer releaseDirectory.close()
	testToolsDirectory, err := openDirectorySnapshotAt(rootDirectory, "test-tools")
	if err != nil {
		return Result{}, ErrVerify
	}
	defer testToolsDirectory.close()
	if !releaseDirectory.exact(releaseNames) || !testToolsDirectory.exact(toolNames) {
		return Result{}, ErrVerify
	}

	paths := []string{"SHA256SUMS", "candidate-manifest.json"}
	for _, item := range expectedReleaseAssets() {
		paths = append(paths, item.name)
	}
	for _, item := range expectedTestTools() {
		paths = append(paths, item.name)
	}
	snapshots := make(map[string]*regularSnapshot, 14)
	defer func() {
		for _, snapshot := range snapshots {
			snapshot.close()
		}
	}()
	metadata := make(map[string]fileMetadata, 14)
	infos := make([]os.FileInfo, 0, 14)
	for _, name := range paths {
		directory := rootDirectory
		basename := name
		if strings.HasPrefix(name, "release/") {
			directory = releaseDirectory
			basename = strings.TrimPrefix(name, "release/")
		} else if strings.HasPrefix(name, "test-tools/") {
			directory = testToolsDirectory
			basename = strings.TrimPrefix(name, "test-tools/")
		}
		snapshot, err := openRegularSnapshotAt(directory.file, basename, artifactMaximum(name))
		if err != nil {
			return Result{}, ErrVerify
		}
		snapshots[name] = snapshot
		metadata[name] = snapshot.metadata
		infos = append(infos, snapshot.info)
	}
	if sameInputFile(infos) {
		return Result{}, ErrVerify
	}

	manifestRaw, err := snapshots["candidate-manifest.json"].readAll()
	if err != nil {
		return Result{}, ErrVerify
	}
	manifest, err := DecodeManifest(manifestRaw)
	if err != nil {
		return Result{}, ErrVerify
	}
	manifestDigest := sha256.Sum256(manifestRaw)
	candidateSetSHA := hex.EncodeToString(manifestDigest[:])

	rootFiles := make(map[string]fileMetadata, 13)
	for name, metric := range metadata {
		if name != "SHA256SUMS" {
			rootFiles[name] = metric
		}
	}
	expectedRootChecksums, err := checksumBytes(rootFiles)
	if err != nil {
		return Result{}, ErrVerify
	}
	actualRootChecksums, err := snapshots["SHA256SUMS"].readAll()
	if err != nil || !bytes.Equal(actualRootChecksums, expectedRootChecksums) {
		return Result{}, ErrVerify
	}
	rootChecksumNames := sortedMetadataNames(rootFiles)
	if _, err := parseChecksums(actualRootChecksums, rootChecksumNames); err != nil || len(rootChecksumNames) != 13 {
		return Result{}, ErrVerify
	}

	for _, asset := range manifest.ReleaseAssets {
		metric, ok := metadata[asset.Name]
		if !ok || asset.Size != metric.size || asset.SHA256 != metric.sha256 {
			return Result{}, ErrVerify
		}
	}
	for _, asset := range manifest.TestTools {
		metric, ok := metadata[asset.Name]
		if !ok || asset.Size != metric.size || asset.SHA256 != metric.sha256 {
			return Result{}, ErrVerify
		}
		if validateToolchain {
			content, err := snapshots[asset.Name].readAll()
			if err != nil || validateGoBinary(content, asset.Platform, "ipgw-live-gate", true, manifest.Toolchain.GoVersion) != nil {
				return Result{}, ErrVerify
			}
		}
	}

	releaseChecksumsRaw, err := snapshots["release/SHA256SUMS"].readAll()
	if err != nil {
		return Result{}, ErrVerify
	}
	releasePayloadMetadata := make(map[string]fileMetadata, 8)
	for _, item := range expectedReleasePayloads() {
		releasePayloadMetadata[item.name] = metadata["release/"+item.name]
	}
	expectedReleaseChecksums, err := checksumBytes(releasePayloadMetadata)
	if err != nil || !bytes.Equal(releaseChecksumsRaw, expectedReleaseChecksums) {
		return Result{}, ErrVerify
	}
	releasePayloadNames := sortedMetadataNames(releasePayloadMetadata)
	if _, err := parseChecksums(releaseChecksumsRaw, releasePayloadNames); err != nil || len(releasePayloadNames) != 8 {
		return Result{}, ErrVerify
	}

	releaseManifestRaw, err := snapshots["release/release-manifest.json"].readAll()
	if err != nil {
		return Result{}, ErrVerify
	}
	releaseManifest, err := DecodeReleaseManifest(releaseManifestRaw)
	if err != nil || releaseManifest.CandidateID != manifest.CandidateID ||
		releaseManifest.SourceCommit != manifest.SourceCommit ||
		releaseManifest.SourceTree != manifest.SourceTree ||
		releaseManifest.BuildInputSHA256 != manifest.BuildInputSHA256 ||
		releaseManifest.ReleaseSHA256SUMSSHA256 != metadata["release/SHA256SUMS"].sha256 {
		return Result{}, ErrVerify
	}
	for index, asset := range releaseManifest.Assets {
		expected := manifestReleaseAsset(manifest.ReleaseAssets, "release/"+asset.Name)
		if expected == nil || expected.Platform != asset.Platform || expected.Size != asset.Size || expected.SHA256 != asset.SHA256 ||
			asset.Size != releasePayloadMetadata[asset.Name].size || asset.SHA256 != releasePayloadMetadata[asset.Name].sha256 ||
			index >= len(releasePayloadNames) || asset.Name != releasePayloadNames[index] {
			return Result{}, ErrVerify
		}
	}

	bundles := make(map[string]bundleSummary, 6)
	for _, target := range targetOrder {
		extension := ".tar.gz"
		if strings.HasPrefix(target, "windows-") {
			extension = ".zip"
		}
		name := "ipgw-meta-" + target + extension
		raw, err := snapshots["release/"+name].readAll()
		if err != nil {
			return Result{}, ErrVerify
		}
		summary, err := verifyBundle(raw, target, manifest.Toolchain.SourceDateEpoch, validateToolchain)
		if err != nil {
			return Result{}, ErrVerify
		}
		bundles[target] = summary
	}
	for index, target := range []string{"linux-amd64", "windows-amd64"} {
		name := "ipgw-meta"
		if target == "windows-amd64" {
			name += ".exe"
		}
		metric, ok := bundles[target].members[name]
		projection := manifest.LiveGateTargets[index]
		if !ok || projection.Platform != target || projection.Name != name ||
			projection.Size != metric.size || projection.SHA256 != metric.sha256 {
			return Result{}, ErrVerify
		}
	}
	for _, name := range paths {
		if !snapshots[name].unchanged() {
			return Result{}, ErrVerify
		}
	}
	if !directoryUnchangedAt(rootDirectory, releaseDirectory, "release", releaseNames) ||
		!directoryUnchangedAt(rootDirectory, testToolsDirectory, "test-tools", toolNames) ||
		!directoryUnchanged(root, rootDirectory, rootEntryNames) {
		return Result{}, ErrVerify
	}
	return Result{
		Root: root, CandidateID: manifest.CandidateID, CandidateSetSHA256: candidateSetSHA,
		BuildInputSHA256: manifest.BuildInputSHA256, SourceCommit: manifest.SourceCommit, SourceTree: manifest.SourceTree,
	}, nil
}

func artifactMaximum(name string) int64 {
	if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip") {
		return MaxArchiveBytes
	}
	if strings.HasPrefix(name, "test-tools/") {
		return MaxBinaryBytes
	}
	if name == "release/install.sh" || name == "release/install.ps1" {
		return 4 * 1024 * 1024
	}
	return MaxManifestBytes
}

func sortedMetadataNames(values map[string]fileMetadata) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func manifestReleaseAsset(assets []Asset, name string) *Asset {
	for index := range assets {
		if assets[index].Name == name {
			return &assets[index]
		}
	}
	return nil
}
