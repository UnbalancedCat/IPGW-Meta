package candidate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPublishCandidateDirectoryDoesNotReplaceRacingDirectory(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "final")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stage, "sentinel", []byte("stage\n"), 0o644)
	// This directory represents a competing creator winning after Assemble's
	// initial non-existence check and before the final publish operation.
	if err := os.Mkdir(final, 0o755); err != nil {
		t.Fatal(err)
	}
	stageInfo, err := os.Lstat(stage)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishCandidateDirectory(stage, final, stageInfo, nil, nil, nil); err == nil {
		t.Fatal("publish replaced a racing destination")
	}
	if _, err := os.Stat(filepath.Join(stage, "sentinel")); err != nil {
		t.Fatalf("stage changed after rejected publish: %v", err)
	}
	entries, err := os.ReadDir(final)
	if err != nil || len(entries) != 0 {
		t.Fatalf("racing destination changed: entries=%d err=%v", len(entries), err)
	}
}

func TestPublishCandidateDirectoryMovesOnlyToAbsentSibling(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "final")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stage, "sentinel", []byte("stage\n"), 0o644)
	stageInfo, err := os.Lstat(stage)
	if err != nil {
		t.Fatal(err)
	}
	err = publishCandidateDirectory(stage, final, stageInfo, nil, nil, nil)
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" && runtime.GOOS != "darwin" {
		if err == nil {
			t.Fatal("unsupported platform published instead of failing closed")
		}
		return
	}
	if err != nil {
		t.Fatalf("publishCandidateDirectory() error = %v", err)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatalf("stage still exists after publish: %v", err)
	}
	if raw := readTestFile(t, filepath.Join(final, "sentinel")); string(raw) != "stage\n" {
		t.Fatalf("published content = %q", raw)
	}
}

func TestPublishCandidateDirectoryRejectsReplacedStageIdentity(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	displaced := filepath.Join(parent, "displaced")
	final := filepath.Join(parent, "final")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stage, "sentinel", []byte("verified\n"), 0o644)
	stageInfo, err := os.Lstat(stage)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(stage, displaced); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stage, "sentinel", []byte("replacement\n"), 0o644)
	if err := publishCandidateDirectory(stage, final, stageInfo, nil, nil, nil); err == nil {
		t.Fatal("publish accepted a replacement stage inode")
	}
	if _, err := os.Lstat(final); !os.IsNotExist(err) {
		t.Fatalf("destination exists after identity rejection: %v", err)
	}
	if raw := readTestFile(t, filepath.Join(stage, "sentinel")); string(raw) != "replacement\n" {
		t.Fatalf("replacement stage changed: %q", raw)
	}
}

func TestPublishCandidateDirectoryRetainsSnapshotAcrossRename(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	final := filepath.Join(parent, "final")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, stage, "sentinel", []byte("sealed\n"), 0o644)
	directory, err := openDirectorySnapshot(stage)
	if err != nil {
		t.Fatal(err)
	}
	defer directory.close()
	if !directory.exact([]string{"sentinel"}) {
		t.Fatal("stage directory is not exact")
	}
	snapshot, err := openRegularSnapshotAt(directory.file, "sentinel", 1024)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.close()
	validate := func(root string) bool {
		return snapshot.unchanged() && directoryUnchanged(root, directory, []string{"sentinel"})
	}
	if !validate(stage) {
		t.Fatal("retained snapshot does not match stage before publish")
	}
	var release func()
	validateAfter := validate
	if runtime.GOOS == "windows" {
		release = func() {
			snapshot.close()
			directory.close()
		}
		validateAfter = func(root string) bool {
			reopened, err := openDirectorySnapshot(root)
			if err != nil {
				return false
			}
			defer reopened.close()
			if !reopened.exact([]string{"sentinel"}) {
				return false
			}
			file, err := openRegularSnapshotAt(reopened.file, "sentinel", 1024)
			if err != nil {
				return false
			}
			defer file.close()
			raw, err := file.readAll()
			return err == nil && string(raw) == "sealed\n"
		}
	}
	if err := publishCandidateDirectory(stage, final, directory.info, validate, release, validateAfter); err != nil {
		t.Fatalf("publish with retained snapshot error = %v", err)
	}
	if !validateAfter(final) {
		t.Fatal("retained snapshot no longer matches published directory")
	}
}
