package app

import (
	"context"
	"strings"
	"testing"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
	"github.com/UnbalancedCat/ipgw-meta/internal/config"
)

type countingCredentialProvider struct {
	calls int
}

func (p *countingCredentialProvider) Credential(context.Context, ipgw.CredentialRequest) (ipgw.Credential, error) {
	p.calls++
	return ipgw.Credential{Password: "fixed-fixture-value"}, nil
}

type countingKeyring struct {
	gets int
}

func (k *countingKeyring) Get(string, string) (string, error) {
	k.gets++
	return "fixed-fixture-value", nil
}

func (*countingKeyring) Set(string, string, string) error { return nil }
func (*countingKeyring) Delete(string, string) error      { return nil }

func TestResolveLoginTargetCombinationMatrix(t *testing.T) {
	cfg := loginTargetTestConfig()
	supplied := &countingCredentialProvider{}

	tests := []struct {
		name           string
		cfg            *config.Config
		options        LoginOptions
		wantName       string
		wantUsername   string
		wantBind       string
		wantSwitch     config.SwitchPolicy
		wantCredential config.CredentialRef
		wantSupplied   bool
		wantErr        string
	}{
		{
			name: "credentials and username are completely direct",
			options: LoginOptions{
				RequestOptions:   RequestOptions{Profile: "missing"},
				ExpectedUsername: "direct-user",
				Credentials:      supplied,
			},
			wantUsername: "direct-user",
			wantSwitch:   config.SwitchRefuse,
			wantSupplied: true,
		},
		{
			name: "credentials without username use default profile metadata",
			cfg:  &cfg,
			options: LoginOptions{
				Credentials: supplied,
			},
			wantName:       "primary",
			wantUsername:   "alice",
			wantBind:       "192.0.2.10",
			wantSwitch:     config.SwitchLogoutCurrent,
			wantCredential: cfg.Profiles["primary"].Credential,
			wantSupplied:   true,
		},
		{
			name: "credentials without username use explicit profile metadata",
			cfg:  &cfg,
			options: LoginOptions{
				RequestOptions: RequestOptions{Profile: "secondary"},
				Credentials:    supplied,
			},
			wantName:       "secondary",
			wantUsername:   "bob",
			wantBind:       "198.51.100.20",
			wantSwitch:     config.SwitchRefuse,
			wantCredential: cfg.Profiles["secondary"].Credential,
			wantSupplied:   true,
		},
		{
			name: "username without credentials matches explicit profile exactly",
			cfg:  &cfg,
			options: LoginOptions{
				RequestOptions:   RequestOptions{Profile: "secondary"},
				ExpectedUsername: "bob",
			},
			wantName:       "secondary",
			wantUsername:   "bob",
			wantBind:       "198.51.100.20",
			wantSwitch:     config.SwitchRefuse,
			wantCredential: cfg.Profiles["secondary"].Credential,
		},
		{
			name: "username without credentials rejects explicit mismatch",
			cfg:  &cfg,
			options: LoginOptions{
				RequestOptions:   RequestOptions{Profile: "primary"},
				ExpectedUsername: "bob",
			},
			wantErr: "not the explicitly requested username",
		},
		{
			name: "username without credentials prefers matching default",
			cfg:  &cfg,
			options: LoginOptions{
				ExpectedUsername: "alice",
			},
			wantName:       "primary",
			wantUsername:   "alice",
			wantBind:       "192.0.2.10",
			wantSwitch:     config.SwitchLogoutCurrent,
			wantCredential: cfg.Profiles["primary"].Credential,
		},
		{
			name: "username without credentials finds unique non-default profile",
			cfg:  &cfg,
			options: LoginOptions{
				ExpectedUsername: "bob",
			},
			wantName:       "secondary",
			wantUsername:   "bob",
			wantBind:       "198.51.100.20",
			wantSwitch:     config.SwitchRefuse,
			wantCredential: cfg.Profiles["secondary"].Credential,
		},
		{
			name:         "no direct inputs use default profile",
			cfg:          &cfg,
			wantName:     "primary",
			wantUsername: "alice",
			wantBind:     "192.0.2.10",
			wantSwitch:   config.SwitchLogoutCurrent,
			wantCredential: config.CredentialRef{
				Provider: config.ProviderEnv, Reference: "IPGW_PRIMARY_PASSWORD",
			},
		},
		{
			name: "no direct inputs use explicit profile",
			cfg:  &cfg,
			options: LoginOptions{
				RequestOptions: RequestOptions{Profile: "secondary"},
			},
			wantName:       "secondary",
			wantUsername:   "bob",
			wantBind:       "198.51.100.20",
			wantSwitch:     config.SwitchRefuse,
			wantCredential: cfg.Profiles["secondary"].Credential,
		},
		{
			name: "username without credentials requires a matching profile",
			cfg:  &cfg,
			options: LoginOptions{
				ExpectedUsername: "nobody",
			},
			wantErr: "no profile has username",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := resolveLoginTarget(test.cfg, test.options)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("resolveLoginTarget() error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveLoginTarget() error = %v", err)
			}
			if target.ProfileName != test.wantName {
				t.Fatalf("ProfileName = %q, want %q", target.ProfileName, test.wantName)
			}
			if target.Profile.Username != test.wantUsername {
				t.Fatalf("Username = %q, want %q", target.Profile.Username, test.wantUsername)
			}
			if target.Profile.Network.BindIP != test.wantBind {
				t.Fatalf("BindIP = %q, want %q", target.Profile.Network.BindIP, test.wantBind)
			}
			if target.Profile.Switch != test.wantSwitch {
				t.Fatalf("Switch = %q, want %q", target.Profile.Switch, test.wantSwitch)
			}
			if target.Profile.Credential != test.wantCredential {
				t.Fatalf("Credential = %#v, want %#v", target.Profile.Credential, test.wantCredential)
			}
			if got := target.Credentials == supplied; got != test.wantSupplied {
				t.Fatalf("supplied provider selected = %t, want %t", got, test.wantSupplied)
			}
		})
	}

	if supplied.calls != 0 {
		t.Fatalf("resolution invoked supplied credentials %d time(s)", supplied.calls)
	}
}

func TestResolveLoginTargetDuplicateUsernameIsDeterministic(t *testing.T) {
	cfg := loginTargetTestConfig()
	cfg.Profiles["z-last"] = profileFixture("shared", "203.0.113.30", config.SwitchRefuse, "IPGW_Z_PASSWORD")
	cfg.Profiles["a-first"] = profileFixture("shared", "203.0.113.31", config.SwitchRefuse, "IPGW_A_PASSWORD")

	for i := 0; i < 32; i++ {
		_, err := resolveLoginTarget(&cfg, LoginOptions{ExpectedUsername: "shared"})
		if err == nil {
			t.Fatal("resolveLoginTarget() accepted duplicate usernames")
		}
		if !strings.Contains(err.Error(), "a-first, z-last") {
			t.Fatalf("duplicate error is not deterministic: %v", err)
		}
	}

	cfg.DefaultProfile = "z-last"
	target, err := resolveLoginTarget(&cfg, LoginOptions{ExpectedUsername: "shared"})
	if err != nil {
		t.Fatalf("matching default profile should resolve ambiguity: %v", err)
	}
	if target.ProfileName != "z-last" {
		t.Fatalf("ProfileName = %q, want matching default profile", target.ProfileName)
	}
}

func TestResolveLoginTargetUsesExactExplicitUsername(t *testing.T) {
	cfg := loginTargetTestConfig()
	_, err := resolveLoginTarget(&cfg, LoginOptions{
		RequestOptions:   RequestOptions{Profile: "primary"},
		ExpectedUsername: "alice ",
	})
	if err == nil {
		t.Fatal("resolveLoginTarget() accepted a non-exact username")
	}
}

func TestLazyPasswordCredentialProviderDoesNotTouchDestinationEarly(t *testing.T) {
	keyring := &countingKeyring{}
	provider := lazyPasswordCredentialProvider(
		config.CredentialRef{Provider: config.ProviderKeyring, Reference: "fixture-account"},
		config.ProviderOptions{Keyring: keyring},
	)
	if keyring.gets != 0 {
		t.Fatalf("provider construction read keyring %d time(s)", keyring.gets)
	}

	credential, err := provider.Credential(context.Background(), ipgw.CredentialRequest{Username: "alice"})
	if err != nil {
		t.Fatalf("Credential() error = %v", err)
	}
	if keyring.gets != 1 {
		t.Fatalf("Credential() read keyring %d time(s), want 1", keyring.gets)
	}
	if credential.Password != "fixed-fixture-value" {
		t.Fatal("Credential() did not return the fixed fixture value")
	}
}

func TestTypedNilCredentialProviderIsNotDirect(t *testing.T) {
	var typedNil *countingCredentialProvider
	if credentialProviderPresent(typedNil) {
		t.Fatal("typed-nil credential provider was treated as present")
	}
	if !loginRequiresConfig(LoginOptions{ExpectedUsername: "alice", Credentials: typedNil}) {
		t.Fatal("typed-nil credential provider bypassed profile resolution")
	}
}

func loginTargetTestConfig() config.Config {
	cfg := config.Default()
	cfg.DefaultProfile = "primary"
	cfg.Profiles["primary"] = profileFixture("alice", "192.0.2.10", config.SwitchLogoutCurrent, "IPGW_PRIMARY_PASSWORD")
	cfg.Profiles["secondary"] = profileFixture("bob", "198.51.100.20", config.SwitchRefuse, "IPGW_SECONDARY_PASSWORD")
	return cfg
}

func profileFixture(username, bindIP string, switchPolicy config.SwitchPolicy, envReference string) config.Profile {
	return config.Profile{
		Username: username,
		Credential: config.CredentialRef{
			Provider:  config.ProviderEnv,
			Reference: envReference,
		},
		Network: config.Network{BindIP: bindIP},
		Switch:  switchPolicy,
	}
}
