//go:build linux || windows

package livegate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWritePrivateBundleFileCreatesExactPrivateFileWithoutClobberOrLKG(t *testing.T) {
	parent := bundleTestOutputDir(t)
	if err := ensureBundlePrivateDirectory(parent); err != nil {
		t.Fatalf("ensure private staging parent: %v", err)
	}
	path := filepath.Join(parent, bundleEvidenceName)
	first := []byte("first bundle bytes\n")
	if err := writePrivateBundleFile(path, first); err != nil {
		t.Fatalf("write first private bundle file: %v", err)
	}
	if err := verifyBundlePrivateFile(path); err != nil {
		t.Fatalf("verify first private bundle file: %v", err)
	}
	if err := writePrivateBundleFile(path, []byte("must not replace\n")); err == nil {
		t.Fatal("second write unexpectedly replaced an existing bundle file")
	}
	actual, err := readPrivateBundleFile(path)
	if err != nil {
		t.Fatalf("read preserved bundle file: %v", err)
	}
	if !bytes.Equal(actual, first) {
		t.Fatalf("existing bundle file was changed: got %q, want %q", actual, first)
	}
	if _, err := os.Lstat(path + ".lkg"); !os.IsNotExist(err) {
		t.Fatalf("bundle writer created an LKG sidecar or inspect failed: %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatalf("read staging parent: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != bundleEvidenceName {
		t.Fatalf("bundle writer left unexpected entries: %v", entries)
	}
}
