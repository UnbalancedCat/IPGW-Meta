//go:build linux

package livegate

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestEnsureBundlePrivateDirectoryConcurrentCreation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "build", "live-evidence")
	const workers = 64
	errs := make([]error, workers)
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(workers)
	for index := range errs {
		go func(index int) {
			defer wait.Done()
			<-start
			errs[index] = ensureBundlePrivateDirectory(path)
		}(index)
	}
	close(start)
	wait.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("ensure[%d] error = %v", index, err)
		}
	}
	if err := verifyBundlePrivateDirectory(path); err != nil {
		t.Fatalf("verify concurrently created directory: %v", err)
	}
}

func TestEnsureBundlePrivateDirectoryExistingNonPrivateFailsWithoutRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "live-evidence")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("create non-private directory: %v", err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatalf("set non-private mode: %v", err)
	}
	if err := ensureBundlePrivateDirectory(path); !errors.Is(err, ErrEvidenceDurability) {
		t.Fatalf("ensure non-private directory error = %v, want ErrEvidenceDurability", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat non-private directory: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("ensure rewrote existing mode to %04o", info.Mode().Perm())
	}
}

func TestBundlePublisherRejectsSymlinkedOutputAncestor(t *testing.T) {
	base := t.TempDir()
	realParent := filepath.Join(base, "real")
	if err := os.Mkdir(realParent, 0o700); err != nil {
		t.Fatalf("Mkdir(real parent) error = %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(realParent, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	outputDir := filepath.Join(link, "build", "live-evidence")
	got, err := (BundlePublisher{OutputDir: outputDir}).Publish(bundleTestEvidence())
	bundleTestRequireDurabilityFailure(t, got, err)
	if _, err := os.Lstat(filepath.Join(realParent, "build")); !os.IsNotExist(err) {
		t.Fatalf("symlink target was traversed or inspect failed: %v", err)
	}
}

func TestBundlePublisherRejectsSymlinkedCandidateDirectory(t *testing.T) {
	outputDir := bundleTestOutputDir(t)
	if err := ensureBundlePrivateDirectory(outputDir); err != nil {
		t.Fatalf("ensure output directory: %v", err)
	}
	input := bundleTestEvidence()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatalf("Mkdir(outside) error = %v", err)
	}
	candidateLink := filepath.Join(outputDir, input.CandidateID)
	if err := os.Symlink(outside, candidateLink); err != nil {
		t.Fatalf("Symlink(candidate) error = %v", err)
	}

	got, err := (BundlePublisher{OutputDir: outputDir}).Publish(input)
	bundleTestRequireDurabilityFailure(t, got, err)
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatalf("ReadDir(outside) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("symlinked candidate target was modified: %v", entries)
	}
}
