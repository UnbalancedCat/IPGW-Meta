package livegate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	bundleEvidenceName  = "evidence.json"
	bundleSummaryName   = "summary.md"
	bundleChecksumsName = "SHA256SUMS"

	maxBundleSequence  = 999
	maxBundleFileBytes = MaxEvidenceJSONBytes + 1
)

var (
	// ErrEvidenceDurability is intentionally fixed and carries no input or
	// filesystem details. Callers may safely present it without leaking live
	// validation material.
	ErrEvidenceDurability = errors.New("livegate: evidence durability failure")
	// ErrEvidencePublishCanceled reports cancellation before the atomic publish commit point.
	ErrEvidencePublishCanceled = errors.New("livegate: evidence publication canceled")
	errBundleCollision         = errors.New("livegate: evidence bundle collision")
)

// PublishedBundle is the durable evidence object and its final directory.
type PublishedBundle struct {
	Evidence Evidence
	Path     string
}

// BundlePublisher writes one closed live-gate evidence bundle beneath
// OutputDir. OutputDir must be an absolute, clean build/live-evidence path.
type BundlePublisher struct {
	OutputDir string
	ops       *bundleOps
}

// bundleOps is deliberately narrow. Tests replace individual durability
// boundaries without changing allocation or validation behavior.
type bundleOps struct {
	writeFile        func(string, []byte) error
	readFile         func(string) ([]byte, error)
	removeFile       func(string) error
	removeAll        func(string) error
	syncDirectory    func(string) error
	publishDirectory func(string, string) error
}

func defaultBundleOps() bundleOps {
	return bundleOps{
		writeFile:        writePrivateBundleFile,
		readFile:         readPrivateBundleFile,
		removeFile:       os.Remove,
		removeAll:        os.RemoveAll,
		syncDirectory:    syncBundleDirectory,
		publishDirectory: publishBundleDirectory,
	}
}

func writePrivateBundleFile(path string, data []byte) (resultErr error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	closed := false
	succeeded := false
	defer func() {
		if !closed {
			if err := file.Close(); resultErr == nil && err != nil {
				resultErr = err
			}
		}
		if !succeeded {
			_ = os.Remove(path)
		}
	}()

	// The parent staging directory is already private. Protect the new leaf
	// while it is still empty, then write and flush the exact final bytes.
	if err := protectBundleFile(path); err != nil {
		return err
	}
	written, err := file.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		closed = true
		return err
	}
	closed = true
	succeeded = true
	return nil
}

func (p BundlePublisher) effectiveOps() bundleOps {
	ops := defaultBundleOps()
	if p.ops == nil {
		return ops
	}
	if p.ops.writeFile != nil {
		ops.writeFile = p.ops.writeFile
	}
	if p.ops.readFile != nil {
		ops.readFile = p.ops.readFile
	}
	if p.ops.removeFile != nil {
		ops.removeFile = p.ops.removeFile
	}
	if p.ops.removeAll != nil {
		ops.removeAll = p.ops.removeAll
	}
	if p.ops.syncDirectory != nil {
		ops.syncDirectory = p.ops.syncDirectory
	}
	if p.ops.publishDirectory != nil {
		ops.publishDirectory = p.ops.publishDirectory
	}
	return ops
}

// Publish allocates the lowest available UTC evidence sequence and publishes a
// complete three-file bundle atomically. Every failure is collapsed to the
// fixed durability sentinel so untrusted evidence and paths are never echoed.
func (p BundlePublisher) Publish(input Evidence) (PublishedBundle, error) {
	return p.PublishContext(context.Background(), input)
}

// PublishContext accepts cancellation only before the atomic directory publish
// begins. A successful publishDirectory call is the commit point; cancellation
// after that point is ignored and callers must not discard the committed bundle.
func (p BundlePublisher) PublishContext(ctx context.Context, input Evidence) (PublishedBundle, error) {
	published, err := p.publish(ctx, input)
	if err != nil {
		if errors.Is(err, ErrEvidencePublishCanceled) {
			return PublishedBundle{}, ErrEvidencePublishCanceled
		}
		return PublishedBundle{}, ErrEvidenceDurability
	}
	return published, nil
}

func (p BundlePublisher) publish(ctx context.Context, input Evidence) (PublishedBundle, error) {
	if ctx == nil {
		return PublishedBundle{}, ErrEvidenceDurability
	}
	if err := bundleContextError(ctx); err != nil {
		return PublishedBundle{}, err
	}
	if err := validateBundleOutputDir(p.OutputDir); err != nil {
		return PublishedBundle{}, err
	}
	if input.EvidenceID != "" {
		return PublishedBundle{}, ErrInvalidEvidence
	}
	started, err := parseTimestamp(input.StartedAt)
	if err != nil {
		return PublishedBundle{}, err
	}
	date := started.UTC().Format("20060102")
	probe := input
	probe.EvidenceID = fmt.Sprintf("EVID-%s-001", date)
	if err := probe.Validate(); err != nil {
		return PublishedBundle{}, err
	}

	if err := ensureBundlePrivateDirectory(p.OutputDir); err != nil {
		return PublishedBundle{}, err
	}
	candidateDir := filepath.Join(p.OutputDir, input.CandidateID)
	if err := ensureBundlePrivateDirectory(candidateDir); err != nil {
		return PublishedBundle{}, err
	}
	ops := p.effectiveOps()

	for sequence := 1; sequence <= maxBundleSequence; sequence++ {
		if err := bundleContextError(ctx); err != nil {
			return PublishedBundle{}, err
		}
		id := fmt.Sprintf("EVID-%s-%03d", date, sequence)
		finalDir := filepath.Join(candidateDir, id)
		exists, err := bundlePathExists(finalDir)
		if err != nil {
			return PublishedBundle{}, err
		}
		if exists {
			continue
		}

		reservation := filepath.Join(candidateDir, "."+id+".reserve")
		if err := reserveBundleID(reservation); err != nil {
			if errors.Is(err, errBundleCollision) {
				continue
			}
			return PublishedBundle{}, err
		}
		published, collision, err := publishReservedBundle(
			ops,
			ctx,
			input,
			id,
			candidateDir,
			finalDir,
			reservation,
		)
		if err != nil {
			if collision {
				continue
			}
			return PublishedBundle{}, err
		}
		return published, nil
	}
	return PublishedBundle{}, ErrEvidenceDurability
}

func publishReservedBundle(
	ops bundleOps,
	ctx context.Context,
	input Evidence,
	id string,
	candidateDir string,
	finalDir string,
	reservation string,
) (published PublishedBundle, collision bool, resultErr error) {
	stageDir := ""
	finalPublished := false
	succeeded := false
	defer func() {
		if succeeded {
			return
		}
		cleanupFailed := false
		if finalPublished {
			if err := invalidatePublishedBundleWithOps(ops, finalDir, candidateDir); err != nil {
				cleanupFailed = true
			}
		}
		if stageDir != "" {
			if err := ops.removeAll(stageDir); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupFailed = true
			}
		}
		if reservation != "" {
			if err := ops.removeFile(reservation); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupFailed = true
			}
		}
		if err := ops.syncDirectory(candidateDir); err != nil {
			cleanupFailed = true
		}
		if cleanupFailed && resultErr == nil {
			resultErr = ErrEvidenceDurability
		}
	}()

	exists, err := bundlePathExists(finalDir)
	if err != nil {
		return PublishedBundle{}, false, err
	}
	if exists {
		return PublishedBundle{}, true, errBundleCollision
	}

	stageDir, err = createBundleStagingDirectory(candidateDir)
	if err != nil {
		return PublishedBundle{}, false, err
	}
	if err := verifyBundlePrivateDirectory(stageDir); err != nil {
		return PublishedBundle{}, false, err
	}

	evidence := input
	evidence.EvidenceID = id
	if err := evidence.Validate(); err != nil {
		return PublishedBundle{}, false, err
	}
	evidenceBytes, err := encodeBundleEvidence(evidence)
	if err != nil {
		return PublishedBundle{}, false, err
	}
	summaryBytes := buildBundleSummary(evidence)

	files := []struct {
		name string
		data []byte
	}{
		{name: bundleEvidenceName, data: evidenceBytes},
		{name: bundleSummaryName, data: summaryBytes},
	}
	for _, file := range files {
		path := filepath.Join(stageDir, file.name)
		if err := ops.writeFile(path, file.data); err != nil {
			return PublishedBundle{}, false, err
		}
		if err := verifyBundleFile(ops, path, file.data); err != nil {
			return PublishedBundle{}, false, err
		}
	}

	checksumBytes := buildBundleChecksums(evidenceBytes, summaryBytes)
	checksumPath := filepath.Join(stageDir, bundleChecksumsName)
	if err := ops.writeFile(checksumPath, checksumBytes); err != nil {
		return PublishedBundle{}, false, err
	}
	if err := verifyBundleFile(ops, checksumPath, checksumBytes); err != nil {
		return PublishedBundle{}, false, err
	}

	expected := map[string][]byte{
		bundleEvidenceName:  evidenceBytes,
		bundleSummaryName:   summaryBytes,
		bundleChecksumsName: checksumBytes,
	}
	if err := verifyCompleteStagingBundle(ops, stageDir, expected); err != nil {
		return PublishedBundle{}, false, err
	}
	if err := ops.syncDirectory(stageDir); err != nil {
		return PublishedBundle{}, false, err
	}
	if err := bundleContextError(ctx); err != nil {
		return PublishedBundle{}, false, err
	}
	// The call below is the publication commit point. Context cancellation is
	// intentionally not observed after it begins.
	if err := ops.publishDirectory(stageDir, finalDir); err != nil {
		if errors.Is(err, errBundleCollision) {
			return PublishedBundle{}, true, err
		}
		return PublishedBundle{}, false, err
	}
	finalPublished = true

	if err := verifyBundlePrivateDirectory(finalDir); err != nil {
		return PublishedBundle{}, false, err
	}
	if err := verifyCompleteStagingBundle(ops, finalDir, expected); err != nil {
		return PublishedBundle{}, false, err
	}
	stageDir = ""
	if err := os.Remove(reservation); err != nil {
		return PublishedBundle{}, false, err
	}
	reservation = ""
	if err := ops.syncDirectory(candidateDir); err != nil {
		return PublishedBundle{}, false, err
	}

	succeeded = true
	return PublishedBundle{Evidence: evidence, Path: finalDir}, false, nil
}

func bundleContextError(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ErrEvidencePublishCanceled
	default:
		return nil
	}
}
func validateBundleOutputDir(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrEvidenceDurability
	}
	if filepath.Base(path) != "live-evidence" ||
		filepath.Base(filepath.Dir(path)) != "build" {
		return ErrEvidenceDurability
	}
	return nil
}

func reserveBundleID(path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return errBundleCollision
		}
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := protectBundleFile(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	if err := verifyBundlePrivateFile(path); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func encodeBundleEvidence(evidence Evidence) ([]byte, error) {
	var encoded bytes.Buffer
	if err := EncodeEvidence(&encoded, evidence); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func buildBundleSummary(evidence Evidence) []byte {
	var summary strings.Builder
	_, _ = fmt.Fprintf(&summary, "# IPGW-Meta live-gate summary\n\n")
	_, _ = fmt.Fprintf(&summary, "- schema_version: %d\n", evidence.SchemaVersion)
	_, _ = fmt.Fprintf(&summary, "- plan_id: %s\n", evidence.PlanID)
	_, _ = fmt.Fprintf(&summary, "- revision: %s\n", evidence.Revision)
	_, _ = fmt.Fprintf(&summary, "- evidence_id: %s\n", evidence.EvidenceID)
	_, _ = fmt.Fprintf(&summary, "- candidate_id: %s\n", evidence.CandidateID)
	_, _ = fmt.Fprintf(&summary, "- candidate_set_sha256: %s\n", evidence.CandidateSetSHA256)
	_, _ = fmt.Fprintf(&summary, "- source_commit: %s\n", evidence.SourceCommit)
	_, _ = fmt.Fprintf(&summary, "- platform: %s\n", evidence.Platform)
	_, _ = fmt.Fprintf(&summary, "- testbed: %s\n", evidence.Testbed)
	_, _ = fmt.Fprintf(&summary, "- network_type: %s\n", evidence.NetworkType)
	_, _ = fmt.Fprintf(&summary, "- auth_method: %s\n", evidence.AuthMethod)
	_, _ = fmt.Fprintf(&summary, "- suite: %s\n", evidence.Suite)
	_, _ = fmt.Fprintf(&summary, "- capability_before: %s\n", joinCapabilities(evidence.CapabilityBefore))
	_, _ = fmt.Fprintf(&summary, "- result: %s\n", evidence.Result)
	_, _ = fmt.Fprintf(&summary, "- capability_after: %s\n", joinCapabilities(evidence.CapabilityAfter))
	_, _ = fmt.Fprintf(&summary, "- started_at: %s\n", evidence.StartedAt)
	_, _ = fmt.Fprintf(&summary, "- finished_at: %s\n\n", evidence.FinishedAt)
	_, _ = fmt.Fprintf(&summary, "## Steps\n\n")
	for _, step := range evidence.Steps {
		errorCode := "null"
		if step.ErrorCode != nil {
			errorCode = string(*step.ErrorCode)
		}
		_, _ = fmt.Fprintf(
			&summary,
			"- %s | %s | %d | %s | %d\n",
			step.Name,
			step.Result,
			step.ExitCode,
			errorCode,
			step.DurationSeconds,
		)
	}
	return []byte(summary.String())
}

func joinCapabilities(capabilities []CapabilityStatus) string {
	values := make([]string, len(capabilities))
	for index, capability := range capabilities {
		values[index] = string(capability)
	}
	return strings.Join(values, ",")
}

func buildBundleChecksums(evidence, summary []byte) []byte {
	evidenceHash := sha256.Sum256(evidence)
	summaryHash := sha256.Sum256(summary)
	return []byte(
		hex.EncodeToString(evidenceHash[:]) + "  " + bundleEvidenceName + "\n" +
			hex.EncodeToString(summaryHash[:]) + "  " + bundleSummaryName + "\n",
	)
}

func verifyBundleFile(ops bundleOps, path string, expected []byte) error {
	if err := verifyBundlePrivateFile(path); err != nil {
		return err
	}
	actual, err := ops.readFile(path)
	if err != nil || !bytes.Equal(actual, expected) {
		return ErrEvidenceDurability
	}
	return nil
}

func verifyCompleteStagingBundle(ops bundleOps, dir string, expected map[string][]byte) error {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != len(expected) {
		return ErrEvidenceDurability
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		data, known := expected[entry.Name()]
		if !known || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return ErrEvidenceDurability
		}
		if _, duplicate := seen[entry.Name()]; duplicate {
			return ErrEvidenceDurability
		}
		seen[entry.Name()] = struct{}{}
		if err := verifyBundleFile(ops, filepath.Join(dir, entry.Name()), data); err != nil {
			return err
		}
	}
	return nil
}

func readPrivateBundleFile(path string) ([]byte, error) {
	file, err := openBundlePrivateFile(path)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, maxBundleFileBytes+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > maxBundleFileBytes {
		return nil, ErrEvidenceDurability
	}
	return data, nil
}

func bundlePathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func invalidatePublishedBundle(finalDir string) error {
	return invalidatePublishedBundleWithOps(defaultBundleOps(), finalDir, filepath.Dir(finalDir))
}

func invalidatePublishedBundleWithOps(ops bundleOps, finalDir, candidateDir string) error {
	failed := false
	checksumPath := filepath.Join(finalDir, bundleChecksumsName)
	if err := ops.removeFile(checksumPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		failed = true
	}

	finalExists, err := bundlePathExists(finalDir)
	if err != nil {
		failed = true
	}
	if finalExists {
		if err := ops.syncDirectory(finalDir); err != nil {
			failed = true
		}
	}
	complete, err := bundleHasExactThreeFileShape(finalDir)
	if err != nil {
		failed = true
	}
	if complete {
		failed = true
	}

	if err := ops.removeAll(finalDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		failed = true
	}
	if err := ops.syncDirectory(candidateDir); err != nil {
		failed = true
	}
	finalExists, err = bundlePathExists(finalDir)
	if err != nil {
		failed = true
	}
	if finalExists {
		failed = true
		complete, inspectErr := bundleHasExactThreeFileShape(finalDir)
		if inspectErr != nil || complete {
			failed = true
		}
	}
	if failed {
		return ErrEvidenceDurability
	}
	return nil
}

func bundleHasExactThreeFileShape(directory string) (bool, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(entries) != 3 {
		return false, nil
	}
	expected := map[string]struct{}{
		bundleEvidenceName:  {},
		bundleSummaryName:   {},
		bundleChecksumsName: {},
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok {
			return false, nil
		}
		info, err := entry.Info()
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return false, nil
		}
	}
	return true, nil
}
