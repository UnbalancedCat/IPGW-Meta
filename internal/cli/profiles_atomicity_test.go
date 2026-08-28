package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
	"github.com/UnbalancedCat/ipgw-meta/internal/config"
	keyring "github.com/zalando/go-keyring"
)

const profilePasswordCanary = "PROFILE-PASSWORD-CANARY-MUST-NOT-LEAK"

func TestProfileAddCommitFailureRemovesFreshKeyringCredential(t *testing.T) {
	configPath := t.TempDir() + string(os.PathSeparator) + "config.yaml"
	prepareProfileConfig(t, configPath)
	backend := newProfileAtomicKeyring(func() error {
		// Store.Update has already loaded the destination when keyring Set runs.
		// Turning the destination into a directory deterministically makes
		// the subsequent atomic config commit fail on every supported platform.
		if err := os.Remove(configPath); err != nil {
			return err
		}
		return os.Mkdir(configPath, 0o700)
	})
	var stdout, stderr bytes.Buffer
	runtime := commandRuntime{
		store:           &config.Store{Path: configPath},
		render:          renderer{mode: outputHuman, out: &stdout, err: &stderr},
		isTTY:           true,
		providerOptions: config.ProviderOptions{Keyring: backend},
	}
	promptCalls := 0
	exit := runtime.profileSaveWithPrompt(context.Background(), []string{
		"synthetic", "--username", "synthetic-user",
	}, "profile.add", func(context.Context, string, config.ProviderOptions) (string, error) {
		promptCalls++
		return profilePasswordCanary, nil
	})

	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}
	if promptCalls != 1 || backend.sets != 1 || backend.deletes != 1 {
		t.Fatalf("prompt=%d sets=%d deletes=%d, want 1/1/1", promptCalls, backend.sets, backend.deletes)
	}
	if len(backend.values) != 0 {
		t.Fatalf("fresh keyring credential survived failed config commit: entries=%d", len(backend.values))
	}
	if stdout.Len() != 0 || stderr.String() != "Error: configuration error\n" {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), profilePasswordCanary) || strings.Contains(stderr.String(), profilePasswordCanary) {
		t.Fatal("profile password leaked through CLI output")
	}
}

func TestProfileAddCleanupFailureReturnsFixedRedactedError(t *testing.T) {
	configPath := t.TempDir() + string(os.PathSeparator) + "config.yaml"
	prepareProfileConfig(t, configPath)
	backend := newProfileAtomicKeyring(func() error {
		if err := os.Remove(configPath); err != nil {
			return err
		}
		return os.Mkdir(configPath, 0o700)
	})
	backend.deleteErr = errors.New("BACKEND-DELETE-ERROR " + profilePasswordCanary)
	runtime := commandRuntime{
		store:           &config.Store{Path: configPath},
		providerOptions: config.ProviderOptions{Keyring: backend},
	}
	profile := config.Profile{
		Username: "synthetic-user",
		Credential: config.CredentialRef{
			Provider:  config.ProviderKeyring,
			Reference: "profile-synthetic-reference",
		},
		Switch: config.SwitchRefuse,
	}

	_, err := runtime.commitProfileAddition("synthetic", profile, true, profilePasswordCanary)
	if !errors.Is(err, errKeyringCleanupIncomplete) {
		t.Fatalf("error = %v, want fixed cleanup-incomplete error", err)
	}
	if err.Error() != errKeyringCleanupIncomplete.Error() {
		t.Fatalf("error = %q, want %q", err, errKeyringCleanupIncomplete)
	}
	for _, forbidden := range []string{profilePasswordCanary, "BACKEND-DELETE-ERROR", profile.Credential.Reference} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("cleanup error leaked %q: %q", forbidden, err)
		}
	}
	if backend.sets != 1 || backend.deletes != 1 || len(backend.values) != 1 {
		t.Fatalf("sets=%d deletes=%d entries=%d, want 1/1/1", backend.sets, backend.deletes, len(backend.values))
	}

	var stdout, stderr bytes.Buffer
	render := renderer{mode: outputJSON, out: &stdout, err: &stderr}
	exit := render.failure("profile.add", configFailure(err))
	if exit != 2 {
		t.Fatalf("JSON exit = %d, want 2", exit)
	}
	if stderr.Len() != 0 {
		t.Fatalf("JSON stderr = %q, want empty", stderr.String())
	}
	for _, forbidden := range []string{profilePasswordCanary, "BACKEND-DELETE-ERROR", profile.Credential.Reference} {
		if strings.Contains(stdout.String(), forbidden) {
			t.Fatalf("JSON output leaked %q: %q", forbidden, stdout.String())
		}
	}
	envelope := decodeSingleEnvelope(t, stdout.Bytes())
	assertEnvelopeXOR(t, envelope, false)
	wiredError := objectField(t, envelope, "error")
	if wiredError["code"] != string(ipgw.CodeConfig) || wiredError["message"] != "configuration error" {
		t.Fatalf("error envelope = %#v", wiredError)
	}
}

func TestProfileAddPublishedConfigErrorRetainsReferencedCredential(t *testing.T) {
	backend := newProfileAtomicKeyring(nil)
	profile := config.Profile{
		Username: "synthetic-user",
		Credential: config.CredentialRef{
			Provider:  config.ProviderKeyring,
			Reference: "profile-published-config",
		},
		Switch: config.SwitchRefuse,
	}
	backend.values[profile.Credential.Reference] = profilePasswordCanary
	runtime := commandRuntime{providerOptions: config.ProviderOptions{Keyring: backend}}
	err := runtime.resolveProfileCommitError(profile, true, config.ErrConfigPublishedDurabilityUnknown)
	if !errors.Is(err, config.ErrConfigPublishedDurabilityUnknown) {
		t.Fatalf("commit error = %v", err)
	}
	if backend.deletes != 0 || backend.values[profile.Credential.Reference] != profilePasswordCanary {
		t.Fatalf("published config compensation deleted its referenced credential: deletes=%d", backend.deletes)
	}
}

func prepareProfileConfig(t *testing.T, path string) {
	t.Helper()
	const emptyConfig = "schema_version: 1\nprofiles: {}\n"
	if err := config.WritePrivateFile(path, []byte(emptyConfig)); err != nil {
		t.Fatalf("prepare private config: %v", err)
	}
}

type profileAtomicKeyring struct {
	values    map[string]string
	afterSet  func() error
	deleteErr error
	sets      int
	deletes   int
}

func newProfileAtomicKeyring(afterSet func() error) *profileAtomicKeyring {
	return &profileAtomicKeyring{values: make(map[string]string), afterSet: afterSet}
}

func (b *profileAtomicKeyring) Get(_ string, user string) (string, error) {
	value, ok := b.values[user]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}

func (b *profileAtomicKeyring) Set(_ string, user, password string) error {
	b.sets++
	b.values[user] = password
	if b.afterSet != nil {
		return b.afterSet()
	}
	return nil
}

func (b *profileAtomicKeyring) Delete(_ string, user string) error {
	b.deletes++
	if b.deleteErr != nil {
		return b.deleteErr
	}
	delete(b.values, user)
	return nil
}
