package candidate

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRegularSnapshotIsBoundedAndBoundToOpenedObject(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "payload")
	original := []byte("original\n")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	directory, err := openDirectorySnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.close()
	snapshot, err := openRegularSnapshotAt(directory.file, "payload", 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()

	displaced := filepath.Join(root, "displaced")
	if err := os.Rename(path, displaced); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatalf("rename open snapshot: %v", err)
		}
		if raw, readErr := snapshot.readAll(); readErr != nil || !bytes.Equal(raw, original) {
			t.Fatalf("locked snapshot read = %q, %v", raw, readErr)
		}
		return
	}
	if err := os.WriteFile(path, []byte("replacement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := snapshot.readAll()
	if err != nil || !bytes.Equal(raw, original) {
		t.Fatalf("snapshot followed replacement path: %q, %v", raw, err)
	}
	if directoryUnchanged(root, directory, []string{"payload"}) {
		t.Fatal("directory replacement was not detected")
	}
}

func TestRegularSnapshotRejectsSameInodeMutationAndNewHardlink(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "payload")
	if err := os.WriteFile(path, []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := openRegularSnapshot(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	if err := os.WriteFile(path, []byte("after!\n"), 0o644); err == nil {
		if snapshot.unchanged() {
			t.Fatal("same-inode same-size mutation was accepted")
		}
	} else if runtime.GOOS != "windows" {
		t.Fatalf("overwrite open snapshot: %v", err)
	}

	path = filepath.Join(root, "hardlink-source")
	if err := os.WriteFile(path, []byte("linked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	linked, err := openRegularSnapshot(path, 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer linked.close()
	if err := os.Link(path, filepath.Join(root, "hardlink-copy")); err != nil {
		t.Skipf("filesystem does not support hardlinks: %v", err)
	}
	if linked.unchanged() {
		t.Fatal("new hardlink was accepted")
	}
}

func TestRegularSnapshotRejectsOversizeBeforeReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payload")
	if err := os.WriteFile(path, []byte("oversize"), 0o644); err != nil {
		t.Fatal(err)
	}
	if snapshot, err := openRegularSnapshot(path, 1); err == nil {
		snapshot.close()
		t.Fatal("oversized input was accepted")
	}
}
