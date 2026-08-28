package config

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

const freshKeyringSecretCanary = "fresh-keyring-secret-canary"

func TestNewOpaqueKeyringReference(t *testing.T) {
	first, err := NewOpaqueKeyringReference("profile-")
	if err != nil {
		t.Fatalf("NewOpaqueKeyringReference() error = %v", err)
	}
	second, err := NewOpaqueKeyringReference("profile-")
	if err != nil {
		t.Fatalf("NewOpaqueKeyringReference() second error = %v", err)
	}
	if first == second {
		t.Fatal("opaque keyring references unexpectedly collided")
	}
	if !strings.HasPrefix(first, "profile-") {
		t.Fatalf("reference %q has the wrong prefix", first)
	}
	suffix := strings.TrimPrefix(first, "profile-")
	if len(suffix) != 32 {
		t.Fatalf("opaque suffix length = %d, want 32", len(suffix))
	}
	if _, err := hex.DecodeString(suffix); err != nil {
		t.Fatalf("opaque suffix is not lowercase hexadecimal: %v", err)
	}
	if suffix != strings.ToLower(suffix) {
		t.Fatalf("opaque suffix %q is not lowercase", suffix)
	}
}

func TestNewOpaqueKeyringReferenceRejectsUnsafePrefix(t *testing.T) {
	for _, prefix := range []string{"", "Profile-", "profile_", "profile  ", "profile/"} {
		if reference, err := NewOpaqueKeyringReference(prefix); err == nil {
			t.Errorf("NewOpaqueKeyringReference(%q) = %q, want error", prefix, reference)
		}
	}
}

func TestSetFreshKeyringPasswordStoresAndVerifies(t *testing.T) {
	backend := newFreshKeyringBackend()
	ref := CredentialRef{Provider: ProviderKeyring, Reference: "profile-0123456789abcdef0123456789abcdef"}
	if err := SetFreshKeyringPassword(ref, freshKeyringSecretCanary, ProviderOptions{Keyring: backend}); err != nil {
		t.Fatalf("SetFreshKeyringPassword() error = %v", err)
	}
	if backend.sets != 1 || backend.deletes != 0 {
		t.Fatalf("backend effects = sets %d, deletes %d; want 1, 0", backend.sets, backend.deletes)
	}
	if got := backend.values[ref.Reference]; got != freshKeyringSecretCanary {
		t.Fatalf("stored password = %q, want canary", got)
	}
}

func TestSetFreshKeyringPasswordNeverOverwritesOccupiedReference(t *testing.T) {
	backend := newFreshKeyringBackend()
	ref := CredentialRef{Provider: ProviderKeyring, Reference: "profile-occupied"}
	backend.values[ref.Reference] = "existing-value"
	err := SetFreshKeyringPassword(ref, freshKeyringSecretCanary, ProviderOptions{Keyring: backend})
	if err == nil {
		t.Fatal("SetFreshKeyringPassword() accepted an occupied reference")
	}
	if backend.sets != 0 || backend.deletes != 0 {
		t.Fatalf("occupied reference caused effects: sets %d, deletes %d", backend.sets, backend.deletes)
	}
	if got := backend.values[ref.Reference]; got != "existing-value" {
		t.Fatalf("occupied value changed to %q", got)
	}
}

func TestSetFreshKeyringPasswordRedactsBackendErrors(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*freshKeyringBackend)
	}{
		{
			name: "preflight",
			configure: func(backend *freshKeyringBackend) {
				backend.getErr = errors.New("backend exposed " + freshKeyringSecretCanary)
			},
		},
		{
			name: "set",
			configure: func(backend *freshKeyringBackend) {
				backend.setErr = errors.New("backend exposed " + freshKeyringSecretCanary)
			},
		},
		{
			name: "readback",
			configure: func(backend *freshKeyringBackend) {
				backend.readbackErr = errors.New("backend exposed " + freshKeyringSecretCanary)
			},
		},
		{
			name: "cleanup",
			configure: func(backend *freshKeyringBackend) {
				backend.corruptSet = true
				backend.deleteErr = errors.New("backend exposed " + freshKeyringSecretCanary)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := newFreshKeyringBackend()
			test.configure(backend)
			ref := CredentialRef{Provider: ProviderKeyring, Reference: "profile-redaction"}
			err := SetFreshKeyringPassword(ref, freshKeyringSecretCanary, ProviderOptions{Keyring: backend})
			if err == nil {
				t.Fatal("SetFreshKeyringPassword() unexpectedly succeeded")
			}
			if strings.Contains(err.Error(), freshKeyringSecretCanary) {
				t.Fatalf("error leaked credential canary: %v", err)
			}
		})
	}
}

func TestSetFreshKeyringPasswordRemovesUnverifiedValue(t *testing.T) {
	backend := newFreshKeyringBackend()
	backend.corruptSet = true
	ref := CredentialRef{Provider: ProviderKeyring, Reference: "profile-readback"}
	err := SetFreshKeyringPassword(ref, freshKeyringSecretCanary, ProviderOptions{Keyring: backend})
	if err == nil {
		t.Fatal("SetFreshKeyringPassword() accepted a readback mismatch")
	}
	if backend.sets != 1 || backend.deletes != 1 {
		t.Fatalf("backend effects = sets %d, deletes %d; want 1, 1", backend.sets, backend.deletes)
	}
	if _, exists := backend.values[ref.Reference]; exists {
		t.Fatal("unverified credential remained in the keyring")
	}
}

func TestSetFreshKeyringPasswordCompensatesWriteThenError(t *testing.T) {
	backend := newFreshKeyringBackend()
	backend.writeBeforeSetError = true
	backend.setErr = errors.New("backend exposed " + freshKeyringSecretCanary)
	ref := CredentialRef{Provider: ProviderKeyring, Reference: "profile-uncertain-set"}
	err := SetFreshKeyringPassword(ref, freshKeyringSecretCanary, ProviderOptions{Keyring: backend})
	if err == nil {
		t.Fatal("SetFreshKeyringPassword() accepted an uncertain keyring write")
	}
	if backend.sets != 1 || backend.deletes != 1 {
		t.Fatalf("backend effects = sets %d, deletes %d; want 1, 1", backend.sets, backend.deletes)
	}
	if _, exists := backend.values[ref.Reference]; exists {
		t.Fatal("write-then-error credential remained in the keyring")
	}
	if strings.Contains(err.Error(), freshKeyringSecretCanary) {
		t.Fatalf("uncertain-write error leaked credential canary: %v", err)
	}
}

func TestSetFreshKeyringPasswordReportsUncertainWriteCleanupFailure(t *testing.T) {
	backend := newFreshKeyringBackend()
	backend.writeBeforeSetError = true
	backend.setErr = errors.New("set exposed " + freshKeyringSecretCanary)
	backend.deleteErr = errors.New("delete exposed " + freshKeyringSecretCanary)
	ref := CredentialRef{Provider: ProviderKeyring, Reference: "profile-uncertain-cleanup"}
	err := SetFreshKeyringPassword(ref, freshKeyringSecretCanary, ProviderOptions{Keyring: backend})
	if err == nil || err.Error() != "keyring credential storage failed and cleanup is incomplete" {
		t.Fatalf("SetFreshKeyringPassword() error = %v", err)
	}
	if backend.sets != 1 || backend.deletes != 1 {
		t.Fatalf("backend effects = sets %d, deletes %d; want 1, 1", backend.sets, backend.deletes)
	}
	for _, forbidden := range []string{freshKeyringSecretCanary, "set exposed", "delete exposed", ref.Reference} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("cleanup failure leaked %q: %v", forbidden, err)
		}
	}
}

type freshKeyringBackend struct {
	values              map[string]string
	getErr              error
	setErr              error
	readbackErr         error
	deleteErr           error
	corruptSet          bool
	writeBeforeSetError bool
	sets                int
	deletes             int
}

func newFreshKeyringBackend() *freshKeyringBackend {
	return &freshKeyringBackend{values: make(map[string]string)}
}

func (f *freshKeyringBackend) Get(_ string, user string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	if f.sets > 0 && f.readbackErr != nil {
		return "", f.readbackErr
	}
	value, exists := f.values[user]
	if !exists {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (f *freshKeyringBackend) Set(_ string, user, password string) error {
	f.sets++
	if f.setErr != nil && !f.writeBeforeSetError {
		return f.setErr
	}
	if f.corruptSet {
		password = "corrupted-value"
	}
	f.values[user] = password
	return f.setErr
}

func (f *freshKeyringBackend) Delete(_ string, user string) error {
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
