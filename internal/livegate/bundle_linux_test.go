//go:build linux

package livegate

import (
	"os"
	"path/filepath"
	"testing"
)

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
