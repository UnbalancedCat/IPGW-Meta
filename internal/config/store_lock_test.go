package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestStoreSaveLoadRoundTripUsesPersistentLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	store := &Store{Path: path}
	cfg := Default()
	cfg.Profiles["primary"] = testStoreProfile("primary-user")
	cfg.DefaultProfile = "primary"
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, found, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !found || loaded.DefaultProfile != "primary" || loaded.Profiles["primary"].Username != "primary-user" {
		t.Fatalf("Load() = %#v, found %v", loaded, found)
	}
	info, err := os.Lstat(path + ".lock")
	if err != nil {
		t.Fatalf("stat persistent lock: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("persistent lock mode = %v, want regular", info.Mode())
	}
}

func TestStoreUpdateSerializesDifferentStoreInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	first := &Store{Path: path}
	second := &Store{Path: path}
	if err := first.Save(Default()); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}

	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondStarted := make(chan struct{})
	secondEntered := make(chan struct{}, 1)
	results := make(chan error, 2)
	go func() {
		results <- first.Update(func(cfg *Config) error {
			close(firstEntered)
			<-releaseFirst
			cfg.Profiles["first"] = testStoreProfile("first-user")
			return nil
		})
	}()
	select {
	case <-firstEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first Update did not enter callback")
	}

	go func() {
		close(secondStarted)
		results <- second.Update(func(cfg *Config) error {
			secondEntered <- struct{}{}
			cfg.Profiles["second"] = testStoreProfile("second-user")
			return nil
		})
	}()
	<-secondStarted

	enteredBeforeRelease := false
	select {
	case <-secondEntered:
		enteredBeforeRelease = true
	case <-time.After(150 * time.Millisecond):
	}
	close(releaseFirst)
	for range 2 {
		select {
		case err := <-results:
			if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent Update did not finish")
		}
	}
	if enteredBeforeRelease {
		t.Fatal("second Store entered its callback before the first Store released the mutation lock")
	}

	cfg, found, err := first.Load()
	if err != nil || !found {
		t.Fatalf("Load() = found %v, error %v", found, err)
	}
	if _, ok := cfg.Profiles["first"]; !ok {
		t.Fatal("serialized updates lost first profile")
	}
	if _, ok := cfg.Profiles["second"]; !ok {
		t.Fatal("serialized updates lost second profile")
	}
}

func TestStoreUpdateFailsClosedWhenMigrationJournalExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	store := &Store{Path: path}
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	journal := filepath.Join(filepath.Dir(path), "migration-v1.pending.yaml")
	if err := WritePrivateFile(journal, []byte("pending\n")); err != nil {
		t.Fatalf("write pending journal: %v", err)
	}

	var called atomic.Bool
	err := store.Update(func(cfg *Config) error {
		called.Store(true)
		cfg.Profiles["must-not-commit"] = testStoreProfile("unused")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "pending migration journal") {
		t.Fatalf("Update() error = %v, want pending migration failure", err)
	}
	if called.Load() {
		t.Fatal("Update callback ran while a pending migration journal existed")
	}
	if err := store.Save(Default()); err == nil || !strings.Contains(err.Error(), "pending migration journal") {
		t.Fatalf("Save() error = %v, want pending migration failure", err)
	}
	cfg, found, loadErr := store.Load()
	if loadErr != nil || !found {
		t.Fatalf("Load() = found %v, error %v", found, loadErr)
	}
	if len(cfg.Profiles) != 0 {
		t.Fatalf("failed Update changed profiles: %#v", cfg.Profiles)
	}
}

func TestStoreUpdateUsesExplicitMigrationJournal(t *testing.T) {
	base := filepath.Join(t.TempDir(), "config")
	path := filepath.Join(base, "config.yaml")
	journal := filepath.Join(base, "custom.pending.yaml")
	store := &Store{Path: path, MigrationJournal: journal}
	if err := store.Save(Default()); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := WritePrivateFile(journal, []byte("pending\n")); err != nil {
		t.Fatalf("write pending journal: %v", err)
	}
	if err := store.Update(func(*Config) error { return nil }); err == nil {
		t.Fatal("Update() succeeded with the explicit pending migration journal")
	}
}

func TestLockedStoreHelperDoesNotReacquireMutationLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	store := &Store{Path: path}
	if err := store.withMutationLock(func(locked *lockedStore) error {
		cfg, found, err := locked.load()
		if err != nil {
			return err
		}
		if found {
			return errors.New("unexpected existing config")
		}
		cfg.Profiles["inside"] = testStoreProfile("inside-user")
		return locked.save(cfg)
	}); err != nil {
		t.Fatalf("withMutationLock() error = %v", err)
	}
	cfg, found, err := store.Load()
	if err != nil || !found || cfg.Profiles["inside"].Username != "inside-user" {
		t.Fatalf("Load() = %#v, found %v, error %v", cfg, found, err)
	}
}

func testStoreProfile(username string) Profile {
	return Profile{
		Username:   username,
		Credential: CredentialRef{Provider: ProviderPrompt},
	}
}
