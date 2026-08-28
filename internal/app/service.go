package app

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
	"github.com/UnbalancedCat/ipgw-meta/internal/config"
)

type RequestOptions struct {
	Profile string
	BindIP  string
}

type LoginOptions struct {
	RequestOptions
	Switch           bool
	Method           ipgw.AuthMethod
	Interactions     ipgw.InteractionHandler
	ExpectedUsername string
	Credentials      ipgw.CredentialProvider
}

type Service struct {
	Paths  config.Paths
	Store  *config.Store
	Input  *os.File
	Output io.Writer
}

type resolvedLoginTarget struct {
	ProfileName string
	Profile     config.Profile
	Credentials ipgw.CredentialProvider
}

func New(paths config.Paths, input *os.File, output io.Writer) *Service {
	return &Service{
		Paths: paths,
		Store: &config.Store{Path: paths.ConfigFile, MigrationJournal: paths.MigrationJournal},
		Input: input, Output: output,
	}
}

func (s *Service) Status(ctx context.Context, options RequestOptions) (ipgw.Status, error) {
	profile, _, err := s.optionalProfile(options.Profile)
	if err != nil {
		return ipgw.Status{}, configError(err)
	}
	client, err := s.client(options.BindIP, profile.Network.BindIP)
	if err != nil {
		return ipgw.Status{}, configError(err)
	}
	return client.Status(ctx)
}

func (s *Service) Login(ctx context.Context, options LoginOptions) (ipgw.LoginResult, error) {
	var cfg *config.Config
	if loginRequiresConfig(options) {
		loaded, _, err := s.Store.Load()
		if err != nil {
			return ipgw.LoginResult{}, configError(err)
		}
		cfg = &loaded
	}
	target, err := resolveLoginTarget(cfg, options)
	if err != nil {
		return ipgw.LoginResult{}, configError(err)
	}
	profile := target.Profile
	client, err := s.client(options.BindIP, profile.Network.BindIP)
	if err != nil {
		return ipgw.LoginResult{}, configError(err)
	}
	switchPolicy := ipgw.SwitchRefuse
	if options.Switch || profile.Switch == config.SwitchLogoutCurrent {
		switchPolicy = ipgw.SwitchLogoutExisting
	}
	method := options.Method
	if method == "" {
		method = ipgw.AuthMethodPassword
	}
	credentials := target.Credentials
	if method == ipgw.AuthMethodPassword {
		if !credentialProviderPresent(credentials) {
			credentials = lazyPasswordCredentialProvider(profile.Credential, config.ProviderOptions{
				BaseDir: filepath.Dir(s.Paths.ConfigFile), Input: s.Input, Output: s.Output,
			})
		}
	}
	return client.Login(ctx, ipgw.LoginRequest{
		Method: method, ExpectedUsername: profile.Username,
		Credentials: credentials, Switch: switchPolicy, Interactions: options.Interactions,
	})
}

func loginRequiresConfig(options LoginOptions) bool {
	return !credentialProviderPresent(options.Credentials) || options.ExpectedUsername == ""
}

// resolveLoginTarget is deliberately pure: it selects profile metadata and a
// provider reference without invoking a credential provider. This preserves
// the SDK guarantee that an already-online or conflicting session is resolved
// before any credential is read.
func resolveLoginTarget(cfg *config.Config, options LoginOptions) (resolvedLoginTarget, error) {
	if !loginRequiresConfig(options) {
		return resolvedLoginTarget{
			Profile: config.Profile{
				Username: options.ExpectedUsername,
				Switch:   config.SwitchRefuse,
			},
			Credentials: options.Credentials,
		}, nil
	}
	if cfg == nil {
		return resolvedLoginTarget{}, fmt.Errorf("login profile configuration is required")
	}

	profile, name, err := resolveConfiguredLoginProfile(*cfg, options.Profile, options.ExpectedUsername)
	if err != nil {
		return resolvedLoginTarget{}, err
	}
	return resolvedLoginTarget{ProfileName: name, Profile: profile, Credentials: options.Credentials}, nil
}

func resolveConfiguredLoginProfile(cfg config.Config, explicitProfile, expectedUsername string) (config.Profile, string, error) {
	if explicitProfile != "" {
		profile, name, err := cfg.Profile(explicitProfile)
		if err != nil {
			return config.Profile{}, "", err
		}
		if expectedUsername != "" && profile.Username != expectedUsername {
			return config.Profile{}, "", fmt.Errorf(
				"profile %q has username %q, not the explicitly requested username %q",
				name, profile.Username, expectedUsername,
			)
		}
		return profile, name, nil
	}

	if expectedUsername == "" {
		return cfg.Profile("")
	}

	if cfg.DefaultProfile != "" {
		profile, name, err := cfg.Profile(cfg.DefaultProfile)
		if err != nil {
			return config.Profile{}, "", err
		}
		if profile.Username == expectedUsername {
			return profile, name, nil
		}
	}

	var matches []string
	for name, profile := range cfg.Profiles {
		if profile.Username == expectedUsername {
			matches = append(matches, name)
		}
	}
	sort.Strings(matches)
	switch len(matches) {
	case 0:
		return config.Profile{}, "", fmt.Errorf("no profile has username %q", expectedUsername)
	case 1:
		name := matches[0]
		return cfg.Profiles[name], name, nil
	default:
		return config.Profile{}, "", fmt.Errorf(
			"username %q matches multiple profiles: %s; select one explicitly",
			expectedUsername, strings.Join(matches, ", "),
		)
	}
}

func credentialProviderPresent(provider ipgw.CredentialProvider) bool {
	if provider == nil {
		return false
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func lazyPasswordCredentialProvider(ref config.CredentialRef, options config.ProviderOptions) ipgw.CredentialProvider {
	return ipgw.CredentialProviderFunc(func(ctx context.Context, request ipgw.CredentialRequest) (ipgw.Credential, error) {
		provider, err := config.NewPasswordProvider(ref, options)
		if err != nil {
			return ipgw.Credential{}, configError(err)
		}
		password, err := provider.Password(ctx, request.Username)
		if err != nil {
			return ipgw.Credential{}, configError(err)
		}
		return ipgw.Credential{Password: password}, nil
	})
}

func (s *Service) Logout(ctx context.Context, options RequestOptions) (ipgw.LogoutResult, error) {
	profile, _, err := s.optionalProfile(options.Profile)
	if err != nil {
		return ipgw.LogoutResult{}, configError(err)
	}
	client, err := s.client(options.BindIP, profile.Network.BindIP)
	if err != nil {
		return ipgw.LogoutResult{}, configError(err)
	}
	return client.Logout(ctx)
}

func (s *Service) ListInterfaces(ctx context.Context) ([]ipgw.Interface, error) {
	client, err := s.client("", "")
	if err != nil {
		return nil, configError(err)
	}
	return client.ListInterfaces(ctx)
}

func (s *Service) optionalProfile(name string) (config.Profile, string, error) {
	cfg, found, err := s.Store.Load()
	if err != nil {
		return config.Profile{}, "", err
	}
	if !found || (name == "" && cfg.DefaultProfile == "") {
		if name != "" {
			return config.Profile{}, "", fmt.Errorf("profile %q does not exist", name)
		}
		return config.Profile{}, "", nil
	}
	return cfg.Profile(name)
}

func (s *Service) client(bindOverride, profileBind string) (*ipgw.Client, error) {
	bind := bindOverride
	if bind == "" {
		bind = profileBind
	}
	var options []ipgw.Option
	if bind != "" {
		ip, err := netip.ParseAddr(bind)
		if err != nil || !ip.Is4() {
			return nil, fmt.Errorf("bind IP %q is not a valid IPv4 address", bind)
		}
		options = append(options, ipgw.WithBindIP(ip))
	}
	return ipgw.NewClient(options...)
}

func configError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*ipgw.Error); ok {
		return err
	}
	return &ipgw.Error{Code: ipgw.CodeConfig, Message: err.Error(), Retryable: false, Cause: err}
}
