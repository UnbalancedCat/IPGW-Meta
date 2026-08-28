package config

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const migrationSecretCanary = "fixed-migration-canary"

func TestBuildMigrationPlanCreatesSecretFreePendingPreview(t *testing.T) {
	paths := migrationFixturePaths(t)
	encoded := base64.StdEncoding.EncodeToString([]byte(migrationSecretCanary))
	writeMigrationFixture(t, paths.LegacyMetaYAML, fmt.Sprintf(`app:
  log_style: charm-color
  log_level: info
default_account: meta-user
accounts:
  - username: meta-user
    password: %s
`, encoded))
	const upstreamCiphertext = "opaque-upstream-fixture"
	writeMigrationFixture(t, paths.LegacyUpstream, `{
  "default_account": "upstream-user",
  "accounts": [{"username": "upstream-user", "encrypted_password": "`+upstreamCiphertext+`"}]
}`)

	plan, err := BuildMigrationPlan(paths, Default())
	if err != nil {
		t.Fatalf("BuildMigrationPlan() error = %v", err)
	}
	defer plan.Close()
	if len(plan.Sources) != 2 {
		t.Fatalf("len(Sources) = %d, want 2", len(plan.Sources))
	}
	for _, source := range plan.Sources {
		if len(source.LocationHash) != 64 {
			t.Fatalf("source location hash length = %d, want 64", len(source.LocationHash))
		}
		if strings.Contains(source.LocationHash, filepath.Base(paths.LegacyMetaYAML)) {
			t.Fatal("source location hash exposed a source path")
		}
	}

	meta := migrationProfileByName(t, &plan, "meta-user")
	if meta.CredentialStatus != MigrationCredentialPendingImportable {
		t.Fatalf("meta credential status = %q", meta.CredentialStatus)
	}
	if got := string(plan.state.secrets["meta-user"]); got != migrationSecretCanary {
		t.Fatalf("decoded in-memory fixture = %q, want fixed canary", got)
	}
	upstream := migrationProfileByName(t, &plan, "upstream-user")
	if upstream.CredentialStatus != MigrationCredentialPendingManual {
		t.Fatalf("upstream credential status = %q", upstream.CredentialStatus)
	}
	if _, exists := plan.state.secrets["upstream-user"]; exists {
		t.Fatal("upstream ciphertext was presented as an importable secret")
	}

	preview, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal(plan): %v", err)
	}
	for _, forbidden := range []string{
		migrationSecretCanary,
		encoded,
		upstreamCiphertext,
		paths.LegacyMetaYAML,
		paths.LegacyUpstream,
	} {
		if bytes.Contains(preview, []byte(forbidden)) {
			t.Fatalf("public preview contains forbidden material %q", forbidden)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.BaseDir, "credentials")); !os.IsNotExist(err) {
		t.Fatalf("planning created a credential directory: %v", err)
	}
}

func TestBuildMigrationPlanStrictParsing(t *testing.T) {
	tests := []struct {
		name     string
		meta     string
		upstream string
	}{
		{
			name: "unknown meta field",
			meta: `default_account: fixture
accounts:
  - username: fixture
    password: Zml4dHVyZQ==
    unexpected: fixed-migration-canary
`,
		},
		{
			name: "trailing meta document",
			meta: `default_account: fixture
accounts: []
---
default_account: second
accounts: []
`,
		},
		{
			name:     "unknown upstream field",
			upstream: `{"default_account":"fixture","accounts":[],"unexpected":"fixed-migration-canary"}`,
		},
		{
			name:     "trailing upstream value",
			upstream: `{"default_account":"fixture","accounts":[]} {"accounts":[]}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths := migrationFixturePaths(t)
			if test.meta != "" {
				writeMigrationFixture(t, paths.LegacyMetaYAML, test.meta)
			}
			if test.upstream != "" {
				writeMigrationFixture(t, paths.LegacyUpstream, test.upstream)
			}
			_, err := BuildMigrationPlan(paths, Default())
			if err == nil {
				t.Fatal("BuildMigrationPlan() accepted a non-strict source")
			}
			if strings.Contains(err.Error(), migrationSecretCanary) {
				t.Fatalf("strict parse error leaked a field value: %v", err)
			}
			assertMigrationPlanningSideEffectFree(t, paths)
		})
	}
}

func TestBuildMigrationPlanRejectsOversizedSource(t *testing.T) {
	paths := migrationFixturePaths(t)
	writeMigrationFixture(t, paths.LegacyMetaYAML, strings.Repeat("x", maxConfigBytes+1))
	_, err := BuildMigrationPlan(paths, Default())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("BuildMigrationPlan() error = %v, want size failure", err)
	}
	assertMigrationPlanningSideEffectFree(t, paths)
}

func TestInvalidBase64AndUpstreamCiphertextRemainManual(t *testing.T) {
	paths := migrationFixturePaths(t)
	writeMigrationFixture(t, paths.LegacyMetaYAML, `default_account: invalid
accounts:
  - username: invalid
    password: "%%%not-base64%%%"
`)
	writeMigrationFixture(t, paths.LegacyUpstream, `{
  "default_account": "upstream",
  "accounts": [{"username":"upstream","encrypted_password":"opaque-fixture-value"}]
}`)
	plan, err := BuildMigrationPlan(paths, Default())
	if err != nil {
		t.Fatalf("BuildMigrationPlan() error = %v", err)
	}
	defer plan.Close()
	for _, name := range []string{"invalid", "upstream"} {
		profile := migrationProfileByName(t, &plan, name)
		if profile.CredentialStatus != MigrationCredentialPendingManual {
			t.Fatalf("%s status = %q, want pending manual", name, profile.CredentialStatus)
		}
		if _, exists := plan.state.secrets[name]; exists {
			t.Fatalf("%s unexpectedly has importable secret material", name)
		}
	}
}

func TestResolveMigrationCredentialExplicitDecisions(t *testing.T) {
	paths := migrationFixturePaths(t)
	encoded := base64.StdEncoding.EncodeToString([]byte(migrationSecretCanary))
	writeMigrationFixture(t, paths.LegacyMetaYAML, fmt.Sprintf(`default_account: env-profile
accounts:
  - {username: env-profile, password: %s}
  - {username: file-profile, password: %s}
  - {username: prompt-profile, password: %s}
  - {username: keyring-profile, password: %s}
`, encoded, encoded, encoded, encoded))
	plan, err := BuildMigrationPlan(paths, Default())
	if err != nil {
		t.Fatalf("BuildMigrationPlan() error = %v", err)
	}
	defer plan.Close()

	envSecret := plan.state.secrets["env-profile"]
	if err := ResolveMigrationCredential(&plan, "env-profile", CredentialRef{Provider: ProviderEnv, Reference: "IPGW_FIXTURE_PASSWORD"}); err != nil {
		t.Fatalf("resolve env: %v", err)
	}
	assertZeroedBytes(t, envSecret)

	fileSecret := plan.state.secrets["file-profile"]
	fileReference := filepath.Join(t.TempDir(), "fixture.password")
	if err := ResolveMigrationCredential(&plan, "file-profile", CredentialRef{Provider: ProviderFile, Reference: fileReference}); err != nil {
		t.Fatalf("resolve file: %v", err)
	}
	assertZeroedBytes(t, fileSecret)
	if _, err := os.Stat(fileReference); !os.IsNotExist(err) {
		t.Fatalf("file decision created or read the destination: %v", err)
	}

	promptSecret := plan.state.secrets["prompt-profile"]
	if err := ResolveMigrationCredential(&plan, "prompt-profile", CredentialRef{Provider: ProviderPrompt}); err != nil {
		t.Fatalf("resolve prompt: %v", err)
	}
	assertZeroedBytes(t, promptSecret)

	keyringSecret := plan.state.secrets["keyring-profile"]
	if err := ResolveMigrationCredential(&plan, "keyring-profile", CredentialRef{Provider: ProviderKeyring}); err != nil {
		t.Fatalf("resolve keyring: %v", err)
	}
	keyringProfile := migrationProfileByName(t, &plan, "keyring-profile")
	if keyringProfile.CredentialStatus != MigrationCredentialKeyringImport || !validMigrationKeyringReference(keyringProfile.Credential.Reference) {
		t.Fatal("keyring decision was not marked as a pending import")
	}
	if !bytes.Equal(keyringSecret, []byte(migrationSecretCanary)) {
		t.Fatal("keyring decision cleared its secret before the transaction phase")
	}

	preview, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal(plan): %v", err)
	}
	for _, forbidden := range []string{"IPGW_FIXTURE_PASSWORD", fileReference, keyringProfile.Credential.Reference, migrationSecretCanary} {
		if bytes.Contains(preview, []byte(forbidden)) {
			t.Fatalf("resolved preview exposed credential material or reference %q", forbidden)
		}
	}
}

func TestResolveMigrationCredentialRejectsRelativeFileAndClearsPlan(t *testing.T) {
	plan := buildSingleImportablePlan(t, migrationFixturePaths(t), Default())
	secret := plan.state.secrets["fixture"]
	err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderFile, Reference: "relative.password"})
	if err == nil {
		t.Fatal("ResolveMigrationCredential() accepted a relative file")
	}
	if !plan.state.closed {
		t.Fatal("failed resolution did not close the plan")
	}
	assertZeroedBytes(t, secret)
}

func TestResolveMigrationCredentialRejectsManualKeyringAndReservedReference(t *testing.T) {
	t.Run("manual source", func(t *testing.T) {
		paths := migrationFixturePaths(t)
		writeMigrationFixture(t, paths.LegacyMetaYAML, `default_account: fixture
accounts: [{username: fixture, password: "not-base64"}]
`)
		plan, err := BuildMigrationPlan(paths, Default())
		if err != nil {
			t.Fatalf("BuildMigrationPlan() error = %v", err)
		}
		if err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderKeyring}); err == nil {
			t.Fatal("manual pending credential was accepted for keyring import")
		}
		if !plan.state.closed {
			t.Fatal("failed manual keyring decision did not close plan")
		}
	})

	t.Run("reserved destination reference", func(t *testing.T) {
		destination := Default()
		destination.Profiles["existing"] = Profile{
			Username:   "existing",
			Credential: CredentialRef{Provider: ProviderKeyring, Reference: "reserved-fixture-reference"},
		}
		plan := buildSingleImportablePlan(t, migrationFixturePaths(t), destination)
		secret := plan.state.secrets["fixture"]
		if err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderKeyring, Reference: "reserved-fixture-reference"}); err == nil {
			t.Fatal("caller-selected keyring reference was accepted")
		}
		assertZeroedBytes(t, secret)
	})
}

func TestMigrationPlanCloseAndConflictSkipClearSecrets(t *testing.T) {
	t.Run("close", func(t *testing.T) {
		plan := buildSingleImportablePlan(t, migrationFixturePaths(t), Default())
		secret := plan.state.secrets["fixture"]
		plan.Close()
		assertZeroedBytes(t, secret)
		if !plan.state.closed || plan.state.secrets != nil {
			t.Fatal("Close() did not irreversibly close the plan")
		}
	})

	t.Run("skip conflict", func(t *testing.T) {
		destination := Default()
		destination.Profiles["fixture"] = Profile{
			Username:   "existing",
			Credential: CredentialRef{Provider: ProviderPrompt},
		}
		plan := buildSingleImportablePlan(t, migrationFixturePaths(t), destination)
		secret := plan.state.secrets["fixture"]
		ResolveMigrationConflict(&plan, "fixture", false)
		assertZeroedBytes(t, secret)
		if len(plan.Profiles) != 0 || len(plan.Conflicts) != 0 {
			t.Fatalf("skip left profiles=%d conflicts=%d", len(plan.Profiles), len(plan.Conflicts))
		}
		plan.Close()
	})
}

func TestApplyMigrationRejectsUnresolvedWithoutSideEffects(t *testing.T) {
	paths := migrationFixturePaths(t)
	plan := buildSingleImportablePlan(t, paths, Default())
	secret := plan.state.secrets["fixture"]
	store := &Store{Path: paths.ConfigFile}
	_, err := ApplyMigration(paths, store, Default(), plan)
	if err == nil {
		t.Fatal("ApplyMigration() unexpectedly succeeded")
	}
	assertZeroedBytes(t, secret)
	assertMigrationApplySideEffectFree(t, paths)
}

func TestApplyMigrationDeepCopiesDestinationOnFailure(t *testing.T) {
	paths := migrationFixturePaths(t)
	plan := buildSingleImportablePlan(t, paths, Default())
	if err := ResolveMigrationCredential(&plan, "fixture", CredentialRef{Provider: ProviderEnv, Reference: "IPGW_FIXTURE_PASSWORD"}); err != nil {
		t.Fatalf("resolve env: %v", err)
	}
	// Keep this test focused on destination isolation rather than backup I/O.
	plan.Sources = nil
	plan.state.sourcePaths = map[string]string{}

	destination := Default()
	destination.DefaultProfile = "existing"
	destination.Profiles["existing"] = Profile{
		Username:   "existing",
		Credential: CredentialRef{Provider: ProviderPrompt},
	}
	want := cloneMigrationDestination(destination)
	if err := os.MkdirAll(paths.ConfigFile, 0o700); err != nil {
		t.Fatalf("create blocking config directory: %v", err)
	}
	_, err := ApplyMigration(paths, &Store{Path: paths.ConfigFile}, destination, plan)
	if err == nil {
		t.Fatal("ApplyMigration() succeeded with a directory config target")
	}
	if !reflect.DeepEqual(destination, want) {
		t.Fatalf("failed apply mutated caller destination\ngot:  %#v\nwant: %#v", destination, want)
	}
	if _, exists := destination.Profiles["fixture"]; exists {
		t.Fatal("failed apply inserted migrated profile into caller map")
	}
}

func TestApplyMigrationWritesOnlyCredentialReferences(t *testing.T) {
	paths := migrationFixturePaths(t)
	encoded := base64.StdEncoding.EncodeToString([]byte(migrationSecretCanary))
	writeMigrationFixture(t, paths.LegacyMetaYAML, fmt.Sprintf(`default_account: env-profile
accounts:
  - {username: env-profile, password: %s}
  - {username: file-profile, password: %s}
`, encoded, encoded))
	plan, err := BuildMigrationPlan(paths, Default())
	if err != nil {
		t.Fatalf("BuildMigrationPlan() error = %v", err)
	}
	if err := ResolveMigrationCredential(&plan, "env-profile", CredentialRef{Provider: ProviderEnv, Reference: "IPGW_FIXTURE_PASSWORD"}); err != nil {
		t.Fatalf("resolve env: %v", err)
	}
	fileReference := filepath.Join(t.TempDir(), "fixture.password")
	if err := ResolveMigrationCredential(&plan, "file-profile", CredentialRef{Provider: ProviderFile, Reference: fileReference}); err != nil {
		t.Fatalf("resolve file: %v", err)
	}
	store := &Store{Path: paths.ConfigFile}
	if _, err := ApplyMigration(paths, store, Default(), plan); err != nil {
		t.Fatalf("ApplyMigration() error = %v", err)
	}
	cfg, found, err := store.Load()
	if err != nil || !found {
		t.Fatalf("load migrated config: found=%t err=%v", found, err)
	}
	if cfg.Profiles["env-profile"].Credential != (CredentialRef{Provider: ProviderEnv, Reference: "IPGW_FIXTURE_PASSWORD"}) {
		t.Fatalf("env credential = %#v", cfg.Profiles["env-profile"].Credential)
	}
	if cfg.Profiles["file-profile"].Credential != (CredentialRef{Provider: ProviderFile, Reference: filepath.Clean(fileReference)}) {
		t.Fatalf("file credential = %#v", cfg.Profiles["file-profile"].Credential)
	}
	if _, err := os.Stat(fileReference); !os.IsNotExist(err) {
		t.Fatalf("migration created or read credential file: %v", err)
	}
	configData, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read migrated config: %v", err)
	}
	for _, forbidden := range []string{migrationSecretCanary, encoded} {
		if bytes.Contains(configData, []byte(forbidden)) {
			t.Fatalf("migrated config contains forbidden secret material %q", forbidden)
		}
	}
}

func buildSingleImportablePlan(t *testing.T, paths Paths, destination Config) MigrationPlan {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(migrationSecretCanary))
	writeMigrationFixture(t, paths.LegacyMetaYAML, fmt.Sprintf(`default_account: fixture
accounts: [{username: fixture, password: %s}]
`, encoded))
	plan, err := BuildMigrationPlan(paths, destination)
	if err != nil {
		t.Fatalf("BuildMigrationPlan() error = %v", err)
	}
	return plan
}

func migrationFixturePaths(t *testing.T) Paths {
	t.Helper()
	root := t.TempDir()
	base := filepath.Join(root, "new-config")
	return Paths{
		BaseDir:         base,
		ConfigFile:      filepath.Join(base, "config.yaml"),
		MigrationMarker: filepath.Join(base, "migration-v1.yaml"),
		LegacyMetaYAML:  filepath.Join(root, "legacy-meta.yaml"),
		LegacyUpstream:  filepath.Join(root, "legacy-neucn.json"),
	}
}

func writeMigrationFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write synthetic migration fixture: %v", err)
	}
}

func migrationProfileByName(t *testing.T, plan *MigrationPlan, name string) *MigratedProfile {
	t.Helper()
	for index := range plan.Profiles {
		if plan.Profiles[index].Name == name {
			return &plan.Profiles[index]
		}
	}
	t.Fatalf("migration profile %q not found", name)
	return nil
}

func assertZeroedBytes(t *testing.T, value []byte) {
	t.Helper()
	for index, item := range value {
		if item != 0 {
			t.Fatalf("secret byte %d was not cleared", index)
		}
	}
}

func assertMigrationPlanningSideEffectFree(t *testing.T, paths Paths) {
	t.Helper()
	if _, err := os.Stat(paths.BaseDir); !os.IsNotExist(err) {
		t.Fatalf("failed planning created target state: %v", err)
	}
	assertNoMigrationBackups(t, paths.LegacyMetaYAML)
	assertNoMigrationBackups(t, paths.LegacyUpstream)
}

func assertMigrationApplySideEffectFree(t *testing.T, paths Paths) {
	t.Helper()
	paths = normalizeMigrationPaths(paths)
	for _, path := range []string{
		paths.ConfigFile,
		paths.MigrationMarker,
		paths.MigrationJournal,
		paths.MigrationConfigRecovery,
		paths.MigrationMarkerRecovery,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("failed apply created %s: %v", filepath.Base(path), err)
		}
	}
	backups, err := filepath.Glob(filepath.Join(paths.BaseDir, "migration-backups", "*"))
	if err != nil {
		t.Fatalf("glob private migration backups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("failed apply left private migration backups: %v", backups)
	}
	assertNoMigrationBackups(t, paths.LegacyMetaYAML)
	assertNoMigrationBackups(t, paths.LegacyUpstream)
}

func assertNoMigrationBackups(t *testing.T, source string) {
	t.Helper()
	matches, err := filepath.Glob(source + ".bak.*")
	if err != nil {
		t.Fatalf("glob migration backups: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("unexpected migration backups: %v", matches)
	}
}
