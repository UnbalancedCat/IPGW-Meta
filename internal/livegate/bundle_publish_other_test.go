//go:build !linux && !windows

package livegate

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBundlePublisherUnsupportedPlatformFailsClosed(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "build", "live-evidence")
	evidence := cloneEvidence(validPasswordEvidence())
	evidence.EvidenceID = ""

	got, err := (BundlePublisher{OutputDir: outputDir}).Publish(evidence)
	if !errors.Is(err, ErrEvidenceDurability) {
		t.Fatalf("Publish() error = %v, want ErrEvidenceDurability", err)
	}
	if !reflect.DeepEqual(got, PublishedBundle{}) {
		t.Fatalf("Publish() result = %#v, want zero value", got)
	}

	candidateDir := filepath.Join(outputDir, evidence.CandidateID)
	entries, readErr := os.ReadDir(candidateDir)
	if os.IsNotExist(readErr) {
		return
	}
	if readErr != nil {
		t.Fatalf("inspect unsupported-platform output: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("unsupported platform left candidate artifacts: %v", entries)
	}
}
