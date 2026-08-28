package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/UnbalancedCat/ipgw-meta/internal/config"
	keyring "github.com/zalando/go-keyring"
	"go.yaml.in/yaml/v3"
)

const migrationSecretCanary = "MIGRATION-CANARY-MUST-NOT-LEAK"

func TestMigrationArgumentParsingRejectsDuplicateAndInvalidMappings(t *testing.T) {
	tests := [][]string{
		{"--credential", "fixture=env:ONE", "--credential=fixture=env:TWO"},
		{"--credential"},
		{"--credential=fixture"},
		{"--credential=fixture=env:bad-name"},
		{"--credential=fixture=file:relative.secret"},
		{"--yes", "--yes"},
	}
	for _, args := range tests {
		if _, err := parseMigrationArguments(args); err == nil {
			t.Errorf("parseMigrationArguments(%q) succeeded", args)
		}
	}
	parsed, err := parseMigrationArguments([]string{
		"--yes", "--credential", "first=env:IPGW_FIRST", "--credential=second=prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.yes || len(parsed.credentials) != 2 {
		t.Fatalf("parsed = %#v", parsed)
	}
}

func TestNonInteractiveMigrationMissingMappingHasNoSideEffects(t *testing.T) {
	paths, encoded := newMigrationFixture(t, migrationSecretCanary)
	exit, stdout, stderr := executeMigration(t, paths, []string{"--json", "profile", "migrate", "--yes"}, false, nil, nil)
	if exit != 2 {
		t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", exit, stdout, stderr)
	}
	object := decodeSingleEnvelope(t, []byte(stdout))
	if got := object["command"]; got != "profile.migrate" {
		t.Fatalf("command = %#v", got)
	}
	errorObject := objectField(t, object, "error")
	if got := errorObject["code"]; got != "config" {
		t.Fatalf("code = %#v, want config", got)
	}
	report := objectField(t, objectField(t, errorObject, "details"), "migration")
	if got := report["source_count"]; got != float64(1) {
		t.Fatalf("migration source_count = %#v, want 1", got)
	}
	profiles, ok := report["profiles"].([]any)
	if !ok || len(profiles) != 1 {
		t.Fatalf("migration profiles = %#v, want one safe profile", report["profiles"])
	}
	profile, ok := profiles[0].(map[string]any)
	if !ok || profile["name"] != "fixture" || profile["credential_status"] != "pending_importable" {
		t.Fatalf("migration profile = %#v", profiles[0])
	}
	if stderr != "" {
		t.Fatalf("JSON stderr = %q, want empty", stderr)
	}
	assertMigrationOutputDoesNotLeak(t, stdout+stderr, migrationSecretCanary, encoded)
	assertNoMigrationArtifacts(t, paths)
}

func TestNonInteractiveMigrationEnvAndFileMappings(t *testing.T) {
	tests := []struct {
		name      string
		argument  func(config.Paths) string
		provider  config.CredentialProvider
		reference func(config.Paths) string
	}{
		{
			name: "env", argument: func(config.Paths) string { return "fixture=env:IPGW_FIXTURE_PASSWORD" },
			provider: config.ProviderEnv, reference: func(config.Paths) string { return "IPGW_FIXTURE_PASSWORD" },
		},
		{
			name: "file", argument: func(paths config.Paths) string {
				return "fixture=file:" + filepath.Join(paths.BaseDir, "credentials", "fixture.secret")
			},
			provider: config.ProviderFile, reference: func(paths config.Paths) string {
				return filepath.Join(paths.BaseDir, "credentials", "fixture.secret")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			paths, encoded := newMigrationFixture(t, migrationSecretCanary+"-"+test.name)
			exit, stdout, stderr := executeMigration(t, paths, []string{
				"profile", "migrate", "--yes", "--credential", test.argument(paths),
			}, false, nil, nil)
			if exit != 0 {
				t.Fatalf("exit = %d; stdout=%q stderr=%q", exit, stdout, stderr)
			}
			assertMigrationOutputDoesNotLeak(t, stdout+stderr, migrationSecretCanary, encoded)
			cfg, found, err := (&config.Store{Path: paths.ConfigFile}).Load()
			if err != nil || !found {
				t.Fatalf("load migrated config: found=%v err=%v", found, err)
			}
			credential := cfg.Profiles["fixture"].Credential
			if credential.Provider != test.provider || credential.Reference != filepath.Clean(test.reference(paths)) {
				t.Fatalf("credential = %#v", credential)
			}
			if test.provider == config.ProviderFile {
				if _, err := os.Stat(test.reference(paths)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("migration created credential file: %v", err)
				}
			}
			assertSecretFreeMigrationArtifacts(t, paths, migrationSecretCanary, encoded)
		})
	}
}

func TestTTYMigrationImportsKeyringThroughInjectedBackend(t *testing.T) {
	paths, encoded := newMigrationFixture(t, migrationSecretCanary)
	backend := newCLIMigrationKeyring()
	providerOptions := &config.ProviderOptions{Keyring: backend}
	input, err := os.CreateTemp(t.TempDir(), "migration-input-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	exit, stdout, stderr := executeMigration(t, paths, []string{
		"profile", "migrate", "--yes", "--credential=fixture=keyring",
	}, true, input, providerOptions)
	if exit != 0 {
		t.Fatalf("exit = %d; stdout=%q stderr=%q", exit, stdout, stderr)
	}
	if backend.sets != 1 || len(backend.values) != 1 {
		t.Fatalf("keyring sets=%d values=%d", backend.sets, len(backend.values))
	}
	for _, value := range backend.values {
		if value != migrationSecretCanary {
			t.Fatal("keyring received the wrong synthetic credential")
		}
	}
	assertMigrationOutputDoesNotLeak(t, stdout+stderr, migrationSecretCanary, encoded)
	assertSecretFreeMigrationArtifacts(t, paths, migrationSecretCanary, encoded)
}

func TestCompletedMigrationRejectsChangedConfigDigest(t *testing.T) {
	paths, _ := newMigrationFixture(t, migrationSecretCanary)
	exit, stdout, stderr := executeMigration(t, paths, []string{
		"profile", "migrate", "--yes", "--credential=fixture=env:IPGW_FIXTURE_PASSWORD",
	}, false, nil, nil)
	if exit != 0 {
		t.Fatalf("initial migration exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
	store := &config.Store{Path: paths.ConfigFile}
	cfg, _, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	profile := cfg.Profiles["fixture"]
	profile.Switch = config.SwitchLogoutCurrent
	cfg.Profiles["fixture"] = profile
	if err := store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	exit, stdout, stderr = executeMigration(t, paths, []string{"profile", "migrate"}, false, nil, nil)
	if exit != 2 {
		t.Fatalf("changed config exit=%d, want 2; stdout=%q stderr=%q", exit, stdout, stderr)
	}
}

func TestMigrationRecoversMarkerVerifiedJournalBeforeAlreadyApplied(t *testing.T) {
	paths, _ := newMigrationFixture(t, migrationSecretCanary)
	exit, stdout, stderr := executeMigration(t, paths, []string{
		"profile", "migrate", "--yes", "--credential=fixture=env:IPGW_FIXTURE_PASSWORD",
	}, false, nil, nil)
	if exit != 0 {
		t.Fatalf("initial migration exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
	writeMarkerVerifiedJournal(t, paths)
	if _, err := os.Stat(paths.MigrationJournal); err != nil {
		t.Fatalf("pending journal was not created: %v", err)
	}
	exit, stdout, stderr = executeMigration(t, paths, []string{"profile", "migrate"}, false, nil, nil)
	if exit != 0 {
		t.Fatalf("verified rerun exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "already applied and verified") {
		t.Fatalf("stdout = %q", stdout)
	}
	if _, err := os.Stat(paths.MigrationJournal); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("verified journal was not cleaned: %v", err)
	}
}

func executeMigration(t *testing.T, paths config.Paths, args []string, isTTY bool, input *os.File, providerOptions *config.ProviderOptions) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	gateway := &recordingGateway{}
	exit := Execute(context.Background(), Options{
		Mode: ModeMeta, Args: args, Paths: paths,
		NewGateway:      func(config.Paths) Gateway { return gateway },
		Input:           input,
		Out:             &stdout,
		Err:             &stderr,
		IsTTY:           isTTY,
		Version:         "test-v1",
		ProviderOptions: providerOptions,
	})
	if gateway.totalCalls() != 0 {
		t.Fatalf("migration called gateway %d time(s)", gateway.totalCalls())
	}
	return exit, stdout.String(), stderr.String()
}

func newMigrationFixture(t *testing.T, secret string) (config.Paths, string) {
	t.Helper()
	base := t.TempDir()
	paths := config.Paths{
		BaseDir:                 base,
		ConfigFile:              filepath.Join(base, "config.yaml"),
		ProtocolCacheDir:        filepath.Join(base, "protocol-cache"),
		MigrationMarker:         filepath.Join(base, "migration-v1.yaml"),
		MigrationJournal:        filepath.Join(base, "migration-v1.pending.yaml"),
		MigrationConfigRecovery: filepath.Join(base, "migration-v1.config.before"),
		MigrationMarkerRecovery: filepath.Join(base, "migration-v1.marker.before"),
		LegacyMetaYAML:          filepath.Join(base, "legacy-meta.yaml"),
		LegacyUpstream:          filepath.Join(base, "legacy-upstream.json"),
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	source := fmt.Sprintf("default_account: fixture\naccounts:\n  - username: fixture\n    password: %s\n", encoded)
	if err := config.WritePrivateFile(paths.LegacyMetaYAML, []byte(source)); err != nil {
		t.Fatal(err)
	}
	return paths, encoded
}

func assertNoMigrationArtifacts(t *testing.T, paths config.Paths) {
	t.Helper()
	for _, path := range []string{
		paths.ConfigFile, paths.MigrationMarker, paths.MigrationJournal,
		paths.MigrationConfigRecovery, paths.MigrationMarkerRecovery,
		filepath.Join(paths.BaseDir, "migration-backups"),
	} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("unexpected migration artifact %s: %v", filepath.Base(path), err)
		}
	}
}

func assertMigrationOutputDoesNotLeak(t *testing.T, output string, values ...string) {
	t.Helper()
	for _, value := range values {
		if value != "" && strings.Contains(output, value) {
			t.Fatalf("migration output leaked a synthetic credential marker")
		}
	}
}

func assertSecretFreeMigrationArtifacts(t *testing.T, paths config.Paths, values ...string) {
	t.Helper()
	for _, path := range []string{paths.ConfigFile, paths.MigrationMarker, paths.MigrationJournal} {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		assertMigrationOutputDoesNotLeak(t, string(data), values...)
	}
}

type cliMigrationKeyring struct {
	values map[string]string
	sets   int
}

func newCLIMigrationKeyring() *cliMigrationKeyring {
	return &cliMigrationKeyring{values: make(map[string]string)}
}

func (f *cliMigrationKeyring) Get(_ string, user string) (string, error) {
	value, exists := f.values[user]
	if !exists {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (f *cliMigrationKeyring) Set(_ string, user, password string) error {
	f.sets++
	f.values[user] = password
	return nil
}

func (f *cliMigrationKeyring) Delete(_ string, user string) error {
	delete(f.values, user)
	return nil
}

type cliMigrationSourceStamp struct {
	Kind           string `yaml:"kind"`
	LocationHash   string `yaml:"location_hash"`
	RedactedDigest string `yaml:"redacted_digest"`
}

type cliMigrationBackup struct {
	SourceLocationHash string `yaml:"source_location_hash"`
	Path               string `yaml:"path"`
}

type cliMigrationMarker struct {
	TransactionID string                    `yaml:"transaction_id"`
	TargetSchema  int                       `yaml:"target_schema"`
	ToolVersion   string                    `yaml:"tool_version"`
	Sources       []cliMigrationSourceStamp `yaml:"sources"`
	Backups       []cliMigrationBackup      `yaml:"backups"`
}

type cliMigrationSnapshot struct {
	Existed bool `yaml:"existed"`
}

type cliMigrationArtifact struct {
	TargetPath  string               `yaml:"target_path"`
	Before      cliMigrationSnapshot `yaml:"before"`
	AfterDigest string               `yaml:"after_digest"`
}

type cliMigrationJournal struct {
	SchemaVersion int                       `yaml:"schema_version"`
	TransactionID string                    `yaml:"transaction_id"`
	Phase         string                    `yaml:"phase"`
	TargetSchema  int                       `yaml:"target_schema"`
	ToolVersion   string                    `yaml:"tool_version"`
	Sources       []cliMigrationSourceStamp `yaml:"sources"`
	Config        cliMigrationArtifact      `yaml:"config"`
	Marker        cliMigrationArtifact      `yaml:"marker"`
	Backups       []cliMigrationBackup      `yaml:"backups"`
}

func writeMarkerVerifiedJournal(t *testing.T, paths config.Paths) {
	t.Helper()
	markerData, err := os.ReadFile(paths.MigrationMarker)
	if err != nil {
		t.Fatal(err)
	}
	configData, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	var marker cliMigrationMarker
	if err := yaml.Unmarshal(markerData, &marker); err != nil {
		t.Fatal(err)
	}
	journal := cliMigrationJournal{
		SchemaVersion: 1, TransactionID: marker.TransactionID, Phase: "marker_verified",
		TargetSchema: marker.TargetSchema, ToolVersion: marker.ToolVersion,
		Sources: marker.Sources, Backups: marker.Backups,
		Config: cliMigrationArtifact{
			TargetPath: paths.ConfigFile, Before: cliMigrationSnapshot{}, AfterDigest: cliMigrationDigest(configData),
		},
		Marker: cliMigrationArtifact{
			TargetPath: paths.MigrationMarker, Before: cliMigrationSnapshot{}, AfterDigest: cliMigrationDigest(markerData),
		},
	}
	data, err := yaml.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := config.WritePrivateFile(paths.MigrationJournal, data); err != nil {
		t.Fatal(err)
	}
}

func cliMigrationDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
