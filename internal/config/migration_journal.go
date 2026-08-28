package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const (
	migrationJournalSchemaVersion = 1
	migrationTransactionIDBytes   = 16
)

type migrationJournalPhase string

const (
	migrationPhasePrepared       migrationJournalPhase = "prepared"
	migrationPhaseBackups        migrationJournalPhase = "backups"
	migrationPhaseKeyring        migrationJournalPhase = "keyring"
	migrationPhaseConfig         migrationJournalPhase = "config"
	migrationPhaseMarkerVerified migrationJournalPhase = "marker_verified"
)

// migrationJournal is deliberately incapable of representing source bytes,
// source-content digests, credentials, or keyring values. Only a digest of a
// canonical, secret-redacted source representation may be persisted.
type migrationJournal struct {
	SchemaVersion  int                            `yaml:"schema_version"`
	TransactionID  string                         `yaml:"transaction_id"`
	Phase          migrationJournalPhase          `yaml:"phase"`
	TargetSchema   int                            `yaml:"target_schema"`
	ToolVersion    string                         `yaml:"tool_version"`
	Sources        []migrationJournalSourceStamp  `yaml:"sources"`
	Config         migrationJournalArtifact       `yaml:"config"`
	Marker         migrationJournalArtifact       `yaml:"marker"`
	Backups        []migrationJournalBackupRecord `yaml:"backups,omitempty"`
	NewKeyringRefs []string                       `yaml:"new_keyring_refs,omitempty"`
}

type migrationJournalSourceStamp struct {
	Kind           MigrationSourceKind `yaml:"kind"`
	LocationHash   string              `yaml:"location_hash"`
	RedactedDigest string              `yaml:"redacted_digest"`
}

type migrationJournalArtifact struct {
	TargetPath  string                   `yaml:"target_path"`
	Before      migrationJournalSnapshot `yaml:"before"`
	AfterDigest string                   `yaml:"after_digest"`
}

type migrationJournalSnapshot struct {
	Existed bool   `yaml:"existed"`
	Path    string `yaml:"path,omitempty"`
	Digest  string `yaml:"digest,omitempty"`
}

type migrationJournalBackupRecord struct {
	SourceLocationHash string `yaml:"source_location_hash"`
	Path               string `yaml:"path"`
}

func newMigrationTransactionID() (string, error) {
	value := make([]byte, migrationTransactionIDBytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate migration transaction ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

// migrationArtifactDigest is only for secret-free Config, marker, and
// recovery-snapshot bytes. Raw migration-source digests must stay in memory.
func migrationArtifactDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func beginMigrationJournal(path string, journal migrationJournal) error {
	if err := validateMigrationJournal(journal, path); err != nil {
		return err
	}
	data, err := marshalMigrationJournal(journal)
	if err != nil {
		return err
	}
	if err := writeExclusiveMigrationFile(path, data); err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	return nil
}

func loadMigrationJournal(path string) (migrationJournal, error) {
	if err := validateCleanAbsoluteFilePath(path); err != nil {
		return migrationJournal{}, fmt.Errorf("load migration journal: invalid path")
	}
	data, err := readBoundedPrivateMigrationFile(path)
	if err != nil {
		return migrationJournal{}, fmt.Errorf("load migration journal: %w", err)
	}
	var journal migrationJournal
	if err := decodeStrictYAML(data, &journal); err != nil {
		// YAML decoder errors may echo an untrusted scalar. Journal parse errors
		// therefore intentionally expose no document content.
		return migrationJournal{}, fmt.Errorf("load migration journal: invalid strict YAML document")
	}
	if err := validateMigrationJournal(journal, path); err != nil {
		return migrationJournal{}, err
	}
	return journal, nil
}

func advanceMigrationJournal(path, transactionID string, next migrationJournalPhase) (migrationJournal, error) {
	if !validMigrationTransactionID(transactionID) {
		return migrationJournal{}, fmt.Errorf("advance migration journal: invalid transaction ID")
	}
	journal, err := loadMigrationJournal(path)
	if err != nil {
		return migrationJournal{}, err
	}
	if journal.TransactionID != transactionID {
		return migrationJournal{}, fmt.Errorf("advance migration journal: transaction ID mismatch")
	}
	expected, ok := journal.Phase.next()
	if !ok || next != expected {
		return migrationJournal{}, fmt.Errorf("advance migration journal: invalid phase transition")
	}
	journal.Phase = next
	data, err := marshalMigrationJournal(journal)
	if err != nil {
		return migrationJournal{}, err
	}
	if err := atomicWriteFile(path, data, 0o600, false); err != nil {
		return migrationJournal{}, fmt.Errorf("advance migration journal: %w", err)
	}
	stored, err := loadMigrationJournal(path)
	if err != nil {
		return migrationJournal{}, fmt.Errorf("verify migration journal update: %w", err)
	}
	if stored.TransactionID != transactionID || stored.Phase != next {
		return migrationJournal{}, fmt.Errorf("verify migration journal update: persisted state mismatch")
	}
	return stored, nil
}

// completeMigrationJournal removes recovery snapshots and the exclusive
// journal only after both committed artifacts still match this transaction.
// Missing snapshots are accepted so marker_verified cleanup is restartable.
func completeMigrationJournal(path, transactionID string) error {
	journal, err := loadMigrationJournal(path)
	if err != nil {
		return err
	}
	if !validMigrationTransactionID(transactionID) || journal.TransactionID != transactionID {
		return fmt.Errorf("complete migration journal: transaction ID mismatch")
	}
	if journal.Phase != migrationPhaseMarkerVerified {
		return fmt.Errorf("complete migration journal: transaction is not marker-verified")
	}
	if err := verifyMigrationArtifact(journal.Config.TargetPath, journal.Config.AfterDigest); err != nil {
		return fmt.Errorf("complete migration journal: verify config: %w", err)
	}
	if err := verifyMigrationArtifact(journal.Marker.TargetPath, journal.Marker.AfterDigest); err != nil {
		return fmt.Errorf("complete migration journal: verify marker: %w", err)
	}
	if err := removeMigrationSnapshot(journal.Config.Before); err != nil {
		return fmt.Errorf("complete migration journal: remove config recovery snapshot: %w", err)
	}
	if err := removeMigrationSnapshot(journal.Marker.Before); err != nil {
		return fmt.Errorf("complete migration journal: remove marker recovery snapshot: %w", err)
	}
	if err := removeRegularMigrationFile(path, false); err != nil {
		return fmt.Errorf("complete migration journal: %w", err)
	}
	return nil
}

func createMigrationSnapshot(sourcePath, recoveryPath string) (migrationJournalSnapshot, error) {
	snapshot, data, err := prepareMigrationSnapshot(sourcePath, recoveryPath)
	if err != nil {
		return migrationJournalSnapshot{}, err
	}
	if err := writePreparedMigrationSnapshot(snapshot, data); err != nil {
		return migrationJournalSnapshot{}, err
	}
	return snapshot, nil
}

// prepareMigrationSnapshot captures secret-free recovery bytes without any
// filesystem mutation so its metadata can be committed in the journal first.
func prepareMigrationSnapshot(sourcePath, recoveryPath string) (migrationJournalSnapshot, []byte, error) {
	if err := validateCleanAbsoluteFilePath(sourcePath); err != nil {
		return migrationJournalSnapshot{}, nil, fmt.Errorf("prepare migration snapshot: invalid source path")
	}
	if err := validateCleanAbsoluteFilePath(recoveryPath); err != nil {
		return migrationJournalSnapshot{}, nil, fmt.Errorf("prepare migration snapshot: invalid recovery path")
	}
	if filepath.Dir(sourcePath) != filepath.Dir(recoveryPath) || sourcePath == recoveryPath {
		return migrationJournalSnapshot{}, nil, fmt.Errorf("prepare migration snapshot: recovery path is outside the transaction directory")
	}
	if _, err := os.Lstat(recoveryPath); err == nil {
		return migrationJournalSnapshot{}, nil, fmt.Errorf("prepare migration snapshot: recovery path already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return migrationJournalSnapshot{}, nil, fmt.Errorf("prepare migration snapshot: inspect recovery path: %w", err)
	}
	data, exists, err := readBoundedRegularMigrationFile(sourcePath)
	if err != nil {
		return migrationJournalSnapshot{}, nil, fmt.Errorf("prepare migration snapshot: %w", err)
	}
	if !exists {
		return migrationJournalSnapshot{Existed: false}, nil, nil
	}
	return migrationJournalSnapshot{
		Existed: true,
		Path:    recoveryPath,
		Digest:  migrationArtifactDigest(data),
	}, data, nil
}

func writePreparedMigrationSnapshot(snapshot migrationJournalSnapshot, data []byte) error {
	if !snapshot.Existed {
		if snapshot.Path != "" || snapshot.Digest != "" || len(data) != 0 {
			return fmt.Errorf("write migration snapshot: invalid absent snapshot metadata")
		}
		return nil
	}
	if migrationArtifactDigest(data) != snapshot.Digest {
		return fmt.Errorf("write migration snapshot: digest mismatch")
	}
	if err := writeExclusiveMigrationFile(snapshot.Path, data); err != nil {
		return fmt.Errorf("write migration snapshot: %w", err)
	}
	return nil
}

func loadMigrationSnapshot(snapshot migrationJournalSnapshot) ([]byte, error) {
	if !snapshot.Existed {
		if snapshot.Path != "" || snapshot.Digest != "" {
			return nil, fmt.Errorf("load migration snapshot: invalid absent snapshot metadata")
		}
		return nil, nil
	}
	if err := validateCleanAbsoluteFilePath(snapshot.Path); err != nil || !validSHA256(snapshot.Digest) {
		return nil, fmt.Errorf("load migration snapshot: invalid metadata")
	}
	data, err := readBoundedPrivateMigrationFile(snapshot.Path)
	if err != nil {
		return nil, fmt.Errorf("load migration snapshot: %w", err)
	}
	if migrationArtifactDigest(data) != snapshot.Digest {
		return nil, fmt.Errorf("load migration snapshot: digest mismatch")
	}
	return data, nil
}

func removeMigrationSnapshot(snapshot migrationJournalSnapshot) error {
	if !snapshot.Existed {
		if snapshot.Path != "" || snapshot.Digest != "" {
			return fmt.Errorf("invalid absent snapshot metadata")
		}
		return nil
	}
	if _, err := os.Lstat(snapshot.Path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect recovery snapshot: %w", err)
	}
	if _, err := loadMigrationSnapshot(snapshot); err != nil {
		return err
	}
	return removeRegularMigrationFile(snapshot.Path, false)
}

func verifyMigrationArtifact(path, expectedDigest string) error {
	data, exists, err := readBoundedRegularMigrationFile(path)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("artifact is missing")
	}
	if migrationArtifactDigest(data) != expectedDigest {
		return fmt.Errorf("artifact digest mismatch")
	}
	return nil
}

func marshalMigrationJournal(journal migrationJournal) ([]byte, error) {
	data, err := yaml.Marshal(journal)
	if err != nil {
		return nil, fmt.Errorf("encode migration journal")
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("migration journal exceeds %d bytes", maxConfigBytes)
	}
	return data, nil
}

func validateMigrationJournal(journal migrationJournal, journalPath string) error {
	if err := validateCleanAbsoluteFilePath(journalPath); err != nil {
		return fmt.Errorf("validate migration journal: invalid journal path")
	}
	if journal.SchemaVersion != migrationJournalSchemaVersion {
		return fmt.Errorf("validate migration journal: unsupported schema")
	}
	if !validMigrationTransactionID(journal.TransactionID) {
		return fmt.Errorf("validate migration journal: invalid transaction ID")
	}
	if !journal.Phase.valid() {
		return fmt.Errorf("validate migration journal: invalid phase")
	}
	if journal.TargetSchema != SchemaVersion {
		return fmt.Errorf("validate migration journal: invalid target schema")
	}
	if !validJournalText(journal.ToolVersion, 128) {
		return fmt.Errorf("validate migration journal: invalid tool version")
	}
	if len(journal.Sources) == 0 {
		return fmt.Errorf("validate migration journal: source stamps are required")
	}

	baseDir := filepath.Dir(journalPath)
	seenSources := make(map[string]struct{}, len(journal.Sources))
	for _, source := range journal.Sources {
		if source.Kind != MigrationSourceMetaYAML && source.Kind != MigrationSourceNEUCNJSON {
			return fmt.Errorf("validate migration journal: invalid source kind")
		}
		if !validSHA256(source.LocationHash) || !validSHA256(source.RedactedDigest) {
			return fmt.Errorf("validate migration journal: invalid redacted source stamp")
		}
		if _, exists := seenSources[source.LocationHash]; exists {
			return fmt.Errorf("validate migration journal: duplicate source stamp")
		}
		seenSources[source.LocationHash] = struct{}{}
	}

	seenPaths := map[string]struct{}{migrationPathKey(journalPath): {}}
	if err := validateMigrationArtifact(journal.Config, baseDir, seenPaths); err != nil {
		return fmt.Errorf("validate migration journal: invalid config metadata")
	}
	if err := validateMigrationArtifact(journal.Marker, baseDir, seenPaths); err != nil {
		return fmt.Errorf("validate migration journal: invalid marker metadata")
	}

	seenBackups := make(map[string]struct{}, len(journal.Backups))
	if len(journal.Backups) != len(journal.Sources) {
		return fmt.Errorf("validate migration journal: every source requires one backup")
	}
	for _, backup := range journal.Backups {
		if _, exists := seenSources[backup.SourceLocationHash]; !exists {
			return fmt.Errorf("validate migration journal: backup has no matching source stamp")
		}
		if _, exists := seenBackups[backup.SourceLocationHash]; exists {
			return fmt.Errorf("validate migration journal: duplicate backup record")
		}
		seenBackups[backup.SourceLocationHash] = struct{}{}
		if err := addMigrationPath(backup.Path, baseDir, seenPaths); err != nil {
			return fmt.Errorf("validate migration journal: invalid backup path")
		}
	}

	seenRefs := make(map[string]struct{}, len(journal.NewKeyringRefs))
	for _, reference := range journal.NewKeyringRefs {
		if !validJournalText(reference, 256) {
			return fmt.Errorf("validate migration journal: invalid keyring reference")
		}
		if _, exists := seenRefs[reference]; exists {
			return fmt.Errorf("validate migration journal: duplicate keyring reference")
		}
		seenRefs[reference] = struct{}{}
	}
	return nil
}

func validateMigrationArtifact(artifact migrationJournalArtifact, baseDir string, seenPaths map[string]struct{}) error {
	if !validSHA256(artifact.AfterDigest) {
		return fmt.Errorf("invalid after digest")
	}
	if err := addMigrationPath(artifact.TargetPath, baseDir, seenPaths); err != nil {
		return err
	}
	if !artifact.Before.Existed {
		if artifact.Before.Path != "" || artifact.Before.Digest != "" {
			return fmt.Errorf("invalid absent snapshot metadata")
		}
		return nil
	}
	if !validSHA256(artifact.Before.Digest) {
		return fmt.Errorf("invalid snapshot digest")
	}
	return addMigrationPath(artifact.Before.Path, baseDir, seenPaths)
}

func addMigrationPath(path, baseDir string, seen map[string]struct{}) error {
	if err := validatePathInsideBase(path, baseDir); err != nil {
		return err
	}
	key := migrationPathKey(path)
	if _, exists := seen[key]; exists {
		return fmt.Errorf("path collision")
	}
	seen[key] = struct{}{}
	return nil
}

func validatePathInsideBase(path, baseDir string) error {
	if err := validateCleanAbsoluteFilePath(path); err != nil {
		return err
	}
	relative, err := filepath.Rel(baseDir, path)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("path is outside transaction directory")
	}
	return nil
}

func validateCleanAbsoluteFilePath(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." {
		return fmt.Errorf("path must be an absolute, clean file path")
	}
	return nil
}

func validMigrationTransactionID(value string) bool {
	return len(value) == migrationTransactionIDBytes*2 && validLowerHex(value)
}

func validSHA256(value string) bool {
	return len(value) == sha256.Size*2 && validLowerHex(value)
}

func validLowerHex(value string) bool {
	if strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validJournalText(value string, maxBytes int) bool {
	if value == "" || len(value) > maxBytes || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func migrationPathKey(path string) string {
	// Treat case-only differences as collisions on every platform. This is
	// stricter on case-sensitive filesystems and safe for portable journals.
	return strings.ToLower(filepath.Clean(path))
}

func (phase migrationJournalPhase) valid() bool {
	switch phase {
	case migrationPhasePrepared, migrationPhaseBackups, migrationPhaseKeyring, migrationPhaseConfig, migrationPhaseMarkerVerified:
		return true
	default:
		return false
	}
}

func (phase migrationJournalPhase) next() (migrationJournalPhase, bool) {
	switch phase {
	case migrationPhasePrepared:
		return migrationPhaseBackups, true
	case migrationPhaseBackups:
		return migrationPhaseKeyring, true
	case migrationPhaseKeyring:
		return migrationPhaseConfig, true
	case migrationPhaseConfig:
		return migrationPhaseMarkerVerified, true
	default:
		return "", false
	}
}

func writeExclusiveMigrationFile(path string, data []byte) (resultErr error) {
	if len(data) > maxConfigBytes {
		return fmt.Errorf("private migration file exceeds %d bytes", maxConfigBytes)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create private migration directory: %w", err)
	}
	if err := restrictPrivateDirectory(directory); err != nil {
		return fmt.Errorf("restrict private migration directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create exclusive private migration file: %w", err)
	}
	created := true
	defer func() {
		_ = file.Close()
		if resultErr != nil && created {
			_ = os.Remove(path)
			_ = syncParentDirectory(directory)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict private migration file mode: %w", err)
	}
	// On Windows chmod is not an ACL boundary. Apply the protected DACL while
	// the newly-created file is still empty; Unix applies 0600 here as well.
	if err := restrictPrivateFile(path, 0o600); err != nil {
		return fmt.Errorf("restrict private migration file permissions: %w", err)
	}
	written, err := file.Write(data)
	if err != nil {
		return fmt.Errorf("write private migration file: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("write private migration file: %w", io.ErrShortWrite)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync private migration file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close private migration file: %w", err)
	}
	created = false
	if err := syncParentDirectory(directory); err != nil {
		return fmt.Errorf("sync private migration directory: %w", err)
	}
	return nil
}

func readBoundedPrivateMigrationFile(path string) ([]byte, error) {
	file, err := openRestrictedPasswordFile(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect private migration file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("private migration path is not a regular file")
	}
	if info.Size() > maxConfigBytes {
		return nil, fmt.Errorf("private migration file exceeds %d bytes", maxConfigBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read private migration file: %w", err)
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("private migration file exceeds %d bytes", maxConfigBytes)
	}
	return data, nil
}

func readBoundedRegularMigrationFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("migration artifact is not a regular file")
	}
	if info.Size() > maxConfigBytes {
		return nil, false, fmt.Errorf("migration artifact exceeds %d bytes", maxConfigBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if !os.SameFile(info, opened) || !opened.Mode().IsRegular() {
		return nil, false, fmt.Errorf("migration artifact changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxConfigBytes+1))
	if err != nil {
		return nil, false, err
	}
	if len(data) > maxConfigBytes {
		return nil, false, fmt.Errorf("migration artifact exceeds %d bytes", maxConfigBytes)
	}
	return data, true, nil
}

func removeRegularMigrationFile(path string, allowMissing bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && allowMissing {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("migration path is not a regular file")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := syncParentDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("sync migration directory: %w", err)
	}
	return nil
}
