package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"go.yaml.in/yaml/v3"
)

const maxConfigBytes = 1 << 20

// ErrConfigPublishedDurabilityUnknown means the new config is already visible
// at its target path, but the final directory durability barrier failed. A
// caller must not compensate side effects as though the config were absent.
var ErrConfigPublishedDurabilityUnknown = errors.New("config was published but durability could not be confirmed")

type publishedWriteError struct {
	path  string
	cause error
}

func (e *publishedWriteError) Error() string { return e.cause.Error() }
func (e *publishedWriteError) Unwrap() error { return e.cause }

type Store struct {
	Path             string
	MigrationJournal string
	mu               sync.Mutex
}

func (s *Store) Load() (Config, bool, error) {
	path, err := s.resolvedPath()
	if err != nil {
		return Config{}, false, err
	}
	return loadConfigFile(path)
}

func loadConfigFile(path string) (Config, bool, error) {
	f, err := openVerifiedStoreConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), false, nil
	}
	if err != nil {
		return Config{}, false, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	limited := io.LimitReader(f, maxConfigBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return Config{}, false, fmt.Errorf("read config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return Config{}, false, fmt.Errorf("config exceeds %d bytes", maxConfigBytes)
	}

	var cfg Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, false, fmt.Errorf("parse config: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Config{}, false, fmt.Errorf("parse config: multiple YAML documents are not allowed")
		}
		return Config{}, false, fmt.Errorf("parse trailing config data: %w", err)
	}
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, false, fmt.Errorf("validate config: %w", err)
	}
	return cfg, true, nil
}

func (s *Store) Save(cfg Config) error {
	data, err := encodeConfig(cfg)
	if err != nil {
		return err
	}
	return s.withMutationLock(func(locked *lockedStore) error {
		if err := locked.rejectPendingMigration(); err != nil {
			return err
		}
		return locked.write(data)
	})
}

func (s *Store) Update(update func(*Config) error) error {
	if update == nil {
		return fmt.Errorf("update callback is required")
	}
	return s.withMutationLock(func(locked *lockedStore) error {
		if err := locked.rejectPendingMigration(); err != nil {
			return err
		}
		cfg, _, err := locked.load()
		if err != nil {
			return err
		}
		if err := update(&cfg); err != nil {
			return err
		}
		return locked.save(cfg)
	})
}

func (s *Store) loadUnlocked() (Config, bool, error) {
	path, err := s.resolvedPath()
	if err != nil {
		return Config{}, false, err
	}
	return loadConfigFile(path)
}

// lockedStore is available to other config-package transactions that need to
// keep the same cross-process mutation lock across more than one artifact.
// Its methods deliberately do not acquire the lock again.
type lockedStore struct {
	path             string
	migrationJournal string
}

func (s *Store) withMutationLock(mutate func(*lockedStore) error) error {
	if mutate == nil {
		return fmt.Errorf("mutation callback is required")
	}
	path, err := s.resolvedPath()
	if err != nil {
		return err
	}
	journal, err := resolveOptionalStorePath(s.MigrationJournal)
	if err != nil {
		return fmt.Errorf("resolve migration journal path: %w", err)
	}
	if journal == "" {
		journal = filepath.Join(filepath.Dir(path), "migration-v1.pending.yaml")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	lock, err := acquireStoreMutationLock(path)
	if err != nil {
		return fmt.Errorf("acquire config mutation lock: %w", err)
	}
	defer func() {
		_ = lock.Close()
	}()
	return mutate(&lockedStore{path: path, migrationJournal: journal})
}

func (s *Store) resolvedPath() (string, error) {
	if s == nil || s.Path == "" {
		return "", fmt.Errorf("config path is required")
	}
	path, err := filepath.Abs(s.Path)
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	path = filepath.Clean(path)
	if filepath.Dir(path) == path {
		return "", fmt.Errorf("config path must name a file")
	}
	return path, nil
}

func resolveOptionalStorePath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absPath), nil
}

func (locked *lockedStore) load() (Config, bool, error) {
	return loadConfigFile(locked.path)
}

func (locked *lockedStore) save(cfg Config) error {
	data, err := encodeConfig(cfg)
	if err != nil {
		return err
	}
	return locked.write(data)
}

func (locked *lockedStore) write(data []byte) error {
	return locked.replacePrepared(data, true)
}

// replacePrepared is used by the migration journal transaction after it has
// captured its own before-image. It performs the same trusted-handle check as
// an ordinary Store write, while allowing the caller to avoid creating an
// unjournaled .lkg side effect.
func (locked *lockedStore) replacePrepared(data []byte, preserveExisting bool) error {
	existing, err := openVerifiedStoreConfig(locked.path)
	if err == nil {
		if closeErr := existing.Close(); closeErr != nil {
			return fmt.Errorf("close verified existing config: %w", closeErr)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("verify existing config before replacement: %w", err)
	}
	err = atomicWriteFile(locked.path, data, 0o600, preserveExisting)
	return classifyConfigWriteError(locked.path, err)
}

func classifyConfigWriteError(path string, err error) error {
	if publishedWriteAt(err, path) {
		return fmt.Errorf("%w: %v", ErrConfigPublishedDurabilityUnknown, err)
	}
	return err
}

func publishedWriteAt(err error, path string) bool {
	var published *publishedWriteError
	return errors.As(err, &published) && filepath.Clean(published.path) == filepath.Clean(path)
}

func (locked *lockedStore) rejectPendingMigration() error {
	_, err := os.Lstat(locked.migrationJournal)
	if err == nil {
		return fmt.Errorf("pending migration journal exists; recover migration before updating config")
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("inspect pending migration journal: %w", err)
}

func encodeConfig(cfg Config) ([]byte, error) {
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]Profile)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	if len(data) > maxConfigBytes {
		return nil, fmt.Errorf("config exceeds %d bytes", maxConfigBytes)
	}
	return data, nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	return atomicWriteFile(path, data, mode, true)
}

// atomicWriteFile writes a fully prepared private temporary file before it
// touches the destination. When preserveExisting is true, an existing regular
// destination is first copied to path+".lkg" using the same durable private
// write path. A failed final replacement therefore leaves the destination
// intact and the last-known-good copy usable.
func atomicWriteFile(path string, data []byte, mode os.FileMode, preserveExisting bool) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create private directory: %w", err)
	}
	// This must happen before CreateTemp: a newly-created empty file must never
	// spend time in a directory that still has broader-than-intended access.
	if err := restrictPrivateDirectory(dir); err != nil {
		return fmt.Errorf("restrict private directory permissions: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".ipgw-meta-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary private file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		// After a successful rename tmpName no longer exists. On every failure
		// this removes the only uncommitted copy.
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("restrict temporary private file mode: %w", err)
	}
	// Windows chmod is not an ACL boundary. Apply the protected current-user
	// DACL (or the Unix mode check) while the temporary file is still empty.
	if err := restrictPrivateFile(tmpName, mode); err != nil {
		return fmt.Errorf("restrict temporary private file permissions: %w", err)
	}
	written, err := tmp.Write(data)
	if err != nil {
		return fmt.Errorf("write temporary private file: %w", err)
	}
	if written != len(data) {
		return fmt.Errorf("write temporary private file: %w", io.ErrShortWrite)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync temporary private file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary private file: %w", err)
	}

	if preserveExisting {
		if err := preserveLastKnownGood(path, mode); err != nil {
			return err
		}
	}
	if err := replaceFile(tmpName, path); err != nil {
		return fmt.Errorf("replace private file atomically: %w", err)
	}
	// On Unix, fsync of the containing directory is what makes the rename
	// durable. Windows replaceFile uses MOVEFILE_WRITE_THROUGH instead.
	if err := syncParentDirectory(dir); err != nil {
		return &publishedWriteError{path: path, cause: fmt.Errorf("sync private file directory: %w", err)}
	}
	return nil
}

func preserveLastKnownGood(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect existing private file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("existing private file is not a regular file")
	}
	if info.Size() > maxConfigBytes {
		return fmt.Errorf("existing private file exceeds %d bytes", maxConfigBytes)
	}

	existing, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open existing private file: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(existing, maxConfigBytes+1))
	closeErr := existing.Close()
	if readErr != nil {
		return fmt.Errorf("read existing private file: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close existing private file: %w", closeErr)
	}
	if len(data) > maxConfigBytes {
		return fmt.Errorf("existing private file exceeds %d bytes", maxConfigBytes)
	}
	if err := atomicWriteFile(path+".lkg", data, mode, false); err != nil {
		return fmt.Errorf("preserve last-known-good private file: %w", err)
	}
	return nil
}

// WritePrivateFile atomically writes a provider-owned, user-private file.
// It is shared by the secret-free launcher/protocol state stores and migrated
// credential files so they use the same cross-platform replacement rules.
func WritePrivateFile(path string, data []byte) error {
	return atomicWrite(path, data, 0o600)
}
