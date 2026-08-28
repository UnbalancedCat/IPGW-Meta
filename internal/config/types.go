package config

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

const SchemaVersion = 1

type CredentialProvider string

const (
	ProviderKeyring CredentialProvider = "keyring"
	ProviderEnv     CredentialProvider = "env"
	ProviderFile    CredentialProvider = "file"
	ProviderPrompt  CredentialProvider = "prompt"
)

type SwitchPolicy string

const (
	SwitchRefuse        SwitchPolicy = "refuse"
	SwitchLogoutCurrent SwitchPolicy = "logout-current"
)

// Config is intentionally secret-free. Credential.Reference identifies a
// provider-owned secret; it must never contain the secret itself.
type Config struct {
	SchemaVersion  int                `yaml:"schema_version" json:"schema_version"`
	DefaultProfile string             `yaml:"default_profile,omitempty" json:"default_profile,omitempty"`
	Profiles       map[string]Profile `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

type Profile struct {
	Username   string        `yaml:"username" json:"username"`
	Credential CredentialRef `yaml:"credential" json:"credential"`
	Network    Network       `yaml:"network,omitempty" json:"network,omitempty"`
	Switch     SwitchPolicy  `yaml:"switch,omitempty" json:"switch,omitempty"`
}

type CredentialRef struct {
	Provider  CredentialProvider `yaml:"provider" json:"provider"`
	Reference string             `yaml:"reference,omitempty" json:"reference,omitempty"`
}

type Network struct {
	BindIP string `yaml:"bind_ip,omitempty" json:"bind_ip,omitempty"`
}

func Default() Config {
	return Config{SchemaVersion: SchemaVersion, Profiles: make(map[string]Profile)}
}

var (
	profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	envNamePattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func (c Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported config schema_version %d (expected %d)", c.SchemaVersion, SchemaVersion)
	}
	if c.DefaultProfile != "" {
		if _, ok := c.Profiles[c.DefaultProfile]; !ok {
			return fmt.Errorf("default profile %q does not exist", c.DefaultProfile)
		}
	}
	for name, profile := range c.Profiles {
		if !profileNamePattern.MatchString(name) {
			return fmt.Errorf("invalid profile name %q", name)
		}
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
	}
	return nil
}

func (p Profile) Validate() error {
	if p.Username == "" {
		return fmt.Errorf("username is required")
	}
	if strings.TrimSpace(p.Username) != p.Username {
		return fmt.Errorf("username must not have leading or trailing whitespace")
	}
	if err := p.Credential.Validate(); err != nil {
		return err
	}
	if p.Switch != "" && p.Switch != SwitchRefuse && p.Switch != SwitchLogoutCurrent {
		return fmt.Errorf("invalid switch policy %q", p.Switch)
	}
	if p.Network.BindIP != "" {
		ip, err := netip.ParseAddr(p.Network.BindIP)
		if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() {
			return fmt.Errorf("bind_ip must be a usable IPv4 address")
		}
	}
	return nil
}

func (r CredentialRef) Validate() error {
	switch r.Provider {
	case ProviderEnv:
		if !envNamePattern.MatchString(r.Reference) {
			return fmt.Errorf("env credential reference must be an environment variable name")
		}
	case ProviderFile, ProviderKeyring:
		if strings.TrimSpace(r.Reference) == "" {
			return fmt.Errorf("%s credential reference is required", r.Provider)
		}
	case ProviderPrompt:
		if r.Reference != "" {
			return fmt.Errorf("prompt credential must not have a reference")
		}
	default:
		return fmt.Errorf("unsupported credential provider %q", r.Provider)
	}
	return nil
}

func (c Config) Profile(name string) (Profile, string, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" {
		return Profile{}, "", fmt.Errorf("no default profile is configured")
	}
	profile, ok := c.Profiles[name]
	if !ok {
		return Profile{}, "", fmt.Errorf("profile %q does not exist", name)
	}
	return profile, name, nil
}
