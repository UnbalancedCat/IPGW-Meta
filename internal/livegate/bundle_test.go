//go:build linux || windows

package livegate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestBundlePublisherPublishesExactBundle(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	published, err := (BundlePublisher{OutputDir: outputDir}).Publish(input)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if published.Evidence.EvidenceID != "EVID-20260829-001" {
		t.Fatalf("EvidenceID = %q, want EVID-20260829-001", published.Evidence.EvidenceID)
	}
	wantPath := filepath.Join(outputDir, input.CandidateID, published.Evidence.EvidenceID)
	if published.Path != wantPath {
		t.Fatalf("Path = %q, want %q", published.Path, wantPath)
	}

	evidenceBytes := bundleTestReadFile(t, filepath.Join(published.Path, bundleEvidenceName))
	var expectedEvidence bytes.Buffer
	if err := EncodeEvidence(&expectedEvidence, published.Evidence); err != nil {
		t.Fatalf("EncodeEvidence() error = %v", err)
	}
	if !bytes.Equal(evidenceBytes, expectedEvidence.Bytes()) {
		t.Fatal("evidence.json bytes differ from the canonical encoder")
	}
	decoded, err := DecodeEvidence(bytes.NewReader(evidenceBytes))
	if err != nil {
		t.Fatalf("DecodeEvidence() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, published.Evidence) {
		t.Fatalf("decoded evidence differs from PublishedBundle.Evidence")
	}

	wantSummary := []byte(fmt.Sprintf(
		"# IPGW-Meta live-gate summary\n\n"+
			"- schema_version: 1\n"+
			"- plan_id: IPGW-META-V1\n"+
			"- revision: 2026-08-28-r2\n"+
			"- evidence_id: %s\n"+
			"- candidate_id: v1.0.0-0123456789ab-12345.1\n"+
			"- candidate_set_sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n"+
			"- source_commit: 0123456789abcdef0123456789abcdef01234567\n"+
			"- platform: linux-amd64\n"+
			"- testbed: nas_vm\n"+
			"- network_type: campus_wired\n"+
			"- auth_method: password\n"+
			"- suite: password_core\n"+
			"- capability_before: synthetic_covered,live_unverified\n"+
			"- result: pass\n"+
			"- capability_after: synthetic_covered,live_verified\n"+
			"- started_at: 2026-08-29T01:02:03Z\n"+
			"- finished_at: 2026-08-29T01:02:10Z\n\n"+
			"## Steps\n\n"+
			"- initial_status_offline | pass | 0 | null | 1\n"+
			"- login_logged_in | pass | 0 | null | 1\n"+
			"- status_online | pass | 0 | null | 1\n"+
			"- second_login_already_online | pass | 0 | null | 1\n"+
			"- logout_logged_out | pass | 0 | null | 1\n"+
			"- final_status_offline | pass | 0 | null | 1\n"+
			"- second_logout_already_offline | pass | 0 | null | 1\n",
		published.Evidence.EvidenceID,
	))
	summaryBytes := bundleTestReadFile(t, filepath.Join(published.Path, bundleSummaryName))
	if !bytes.Equal(summaryBytes, wantSummary) {
		t.Fatalf("summary.md = %q, want exact template %q", summaryBytes, wantSummary)
	}

	evidenceHash := sha256.Sum256(evidenceBytes)
	summaryHash := sha256.Sum256(summaryBytes)
	wantChecksums := []byte(
		hex.EncodeToString(evidenceHash[:]) + "  evidence.json\n" +
			hex.EncodeToString(summaryHash[:]) + "  summary.md\n",
	)
	checksumBytes := bundleTestReadFile(t, filepath.Join(published.Path, bundleChecksumsName))
	if !bytes.Equal(checksumBytes, wantChecksums) {
		t.Fatalf("SHA256SUMS = %q, want %q", checksumBytes, wantChecksums)
	}

	bundleTestRequireExactFiles(t, published.Path)
	for _, directory := range []string{
		outputDir,
		filepath.Join(outputDir, input.CandidateID),
		published.Path,
	} {
		if err := verifyBundlePrivateDirectory(directory); err != nil {
			t.Fatalf("private directory verification failed for %q: %v", directory, err)
		}
	}
	for _, name := range []string{bundleEvidenceName, bundleSummaryName, bundleChecksumsName} {
		if err := verifyBundlePrivateFile(filepath.Join(published.Path, name)); err != nil {
			t.Fatalf("private file verification failed for %q: %v", name, err)
		}
	}
}

func TestBundlePublisherSkipsExistingFinalWithoutClobber(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	candidateDir := filepath.Join(outputDir, input.CandidateID)
	if err := ensureBundlePrivateDirectory(outputDir); err != nil {
		t.Fatalf("ensure output directory: %v", err)
	}
	if err := ensureBundlePrivateDirectory(candidateDir); err != nil {
		t.Fatalf("ensure candidate directory: %v", err)
	}

	existingDir := filepath.Join(candidateDir, "EVID-20260829-001")
	if err := ensureBundlePrivateDirectory(existingDir); err != nil {
		t.Fatalf("ensure collision directory: %v", err)
	}
	const sentinel = "pre-existing-final-must-survive\n"
	sentinelPath := filepath.Join(existingDir, "sentinel")
	if err := defaultBundleOps().writeFile(sentinelPath, []byte(sentinel)); err != nil {
		t.Fatalf("write collision sentinel: %v", err)
	}

	published, err := (BundlePublisher{OutputDir: outputDir}).Publish(input)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Evidence.EvidenceID != "EVID-20260829-002" {
		t.Fatalf("EvidenceID = %q, want EVID-20260829-002", published.Evidence.EvidenceID)
	}
	if got := string(bundleTestReadFile(t, sentinelPath)); got != sentinel {
		t.Fatalf("pre-existing final was changed: %q", got)
	}
	bundleTestRequireExactFiles(t, published.Path)
}

func TestBundlePublisherDoesNotClobberPreexistingEmptyTarget(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	candidateDir := filepath.Join(outputDir, input.CandidateID)
	if err := ensureBundlePrivateDirectory(outputDir); err != nil {
		t.Fatalf("ensure output directory: %v", err)
	}
	if err := ensureBundlePrivateDirectory(candidateDir); err != nil {
		t.Fatalf("ensure candidate directory: %v", err)
	}

	emptyTarget := filepath.Join(candidateDir, "EVID-20260829-001")
	if err := ensureBundlePrivateDirectory(emptyTarget); err != nil {
		t.Fatalf("ensure empty collision target: %v", err)
	}
	published, err := (BundlePublisher{OutputDir: outputDir}).Publish(input)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Evidence.EvidenceID != "EVID-20260829-002" {
		t.Fatalf("EvidenceID = %q, want EVID-20260829-002", published.Evidence.EvidenceID)
	}
	entries, err := os.ReadDir(emptyTarget)
	if err != nil {
		t.Fatalf("ReadDir(empty target) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("pre-existing empty target was clobbered: %v", entries)
	}
}

func TestBundlePublisherDoesNotClobberCommitRaceTarget(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	ops := defaultBundleOps()
	originalPublish := ops.publishDirectory
	createdTarget := ""
	ops.publishDirectory = func(stageDir, finalDir string) error {
		if createdTarget == "" {
			if err := ensureBundlePrivateDirectory(finalDir); err != nil {
				return err
			}
			createdTarget = finalDir
		}
		return originalPublish(stageDir, finalDir)
	}

	published, err := (BundlePublisher{OutputDir: outputDir, ops: &ops}).Publish(input)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Evidence.EvidenceID != "EVID-20260829-002" {
		t.Fatalf("EvidenceID = %q, want EVID-20260829-002", published.Evidence.EvidenceID)
	}
	if createdTarget == "" {
		t.Fatal("publish race target was not injected")
	}
	entries, err := os.ReadDir(createdTarget)
	if err != nil {
		t.Fatalf("ReadDir(race target) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("commit-race target was clobbered: %v", entries)
	}
	bundleTestRequireExactFiles(t, published.Path)
}
func TestBundlePublisherAllocatesUniqueIDsConcurrently(t *testing.T) {
	const publishers = 6
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	results := make([]PublishedBundle, publishers)
	errs := make([]error, publishers)

	var wait sync.WaitGroup
	wait.Add(publishers)
	for index := 0; index < publishers; index++ {
		go func() {
			defer wait.Done()
			results[index], errs[index] = (BundlePublisher{OutputDir: outputDir}).Publish(input)
		}()
	}
	wait.Wait()

	ids := make([]string, publishers)
	for index := range results {
		if errs[index] != nil {
			t.Fatalf("Publish()[%d] error = %v", index, errs[index])
		}
		ids[index] = results[index].Evidence.EvidenceID
		bundleTestRequireExactFiles(t, results[index].Path)
	}
	sort.Strings(ids)
	for index, id := range ids {
		want := fmt.Sprintf("EVID-20260829-%03d", index+1)
		if id != want {
			t.Fatalf("sorted EvidenceID[%d] = %q, want %q; all IDs = %v", index, id, want, ids)
		}
	}
}

func TestBundlePublisherInjectedFailuresLeaveNoArtifacts(t *testing.T) {
	injected := errors.New("injected durability failure")
	tests := []struct {
		name   string
		mutate func(*bundleOps)
	}{
		{
			name: "write",
			mutate: func(ops *bundleOps) {
				ops.writeFile = func(string, []byte) error { return injected }
			},
		},
		{
			name: "read",
			mutate: func(ops *bundleOps) {
				ops.readFile = func(string) ([]byte, error) { return nil, injected }
			},
		},
		{
			name: "stage_sync",
			mutate: func(ops *bundleOps) {
				ops.syncDirectory = func(string) error { return injected }
			},
		},
		{
			name: "publish",
			mutate: func(ops *bundleOps) {
				ops.publishDirectory = func(string, string) error { return injected }
			},
		},
		{
			name: "publish_reports_success_without_move",
			mutate: func(ops *bundleOps) {
				ops.publishDirectory = func(string, string) error { return nil }
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outputDir := bundleTestOutputDir(t)
			input := bundleTestEvidence()
			ops := defaultBundleOps()
			test.mutate(&ops)
			got, err := (BundlePublisher{OutputDir: outputDir, ops: &ops}).Publish(input)
			bundleTestRequireDurabilityFailure(t, got, err)
			bundleTestRequireEmptyCandidate(t, outputDir, input.CandidateID)
		})
	}
}

func TestBundlePublisherPostPublishSyncFailureInvalidatesFinal(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	ops := defaultBundleOps()
	syncCalls := 0
	originalSync := ops.syncDirectory
	ops.syncDirectory = func(path string) error {
		syncCalls++
		if syncCalls == 2 {
			return errors.New("post-publish sync failure")
		}
		return originalSync(path)
	}

	got, err := (BundlePublisher{OutputDir: outputDir, ops: &ops}).Publish(input)
	bundleTestRequireDurabilityFailure(t, got, err)
	if syncCalls != 5 {
		t.Fatalf("sync calls = %d, want 5", syncCalls)
	}
	bundleTestRequireEmptyCandidate(t, outputDir, input.CandidateID)
}

func TestBundlePublisherPublishContextCancelsBeforeCommit(t *testing.T) {
	t.Run("already_canceled", func(t *testing.T) {
		outputDir := bundleTestOutputDir(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		got, err := (BundlePublisher{OutputDir: outputDir}).PublishContext(ctx, bundleTestEvidence())
		if err != ErrEvidencePublishCanceled {
			t.Fatalf("PublishContext() error = %v, want exact ErrEvidencePublishCanceled", err)
		}
		if !reflect.DeepEqual(got, PublishedBundle{}) {
			t.Fatalf("PublishContext() result = %#v, want zero value", got)
		}
		if _, err := os.Lstat(outputDir); !os.IsNotExist(err) {
			t.Fatalf("pre-canceled publication created output or inspect failed: %v", err)
		}
	})

	t.Run("canceled_after_stage_sync", func(t *testing.T) {
		outputDir := bundleTestOutputDir(t)
		input := bundleTestEvidence()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ops := defaultBundleOps()
		originalSync := ops.syncDirectory
		canceled := false
		ops.syncDirectory = func(path string) error {
			if err := originalSync(path); err != nil {
				return err
			}
			if !canceled && strings.HasPrefix(filepath.Base(path), ".livegate-stage-") {
				canceled = true
				cancel()
			}
			return nil
		}

		got, err := (BundlePublisher{OutputDir: outputDir, ops: &ops}).PublishContext(ctx, input)
		if err != ErrEvidencePublishCanceled {
			t.Fatalf("PublishContext() error = %v, want exact ErrEvidencePublishCanceled", err)
		}
		if !reflect.DeepEqual(got, PublishedBundle{}) {
			t.Fatalf("PublishContext() result = %#v, want zero value", got)
		}
		if !canceled {
			t.Fatal("test did not cancel at the pre-commit boundary")
		}
		bundleTestRequireEmptyCandidate(t, outputDir, input.CandidateID)
	})
}

func TestBundlePublisherIgnoresCancellationAfterCommit(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ops := defaultBundleOps()
	originalPublish := ops.publishDirectory
	ops.publishDirectory = func(stageDir, finalDir string) error {
		if err := originalPublish(stageDir, finalDir); err != nil {
			return err
		}
		cancel()
		return nil
	}

	published, err := (BundlePublisher{OutputDir: outputDir, ops: &ops}).PublishContext(ctx, input)
	if err != nil {
		t.Fatalf("PublishContext() error after commit = %v", err)
	}
	if ctx.Err() == nil {
		t.Fatal("test context was not canceled after commit")
	}
	bundleTestRequireExactFiles(t, published.Path)
}

func TestInvalidatePublishedBundleRemovesChecksumBeforeDirectory(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	published, err := (BundlePublisher{OutputDir: outputDir}).Publish(bundleTestEvidence())
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := invalidatePublishedBundle(published.Path); err != nil {
		t.Fatalf("invalidatePublishedBundle() error = %v", err)
	}
	if _, err := os.Lstat(published.Path); !os.IsNotExist(err) {
		t.Fatalf("invalidated bundle remains or inspect failed: %v", err)
	}
}

func TestBundlePublisherInvalidationFailureNeverLeavesExactThreeBundle(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*bundleOps)
	}{
		{
			name: "checksum_remove",
			mutate: func(ops *bundleOps) {
				originalRemove := ops.removeFile
				ops.removeFile = func(path string) error {
					if filepath.Base(path) == bundleChecksumsName {
						return errors.New("injected checksum remove failure")
					}
					return originalRemove(path)
				}
			},
		},
		{
			name: "final_remove_all",
			mutate: func(ops *bundleOps) {
				originalRemoveAll := ops.removeAll
				ops.removeAll = func(path string) error {
					if evidenceIDPattern.MatchString(filepath.Base(path)) {
						return errors.New("injected final remove-all failure")
					}
					return originalRemoveAll(path)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outputDir := bundleTestOutputDir(t)
			input := bundleTestEvidence()
			ops := defaultBundleOps()
			syncCalls := 0
			originalSync := ops.syncDirectory
			ops.syncDirectory = func(path string) error {
				syncCalls++
				if syncCalls == 2 {
					return errors.New("force post-publish invalidation")
				}
				return originalSync(path)
			}
			test.mutate(&ops)

			got, err := (BundlePublisher{OutputDir: outputDir, ops: &ops}).Publish(input)
			bundleTestRequireDurabilityFailure(t, got, err)
			bundleTestRequireNoExactThreeBundle(t, outputDir, input.CandidateID)
		})
	}
}
func TestBundlePublisherRejectsUnexpectedFourthFile(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	ops := defaultBundleOps()
	originalWrite := ops.writeFile
	ops.writeFile = func(path string, data []byte) error {
		if err := originalWrite(path, data); err != nil {
			return err
		}
		if filepath.Base(path) == bundleChecksumsName {
			return originalWrite(filepath.Join(filepath.Dir(path), "unexpected-fourth-file"), []byte("x\n"))
		}
		return nil
	}

	got, err := (BundlePublisher{OutputDir: outputDir, ops: &ops}).Publish(input)
	bundleTestRequireDurabilityFailure(t, got, err)
	bundleTestRequireEmptyCandidate(t, outputDir, input.CandidateID)
}

func TestBundlePublisherErrorsDoNotLeakCanary(t *testing.T) {
	const canary = "CANARY_password_ticket_9f91?token=do-not-echo"
	outputDir := bundleTestOutputDir(t)
	input := bundleTestEvidence()
	ops := defaultBundleOps()
	ops.writeFile = func(string, []byte) error { return errors.New(canary) }

	got, err := (BundlePublisher{OutputDir: outputDir, ops: &ops}).Publish(input)
	bundleTestRequireDurabilityFailure(t, got, err)
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("Publish() leaked canary in error: %q", err)
	}
	bundleTestRequireEmptyCandidate(t, outputDir, input.CandidateID)
	if err := filepath.WalkDir(outputDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if bytes.Contains(data, []byte(canary)) {
			t.Fatalf("canary persisted in %q", path)
		}
		return nil
	}); err != nil {
		t.Fatalf("scan output directory: %v", err)
	}
}

func TestBundlePublisherRejectsInvalidContractInputs(t *testing.T) {
	input := bundleTestEvidence()
	validOutput := bundleTestOutputDir(t)
	tests := []struct {
		name      string
		outputDir string
		mutate    func(*Evidence)
	}{
		{name: "relative_output", outputDir: filepath.Join("build", "live-evidence")},
		{name: "wrong_suffix", outputDir: filepath.Join(filepath.Dir(validOutput), "evidence")},
		{name: "preallocated_id", outputDir: validOutput, mutate: func(e *Evidence) {
			e.EvidenceID = "EVID-20260829-001"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evidence := cloneEvidence(input)
			if test.mutate != nil {
				test.mutate(&evidence)
			}
			got, err := (BundlePublisher{OutputDir: test.outputDir}).Publish(evidence)
			bundleTestRequireDurabilityFailure(t, got, err)
		})
	}
}

func bundleTestEvidence() Evidence {
	evidence := cloneEvidence(validPasswordEvidence())
	evidence.EvidenceID = ""
	return evidence
}

func bundleTestOutputDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "build", "live-evidence")
}

func bundleTestReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	return data
}

func bundleTestRequireExactFiles(t *testing.T, directory string) {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", directory, err)
	}
	names := make([]string, len(entries))
	for index, entry := range entries {
		if !entry.Type().IsRegular() || entry.Type()&os.ModeSymlink != 0 {
			t.Fatalf("bundle entry %q is not a regular file", entry.Name())
		}
		names[index] = entry.Name()
	}
	want := []string{bundleChecksumsName, bundleEvidenceName, bundleSummaryName}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("bundle files = %v, want %v", names, want)
	}
}

func bundleTestRequireDurabilityFailure(t *testing.T, got PublishedBundle, err error) {
	t.Helper()
	if err != ErrEvidenceDurability {
		t.Fatalf("Publish() error = %v, want exact ErrEvidenceDurability", err)
	}
	if !reflect.DeepEqual(got, PublishedBundle{}) {
		t.Fatalf("Publish() result on failure = %#v, want zero value", got)
	}
}

func bundleTestRequireEmptyCandidate(t *testing.T, outputDir, candidateID string) {
	t.Helper()
	candidateDir := filepath.Join(outputDir, candidateID)
	entries, err := os.ReadDir(candidateDir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", candidateDir, err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for index, entry := range entries {
			names[index] = entry.Name()
		}
		t.Fatalf("failed publication left candidate artifacts: %v", names)
	}
}
func bundleTestRequireNoExactThreeBundle(t *testing.T, outputDir, candidateID string) {
	t.Helper()
	candidateDir := filepath.Join(outputDir, candidateID)
	entries, err := os.ReadDir(candidateDir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatalf("ReadDir(%q) error = %v", candidateDir, err)
	}
	for _, entry := range entries {
		if !evidenceIDPattern.MatchString(entry.Name()) {
			continue
		}
		complete, err := bundleHasExactThreeFileShape(filepath.Join(candidateDir, entry.Name()))
		if err != nil {
			t.Fatalf("inspect possible bundle %q: %v", entry.Name(), err)
		}
		if complete {
			t.Fatalf("reported failure left an exact-three recognizable bundle: %q", entry.Name())
		}
	}
}
