package config

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

const journalSecretCanary = "journal-secret-canary-must-not-appear"

func TestMigrationJournalRoundTripAndMonotonicPhases(t *testing.T) {
	paths, journal := migrationJournalFixture(t)
	writeJournalArtifact(t, journal.Config.TargetPath, "config-v1\n")
	writeJournalArtifact(t, journal.Marker.TargetPath, "marker-v1\n")

	if err := beginMigrationJournal(paths.MigrationJournal, journal); err != nil {
		t.Fatalf("beginMigrationJournal() error = %v", err)
	}
	assertPrivateMigrationFile(t, paths.MigrationJournal)

	stored, err := loadMigrationJournal(paths.MigrationJournal)
	if err != nil {
		t.Fatalf("loadMigrationJournal() error = %v", err)
	}
	if !reflect.DeepEqual(stored, journal) {
		t.Fatalf("loaded journal mismatch\n got: %#v\nwant: %#v", stored, journal)
	}
	if err := completeMigrationJournal(paths.MigrationJournal, journal.TransactionID); err == nil {
		t.Fatal("completeMigrationJournal() before marker_verified succeeded")
	}
	if _, err := os.Stat(paths.MigrationJournal); err != nil {
		t.Fatalf("journal removed after rejected completion: %v", err)
	}

	for _, phase := range []migrationJournalPhase{
		migrationPhaseBackups,
		migrationPhaseKeyring,
		migrationPhaseConfig,
		migrationPhaseMarkerVerified,
	} {
		stored, err = advanceMigrationJournal(paths.MigrationJournal, journal.TransactionID, phase)
		if err != nil {
			t.Fatalf("advanceMigrationJournal(%q) error = %v", phase, err)
		}
		if stored.Phase != phase {
			t.Fatalf("stored phase = %q, want %q", stored.Phase, phase)
		}
		assertPrivateMigrationFile(t, paths.MigrationJournal)
	}
	if err := completeMigrationJournal(paths.MigrationJournal, journal.TransactionID); err != nil {
		t.Fatalf("completeMigrationJournal() error = %v", err)
	}
	if _, err := os.Stat(paths.MigrationJournal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal exists after completion: %v", err)
	}
}

func TestMigrationJournalStrictBoundedLoad(t *testing.T) {
	tests := map[string]func([]byte) []byte{
		"unknown field": func(data []byte) []byte {
			return append(data, []byte("unknown_field: true\n")...)
		},
		"trailing document": func(data []byte) []byte {
			return append(data, []byte("---\nphase: prepared\n")...)
		},
		"oversize": func([]byte) []byte {
			return bytes.Repeat([]byte("x"), maxConfigBytes+1)
		},
		"invalid phase does not echo content": func(data []byte) []byte {
			return bytes.Replace(data, []byte("phase: prepared"), []byte("phase: "+journalSecretCanary), 1)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			paths, journal := migrationJournalFixture(t)
			if err := beginMigrationJournal(paths.MigrationJournal, journal); err != nil {
				t.Fatalf("beginMigrationJournal() error = %v", err)
			}
			original, err := os.ReadFile(paths.MigrationJournal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(paths.MigrationJournal, mutate(original), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = loadMigrationJournal(paths.MigrationJournal)
			if err == nil {
				t.Fatal("loadMigrationJournal() succeeded for invalid journal")
			}
			if strings.Contains(err.Error(), journalSecretCanary) {
				t.Fatalf("load error leaked canary: %v", err)
			}
		})
	}
}

func TestMigrationJournalRejectsInvalidIdentityPhaseAndPaths(t *testing.T) {
	tests := map[string]func(*migrationJournal, Paths){
		"transaction ID": func(journal *migrationJournal, _ Paths) {
			journal.TransactionID = "NOT-A-TRANSACTION-ID"
		},
		"phase": func(journal *migrationJournal, _ Paths) {
			journal.Phase = "future"
		},
		"relative config path": func(journal *migrationJournal, _ Paths) {
			journal.Config.TargetPath = "config.yaml"
		},
		"outside backup path": func(journal *migrationJournal, paths Paths) {
			journal.Backups[0].Path = filepath.Join(filepath.Dir(paths.BaseDir), "outside.backup")
		},
		"path collision": func(journal *migrationJournal, _ Paths) {
			journal.Marker.TargetPath = journal.Config.TargetPath
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			paths, journal := migrationJournalFixture(t)
			mutate(&journal, paths)
			err := beginMigrationJournal(paths.MigrationJournal, journal)
			if err == nil {
				t.Fatal("beginMigrationJournal() succeeded")
			}
			if _, statErr := os.Stat(paths.MigrationJournal); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("invalid begin created journal: %v", statErr)
			}
		})
	}

	paths, journal := migrationJournalFixture(t)
	if err := beginMigrationJournal(paths.MigrationJournal, journal); err != nil {
		t.Fatal(err)
	}
	if _, err := advanceMigrationJournal(paths.MigrationJournal, journal.TransactionID, migrationPhaseKeyring); err == nil {
		t.Fatal("skipped phase transition succeeded")
	}
	stored, err := loadMigrationJournal(paths.MigrationJournal)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Phase != migrationPhasePrepared {
		t.Fatalf("rejected transition changed phase to %q", stored.Phase)
	}
}

func TestMigrationJournalBeginIsExclusiveAndSecretFree(t *testing.T) {
	paths, journal := migrationJournalFixture(t)
	if err := beginMigrationJournal(paths.MigrationJournal, journal); err != nil {
		t.Fatal(err)
	}
	if err := beginMigrationJournal(paths.MigrationJournal, journal); err == nil {
		t.Fatal("second beginMigrationJournal() succeeded")
	}
	data, err := os.ReadFile(paths.MigrationJournal)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{journalSecretCanary, "password:", "secret:", "raw_source_digest:", "source_digest:"} {
		if bytes.Contains(data, []byte(forbidden)) {
			t.Fatalf("journal contains forbidden value %q", forbidden)
		}
	}

	journal.ToolVersion = journalSecretCanary + "\n"
	err = beginMigrationJournal(filepath.Join(paths.BaseDir, "second.pending.yaml"), journal)
	if err == nil {
		t.Fatal("begin with invalid tool version succeeded")
	}
	if strings.Contains(err.Error(), journalSecretCanary) {
		t.Fatalf("validation error leaked canary: %v", err)
	}
}

func TestMigrationSnapshotDigestPermissionsAndSafeRemoval(t *testing.T) {
	base := filepath.Join(t.TempDir(), "state")
	source := filepath.Join(base, "config.yaml")
	recovery := filepath.Join(base, "config.before")
	writeJournalArtifact(t, source, "secret-free-config\n")

	snapshot, err := createMigrationSnapshot(source, recovery)
	if err != nil {
		t.Fatalf("createMigrationSnapshot() error = %v", err)
	}
	if !snapshot.Existed || snapshot.Path != recovery {
		t.Fatalf("snapshot metadata = %#v", snapshot)
	}
	assertPrivateMigrationFile(t, recovery)
	data, err := loadMigrationSnapshot(snapshot)
	if err != nil {
		t.Fatalf("loadMigrationSnapshot() error = %v", err)
	}
	if string(data) != "secret-free-config\n" {
		t.Fatalf("snapshot data = %q", data)
	}

	if err := os.WriteFile(recovery, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeMigrationSnapshot(snapshot); err == nil {
		t.Fatal("removeMigrationSnapshot() removed digest-mismatched data")
	}
	if _, err := os.Stat(recovery); err != nil {
		t.Fatalf("digest-mismatched recovery was removed: %v", err)
	}
	if err := os.WriteFile(recovery, []byte("secret-free-config\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeMigrationSnapshot(snapshot); err != nil {
		t.Fatalf("removeMigrationSnapshot() error = %v", err)
	}
	if _, err := os.Stat(recovery); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery still exists: %v", err)
	}

	missingSnapshot, err := createMigrationSnapshot(filepath.Join(base, "missing.yaml"), filepath.Join(base, "missing.before"))
	if err != nil {
		t.Fatalf("snapshot missing source: %v", err)
	}
	if missingSnapshot != (migrationJournalSnapshot{}) {
		t.Fatalf("missing snapshot metadata = %#v", missingSnapshot)
	}
}

func TestMigrationPathsAreIsolated(t *testing.T) {
	defaults, err := DefaultPaths()
	if err != nil {
		t.Fatalf("DefaultPaths() error = %v", err)
	}
	assertDistinctMigrationPaths(t, defaults)

	oldLegacyMeta := defaults.LegacyMetaYAML
	oldLegacyUpstream := defaults.LegacyUpstream
	configPath := filepath.Join(t.TempDir(), "custom", "config.yaml")
	paths := WithConfigPath(defaults, configPath)
	if paths.BaseDir != filepath.Dir(configPath) || paths.ConfigFile != configPath {
		t.Fatalf("WithConfigPath() base/config = %q/%q", paths.BaseDir, paths.ConfigFile)
	}
	if paths.MigrationJournal != filepath.Join(paths.BaseDir, "migration-v1.pending.yaml") ||
		paths.MigrationConfigRecovery != filepath.Join(paths.BaseDir, "migration-v1.config.before") ||
		paths.MigrationMarkerRecovery != filepath.Join(paths.BaseDir, "migration-v1.marker.before") {
		t.Fatalf("migration paths are not based in custom directory: %#v", paths)
	}
	if paths.ProtocolCacheDir != filepath.Join(paths.BaseDir, "protocol-cache") {
		t.Fatalf("existing protocol-cache derivation changed: %q", paths.ProtocolCacheDir)
	}
	if paths.LegacyMetaYAML != oldLegacyMeta || paths.LegacyUpstream != oldLegacyUpstream {
		t.Fatal("WithConfigPath() changed legacy discovery paths")
	}
	assertDistinctMigrationPaths(t, paths)
}

func migrationJournalFixture(t *testing.T) (Paths, migrationJournal) {
	t.Helper()
	base := filepath.Join(t.TempDir(), "state")
	paths := WithConfigPath(Paths{}, filepath.Join(base, "config.yaml"))
	transactionID := strings.Repeat("a", migrationTransactionIDBytes*2)
	locationHash := migrationArtifactDigest([]byte("source-location"))
	return paths, migrationJournal{
		SchemaVersion: migrationJournalSchemaVersion,
		TransactionID: transactionID,
		Phase:         migrationPhasePrepared,
		TargetSchema:  SchemaVersion,
		ToolVersion:   "test-v1",
		Sources: []migrationJournalSourceStamp{{
			Kind:           MigrationSourceMetaYAML,
			LocationHash:   locationHash,
			RedactedDigest: migrationArtifactDigest([]byte("redacted-source-with-fixed-placeholder")),
		}},
		Config: migrationJournalArtifact{
			TargetPath:  paths.ConfigFile,
			Before:      migrationJournalSnapshot{},
			AfterDigest: migrationArtifactDigest([]byte("config-v1\n")),
		},
		Marker: migrationJournalArtifact{
			TargetPath:  paths.MigrationMarker,
			Before:      migrationJournalSnapshot{},
			AfterDigest: migrationArtifactDigest([]byte("marker-v1\n")),
		},
		Backups: []migrationJournalBackupRecord{{
			SourceLocationHash: locationHash,
			Path:               filepath.Join(base, "backups", "meta.backup"),
		}},
		NewKeyringRefs: []string{"migration/test/ref"},
	}
}

func writeJournalArtifact(t *testing.T, path, value string) {
	t.Helper()
	if err := atomicWriteFile(path, []byte(value), 0o600, false); err != nil {
		t.Fatalf("write artifact %q: %v", path, err)
	}
}

func assertPrivateMigrationFile(t *testing.T, path string) {
	t.Helper()
	file, err := openRestrictedPasswordFile(path)
	if err != nil {
		t.Fatalf("private file validation failed for %q: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close private file: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("private file mode = %o", info.Mode().Perm())
		}
	}
}

func assertDistinctMigrationPaths(t *testing.T, paths Paths) {
	t.Helper()
	values := []string{
		paths.ConfigFile,
		paths.ProtocolCacheDir,
		paths.MigrationMarker,
		paths.MigrationJournal,
		paths.MigrationConfigRecovery,
		paths.MigrationMarkerRecovery,
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			t.Fatal("state path is empty")
		}
		key := migrationPathKey(value)
		if _, exists := seen[key]; exists {
			t.Fatalf("state path collision: %q", value)
		}
		seen[key] = struct{}{}
	}
}
