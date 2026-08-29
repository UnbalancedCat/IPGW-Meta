// Package candidate assembles and verifies the immutable v1 release candidate.
// It never builds product binaries and never contacts GitHub.
package candidate

import (
	"errors"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	SchemaVersion                = 1
	PlanID                       = "IPGW-META-V1"
	Revision                     = "2026-08-28-r2"
	Version                      = "v1.0.0"
	GoVersion                    = "go1.25.0"
	GoToolchain                  = "local"
	HostPlatform                 = "linux-amd64"
	GOAMD64                      = "v1"
	GOARM64                      = "v8.0"
	BuildRecipe                  = "candidate-v1"
	MaxManifestBytes       int64 = 64 * 1024
	MaxBuildInputPathBytes       = 4096
	MaxBinaryBytes         int64 = 64 * 1024 * 1024
	MaxArchiveBytes        int64 = 100 * 1024 * 1024
)

var (
	ErrInvalidManifest = errors.New("candidate: invalid manifest")
	ErrInvalidSource   = errors.New("candidate: invalid Git source")
	ErrInvalidInput    = errors.New("candidate: invalid assembly input")
	ErrAssemble        = errors.New("candidate: assembly failed")
	ErrVerify          = errors.New("candidate: verification failed")

	lowerHex40         = regexp.MustCompile(`^[0-9a-f]{40}$`)
	lowerHex64         = regexp.MustCompile(`^[0-9a-f]{64}$`)
	candidateIDPattern = regexp.MustCompile(`^v1\.0\.0-([0-9a-f]{12})-([1-9][0-9]*)\.([1-9][0-9]*)$`)
)

var targetOrder = [...]string{
	"darwin-amd64",
	"darwin-arm64",
	"linux-amd64",
	"linux-arm64",
	"windows-amd64",
	"windows-arm64",
}

var productOrder = [...]string{"ipgw", "ipgw-meta", "ipgw-legacy"}

// Toolchain is the closed candidate-v1 toolchain identity.
type Toolchain struct {
	GoVersion       string `json:"go_version"`
	GoToolchain     string `json:"go_toolchain"`
	HostPlatform    string `json:"host_platform"`
	CGOEnabled      bool   `json:"cgo_enabled"`
	GOAMD64         string `json:"goamd64"`
	GOARM64         string `json:"goarm64"`
	SourceDateEpoch int64  `json:"source_date_epoch"`
	BuildRecipe     string `json:"build_recipe"`
}

// Asset is a closed release_assets or test_tools member.
type Asset struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// LiveGateTarget is the fixed runner projection entry. Its field order is
// part of candidate-manifest v1.
type LiveGateTarget struct {
	Platform string `json:"platform"`
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	SHA256   string `json:"sha256"`
}

// Manifest is the closed, canonical candidate-manifest v1 object. Field order
// is significant and follows the declaration order.
type Manifest struct {
	SchemaVersion      int              `json:"schema_version"`
	PlanID             string           `json:"plan_id"`
	Revision           string           `json:"revision"`
	Version            string           `json:"version"`
	CandidateID        string           `json:"candidate_id"`
	SourceCommit       string           `json:"source_commit"`
	SourceTree         string           `json:"source_tree"`
	WorkflowRunID      int64            `json:"workflow_run_id"`
	WorkflowRunAttempt int64            `json:"workflow_run_attempt"`
	Toolchain          Toolchain        `json:"toolchain"`
	BuildInputSHA256   string           `json:"build_input_sha256"`
	ReleaseAssets      []Asset          `json:"release_assets"`
	TestTools          []Asset          `json:"test_tools"`
	LiveGateTargets    []LiveGateTarget `json:"live_gate_targets"`
}

// ReleaseManifest is the closed, canonical public release manifest.
type ReleaseManifest struct {
	SchemaVersion           int     `json:"schema_version"`
	PlanID                  string  `json:"plan_id"`
	Revision                string  `json:"revision"`
	Version                 string  `json:"version"`
	CandidateID             string  `json:"candidate_id"`
	SourceCommit            string  `json:"source_commit"`
	SourceTree              string  `json:"source_tree"`
	BuildInputSHA256        string  `json:"build_input_sha256"`
	ReleaseSHA256SUMSSHA256 string  `json:"release_sha256sums_sha256"`
	Assets                  []Asset `json:"assets"`
}

func (m Manifest) Validate() error {
	if m.SchemaVersion != SchemaVersion || m.PlanID != PlanID ||
		m.Revision != Revision || m.Version != Version ||
		!lowerHex40.MatchString(m.SourceCommit) ||
		!lowerHex40.MatchString(m.SourceTree) ||
		m.WorkflowRunID < 1 || m.WorkflowRunAttempt < 1 ||
		!lowerHex64.MatchString(m.BuildInputSHA256) ||
		!validCandidateID(m.CandidateID, m.SourceCommit, m.WorkflowRunID, m.WorkflowRunAttempt) ||
		!m.Toolchain.valid() ||
		!validAssetSet(m.ReleaseAssets, expectedReleaseAssets()) ||
		!validAssetSet(m.TestTools, expectedTestTools()) ||
		!validLiveGateTargets(m.LiveGateTargets) {
		return ErrInvalidManifest
	}
	return nil
}

func (m ReleaseManifest) Validate() error {
	if m.SchemaVersion != SchemaVersion || m.PlanID != PlanID ||
		m.Revision != Revision || m.Version != Version ||
		!lowerHex40.MatchString(m.SourceCommit) ||
		!lowerHex40.MatchString(m.SourceTree) ||
		!lowerHex64.MatchString(m.BuildInputSHA256) ||
		!lowerHex64.MatchString(m.ReleaseSHA256SUMSSHA256) ||
		!validCandidateIDPrefix(m.CandidateID, m.SourceCommit) ||
		!validAssetSet(m.Assets, expectedReleasePayloads()) {
		return ErrInvalidManifest
	}
	return nil
}

func (t Toolchain) valid() bool {
	return t.GoVersion == GoVersion &&
		t.GoToolchain == GoToolchain &&
		t.HostPlatform == HostPlatform &&
		!t.CGOEnabled &&
		t.GOAMD64 == GOAMD64 &&
		t.GOARM64 == GOARM64 &&
		t.SourceDateEpoch > 0 &&
		t.BuildRecipe == BuildRecipe
}

func validCandidateID(id, commit string, runID, attempt int64) bool {
	match := candidateIDPattern.FindStringSubmatch(id)
	if len(match) != 4 || len(commit) < 12 || match[1] != commit[:12] {
		return false
	}
	parsedRun, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil || parsedRun != runID {
		return false
	}
	parsedAttempt, err := strconv.ParseInt(match[3], 10, 64)
	return err == nil && parsedAttempt == attempt
}

func validCandidateIDPrefix(id, commit string) bool {
	match := candidateIDPattern.FindStringSubmatch(id)
	if len(match) != 4 || !lowerHex40.MatchString(commit) || match[1] != commit[:12] {
		return false
	}
	if _, ok := decimalPositive(match[2]); !ok {
		return false
	}
	if _, ok := decimalPositive(match[3]); !ok {
		return false
	}
	return true
}

type expectedAsset struct {
	name     string
	platform string
}

func expectedReleasePayloads() []expectedAsset {
	values := make([]expectedAsset, 0, 8)
	values = append(values,
		expectedAsset{name: "install.ps1", platform: "windows"},
		expectedAsset{name: "install.sh", platform: "unix"},
	)
	for _, target := range targetOrder {
		extension := ".tar.gz"
		if strings.HasPrefix(target, "windows-") {
			extension = ".zip"
		}
		values = append(values, expectedAsset{
			name:     "ipgw-meta-" + target + extension,
			platform: target,
		})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].name < values[j].name })
	return values
}

func expectedReleaseAssets() []expectedAsset {
	values := []expectedAsset{
		{name: "release/SHA256SUMS", platform: "all"},
		{name: "release/release-manifest.json", platform: "all"},
	}
	for _, payload := range expectedReleasePayloads() {
		values = append(values, expectedAsset{
			name:     "release/" + payload.name,
			platform: payload.platform,
		})
	}
	sort.Slice(values, func(i, j int) bool { return values[i].name < values[j].name })
	return values
}

func expectedTestTools() []expectedAsset {
	return []expectedAsset{
		{name: "test-tools/ipgw-live-gate-linux-amd64", platform: "linux-amd64"},
		{name: "test-tools/ipgw-live-gate-windows-amd64.exe", platform: "windows-amd64"},
	}
}

func validAssetSet(actual []Asset, expected []expectedAsset) bool {
	if len(actual) != len(expected) {
		return false
	}
	seenFold := make(map[string]struct{}, len(actual))
	for index, asset := range actual {
		if asset.Name != expected[index].name || asset.Platform != expected[index].platform ||
			asset.Size < 1 || !lowerHex64.MatchString(asset.SHA256) ||
			!validAssetName(asset.Name) ||
			(index > 0 && actual[index-1].Name >= asset.Name) {
			return false
		}
		folded := strings.ToLower(asset.Name)
		if _, duplicate := seenFold[folded]; duplicate {
			return false
		}
		seenFold[folded] = struct{}{}
	}
	return true
}

func validAssetName(name string) bool {
	if name == "" || path.IsAbs(name) || strings.Contains(name, `\`) || path.Clean(name) != name {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	for _, character := range []byte(name) {
		if character < 0x20 || character > 0x7e || character == 0x7f {
			return false
		}
	}
	return true
}

func validLiveGateTargets(targets []LiveGateTarget) bool {
	if len(targets) != 2 {
		return false
	}
	expectedPlatforms := [...]string{"linux-amd64", "windows-amd64"}
	expectedNames := [...]string{"ipgw-meta", "ipgw-meta.exe"}
	for index, target := range targets {
		if target.Platform != expectedPlatforms[index] || target.Name != expectedNames[index] ||
			target.Size < 1 || target.Size > MaxBinaryBytes ||
			!lowerHex64.MatchString(target.SHA256) {
			return false
		}
	}
	return true
}
