package candidate

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// AssembleOptions identifies prebuilt outputs. Assemble never invokes the Go
// toolchain; the fixed build directory must already contain all 20 outputs.
type AssembleOptions struct {
	RepositoryRoot     string
	SourceCommit       string
	CandidateID        string
	WorkflowRunID      int64
	WorkflowRunAttempt int64
	BuildDir           string
	OutputDir          string
}

// Result is the verified identity of a complete candidate set.
type Result struct {
	Root               string
	CandidateID        string
	CandidateSetSHA256 string
	BuildInputSHA256   string
	SourceCommit       string
	SourceTree         string
}

// Assemble creates a complete candidate in a private sibling directory,
// verifies it, and publishes it with one no-clobber rename.
func Assemble(ctx context.Context, options AssembleOptions) (Result, error) {
	if !validPackagerHost() {
		return Result{}, ErrInvalidInput
	}
	return assembleCandidate(ctx, options, true)
}

func assembleCandidate(ctx context.Context, options AssembleOptions, validateToolchain bool) (Result, error) {
	if ctx == nil || !absoluteClean(options.RepositoryRoot) || !absoluteClean(options.BuildDir) ||
		!absoluteClean(options.OutputDir) || options.OutputDir == options.RepositoryRoot ||
		options.OutputDir == options.BuildDir || options.WorkflowRunID < 1 || options.WorkflowRunAttempt < 1 ||
		!validCandidateID(options.CandidateID, options.SourceCommit, options.WorkflowRunID, options.WorkflowRunAttempt) {
		return Result{}, ErrInvalidInput
	}
	for _, directory := range []string{options.RepositoryRoot, options.BuildDir, filepath.Dir(options.OutputDir)} {
		info, err := os.Lstat(directory)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Result{}, ErrInvalidInput
		}
	}
	if _, err := os.Lstat(options.OutputDir); !os.IsNotExist(err) {
		return Result{}, ErrInvalidInput
	}

	source, err := InspectGitSource(ctx, options.RepositoryRoot, options.SourceCommit)
	if err != nil {
		return Result{}, ErrInvalidInput
	}
	if _, _, ok := zipDOSTime(timeFromEpoch(source.CommitterEpoch)); !ok {
		return Result{}, ErrInvalidInput
	}
	licenseRaw, err := readGitBlob(ctx, options.RepositoryRoot, source.Commit, "LICENSE", 4*1024*1024)
	license, err := normalizeText(licenseRaw)
	if err != nil || len(license) == 0 {
		return Result{}, ErrInvalidInput
	}
	installSHRaw, err := readGitBlob(ctx, options.RepositoryRoot, source.Commit, "install.sh", 4*1024*1024)
	if err != nil {
		return Result{}, ErrInvalidInput
	}
	installPSRaw, err := readGitBlob(ctx, options.RepositoryRoot, source.Commit, "install.ps1", 4*1024*1024)
	if err != nil {
		return Result{}, ErrInvalidInput
	}
	installSH, err := pinShellInstaller(installSHRaw)
	if err != nil {
		return Result{}, ErrInvalidInput
	}
	installPS, err := pinPowerShellInstaller(installPSRaw)
	if err != nil {
		return Result{}, ErrInvalidInput
	}

	stage, err := os.MkdirTemp(filepath.Dir(options.OutputDir), ".candidate-set.")
	if err != nil {
		return Result{}, ErrAssemble
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := os.Chmod(stage, 0o755); err != nil ||
		os.Mkdir(filepath.Join(stage, "release"), 0o755) != nil ||
		os.Mkdir(filepath.Join(stage, "test-tools"), 0o755) != nil {
		return Result{}, ErrAssemble
	}

	var inputInfos []os.FileInfo
	releasePayloadMetadata := make(map[string]fileMetadata, 8)
	var liveTargets []LiveGateTarget
	for _, target := range targetOrder {
		binaries := make(map[string][]byte, 3)
		for _, product := range productOrder {
			name := product
			if strings.HasPrefix(target, "windows-") {
				name += ".exe"
			}
			content, info, err := readRegular(filepath.Join(options.BuildDir, target, name), MaxBinaryBytes)
			if err != nil {
				return Result{}, ErrInvalidInput
			}
			if validateToolchain && validateGoBinary(content, target, product, false, GoVersion) != nil {
				return Result{}, ErrInvalidInput
			}
			inputInfos = append(inputInfos, info)
			binaries[name] = content
			if product == "ipgw-meta" && (target == "linux-amd64" || target == "windows-amd64") {
				metric := metadataBytes(content)
				liveTargets = append(liveTargets, LiveGateTarget{
					Platform: target, Name: name, Size: metric.size, SHA256: metric.sha256,
				})
			}
		}
		extension := ".tar.gz"
		if strings.HasPrefix(target, "windows-") {
			extension = ".zip"
		}
		basename := "ipgw-meta-" + target + extension
		bundlePath := filepath.Join(stage, "release", basename)
		if _, err := createBundle(bundlePath, target, binaries, license, source.CommitterEpoch); err != nil {
			return Result{}, ErrAssemble
		}
		metric, err := metadataFile(bundlePath, MaxArchiveBytes)
		if err != nil {
			return Result{}, ErrAssemble
		}
		releasePayloadMetadata[basename] = metric
	}
	if len(liveTargets) != 2 || liveTargets[0].Platform != "linux-amd64" || liveTargets[1].Platform != "windows-amd64" {
		return Result{}, ErrAssemble
	}

	if err := writeFileExclusive(filepath.Join(stage, "release", "install.sh"), installSH, 0o755); err != nil ||
		writeFileExclusive(filepath.Join(stage, "release", "install.ps1"), installPS, 0o644) != nil {
		return Result{}, ErrAssemble
	}
	releasePayloadMetadata["install.sh"] = metadataBytes(installSH)
	releasePayloadMetadata["install.ps1"] = metadataBytes(installPS)

	releaseChecksums, err := checksumBytes(releasePayloadMetadata)
	if err != nil || len(strings.Split(strings.TrimSuffix(string(releaseChecksums), "\n"), "\n")) != 8 {
		return Result{}, ErrAssemble
	}
	if err := writeFileExclusive(filepath.Join(stage, "release", "SHA256SUMS"), releaseChecksums, 0o644); err != nil {
		return Result{}, ErrAssemble
	}
	releaseChecksumMetadata := metadataBytes(releaseChecksums)
	releaseAssets := make([]Asset, 0, 8)
	for _, expected := range expectedReleasePayloads() {
		releaseAssets = append(releaseAssets, assetFromMetadata(expected.name, expected.platform, releasePayloadMetadata[expected.name]))
	}
	releaseManifest := ReleaseManifest{
		SchemaVersion: SchemaVersion, PlanID: PlanID, Revision: Revision, Version: Version,
		CandidateID: options.CandidateID, SourceCommit: source.Commit, SourceTree: source.Tree,
		BuildInputSHA256:        source.BuildInputSHA256,
		ReleaseSHA256SUMSSHA256: releaseChecksumMetadata.sha256,
		Assets:                  releaseAssets,
	}
	releaseManifestRaw, err := EncodeReleaseManifest(releaseManifest)
	if err != nil || writeFileExclusive(filepath.Join(stage, "release", "release-manifest.json"), releaseManifestRaw, 0o644) != nil {
		return Result{}, ErrAssemble
	}

	testToolAssets := make([]Asset, 0, 2)
	for _, expected := range expectedTestTools() {
		basename := filepath.Base(filepath.FromSlash(expected.name))
		content, info, err := readRegular(filepath.Join(options.BuildDir, "test-tools", basename), MaxBinaryBytes)
		if err != nil {
			return Result{}, ErrInvalidInput
		}
		if validateToolchain && validateGoBinary(content, expected.platform, "ipgw-live-gate", true, GoVersion) != nil {
			return Result{}, ErrInvalidInput
		}
		inputInfos = append(inputInfos, info)
		if err := writeFileExclusive(filepath.Join(stage, filepath.FromSlash(expected.name)), content, 0o755); err != nil {
			return Result{}, ErrAssemble
		}
		testToolAssets = append(testToolAssets, assetFromMetadata(expected.name, expected.platform, metadataBytes(content)))
	}
	if sameInputFile(inputInfos) {
		return Result{}, ErrInvalidInput
	}

	publicAssets := make([]Asset, 0, 10)
	publicMetadata := make(map[string]fileMetadata, 10)
	for name, metric := range releasePayloadMetadata {
		publicMetadata["release/"+name] = metric
	}
	publicMetadata["release/SHA256SUMS"] = releaseChecksumMetadata
	publicMetadata["release/release-manifest.json"] = metadataBytes(releaseManifestRaw)
	for _, expected := range expectedReleaseAssets() {
		publicAssets = append(publicAssets, assetFromMetadata(expected.name, expected.platform, publicMetadata[expected.name]))
	}
	manifest := Manifest{
		SchemaVersion: SchemaVersion, PlanID: PlanID, Revision: Revision, Version: Version,
		CandidateID: options.CandidateID, SourceCommit: source.Commit, SourceTree: source.Tree,
		WorkflowRunID: options.WorkflowRunID, WorkflowRunAttempt: options.WorkflowRunAttempt,
		Toolchain: Toolchain{
			GoVersion: GoVersion, GoToolchain: GoToolchain, HostPlatform: HostPlatform,
			CGOEnabled: false, GOAMD64: GOAMD64, GOARM64: GOARM64,
			SourceDateEpoch: source.CommitterEpoch, BuildRecipe: BuildRecipe,
		},
		BuildInputSHA256: source.BuildInputSHA256,
		ReleaseAssets:    publicAssets, TestTools: testToolAssets, LiveGateTargets: liveTargets,
	}
	manifestRaw, err := EncodeManifest(manifest)
	if err != nil || writeFileExclusive(filepath.Join(stage, "candidate-manifest.json"), manifestRaw, 0o644) != nil {
		return Result{}, ErrAssemble
	}
	rootFiles := make(map[string]fileMetadata, 13)
	rootFiles["candidate-manifest.json"] = metadataBytes(manifestRaw)
	for name, metric := range publicMetadata {
		rootFiles[name] = metric
	}
	for _, tool := range testToolAssets {
		rootFiles[tool.Name] = fileMetadata{size: tool.Size, sha256: tool.SHA256}
	}
	rootChecksums, err := checksumBytes(rootFiles)
	if err != nil || len(strings.Split(strings.TrimSuffix(string(rootChecksums), "\n"), "\n")) != 13 ||
		writeFileExclusive(filepath.Join(stage, "SHA256SUMS"), rootChecksums, 0o644) != nil {
		return Result{}, ErrAssemble
	}

	seal, err := openCandidateSeal(stage)
	if err != nil {
		return Result{}, ErrAssemble
	}
	defer seal.close()
	stageInfo := seal.rootDirectory.info
	verified, err := verifyCandidate(stage, validateToolchain)
	if err != nil {
		return Result{}, ErrAssemble
	}
	postVerify := func(root string) bool {
		result, verifyErr := verifyCandidate(root, validateToolchain)
		if verifyErr != nil {
			return false
		}
		expected := verified
		expected.Root = root
		return result == expected
	}
	validateSeal := func(root string) bool {
		switch root {
		case stage:
			return seal.unchanged(root)
		case options.OutputDir:
			return runtime.GOOS != "windows" && seal.unchangedAfterRename(root)
		default:
			return false
		}
	}
	if err := publishCandidateDirectory(stage, options.OutputDir, stageInfo, validateSeal, seal.close, postVerify); err != nil {
		return Result{}, ErrAssemble
	}
	published = true
	verified.Root = options.OutputDir
	return verified, nil
}

func pinShellInstaller(raw []byte) ([]byte, error) {
	normalized, err := normalizeText(raw)
	if err != nil || !bytes.HasPrefix(normalized, []byte("#!/usr/bin/env bash\n")) {
		return nil, ErrInvalidInput
	}
	return append([]byte("#!/usr/bin/env bash\nIPGW_VERSION='v1.0.0'\nexport IPGW_VERSION\n"), normalized[len("#!/usr/bin/env bash\n"):]...), nil
}

func pinPowerShellInstaller(raw []byte) ([]byte, error) {
	normalized, err := normalizeText(raw)
	if err != nil {
		return nil, ErrInvalidInput
	}
	lines := strings.Split(string(normalized), "\n")
	for index, line := range lines {
		if line == ")" {
			insertion := []string{"", "# Generated release asset: remain pinned to this release batch.", "$Version = 'v1.0.0'"}
			lines = append(lines[:index+1], append(insertion, lines[index+1:]...)...)
			return []byte(strings.Join(lines, "\n")), nil
		}
	}
	return nil, ErrInvalidInput
}

func sameInputFile(infos []os.FileInfo) bool {
	for left := range infos {
		for right := left + 1; right < len(infos); right++ {
			if os.SameFile(infos[left], infos[right]) {
				return true
			}
		}
	}
	return false
}

func timeFromEpoch(epoch int64) time.Time { return time.Unix(epoch, 0).UTC() }

func validPackagerHost() bool {
	return runtime.Version() == GoVersion && runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
}
