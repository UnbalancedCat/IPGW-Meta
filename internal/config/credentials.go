package config

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

const maxCredentialBytes = 16 << 10

var ErrUnsupportedProvider = errors.New("credential provider is unsupported")

type PasswordProvider interface {
	Password(context.Context, string) (string, error)
}

type PasswordProviderFunc func(context.Context, string) (string, error)

func (f PasswordProviderFunc) Password(ctx context.Context, username string) (string, error) {
	return f(ctx, username)
}

type ProviderOptions struct {
	BaseDir   string
	Input     *os.File
	Output    io.Writer
	LookupEnv func(string) (string, bool)
	Keyring   KeyringBackend
}

const KeyringService = "ipgw-meta"

type KeyringBackend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type systemKeyring struct{}

func (systemKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (systemKeyring) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (systemKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

func NewPasswordProvider(ref CredentialRef, options ProviderOptions) (PasswordProvider, error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	lookupEnv := options.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	switch ref.Provider {
	case ProviderEnv:
		return PasswordProviderFunc(func(ctx context.Context, _ string) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			value, ok := lookupEnv(ref.Reference)
			if !ok || value == "" {
				return "", fmt.Errorf("credential environment variable %s is unset or empty", ref.Reference)
			}
			return value, nil
		}), nil
	case ProviderFile:
		path := ref.Reference
		if !filepath.IsAbs(path) {
			path = filepath.Join(options.BaseDir, path)
		}
		return PasswordProviderFunc(func(ctx context.Context, _ string) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return readRestrictedPassword(path)
		}), nil
	case ProviderPrompt:
		if options.Input == nil || !terminalIsInteractive(options.Input) {
			return nil, fmt.Errorf("interactive prompt requires a terminal; use env, file, or keyring credentials")
		}
		out := options.Output
		if out == nil {
			out = io.Discard
		}
		return PasswordProviderFunc(func(ctx context.Context, username string) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			_, _ = fmt.Fprintf(out, "Password for %s: ", username)
			value, err := readTerminalPassword(options.Input)
			_, _ = fmt.Fprintln(out)
			if err != nil {
				return "", fmt.Errorf("read password: %w", err)
			}
			if len(value) == 0 {
				return "", fmt.Errorf("password is empty")
			}
			return string(value), nil
		}), nil
	case ProviderKeyring:
		backend := options.Keyring
		if backend == nil {
			backend = systemKeyring{}
		}
		return PasswordProviderFunc(func(ctx context.Context, _ string) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			password, err := backend.Get(KeyringService, ref.Reference)
			if err != nil {
				return "", fmt.Errorf("read keyring credential %q: %w", ref.Reference, err)
			}
			if password == "" {
				return "", fmt.Errorf("keyring credential %q is empty", ref.Reference)
			}
			return password, nil
		}), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, ref.Provider)
	}
}

func PromptPassword(ctx context.Context, username string, options ProviderOptions) (string, error) {
	provider, err := NewPasswordProvider(CredentialRef{Provider: ProviderPrompt}, options)
	if err != nil {
		return "", err
	}
	return provider.Password(ctx, username)
}

func SetKeyringPassword(ref CredentialRef, password string, options ProviderOptions) error {
	if ref.Provider != ProviderKeyring || ref.Reference == "" {
		return fmt.Errorf("a keyring credential reference is required")
	}
	if password == "" {
		return fmt.Errorf("password is empty")
	}
	backend := options.Keyring
	if backend == nil {
		backend = systemKeyring{}
	}
	if err := backend.Set(KeyringService, ref.Reference, password); err != nil {
		return fmt.Errorf("store keyring credential %q: %w", ref.Reference, err)
	}
	return nil
}

// NewOpaqueKeyringReference returns an unpredictable provider-owned name.
// Profile and migration code use fresh references so keyring Set, whose
// backend API is an upsert, cannot overwrite a predictable existing entry.
func NewOpaqueKeyringReference(prefix string) (string, error) {
	if prefix == "" || strings.Trim(prefix, "abcdefghijklmnopqrstuvwxyz-") != "" {
		return "", fmt.Errorf("invalid keyring reference prefix")
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate keyring reference: %w", err)
	}
	return prefix + hex.EncodeToString(random), nil
}

// SetFreshKeyringPassword provisions a new reference without intentionally
// overwriting an existing item. Backend errors are deliberately not wrapped:
// some platform keyring implementations include submitted values in errors.
func SetFreshKeyringPassword(ref CredentialRef, password string, options ProviderOptions) error {
	if ref.Provider != ProviderKeyring || ref.Validate() != nil {
		return fmt.Errorf("a keyring credential reference is required")
	}
	if password == "" {
		return fmt.Errorf("password is empty")
	}
	backend := options.Keyring
	if backend == nil {
		backend = systemKeyring{}
	}
	_, err := backend.Get(KeyringService, ref.Reference)
	switch {
	case err == nil:
		return fmt.Errorf("keyring credential reference is already occupied")
	case errors.Is(err, keyring.ErrNotFound):
		// Continue with the fresh, opaque reference.
	default:
		return fmt.Errorf("keyring is unavailable for credential provisioning")
	}
	if err := backend.Set(KeyringService, ref.Reference, password); err != nil {
		// A platform backend may persist the value and still report an error.
		// The reference was preflighted as fresh, so always compensate the
		// uncertain write before returning to the caller.
		if deleteErr := backend.Delete(KeyringService, ref.Reference); deleteErr != nil && !errors.Is(deleteErr, keyring.ErrNotFound) {
			return fmt.Errorf("keyring credential storage failed and cleanup is incomplete")
		}
		return fmt.Errorf("keyring credential could not be stored")
	}
	stored, err := backend.Get(KeyringService, ref.Reference)
	if err == nil && stored == password {
		return nil
	}
	if deleteErr := backend.Delete(KeyringService, ref.Reference); deleteErr != nil && !errors.Is(deleteErr, keyring.ErrNotFound) {
		return fmt.Errorf("keyring credential verification failed and cleanup is incomplete")
	}
	return fmt.Errorf("keyring credential verification failed")
}

func DeleteKeyringPassword(ref CredentialRef, options ProviderOptions) error {
	if ref.Provider != ProviderKeyring || ref.Reference == "" {
		return nil
	}
	backend := options.Keyring
	if backend == nil {
		backend = systemKeyring{}
	}
	if err := backend.Delete(KeyringService, ref.Reference); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete keyring credential %q: %w", ref.Reference, err)
	}
	return nil
}

func readRestrictedPassword(path string) (string, error) {
	f, err := openRestrictedPasswordFile(path)
	if err != nil {
		return "", fmt.Errorf("open credential file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxCredentialBytes+1))
	if err != nil {
		return "", fmt.Errorf("read credential file: %w", err)
	}
	if len(data) > maxCredentialBytes {
		return "", fmt.Errorf("credential file exceeds %d bytes", maxCredentialBytes)
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if value == "" {
		return "", fmt.Errorf("credential file is empty")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return "", fmt.Errorf("credential file contains a NUL byte")
	}
	return value, nil
}

func confirm(reader io.Reader, writer io.Writer, prompt string) (bool, error) {
	if reader == nil {
		return false, fmt.Errorf("confirmation input is unavailable")
	}
	if writer != nil {
		_, _ = fmt.Fprint(writer, prompt)
	}
	line, err := bufio.NewReader(reader).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
