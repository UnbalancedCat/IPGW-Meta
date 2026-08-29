package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	keyring "github.com/zalando/go-keyring"
	"go.yaml.in/yaml/v3"
)

type MigrationSourceKind string

const (
	MigrationSourceMetaYAML  MigrationSourceKind = "meta_yaml"
	MigrationSourceNEUCNJSON MigrationSourceKind = "neucn_json"
)

type MigrationCredentialStatus string

const (
	MigrationCredentialPendingImportable MigrationCredentialStatus = "pending_importable"
	MigrationCredentialPendingManual     MigrationCredentialStatus = "pending_manual"
	MigrationCredentialResolved          MigrationCredentialStatus = "resolved"
	MigrationCredentialKeyringImport     MigrationCredentialStatus = "resolved_keyring_import"
)

type MigrationSource struct {
	Kind         MigrationSourceKind `json:"kind"`
	LocationHash string              `json:"location_hash"`
	rawDigest    [sha256.Size]byte
	redactedHash string
}

type MigrationPlan struct {
	Sources   []MigrationSource   `json:"sources"`
	Profiles  []MigratedProfile   `json:"profiles"`
	Conflicts []MigrationConflict `json:"conflicts"`
	Warnings  []string            `json:"warnings"`
	state     *migrationPlanState
}

type migrationPlanState struct {
	secrets             map[string][]byte
	sourcePaths         map[string]string
	sourceRawDigests    map[string][sha256.Size]byte
	sourceStamps        map[string]migrationJournalSourceStamp
	reservedKeyringRefs map[string]string
	destinationDigest   string
	closed              bool
}

type MigratedProfile struct {
	Name             string                    `json:"name"`
	Username         string                    `json:"username"`
	Credential       CredentialRef             `json:"-"`
	CredentialStatus MigrationCredentialStatus `json:"credential_status"`
	Default          bool                      `json:"default"`
	Source           MigrationSourceKind       `json:"source"`
}

type MigrationConflict struct {
	Profile string `json:"profile"`
	Reason  string `json:"reason"`
}

type MigrationResult struct {
	AppliedProfiles []string          `json:"applied_profiles"`
	Backups         map[string]string `json:"backups"`
	Marker          string            `json:"marker"`
}

type migrationMarker struct {
	SchemaVersion int                            `yaml:"schema_version"`
	TransactionID string                         `yaml:"transaction_id"`
	TargetSchema  int                            `yaml:"target_schema"`
	ToolVersion   string                         `yaml:"tool_version"`
	CompletedAt   time.Time                      `yaml:"completed_at"`
	Sources       []migrationJournalSourceStamp  `yaml:"sources"`
	ConfigDigest  string                         `yaml:"config_digest"`
	Profiles      []string                       `yaml:"profiles"`
	Backups       []migrationJournalBackupRecord `yaml:"backups"`
}

type MigrationApplyOptions struct {
	ToolVersion     string
	ProviderOptions ProviderOptions
}

// migrationApplyTestHooks provides per-transaction failure injection without
// package-global state, so race tests can exercise every persisted journal
// phase deterministically. It is deliberately inaccessible outside config.
type migrationApplyTestHooks struct {
	failAfterJournalPhase func(migrationJournalPhase) bool
}

func (hooks *migrationApplyTestHooks) shouldFailAfterJournalPhase(phase migrationJournalPhase) bool {
	return hooks != nil && hooks.failAfterJournalPhase != nil && hooks.failAfterJournalPhase(phase)
}

type migrationCandidate struct {
	profile MigratedProfile
	secret  []byte
}

type legacyMetaSource struct {
	App            legacyMetaApp       `yaml:"app,omitempty"`
	DefaultAccount string              `yaml:"default_account"`
	Accounts       []legacyMetaAccount `yaml:"accounts"`
}

type legacyMetaApp struct {
	LogStyle string `yaml:"log_style,omitempty"`
	LogLevel string `yaml:"log_level,omitempty"`
}

type legacyMetaAccount struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type legacyNEUCNSource struct {
	DefaultAccount string               `json:"default_account"`
	Accounts       []legacyNEUCNAccount `json:"accounts"`
}

type legacyNEUCNAccount struct {
	Username          string `json:"username"`
	EncryptedPassword string `json:"encrypted_password"`
}

func newMigrationPlan() MigrationPlan {
	return MigrationPlan{state: &migrationPlanState{
		secrets:             make(map[string][]byte),
		sourcePaths:         make(map[string]string),
		sourceRawDigests:    make(map[string][sha256.Size]byte),
		sourceStamps:        make(map[string]migrationJournalSourceStamp),
		reservedKeyringRefs: make(map[string]string),
	}}
}

// Close irreversibly clears every decoded legacy secret held by the plan.
// A closed plan cannot be resolved or applied and must be rebuilt from source.
func (p *MigrationPlan) Close() {
	if p == nil || p.state == nil || p.state.closed {
		return
	}
	clearMigrationSecrets(p.state.secrets)
	for location := range p.state.sourceRawDigests {
		p.state.sourceRawDigests[location] = [sha256.Size]byte{}
		delete(p.state.sourceRawDigests, location)
	}
	p.state.secrets = nil
	p.state.sourcePaths = nil
	p.state.sourceRawDigests = nil
	p.state.sourceStamps = nil
	p.state.destinationDigest = ""
	p.state.closed = true
}

func MigrationAlreadyApplied(markerPath string) (MigrationResult, bool, error) {
	marker, exists, err := loadMigrationMarker(markerPath)
	if err != nil || !exists {
		return MigrationResult{}, exists, err
	}
	return migrationResultFromMarker(marker, markerPath), true, nil
}

func BuildMigrationPlan(paths Paths, destination Config) (plan MigrationPlan, resultErr error) {
	plan = newMigrationPlan()
	defer func() {
		if resultErr != nil {
			plan.Close()
		}
	}()
	destinationData, err := encodeConfig(destination)
	if err != nil {
		return plan, fmt.Errorf("validate migration destination: %w", err)
	}
	plan.state.destinationDigest = migrationArtifactDigest(destinationData)

	for name, profile := range destination.Profiles {
		if profile.Credential.Provider == ProviderKeyring && profile.Credential.Reference != "" {
			plan.state.reservedKeyringRefs[profile.Credential.Reference] = "destination profile " + name
		}
	}

	var candidates []migrationCandidate
	metaSource, metaCandidates, found, err := parseLegacyMetaSource(paths.LegacyMetaYAML)
	if err != nil {
		return plan, err
	}
	if found {
		if err := plan.addSource(metaSource, paths.LegacyMetaYAML); err != nil {
			clearMigrationCandidates(metaCandidates)
			return plan, err
		}
		candidates = append(candidates, metaCandidates...)
	}

	upstreamPath, upstreamExists, err := resolveLegacyNEUCNPath(paths.LegacyUpstream)
	if err != nil {
		clearMigrationCandidates(candidates)
		return plan, err
	}
	if upstreamExists {
		upstreamSource, upstreamCandidates, _, parseErr := parseLegacyNEUCNSource(upstreamPath)
		if parseErr != nil {
			clearMigrationCandidates(candidates)
			return plan, parseErr
		}
		if err := plan.addSource(upstreamSource, upstreamPath); err != nil {
			clearMigrationCandidates(candidates)
			clearMigrationCandidates(upstreamCandidates)
			return plan, err
		}
		candidates = append(candidates, upstreamCandidates...)
	}

	seen := make(map[string]MigrationSourceKind)
	for index := range candidates {
		candidate := &candidates[index]
		profile := candidate.profile
		if previousSource, exists := seen[profile.Name]; exists {
			plan.Conflicts = append(plan.Conflicts, MigrationConflict{
				Profile: profile.Name,
				Reason:  fmt.Sprintf("profile is present in both %s and %s; the first source is selected", previousSource, profile.Source),
			})
			wipeBytes(candidate.secret)
			candidate.secret = nil
			continue
		}
		seen[profile.Name] = profile.Source
		plan.Profiles = append(plan.Profiles, profile)
		if len(candidate.secret) != 0 {
			plan.state.secrets[profile.Name] = candidate.secret
			candidate.secret = nil
		}
		if _, exists := destination.Profiles[profile.Name]; exists {
			plan.Conflicts = append(plan.Conflicts, MigrationConflict{Profile: profile.Name, Reason: "profile already exists in destination"})
		}
		if profile.CredentialStatus == MigrationCredentialPendingManual {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s profile %q requires an explicit credential setup decision", profile.Source, profile.Name))
		}
	}
	clearMigrationCandidates(candidates)

	sort.Slice(plan.Profiles, func(i, j int) bool { return plan.Profiles[i].Name < plan.Profiles[j].Name })
	sort.Slice(plan.Conflicts, func(i, j int) bool {
		if plan.Conflicts[i].Profile == plan.Conflicts[j].Profile {
			return plan.Conflicts[i].Reason < plan.Conflicts[j].Reason
		}
		return plan.Conflicts[i].Profile < plan.Conflicts[j].Profile
	})
	sort.Strings(plan.Warnings)
	return plan, nil
}

func (p *MigrationPlan) addSource(source MigrationSource, path string) error {
	if p == nil || p.state == nil || p.state.closed {
		return fmt.Errorf("migration plan is closed")
	}
	if source.LocationHash == "" {
		return fmt.Errorf("migration source location hash is empty")
	}
	if _, exists := p.state.sourcePaths[source.LocationHash]; exists {
		return fmt.Errorf("migration sources resolve to the same location")
	}
	if source.rawDigest == ([sha256.Size]byte{}) || !validSHA256(source.redactedHash) {
		return fmt.Errorf("migration source preview state is incomplete")
	}
	p.Sources = append(p.Sources, source)
	p.Sources[len(p.Sources)-1].rawDigest = [sha256.Size]byte{}
	p.Sources[len(p.Sources)-1].redactedHash = ""
	p.state.sourcePaths[source.LocationHash] = path
	p.state.sourceRawDigests[source.LocationHash] = source.rawDigest
	p.state.sourceStamps[source.LocationHash] = migrationJournalSourceStamp{
		Kind:           source.Kind,
		LocationHash:   source.LocationHash,
		RedactedDigest: source.redactedHash,
	}
	return nil
}

// ResolveMigrationConflict applies the user's decision to every conflict for
// one profile. replace keeps the first deterministic migration candidate;
// skip removes it and clears any decoded legacy secret immediately.
func ResolveMigrationConflict(plan *MigrationPlan, profileName string, replace bool) {
	if plan == nil || plan.state == nil || plan.state.closed {
		return
	}
	conflicts := plan.Conflicts[:0]
	for _, conflict := range plan.Conflicts {
		if conflict.Profile != profileName {
			conflicts = append(conflicts, conflict)
		}
	}
	plan.Conflicts = conflicts
	if replace {
		return
	}
	profiles := plan.Profiles[:0]
	for _, profile := range plan.Profiles {
		if profile.Name == profileName {
			plan.clearProfileSecret(profileName)
			continue
		}
		profiles = append(profiles, profile)
	}
	plan.Profiles = profiles
}

// ResolveMigrationCredential records a secret-free provider decision. Env and
// file decisions only store references, prompt imports nothing, and keyring is
// accepted only when the plan holds an importable legacy secret. The actual
// keyring existence check and write belong to the later transaction phase.
func ResolveMigrationCredential(plan *MigrationPlan, profileName string, ref CredentialRef) (resultErr error) {
	if plan == nil || plan.state == nil || plan.state.closed {
		return fmt.Errorf("migration plan is closed")
	}
	defer func() {
		if resultErr != nil {
			plan.Close()
		}
	}()

	index := -1
	for i := range plan.Profiles {
		if plan.Profiles[i].Name == profileName {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("migration profile %q does not exist", profileName)
	}
	profile := &plan.Profiles[index]
	if profile.CredentialStatus == MigrationCredentialResolved || profile.CredentialStatus == MigrationCredentialKeyringImport {
		return fmt.Errorf("migration credential for profile %q is already resolved", profileName)
	}
	if ref.Provider == ProviderKeyring {
		if ref.Reference != "" {
			return fmt.Errorf("profile %q keyring migration reference must be generated by the migration transaction", profileName)
		}
		secret := plan.state.secrets[profileName]
		if len(secret) == 0 || profile.CredentialStatus != MigrationCredentialPendingImportable {
			return fmt.Errorf("profile %q has no importable legacy secret for keyring", profileName)
		}
		referenceID, err := newMigrationTransactionID()
		if err != nil {
			return fmt.Errorf("profile %q keyring migration reference could not be generated", profileName)
		}
		ref.Reference = "migration-" + referenceID
	}
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("profile %q credential decision: %w", profileName, err)
	}

	switch ref.Provider {
	case ProviderEnv:
		plan.clearProfileSecret(profileName)
		profile.Credential = ref
		profile.CredentialStatus = MigrationCredentialResolved
	case ProviderFile:
		if !filepath.IsAbs(ref.Reference) {
			return fmt.Errorf("profile %q file credential reference must be an absolute path", profileName)
		}
		ref.Reference = filepath.Clean(ref.Reference)
		plan.clearProfileSecret(profileName)
		profile.Credential = ref
		profile.CredentialStatus = MigrationCredentialResolved
	case ProviderPrompt:
		plan.clearProfileSecret(profileName)
		profile.Credential = ref
		profile.CredentialStatus = MigrationCredentialResolved
	case ProviderKeyring:
		secret := plan.state.secrets[profileName]
		if len(secret) == 0 || profile.CredentialStatus != MigrationCredentialPendingImportable {
			return fmt.Errorf("profile %q has no importable legacy secret for keyring", profileName)
		}
		if owner, exists := plan.state.reservedKeyringRefs[ref.Reference]; exists {
			return fmt.Errorf("keyring reference %q is already reserved by %s", ref.Reference, owner)
		}
		plan.state.reservedKeyringRefs[ref.Reference] = "migration profile " + profileName
		profile.Credential = ref
		profile.CredentialStatus = MigrationCredentialKeyringImport
	default:
		return fmt.Errorf("unsupported migration credential provider %q", ref.Provider)
	}
	return nil
}

func ApplyMigration(paths Paths, store *Store, destination Config, plan MigrationPlan) (MigrationResult, error) {
	return ApplyMigrationWithOptions(paths, store, destination, plan, MigrationApplyOptions{ToolVersion: "compat"})
}

func ApplyMigrationWithOptions(paths Paths, store *Store, destination Config, plan MigrationPlan, options MigrationApplyOptions) (MigrationResult, error) {
	return applyMigrationWithTestHooks(paths, store, destination, plan, options, nil)
}

func applyMigrationWithTestHooks(paths Paths, store *Store, destination Config, plan MigrationPlan, options MigrationApplyOptions, testHooks *migrationApplyTestHooks) (MigrationResult, error) {
	paths = normalizeMigrationPaths(paths)
	if plan.state == nil || plan.state.closed {
		return MigrationResult{}, fmt.Errorf("migration plan is closed")
	}
	defer plan.Close()
	if store == nil || filepath.Clean(store.Path) != filepath.Clean(paths.ConfigFile) {
		return MigrationResult{}, fmt.Errorf("migration config store does not match the target path")
	}
	if !validJournalText(options.ToolVersion, 128) {
		return MigrationResult{}, fmt.Errorf("migration tool version is required")
	}
	if store.MigrationJournal != "" && filepath.Clean(store.MigrationJournal) != filepath.Clean(paths.MigrationJournal) {
		return MigrationResult{}, fmt.Errorf("migration config store does not match the journal path")
	}
	if _, markerExists, markerErr := loadMigrationMarker(paths.MigrationMarker); markerErr != nil {
		return MigrationResult{}, markerErr
	} else if !markerExists {
		// Reject deterministic plan/source failures before creating the fixed
		// mutation lock. Every check is repeated while holding the lock below.
		previewData, previewErr := encodeConfig(destination)
		if previewErr != nil || migrationArtifactDigest(previewData) != plan.state.destinationDigest {
			return MigrationResult{}, fmt.Errorf("migration destination changed after preview; rebuild the migration plan")
		}
		if _, validateErr := validateMigrationPlanForApply(plan); validateErr != nil {
			return MigrationResult{}, validateErr
		}
		sourceData, _, sourceErr := verifyMigrationPlanSources(plan)
		clearMigrationSourceData(sourceData)
		if sourceErr != nil {
			return MigrationResult{}, sourceErr
		}
	}

	var result MigrationResult
	err := store.withMutationLock(func(locked *lockedStore) error {
		var applyErr error
		result, applyErr = applyMigrationLocked(paths, locked, destination, plan, options, testHooks)
		return applyErr
	})
	if err != nil {
		return MigrationResult{}, err
	}
	return result, nil
}

// applyMigrationLocked owns the complete recovery, generation check and
// transaction sequence. The caller must hold Store.withMutationLock and must
// not call a lock-taking Store method from this function.
func applyMigrationLocked(paths Paths, locked *lockedStore, destination Config, plan MigrationPlan, options MigrationApplyOptions, testHooks *migrationApplyTestHooks) (MigrationResult, error) {
	if err := recoverPendingMigrationIfPresent(paths, options); err != nil {
		return MigrationResult{}, err
	}

	if marker, exists, err := loadMigrationMarker(paths.MigrationMarker); err != nil {
		return MigrationResult{}, err
	} else if exists {
		if err := verifyMigrationArtifact(paths.ConfigFile, marker.ConfigDigest); err != nil {
			return MigrationResult{}, fmt.Errorf("migration marker does not match the target config")
		}
		if !migrationMarkerMatchesPlan(marker, plan) {
			return MigrationResult{}, fmt.Errorf("migration sources changed after the completed transaction")
		}
		return migrationResultFromMarker(marker, paths.MigrationMarker), nil
	}

	previewData, err := encodeConfig(destination)
	if err != nil || migrationArtifactDigest(previewData) != plan.state.destinationDigest {
		return MigrationResult{}, fmt.Errorf("migration destination changed after preview; rebuild the migration plan")
	}
	current, _, err := locked.load()
	if err != nil {
		return MigrationResult{}, fmt.Errorf("reload migration destination: %w", err)
	}
	currentData, err := encodeConfig(current)
	if err != nil || migrationArtifactDigest(currentData) != plan.state.destinationDigest {
		return MigrationResult{}, fmt.Errorf("migration destination changed after preview; rebuild the migration plan")
	}

	keyringProfiles, err := validateMigrationPlanForApply(plan)
	if err != nil {
		return MigrationResult{}, err
	}

	candidate := cloneMigrationDestination(current)
	for _, migrated := range plan.Profiles {
		candidate.Profiles[migrated.Name] = Profile{
			Username: migrated.Username, Credential: migrated.Credential, Switch: SwitchRefuse,
		}
		if migrated.Default && candidate.DefaultProfile == "" {
			candidate.DefaultProfile = migrated.Name
		}
	}
	if candidate.DefaultProfile == "" && len(plan.Profiles) > 0 {
		candidate.DefaultProfile = plan.Profiles[0].Name
	}
	if err := candidate.Validate(); err != nil {
		return MigrationResult{}, fmt.Errorf("validate migrated config: %w", err)
	}
	keyringBackend := options.ProviderOptions.Keyring
	if keyringBackend == nil {
		keyringBackend = systemKeyring{}
	}
	for _, profile := range keyringProfiles {
		_, keyringErr := keyringBackend.Get(KeyringService, profile.Credential.Reference)
		switch {
		case keyringErr == nil:
			return MigrationResult{}, fmt.Errorf("profile %q generated keyring reference is already occupied", profile.Name)
		case errors.Is(keyringErr, keyring.ErrNotFound):
			// The migration only writes fresh, random references.
		default:
			return MigrationResult{}, fmt.Errorf("profile %q keyring is unavailable for migration", profile.Name)
		}
	}
	candidateData, err := yaml.Marshal(candidate)
	if err != nil || len(candidateData) > maxConfigBytes {
		return MigrationResult{}, fmt.Errorf("prepare migrated config")
	}

	transactionID, err := newMigrationTransactionID()
	if err != nil {
		return MigrationResult{}, err
	}
	sourceData, stamps, err := verifyMigrationPlanSources(plan)
	if err != nil {
		return MigrationResult{}, err
	}
	defer clearMigrationSourceData(sourceData)

	result := MigrationResult{Backups: make(map[string]string), Marker: paths.MigrationMarker}
	backups := make([]migrationJournalBackupRecord, 0, len(plan.Sources))
	for index, source := range plan.Sources {
		backup := filepath.Join(paths.BaseDir, "migration-backups", fmt.Sprintf("%s-%s-%02d.backup", source.Kind, transactionID, index+1))
		backups = append(backups, migrationJournalBackupRecord{SourceLocationHash: source.LocationHash, Path: backup})
		result.Backups[source.LocationHash] = backup
	}
	for _, migrated := range plan.Profiles {
		result.AppliedProfiles = append(result.AppliedProfiles, migrated.Name)
	}

	marker := migrationMarker{
		SchemaVersion: migrationJournalSchemaVersion,
		TransactionID: transactionID,
		TargetSchema:  SchemaVersion,
		ToolVersion:   options.ToolVersion,
		CompletedAt:   time.Now().UTC(),
		Sources:       stamps,
		ConfigDigest:  migrationArtifactDigest(candidateData),
		Profiles:      append([]string(nil), result.AppliedProfiles...),
		Backups:       backups,
	}
	markerData, err := yaml.Marshal(marker)
	if err != nil || len(markerData) > maxConfigBytes {
		return MigrationResult{}, fmt.Errorf("prepare migration marker")
	}

	configBefore, configBeforeData, err := prepareMigrationSnapshot(paths.ConfigFile, paths.MigrationConfigRecovery)
	if err != nil {
		return MigrationResult{}, err
	}
	markerBefore, markerBeforeData, err := prepareMigrationSnapshot(paths.MigrationMarker, paths.MigrationMarkerRecovery)
	if err != nil {
		return MigrationResult{}, err
	}
	journal := migrationJournal{
		SchemaVersion: migrationJournalSchemaVersion,
		TransactionID: transactionID,
		Phase:         migrationPhasePrepared,
		TargetSchema:  SchemaVersion,
		ToolVersion:   options.ToolVersion,
		Sources:       stamps,
		Config: migrationJournalArtifact{
			TargetPath: paths.ConfigFile, Before: configBefore, AfterDigest: marker.ConfigDigest,
		},
		Marker: migrationJournalArtifact{
			TargetPath: paths.MigrationMarker, Before: markerBefore, AfterDigest: migrationArtifactDigest(markerData),
		},
		Backups: backups,
	}
	for _, profile := range keyringProfiles {
		journal.NewKeyringRefs = append(journal.NewKeyringRefs, profile.Credential.Reference)
	}
	if err := beginMigrationJournal(paths.MigrationJournal, journal); err != nil {
		return MigrationResult{}, err
	}
	fail := func(message string) (MigrationResult, error) {
		committed, recoverErr := recoverPendingMigration(paths, options)
		if recoverErr != nil {
			return MigrationResult{}, fmt.Errorf("migration failed and automatic rollback is incomplete; recovery required")
		}
		if committed {
			return result, nil
		}
		return MigrationResult{}, errors.New(message)
	}
	if testHooks.shouldFailAfterJournalPhase(migrationPhasePrepared) {
		return fail("migration failed after preparing journal")
	}

	if err := writePreparedMigrationSnapshot(configBefore, configBeforeData); err != nil {
		return fail("migration failed while saving config recovery state")
	}
	if err := writePreparedMigrationSnapshot(markerBefore, markerBeforeData); err != nil {
		return fail("migration failed while saving marker recovery state")
	}
	for _, backup := range backups {
		if err := writeExclusiveMigrationFile(backup.Path, sourceData[backup.SourceLocationHash]); err != nil {
			return fail("migration failed while writing private backups")
		}
	}
	if _, err := advanceMigrationJournal(paths.MigrationJournal, transactionID, migrationPhaseBackups); err != nil {
		return fail("migration failed while committing backups")
	}
	if testHooks.shouldFailAfterJournalPhase(migrationPhaseBackups) {
		return fail("migration failed after committing backups")
	}
	for _, profile := range keyringProfiles {
		secret := plan.state.secrets[profile.Name]
		password := string(secret)
		if err := keyringBackend.Set(KeyringService, profile.Credential.Reference, password); err != nil {
			password = ""
			return fail("migration failed while writing a keyring credential")
		}
		stored, err := keyringBackend.Get(KeyringService, profile.Credential.Reference)
		if err != nil || stored != password {
			password = ""
			stored = ""
			return fail("migration failed while verifying a keyring credential")
		}
		password = ""
		stored = ""
		plan.clearProfileSecret(profile.Name)
	}
	if _, err := advanceMigrationJournal(paths.MigrationJournal, transactionID, migrationPhaseKeyring); err != nil {
		return fail("migration failed while committing credential decisions")
	}
	if testHooks.shouldFailAfterJournalPhase(migrationPhaseKeyring) {
		return fail("migration failed after committing credential decisions")
	}
	if err := locked.replacePrepared(candidateData, false); err != nil {
		return fail("migration failed while committing config")
	}
	if err := verifyMigrationArtifact(paths.ConfigFile, marker.ConfigDigest); err != nil {
		return fail("migration failed while verifying config")
	}
	if _, found, err := locked.load(); err != nil || !found {
		return fail("migration failed while strictly reloading config")
	}
	if _, err := advanceMigrationJournal(paths.MigrationJournal, transactionID, migrationPhaseConfig); err != nil {
		return fail("migration failed while recording config commit")
	}
	if testHooks.shouldFailAfterJournalPhase(migrationPhaseConfig) {
		return fail("migration failed after recording config commit")
	}
	if err := atomicWriteFile(paths.MigrationMarker, markerData, 0o600, false); err != nil {
		return fail("migration failed while committing marker")
	}
	storedMarker, exists, err := loadMigrationMarker(paths.MigrationMarker)
	if err != nil || !exists || storedMarker.TransactionID != transactionID {
		return fail("migration failed while strictly reloading marker")
	}
	if err := verifyMigrationArtifact(paths.MigrationMarker, journal.Marker.AfterDigest); err != nil {
		return fail("migration failed while verifying marker")
	}
	if _, err := advanceMigrationJournal(paths.MigrationJournal, transactionID, migrationPhaseMarkerVerified); err != nil {
		return fail("migration failed while recording marker verification")
	}
	if testHooks.shouldFailAfterJournalPhase(migrationPhaseMarkerVerified) {
		return fail("migration committed but cleanup requires recovery")
	}
	if err := completeMigrationJournal(paths.MigrationJournal, transactionID); err != nil {
		return fail("migration committed but cleanup requires recovery")
	}
	return result, nil
}

func validateMigrationPlanForApply(plan MigrationPlan) ([]MigratedProfile, error) {
	if len(plan.Conflicts) != 0 {
		return nil, fmt.Errorf("migration has %d unresolved conflict(s)", len(plan.Conflicts))
	}
	var keyringProfiles []MigratedProfile
	for _, profile := range plan.Profiles {
		if profile.CredentialStatus != MigrationCredentialResolved && profile.CredentialStatus != MigrationCredentialKeyringImport {
			return nil, fmt.Errorf("profile %q has an unresolved credential decision", profile.Name)
		}
		if profile.CredentialStatus == MigrationCredentialKeyringImport {
			if profile.Credential.Provider != ProviderKeyring || !validMigrationKeyringReference(profile.Credential.Reference) || len(plan.state.secrets[profile.Name]) == 0 {
				return nil, fmt.Errorf("profile %q has invalid keyring import state", profile.Name)
			}
			keyringProfiles = append(keyringProfiles, profile)
		} else if profile.Credential.Provider == ProviderKeyring {
			return nil, fmt.Errorf("profile %q keyring credential was not prepared for import", profile.Name)
		}
		if err := profile.Credential.Validate(); err != nil {
			return nil, fmt.Errorf("profile %q credential decision: %w", profile.Name, err)
		}
		if profile.Credential.Provider == ProviderFile && !filepath.IsAbs(profile.Credential.Reference) {
			return nil, fmt.Errorf("profile %q file credential reference must be an absolute path", profile.Name)
		}
	}
	return keyringProfiles, nil
}

func validMigrationKeyringReference(reference string) bool {
	const prefix = "migration-"
	return strings.HasPrefix(reference, prefix) && validMigrationTransactionID(strings.TrimPrefix(reference, prefix))
}

func cloneMigrationDestination(source Config) Config {
	clone := source
	clone.Profiles = make(map[string]Profile, len(source.Profiles))
	for name, profile := range source.Profiles {
		clone.Profiles[name] = profile
	}
	return clone
}

func normalizeMigrationPaths(paths Paths) Paths {
	if paths.BaseDir == "" && paths.ConfigFile != "" {
		paths.BaseDir = filepath.Dir(paths.ConfigFile)
	}
	if paths.MigrationMarker == "" {
		paths.MigrationMarker = filepath.Join(paths.BaseDir, "migration-v1.yaml")
	}
	if paths.MigrationJournal == "" {
		paths.MigrationJournal = filepath.Join(paths.BaseDir, "migration-v1.pending.yaml")
	}
	if paths.MigrationConfigRecovery == "" {
		paths.MigrationConfigRecovery = filepath.Join(paths.BaseDir, "migration-v1.config.before")
	}
	if paths.MigrationMarkerRecovery == "" {
		paths.MigrationMarkerRecovery = filepath.Join(paths.BaseDir, "migration-v1.marker.before")
	}
	return paths
}

func loadMigrationMarker(path string) (migrationMarker, bool, error) {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return migrationMarker{}, false, nil
	} else if err != nil {
		return migrationMarker{}, false, fmt.Errorf("inspect migration marker: %w", migrationErrorCause(err))
	}
	data, err := readBoundedPrivateMigrationFile(path)
	if err != nil {
		return migrationMarker{}, true, fmt.Errorf("read migration marker: %w", migrationErrorCause(err))
	}
	var marker migrationMarker
	if err := decodeStrictYAML(data, &marker); err != nil {
		return migrationMarker{}, true, fmt.Errorf("parse migration marker: invalid strict YAML document")
	}
	if err := validateMigrationMarker(marker, filepath.Dir(path)); err != nil {
		return migrationMarker{}, true, err
	}
	return marker, true, nil
}

func validateMigrationMarker(marker migrationMarker, baseDir string) error {
	_, completedOffset := marker.CompletedAt.Zone()
	if marker.SchemaVersion != migrationJournalSchemaVersion || !validMigrationTransactionID(marker.TransactionID) ||
		marker.TargetSchema != SchemaVersion || !validJournalText(marker.ToolVersion, 128) || marker.CompletedAt.IsZero() ||
		completedOffset != 0 || !validSHA256(marker.ConfigDigest) || len(marker.Sources) == 0 ||
		len(marker.Backups) != len(marker.Sources) {
		return fmt.Errorf("validate migration marker: invalid metadata")
	}
	seenSources := make(map[string]struct{}, len(marker.Sources))
	for _, source := range marker.Sources {
		if (source.Kind != MigrationSourceMetaYAML && source.Kind != MigrationSourceNEUCNJSON) ||
			!validSHA256(source.LocationHash) || !validSHA256(source.RedactedDigest) {
			return fmt.Errorf("validate migration marker: invalid source stamp")
		}
		if _, exists := seenSources[source.LocationHash]; exists {
			return fmt.Errorf("validate migration marker: duplicate source stamp")
		}
		seenSources[source.LocationHash] = struct{}{}
	}
	seenProfiles := make(map[string]struct{}, len(marker.Profiles))
	for _, profile := range marker.Profiles {
		if !profileNamePattern.MatchString(profile) {
			return fmt.Errorf("validate migration marker: invalid profile")
		}
		if _, exists := seenProfiles[profile]; exists {
			return fmt.Errorf("validate migration marker: duplicate profile")
		}
		seenProfiles[profile] = struct{}{}
	}
	seenBackups := make(map[string]struct{}, len(marker.Backups))
	for _, backup := range marker.Backups {
		if _, exists := seenSources[backup.SourceLocationHash]; !exists {
			return fmt.Errorf("validate migration marker: backup source mismatch")
		}
		if _, exists := seenBackups[backup.SourceLocationHash]; exists {
			return fmt.Errorf("validate migration marker: duplicate backup")
		}
		seenBackups[backup.SourceLocationHash] = struct{}{}
		if err := validatePathInsideBase(backup.Path, baseDir); err != nil {
			return fmt.Errorf("validate migration marker: invalid backup path")
		}
	}
	return nil
}

func migrationResultFromMarker(marker migrationMarker, markerPath string) MigrationResult {
	result := MigrationResult{
		AppliedProfiles: append([]string(nil), marker.Profiles...),
		Backups:         make(map[string]string, len(marker.Backups)),
		Marker:          markerPath,
	}
	for _, backup := range marker.Backups {
		result.Backups[backup.SourceLocationHash] = backup.Path
	}
	return result
}

func migrationMarkerMatchesPlan(marker migrationMarker, plan MigrationPlan) bool {
	if plan.state == nil || len(marker.Sources) != len(plan.Sources) {
		return false
	}
	for _, source := range marker.Sources {
		stamp, exists := plan.state.sourceStamps[source.LocationHash]
		if !exists || stamp != source {
			return false
		}
	}
	return true
}

func redactedMigrationSourceDigest(kind MigrationSourceKind, data []byte) (string, error) {
	const placeholder = "<credential-redacted>"
	var canonical []byte
	var err error
	switch kind {
	case MigrationSourceMetaYAML:
		var source legacyMetaSource
		if err := decodeStrictYAML(data, &source); err != nil {
			return "", err
		}
		for index := range source.Accounts {
			source.Accounts[index].Password = placeholder
		}
		canonical, err = yaml.Marshal(source)
	case MigrationSourceNEUCNJSON:
		var source legacyNEUCNSource
		if err := decodeStrictJSON(data, &source); err != nil {
			return "", err
		}
		for index := range source.Accounts {
			source.Accounts[index].EncryptedPassword = placeholder
		}
		canonical, err = json.Marshal(source)
	default:
		return "", fmt.Errorf("unsupported migration source kind")
	}
	if err != nil {
		return "", err
	}
	return migrationArtifactDigest(canonical), nil
}

func verifyMigrationPlanSources(plan MigrationPlan) (map[string][]byte, []migrationJournalSourceStamp, error) {
	if plan.state == nil || len(plan.Sources) == 0 || len(plan.Sources) != len(plan.state.sourceRawDigests) {
		return nil, nil, fmt.Errorf("migration source state is incomplete")
	}
	dataByLocation := make(map[string][]byte, len(plan.Sources))
	stamps := make([]migrationJournalSourceStamp, 0, len(plan.Sources))
	for _, source := range plan.Sources {
		path, hasPath := plan.state.sourcePaths[source.LocationHash]
		rawDigest, hasDigest := plan.state.sourceRawDigests[source.LocationHash]
		stamp, hasStamp := plan.state.sourceStamps[source.LocationHash]
		if !hasPath || !hasDigest || !hasStamp || stamp.Kind != source.Kind {
			clearMigrationSourceData(dataByLocation)
			return nil, nil, fmt.Errorf("migration source state is incomplete")
		}
		data, exists, err := readBoundedMigrationFile(path)
		if err != nil || !exists || sha256.Sum256(data) != rawDigest {
			clearMigrationSourceData(dataByLocation)
			return nil, nil, fmt.Errorf("migration source changed after preview; rebuild the migration plan")
		}
		redactedHash, err := redactedMigrationSourceDigest(source.Kind, data)
		if err != nil || redactedHash != stamp.RedactedDigest {
			clearMigrationSourceData(dataByLocation)
			return nil, nil, fmt.Errorf("migration source changed after preview; rebuild the migration plan")
		}
		dataByLocation[source.LocationHash] = data
		stamps = append(stamps, stamp)
	}
	return dataByLocation, stamps, nil
}

func clearMigrationSourceData(values map[string][]byte) {
	for key, value := range values {
		wipeBytes(value)
		delete(values, key)
	}
}

// RecoverPendingMigration deterministically finishes cleanup for a committed
// transaction or rolls an incomplete transaction back. Unknown concurrent
// artifact state is never overwritten and keeps the journal for recovery.
func RecoverPendingMigration(paths Paths, options MigrationApplyOptions) error {
	paths = normalizeMigrationPaths(paths)
	if _, err := os.Lstat(paths.MigrationJournal); errors.Is(err, os.ErrNotExist) {
		// Keep a no-journal recovery check side-effect free, but never perform a
		// second unlocked journal check: a transaction could create its journal
		// between the two checks. A newly-created journal is serialized and
		// handled by Apply (or the next explicit recovery attempt).
		return inspectOrphanMigrationRecovery(paths)
	}
	store := &Store{Path: paths.ConfigFile, MigrationJournal: paths.MigrationJournal}
	return store.withMutationLock(func(_ *lockedStore) error {
		return recoverPendingMigrationIfPresent(paths, options)
	})
}

func recoverPendingMigrationIfPresent(paths Paths, options MigrationApplyOptions) error {
	_, err := os.Lstat(paths.MigrationJournal)
	if errors.Is(err, os.ErrNotExist) {
		return inspectOrphanMigrationRecovery(paths)
	}
	if err != nil {
		return fmt.Errorf("migration recovery required: cannot inspect pending journal")
	}
	_, recoverErr := recoverPendingMigration(paths, options)
	return recoverErr
}

func inspectOrphanMigrationRecovery(paths Paths) error {
	for _, recoveryPath := range []string{paths.MigrationConfigRecovery, paths.MigrationMarkerRecovery} {
		if _, recoveryErr := os.Lstat(recoveryPath); recoveryErr == nil {
			return fmt.Errorf("migration recovery required: orphan recovery state exists")
		} else if !errors.Is(recoveryErr, os.ErrNotExist) {
			return fmt.Errorf("migration recovery required: cannot inspect recovery state")
		}
	}
	return nil
}

func recoverPendingMigration(paths Paths, options MigrationApplyOptions) (bool, error) {
	journal, err := loadMigrationJournal(paths.MigrationJournal)
	if err != nil {
		return false, fmt.Errorf("migration recovery required: pending journal is invalid")
	}
	if journal.Phase == migrationPhaseMarkerVerified {
		if err := completeMigrationJournal(paths.MigrationJournal, journal.TransactionID); err != nil {
			return false, fmt.Errorf("migration recovery required: committed artifacts do not match the journal")
		}
		return true, nil
	}

	markerState, err := inspectMigrationRollbackArtifact(journal.Marker)
	if err != nil {
		return false, fmt.Errorf("migration recovery required: marker has unknown concurrent state")
	}
	configState, err := inspectMigrationRollbackArtifact(journal.Config)
	if err != nil {
		return false, fmt.Errorf("migration recovery required: config has unknown concurrent state")
	}
	if err := rollbackMigrationArtifact(journal.Marker, markerState); err != nil {
		return false, fmt.Errorf("migration recovery required: marker rollback failed")
	}
	if err := rollbackMigrationArtifact(journal.Config, configState); err != nil {
		return false, fmt.Errorf("migration recovery required: config rollback failed")
	}
	if journal.Phase != migrationPhasePrepared {
		backend := options.ProviderOptions.Keyring
		if backend == nil {
			backend = systemKeyring{}
		}
		for _, reference := range journal.NewKeyringRefs {
			if err := backend.Delete(KeyringService, reference); err != nil && !errors.Is(err, keyring.ErrNotFound) {
				return false, fmt.Errorf("migration recovery required: keyring rollback failed")
			}
		}
	}
	for _, backup := range journal.Backups {
		if err := removeRegularMigrationFile(backup.Path, true); err != nil {
			return false, fmt.Errorf("migration recovery required: backup rollback failed")
		}
	}
	if err := removeMigrationSnapshot(journal.Marker.Before); err != nil {
		return false, fmt.Errorf("migration recovery required: marker snapshot cleanup failed")
	}
	if err := removeMigrationSnapshot(journal.Config.Before); err != nil {
		return false, fmt.Errorf("migration recovery required: config snapshot cleanup failed")
	}
	if err := removeRegularMigrationFile(paths.MigrationJournal, false); err != nil {
		return false, fmt.Errorf("migration recovery required: journal cleanup failed")
	}
	return false, nil
}

type migrationRollbackArtifactState uint8

const (
	migrationArtifactBefore migrationRollbackArtifactState = iota
	migrationArtifactAfter
)

func inspectMigrationRollbackArtifact(artifact migrationJournalArtifact) (migrationRollbackArtifactState, error) {
	data, exists, err := readBoundedRegularMigrationFile(artifact.TargetPath)
	if err != nil {
		return 0, err
	}
	if !exists {
		if artifact.Before.Existed {
			return 0, fmt.Errorf("artifact disappeared")
		}
		return migrationArtifactBefore, nil
	}
	digest := migrationArtifactDigest(data)
	if artifact.Before.Existed && digest == artifact.Before.Digest {
		return migrationArtifactBefore, nil
	}
	if digest != artifact.AfterDigest {
		return 0, fmt.Errorf("artifact digest is unknown")
	}
	if artifact.Before.Existed {
		if _, err := loadMigrationSnapshot(artifact.Before); err != nil {
			return 0, err
		}
	}
	return migrationArtifactAfter, nil
}

func rollbackMigrationArtifact(artifact migrationJournalArtifact, state migrationRollbackArtifactState) error {
	if state == migrationArtifactBefore {
		return nil
	}
	if !artifact.Before.Existed {
		return removeRegularMigrationFile(artifact.TargetPath, false)
	}
	data, err := loadMigrationSnapshot(artifact.Before)
	if err != nil {
		return err
	}
	if err := atomicWriteFile(artifact.TargetPath, data, 0o600, false); err != nil {
		return err
	}
	return verifyMigrationArtifact(artifact.TargetPath, artifact.Before.Digest)
}

func parseLegacyMetaSource(path string) (MigrationSource, []migrationCandidate, bool, error) {
	data, exists, err := readBoundedMigrationFile(path)
	if err != nil || !exists {
		return MigrationSource{}, nil, exists, migrationSourceReadError(MigrationSourceMetaYAML, err)
	}
	var source legacyMetaSource
	if err := decodeStrictYAML(data, &source); err != nil {
		return MigrationSource{}, nil, true, fmt.Errorf("parse %s migration source: %w", MigrationSourceMetaYAML, err)
	}
	preview, err := newMigrationSource(MigrationSourceMetaYAML, path, data)
	if err != nil {
		return MigrationSource{}, nil, true, err
	}
	candidates := make([]migrationCandidate, 0, len(source.Accounts))
	for _, account := range source.Accounts {
		if !profileNamePattern.MatchString(account.Username) {
			clearMigrationCandidates(candidates)
			return MigrationSource{}, nil, true, fmt.Errorf("%s migration source contains an account that cannot be represented by a profile name", MigrationSourceMetaYAML)
		}
		status := MigrationCredentialPendingManual
		decoded, decodeErr := base64.StdEncoding.Strict().DecodeString(account.Password)
		if decodeErr == nil && len(decoded) > 0 && len(decoded) <= maxCredentialBytes && bytes.IndexByte(decoded, 0) < 0 {
			status = MigrationCredentialPendingImportable
		} else {
			wipeBytes(decoded)
			decoded = nil
		}
		candidates = append(candidates, migrationCandidate{
			profile: MigratedProfile{
				Name: account.Username, Username: account.Username,
				CredentialStatus: status,
				Default:          account.Username == source.DefaultAccount,
				Source:           MigrationSourceMetaYAML,
			},
			secret: decoded,
		})
	}
	return preview, candidates, true, nil
}

func resolveLegacyNEUCNPath(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("inspect %s migration source: %w", MigrationSourceNEUCNJSON, migrationErrorCause(err))
	}
	if !info.IsDir() {
		return path, true, nil
	}
	for _, name := range []string{"config.json", "config"} {
		candidate := filepath.Join(path, name)
		candidateInfo, candidateErr := os.Stat(candidate)
		if candidateErr == nil && candidateInfo.Mode().IsRegular() {
			return candidate, true, nil
		}
		if candidateErr != nil && !errors.Is(candidateErr, os.ErrNotExist) {
			return "", false, fmt.Errorf("inspect %s migration source candidate: %w", MigrationSourceNEUCNJSON, migrationErrorCause(candidateErr))
		}
	}
	return "", false, fmt.Errorf("%s migration source directory has no supported config file", MigrationSourceNEUCNJSON)
}

func parseLegacyNEUCNSource(path string) (MigrationSource, []migrationCandidate, bool, error) {
	data, exists, err := readBoundedMigrationFile(path)
	if err != nil || !exists {
		return MigrationSource{}, nil, exists, migrationSourceReadError(MigrationSourceNEUCNJSON, err)
	}
	var source legacyNEUCNSource
	if err := decodeStrictJSON(data, &source); err != nil {
		return MigrationSource{}, nil, true, fmt.Errorf("parse %s migration source: %w", MigrationSourceNEUCNJSON, err)
	}
	preview, err := newMigrationSource(MigrationSourceNEUCNJSON, path, data)
	if err != nil {
		return MigrationSource{}, nil, true, err
	}
	candidates := make([]migrationCandidate, 0, len(source.Accounts))
	for _, account := range source.Accounts {
		if !profileNamePattern.MatchString(account.Username) {
			clearMigrationCandidates(candidates)
			return MigrationSource{}, nil, true, fmt.Errorf("%s migration source contains an account that cannot be represented by a profile name", MigrationSourceNEUCNJSON)
		}
		// The upstream ciphertext is intentionally never guessed or decrypted.
		// Even a non-empty field remains a manual pending decision.
		candidates = append(candidates, migrationCandidate{profile: MigratedProfile{
			Name: account.Username, Username: account.Username,
			CredentialStatus: MigrationCredentialPendingManual,
			Default:          account.Username == source.DefaultAccount,
			Source:           MigrationSourceNEUCNJSON,
		}})
	}
	return preview, candidates, true, nil
}

func newMigrationSource(kind MigrationSourceKind, path string, data []byte) (MigrationSource, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return MigrationSource{}, fmt.Errorf("resolve %s migration source location: %w", kind, migrationErrorCause(err))
	}
	normalized := filepath.ToSlash(filepath.Clean(absPath))
	digest := sha256.Sum256([]byte(normalized))
	redactedHash, err := redactedMigrationSourceDigest(kind, data)
	if err != nil {
		return MigrationSource{}, fmt.Errorf("prepare redacted migration source stamp")
	}
	return MigrationSource{
		Kind: kind, LocationHash: hex.EncodeToString(digest[:]),
		rawDigest: sha256.Sum256(data), redactedHash: redactedHash,
	}, nil
}

func readBoundedMigrationFile(path string) ([]byte, bool, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > maxConfigBytes {
		return nil, false, fmt.Errorf("migration source exceeds %d bytes", maxConfigBytes)
	}
	return data, true, nil
}

func decodeStrictYAML(data []byte, target any) error {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("document is empty")
		}
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple YAML documents are not allowed")
		}
		return fmt.Errorf("parse trailing YAML data: %w", err)
	}
	return nil
}

func decodeStrictJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("document is empty")
		}
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return fmt.Errorf("parse trailing JSON data: %w", err)
	}
	return nil
}

func migrationSourceReadError(kind MigrationSourceKind, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("read %s migration source: %w", kind, migrationErrorCause(err))
}

func migrationErrorCause(err error) error {
	var pathError *os.PathError
	if errors.As(err, &pathError) && pathError.Err != nil {
		return pathError.Err
	}
	return err
}

func (p *MigrationPlan) clearProfileSecret(profileName string) {
	if p == nil || p.state == nil {
		return
	}
	secret := p.state.secrets[profileName]
	wipeBytes(secret)
	delete(p.state.secrets, profileName)
}

func clearMigrationSecrets(secrets map[string][]byte) {
	for name, secret := range secrets {
		wipeBytes(secret)
		delete(secrets, name)
	}
}

func clearMigrationCandidates(candidates []migrationCandidate) {
	for index := range candidates {
		wipeBytes(candidates[index].secret)
		candidates[index].secret = nil
	}
}

func wipeBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func backupSource(source string) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open migration source for backup: %w", migrationErrorCause(err))
	}
	data, readErr := io.ReadAll(io.LimitReader(input, maxConfigBytes+1))
	closeErr := input.Close()
	if readErr != nil {
		return "", fmt.Errorf("read migration source for backup: %w", migrationErrorCause(readErr))
	}
	if closeErr != nil {
		return "", fmt.Errorf("close migration source for backup: %w", migrationErrorCause(closeErr))
	}
	if len(data) > maxConfigBytes {
		return "", fmt.Errorf("migration source changed and now exceeds %d bytes", maxConfigBytes)
	}

	timestamp := time.Now().UTC().Format("20060102T150405.000000000Z")
	backup := source + ".bak." + timestamp
	output, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create migration backup: %w", migrationErrorCause(err))
	}
	removeBackup := true
	defer func() {
		_ = output.Close()
		if removeBackup {
			_ = os.Remove(backup)
		}
	}()
	if _, err := output.Write(data); err != nil {
		return "", fmt.Errorf("write migration backup: %w", migrationErrorCause(err))
	}
	if err := output.Sync(); err != nil {
		return "", fmt.Errorf("sync migration backup: %w", migrationErrorCause(err))
	}
	if err := output.Close(); err != nil {
		return "", fmt.Errorf("close migration backup: %w", migrationErrorCause(err))
	}
	removeBackup = false
	return backup, nil
}
