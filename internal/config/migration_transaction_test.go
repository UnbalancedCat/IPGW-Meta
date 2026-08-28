package config

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

func TestApplyMigrationTransactionWritesVerifiedSecretFreeState(t *testing.T) {
	paths := migrationFixturePaths(t)
	encoded := base64.StdEncoding.EncodeToString([]byte(migrationSecretCanary))
	writeMigrationFixture(t, paths.LegacyMetaYAML, fmt.Sprintf(`default_account: fixture
accounts: [{username: fixture, password: %s}]
`, encoded))
	plan, err := BuildMigrationPlan(paths, Default())
	if err != nil {
		t.Fatal(err)
	}
	if err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderEnv, Reference: "IPGW_FIXTURE_PASSWORD"}); err != nil {
		t.Fatal(err)
	}
	store := &Store{Path: paths.ConfigFile}
	result, err := ApplyMigrationWithOptions(paths, store, Default(), plan, MigrationApplyOptions{ToolVersion: "test-v1"})
	if err != nil {
		t.Fatalf("ApplyMigrationWithOptions() error = %v", err)
	}
	paths = normalizeMigrationPaths(paths)
	if _, err := os.Stat(paths.MigrationJournal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("completed transaction left journal: %v", err)
	}
	for _, recovery := range []string{paths.MigrationConfigRecovery, paths.MigrationMarkerRecovery} {
		if _, err := os.Stat(recovery); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("completed transaction left recovery file: %v", err)
		}
	}

	marker, exists, err := loadMigrationMarker(paths.MigrationMarker)
	if err != nil || !exists {
		t.Fatalf("loadMigrationMarker(): exists=%t err=%v", exists, err)
	}
	if marker.ToolVersion != "test-v1" || marker.TargetSchema != SchemaVersion || len(marker.Sources) != 1 {
		t.Fatalf("marker metadata = %#v", marker)
	}
	if err := verifyMigrationArtifact(paths.ConfigFile, marker.ConfigDigest); err != nil {
		t.Fatalf("marker config digest: %v", err)
	}
	for _, path := range []string{paths.ConfigFile, paths.MigrationMarker, result.Backups[marker.Sources[0].LocationHash]} {
		assertPrivateMigrationFile(t, path)
	}
	for _, path := range []string{paths.ConfigFile, paths.MigrationMarker} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{migrationSecretCanary, encoded, "raw_source_digest", "source_digest:"} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Fatalf("%s contains forbidden migration material %q", filepath.Base(path), forbidden)
			}
		}
	}

	previous, already, err := MigrationAlreadyApplied(paths.MigrationMarker)
	if err != nil || !already || previous.Marker != result.Marker {
		t.Fatalf("MigrationAlreadyApplied(): already=%t result=%#v err=%v", already, previous, err)
	}
	cfg, found, err := store.Load()
	if err != nil || !found {
		t.Fatalf("load migrated config: found=%t err=%v", found, err)
	}
	retryPlan, err := BuildMigrationPlan(paths, cfg)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := ApplyMigrationWithOptions(paths, store, cfg, retryPlan, MigrationApplyOptions{ToolVersion: "test-v1"})
	if err != nil {
		t.Fatalf("idempotent ApplyMigrationWithOptions() error = %v", err)
	}
	if !reflectMigrationResults(result, retry) {
		t.Fatalf("idempotent result = %#v, want %#v", retry, result)
	}
}

func TestApplyMigrationSourceChangeFailsBeforeFirstSideEffect(t *testing.T) {
	paths := migrationFixturePaths(t)
	plan := buildSingleImportablePlan(t, paths, Default())
	if err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderEnv, Reference: "IPGW_FIXTURE_PASSWORD"}); err != nil {
		t.Fatal(err)
	}
	changedSecret := migrationSecretCanary + "-changed"
	changedEncoded := base64.StdEncoding.EncodeToString([]byte(changedSecret))
	writeMigrationFixture(t, paths.LegacyMetaYAML, fmt.Sprintf(`default_account: fixture
accounts: [{username: fixture, password: %s}]
`, changedEncoded))
	_, err := ApplyMigrationWithOptions(paths, &Store{Path: paths.ConfigFile}, Default(), plan, MigrationApplyOptions{ToolVersion: "test-v1"})
	if err == nil || !strings.Contains(err.Error(), "changed after preview") {
		t.Fatalf("ApplyMigrationWithOptions() error = %v", err)
	}
	if strings.Contains(err.Error(), changedSecret) || strings.Contains(err.Error(), changedEncoded) {
		t.Fatalf("source-change error leaked secret: %v", err)
	}
	if _, statErr := os.Stat(paths.BaseDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("source-change preflight created target state: %v", statErr)
	}
}

func TestApplyMigrationRejectsDestinationGenerationChange(t *testing.T) {
	paths := migrationFixturePaths(t)
	destination := Default()
	plan := buildSingleImportablePlan(t, paths, destination)
	if err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderEnv, Reference: "IPGW_FIXTURE_PASSWORD"}); err != nil {
		t.Fatal(err)
	}

	store := &Store{Path: paths.ConfigFile, MigrationJournal: normalizeMigrationPaths(paths).MigrationJournal}
	concurrent := Default()
	concurrent.Profiles["concurrent"] = Profile{
		Username:   "concurrent",
		Credential: CredentialRef{Provider: ProviderPrompt},
		Switch:     SwitchRefuse,
	}
	concurrent.DefaultProfile = "concurrent"
	if err := store.Save(concurrent); err != nil {
		t.Fatalf("concurrent Store.Save() error = %v", err)
	}

	_, err := ApplyMigrationWithOptions(paths, store, destination, plan, MigrationApplyOptions{ToolVersion: "test-v1"})
	if err == nil || !strings.Contains(err.Error(), "destination changed after preview") {
		t.Fatalf("ApplyMigrationWithOptions() error = %v", err)
	}
	loaded, found, loadErr := store.Load()
	if loadErr != nil || !found {
		t.Fatalf("load concurrent destination: found=%t err=%v", found, loadErr)
	}
	if _, exists := loaded.Profiles["concurrent"]; !exists || len(loaded.Profiles) != 1 {
		t.Fatalf("concurrent destination was overwritten: %#v", loaded.Profiles)
	}

	normalized := normalizeMigrationPaths(paths)
	for _, path := range []string{
		normalized.MigrationMarker,
		normalized.MigrationJournal,
		normalized.MigrationConfigRecovery,
		normalized.MigrationMarkerRecovery,
		filepath.Join(normalized.BaseDir, "migration-backups"),
	} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("generation mismatch created migration artifact %s: %v", path, statErr)
		}
	}
}

func TestApplyMigrationKeyringTransaction(t *testing.T) {
	t.Run("success uses a generated fresh reference", func(t *testing.T) {
		paths := migrationFixturePaths(t)
		plan := buildSingleImportablePlan(t, paths, Default())
		secret := plan.state.secrets["fixture"]
		if err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderKeyring}); err != nil {
			t.Fatal(err)
		}
		reference := migrationProfileByName(t, &plan, "fixture").Credential.Reference
		backend := newMigrationFakeKeyring()
		result, err := ApplyMigrationWithOptions(paths, &Store{Path: paths.ConfigFile}, Default(), plan, MigrationApplyOptions{
			ToolVersion: "test-v1", ProviderOptions: ProviderOptions{Keyring: backend},
		})
		if err != nil {
			t.Fatalf("ApplyMigrationWithOptions() error = %v", err)
		}
		if !validMigrationKeyringReference(reference) || backend.values[reference] != migrationSecretCanary || backend.sets != 1 {
			t.Fatalf("keyring state reference=%q sets=%d values=%#v", reference, backend.sets, backend.values)
		}
		assertZeroedBytes(t, secret)
		cfg, found, err := (&Store{Path: paths.ConfigFile}).Load()
		if err != nil || !found {
			t.Fatalf("load migrated config: found=%t err=%v", found, err)
		}
		if got := cfg.Profiles["fixture"].Credential; got != (CredentialRef{Provider: ProviderKeyring, Reference: reference}) {
			t.Fatalf("migrated credential = %#v", got)
		}
		for _, path := range []string{paths.ConfigFile, paths.MigrationMarker} {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if bytes.Contains(data, []byte(migrationSecretCanary)) {
				t.Fatalf("%s leaked the imported password", filepath.Base(path))
			}
		}
		if len(result.Backups) != 1 {
			t.Fatalf("backups = %#v", result.Backups)
		}
	})

	for _, test := range []struct {
		name       string
		configure  func(*migrationFakeKeyring, string)
		wantDelete bool
	}{
		{
			name: "occupied reference is never overwritten",
			configure: func(backend *migrationFakeKeyring, reference string) {
				backend.values[reference] = "existing-value"
			},
		},
		{
			name: "backend preflight error is redacted",
			configure: func(backend *migrationFakeKeyring, _ string) {
				backend.getErr = errors.New("backend exposed " + migrationSecretCanary)
			},
		},
		{
			name: "set error rolls transaction back",
			configure: func(backend *migrationFakeKeyring, _ string) {
				backend.setErr = errors.New("set exposed " + migrationSecretCanary)
			},
			wantDelete: true,
		},
		{
			name: "readback mismatch rolls transaction back",
			configure: func(backend *migrationFakeKeyring, _ string) {
				backend.corruptSet = true
			},
			wantDelete: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			paths := migrationFixturePaths(t)
			plan := buildSingleImportablePlan(t, paths, Default())
			secret := plan.state.secrets["fixture"]
			if err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderKeyring}); err != nil {
				t.Fatal(err)
			}
			reference := migrationProfileByName(t, &plan, "fixture").Credential.Reference
			backend := newMigrationFakeKeyring()
			test.configure(backend, reference)
			_, err := ApplyMigrationWithOptions(paths, &Store{Path: paths.ConfigFile}, Default(), plan, MigrationApplyOptions{
				ToolVersion: "test-v1", ProviderOptions: ProviderOptions{Keyring: backend},
			})
			if err == nil {
				t.Fatal("ApplyMigrationWithOptions() unexpectedly succeeded")
			}
			if strings.Contains(err.Error(), migrationSecretCanary) || strings.Contains(err.Error(), "existing-value") {
				t.Fatalf("migration error leaked backend material: %v", err)
			}
			if test.wantDelete && backend.deletes == 0 {
				t.Fatal("failed keyring transaction did not attempt rollback")
			}
			if test.wantDelete {
				if _, exists := backend.values[reference]; exists {
					t.Fatal("failed keyring transaction left a generated entry")
				}
			}
			assertZeroedBytes(t, secret)
			assertMigrationApplySideEffectFree(t, paths)
		})
	}
}

func TestRecoverPendingMigrationRemovesKeyringWriteBeforeCheckpoint(t *testing.T) {
	fixture := newCrashedMigrationFixture(t, migrationPhaseBackups)
	journal, err := loadMigrationJournal(fixture.paths.MigrationJournal)
	if err != nil {
		t.Fatal(err)
	}
	reference := "migration-" + strings.Repeat("c", migrationTransactionIDBytes*2)
	journal.NewKeyringRefs = []string{reference}
	data, err := marshalMigrationJournal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteFile(fixture.paths.MigrationJournal, data, 0o600, false); err != nil {
		t.Fatal(err)
	}
	backend := newMigrationFakeKeyring()
	backend.values[reference] = migrationSecretCanary
	if err := RecoverPendingMigration(fixture.paths, MigrationApplyOptions{ProviderOptions: ProviderOptions{Keyring: backend}}); err != nil {
		t.Fatalf("RecoverPendingMigration() error = %v", err)
	}
	if _, exists := backend.values[reference]; exists || backend.deletes != 1 {
		t.Fatalf("keyring rollback values=%#v deletes=%d", backend.values, backend.deletes)
	}
}

func TestRecoverPendingMigrationAcrossEveryPhase(t *testing.T) {
	for _, phase := range []migrationJournalPhase{
		migrationPhasePrepared,
		migrationPhaseBackups,
		migrationPhaseKeyring,
		migrationPhaseConfig,
		migrationPhaseMarkerVerified,
	} {
		t.Run(string(phase), func(t *testing.T) {
			fixture := newCrashedMigrationFixture(t, phase)
			if err := RecoverPendingMigration(fixture.paths, MigrationApplyOptions{}); err != nil {
				t.Fatalf("RecoverPendingMigration() error = %v", err)
			}
			if _, err := os.Stat(fixture.paths.MigrationJournal); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("recovery left journal: %v", err)
			}
			for _, recovery := range []string{fixture.paths.MigrationConfigRecovery, fixture.paths.MigrationMarkerRecovery} {
				if _, err := os.Stat(recovery); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("recovery left snapshot: %v", err)
				}
			}

			configData, err := os.ReadFile(fixture.paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if phase == migrationPhaseMarkerVerified {
				if !bytes.Equal(configData, fixture.configAfter) {
					t.Fatalf("committed recovery changed config to %q", configData)
				}
				if markerData, err := os.ReadFile(fixture.paths.MigrationMarker); err != nil || !bytes.Equal(markerData, fixture.markerAfter) {
					t.Fatalf("committed recovery changed marker: data=%q err=%v", markerData, err)
				}
				if _, err := os.Stat(fixture.backupPath); err != nil {
					t.Fatalf("committed recovery removed backup: %v", err)
				}
				return
			}
			if !bytes.Equal(configData, fixture.configBefore) {
				t.Fatalf("rollback config = %q, want %q", configData, fixture.configBefore)
			}
			if _, err := os.Stat(fixture.paths.MigrationMarker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rollback left marker: %v", err)
			}
			if _, err := os.Stat(fixture.backupPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rollback left backup: %v", err)
			}
		})
	}
}

func TestRecoverPendingMigrationPreservesUnknownConcurrentArtifact(t *testing.T) {
	fixture := newCrashedMigrationFixture(t, migrationPhaseKeyring)
	unknown := []byte("unknown-concurrent-config\n")
	if err := atomicWriteFile(fixture.paths.ConfigFile, unknown, 0o600, false); err != nil {
		t.Fatal(err)
	}
	err := RecoverPendingMigration(fixture.paths, MigrationApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "recovery required") {
		t.Fatalf("RecoverPendingMigration() error = %v", err)
	}
	data, readErr := os.ReadFile(fixture.paths.ConfigFile)
	if readErr != nil || !bytes.Equal(data, unknown) {
		t.Fatalf("recovery overwrote concurrent config: data=%q err=%v", data, readErr)
	}
	for _, path := range []string{fixture.paths.MigrationJournal, fixture.paths.MigrationConfigRecovery, fixture.backupPath} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("unknown-state recovery removed %s: %v", filepath.Base(path), statErr)
		}
	}
}

func TestMigrationMarkerRejectsUnknownTrailingAndOversize(t *testing.T) {
	base := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(base, "migration-v1.yaml")
	for name, value := range map[string][]byte{
		"unknown":  []byte("schema_version: 1\nunknown: true\n"),
		"trailing": []byte("schema_version: 1\n---\nschema_version: 1\n"),
		"oversize": bytes.Repeat([]byte("x"), maxConfigBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := atomicWriteFile(path, value, 0o600, false); err != nil {
				t.Fatal(err)
			}
			_, _, err := MigrationAlreadyApplied(path)
			if err == nil {
				t.Fatal("MigrationAlreadyApplied() accepted invalid marker")
			}
			if removeErr := os.Remove(path); removeErr != nil {
				t.Fatal(removeErr)
			}
		})
	}
}

type crashedMigrationFixture struct {
	paths        Paths
	configBefore []byte
	configAfter  []byte
	markerAfter  []byte
	backupPath   string
}

func newCrashedMigrationFixture(t *testing.T, phase migrationJournalPhase) crashedMigrationFixture {
	t.Helper()
	base := filepath.Join(t.TempDir(), "state")
	paths := normalizeMigrationPaths(WithConfigPath(Paths{}, filepath.Join(base, "config.yaml")))
	configBefore := []byte("schema_version: 1\nprofiles: {}\n")
	configAfter := []byte("schema_version: 1\ndefault_profile: fixture\nprofiles:\n  fixture:\n    username: fixture\n    credential:\n      provider: prompt\n")
	markerAfter := []byte("committed-marker\n")
	if err := atomicWriteFile(paths.ConfigFile, configBefore, 0o600, false); err != nil {
		t.Fatal(err)
	}
	configSnapshot, configSnapshotData, err := prepareMigrationSnapshot(paths.ConfigFile, paths.MigrationConfigRecovery)
	if err != nil {
		t.Fatal(err)
	}
	markerSnapshot, markerSnapshotData, err := prepareMigrationSnapshot(paths.MigrationMarker, paths.MigrationMarkerRecovery)
	if err != nil {
		t.Fatal(err)
	}
	locationHash := migrationArtifactDigest([]byte("crash-source-location"))
	backupPath := filepath.Join(base, "migration-backups", "meta-crash.backup")
	journal := migrationJournal{
		SchemaVersion: migrationJournalSchemaVersion,
		TransactionID: strings.Repeat("b", migrationTransactionIDBytes*2),
		Phase:         phase,
		TargetSchema:  SchemaVersion,
		ToolVersion:   "test-v1",
		Sources: []migrationJournalSourceStamp{{
			Kind: MigrationSourceMetaYAML, LocationHash: locationHash,
			RedactedDigest: migrationArtifactDigest([]byte("redacted-crash-source")),
		}},
		Config: migrationJournalArtifact{
			TargetPath: paths.ConfigFile, Before: configSnapshot, AfterDigest: migrationArtifactDigest(configAfter),
		},
		Marker: migrationJournalArtifact{
			TargetPath: paths.MigrationMarker, Before: markerSnapshot, AfterDigest: migrationArtifactDigest(markerAfter),
		},
		Backups: []migrationJournalBackupRecord{{SourceLocationHash: locationHash, Path: backupPath}},
	}
	if err := beginMigrationJournal(paths.MigrationJournal, journal); err != nil {
		t.Fatal(err)
	}
	if err := writePreparedMigrationSnapshot(configSnapshot, configSnapshotData); err != nil {
		t.Fatal(err)
	}
	if err := writePreparedMigrationSnapshot(markerSnapshot, markerSnapshotData); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusiveMigrationFile(backupPath, []byte("private-source-backup\n")); err != nil {
		t.Fatal(err)
	}
	if phase == migrationPhaseKeyring || phase == migrationPhaseConfig || phase == migrationPhaseMarkerVerified {
		if err := atomicWriteFile(paths.ConfigFile, configAfter, 0o600, false); err != nil {
			t.Fatal(err)
		}
	}
	if phase == migrationPhaseConfig || phase == migrationPhaseMarkerVerified {
		if err := atomicWriteFile(paths.MigrationMarker, markerAfter, 0o600, false); err != nil {
			t.Fatal(err)
		}
	}
	return crashedMigrationFixture{
		paths: paths, configBefore: configBefore, configAfter: configAfter,
		markerAfter: markerAfter, backupPath: backupPath,
	}
}

func reflectMigrationResults(left, right MigrationResult) bool {
	if left.Marker != right.Marker || len(left.AppliedProfiles) != len(right.AppliedProfiles) || len(left.Backups) != len(right.Backups) {
		return false
	}
	for index := range left.AppliedProfiles {
		if left.AppliedProfiles[index] != right.AppliedProfiles[index] {
			return false
		}
	}
	for key, value := range left.Backups {
		if right.Backups[key] != value {
			return false
		}
	}
	return true
}

type migrationFakeKeyring struct {
	values     map[string]string
	getErr     error
	setErr     error
	deleteErr  error
	corruptSet bool
	sets       int
	deletes    int
}

func newMigrationFakeKeyring() *migrationFakeKeyring {
	return &migrationFakeKeyring{values: make(map[string]string)}
}

func (f *migrationFakeKeyring) Get(_ string, user string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	value, exists := f.values[user]
	if !exists {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (f *migrationFakeKeyring) Set(_ string, user, password string) error {
	f.sets++
	if f.setErr != nil {
		return f.setErr
	}
	if f.corruptSet {
		f.values[user] = "corrupt-readback"
	} else {
		f.values[user] = password
	}
	return nil
}

func (f *migrationFakeKeyring) Delete(_ string, user string) error {
	f.deletes++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, exists := f.values[user]; !exists {
		return keyring.ErrNotFound
	}
	delete(f.values, user)
	return nil
}
