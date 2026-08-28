package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/UnbalancedCat/ipgw-meta/internal/config"
)

var errKeyringCleanupIncomplete = errors.New("keyring credential cleanup is incomplete; manual cleanup is required")

type profilePasswordPrompt func(context.Context, string, config.ProviderOptions) (string, error)

type namedProfile struct {
	Name    string         `json:"name"`
	Default bool           `json:"default"`
	Profile config.Profile `json:"profile"`
}

func (r commandRuntime) runProfile(ctx context.Context, args []string) int {
	if hasPasswordArgument(args) {
		return r.render.failure("profile", passwordArgumentError())
	}
	if len(args) == 0 {
		return r.render.failure("profile", invalidArgument(fmt.Errorf("usage: profile <list|show|add|remove|migrate>")))
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return r.render.failure("profile.list", invalidArgument(fmt.Errorf("profile list takes no arguments")))
		}
		return r.profileList()
	case "show":
		if len(args) > 2 {
			return r.render.failure("profile.show", invalidArgument(fmt.Errorf("usage: profile show [NAME]")))
		}
		name := r.profile
		if len(args) == 2 {
			if strings.HasPrefix(args[1], "-") {
				return r.render.failure("profile.show", invalidArgument(fmt.Errorf("invalid profile argument")))
			}
			name = args[1]
		}
		return r.profileShow(name)
	case "add":
		return r.profileSave(ctx, args[1:], "profile.add")
	case "remove":
		if len(args) != 2 || strings.HasPrefix(args[1], "-") {
			return r.render.failure("profile.remove", invalidArgument(fmt.Errorf("usage: profile remove NAME")))
		}
		return r.profileRemove(args[1])
	case "migrate":
		return r.runMigrate(args[1:])
	default:
		return r.render.failure("profile", invalidArgument(fmt.Errorf("unknown profile command")))
	}
}

func (r commandRuntime) profileList() int {
	cfg, _, err := r.store.Load()
	if err != nil {
		return r.render.failure("profile.list", configFailure(err))
	}
	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]namedProfile, 0, len(names))
	for _, name := range names {
		profiles = append(profiles, namedProfile{Name: name, Default: name == cfg.DefaultProfile, Profile: cfg.Profiles[name]})
		if r.render.mode == outputHuman {
			marker := " "
			if name == cfg.DefaultProfile {
				marker = "*"
			}
			_, _ = fmt.Fprintf(r.render.out, "[%s] %s\t%s\t%s\n", marker, name, cfg.Profiles[name].Username, cfg.Profiles[name].Credential.Provider)
		}
	}
	return r.render.success("profile.list", map[string]any{"profiles": profiles})
}

func (r commandRuntime) profileShow(name string) int {
	cfg, _, err := r.store.Load()
	if err != nil {
		return r.render.failure("profile.show", configFailure(err))
	}
	profile, resolved, err := cfg.Profile(name)
	if err != nil {
		return r.render.failure("profile.show", configFailure(err))
	}
	wired := namedProfile{Name: resolved, Default: resolved == cfg.DefaultProfile, Profile: profile}
	if r.render.mode == outputHuman {
		_, _ = fmt.Fprintf(r.render.out, "Profile: %s\nUsername: %s\nCredential: %s:%s\nBind IP: %s\nSwitch: %s\n", resolved, profile.Username, profile.Credential.Provider, profile.Credential.Reference, profile.Network.BindIP, profile.Switch)
	}
	return r.render.success("profile.show", wired)
}

func (r commandRuntime) profileSave(ctx context.Context, args []string, command string) int {
	return r.profileSaveWithPrompt(ctx, args, command, config.PromptPassword)
}

func (r commandRuntime) profileSaveWithPrompt(ctx context.Context, args []string, command string, prompt profilePasswordPrompt) int {
	if hasPasswordArgument(args) {
		return r.render.failure(command, passwordArgumentError())
	}
	if len(args) == 0 {
		return r.render.failure(command, invalidArgument(fmt.Errorf("usage: profile add NAME --username USER --credential-provider PROVIDER")))
	}
	name := args[0]
	if strings.HasPrefix(name, "-") {
		return r.render.failure(command, invalidArgument(fmt.Errorf("invalid profile argument")))
	}
	values := map[string]string{}
	setDefault := false
	for i := 1; i < len(args); i++ {
		argument := args[i]
		if argument == "--default" {
			if setDefault {
				return r.render.failure(command, invalidArgument(fmt.Errorf("duplicate profile flag")))
			}
			setDefault = true
			continue
		}
		if !isProfileValueFlag(argument) {
			return r.render.failure(command, invalidArgument(fmt.Errorf("invalid profile argument")))
		}
		if _, duplicate := values[argument]; duplicate {
			return r.render.failure(command, invalidArgument(fmt.Errorf("duplicate profile flag")))
		}
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return r.render.failure(command, invalidArgument(fmt.Errorf("profile flag requires a value")))
		}
		values[argument] = args[i+1]
		i++
	}
	profile := config.Profile{
		Username:   values["--username"],
		Credential: config.CredentialRef{Provider: config.CredentialProvider(values["--credential-provider"]), Reference: values["--credential-ref"]},
		Network:    config.Network{BindIP: values["--network-bind-ip"]},
		Switch:     config.SwitchPolicy(values["--switch"]),
	}
	if profile.Credential.Provider == "" {
		profile.Credential.Provider = config.ProviderKeyring
	}
	if profile.Credential.Provider == config.ProviderKeyring && profile.Credential.Reference == "" {
		reference, err := config.NewOpaqueKeyringReference("profile-")
		if err != nil {
			return r.render.failure(command, configFailure(err))
		}
		profile.Credential.Reference = reference
	}
	if profile.Switch == "" {
		profile.Switch = config.SwitchRefuse
	}
	if err := profile.Validate(); err != nil {
		return r.render.failure(command, configFailure(err))
	}
	current, _, err := r.store.Load()
	if err != nil {
		return r.render.failure(command, configFailure(err))
	}
	if _, exists := current.Profiles[name]; exists {
		return r.render.failure(command, configFailure(fmt.Errorf("profile %q already exists", name)))
	}
	password := ""
	if profile.Credential.Provider == config.ProviderKeyring {
		if !r.isTTY || r.render.mode != outputHuman {
			return r.render.failure(command, configFailure(fmt.Errorf("keyring provisioning requires a human TTY; no password is accepted on the command line")))
		}
		if prompt == nil {
			return r.render.failure(command, configFailure(fmt.Errorf("keyring password prompt is unavailable")))
		}
		password, err = prompt(ctx, profile.Username, r.providerOptions)
		if err != nil {
			return r.render.failure(command, configFailure(err))
		}
	}
	savedAsDefault, err := r.commitProfileAddition(name, profile, setDefault, password)
	if err != nil {
		return r.render.failure(command, configFailure(err))
	}
	if r.render.mode == outputHuman {
		_, _ = fmt.Fprintf(r.render.out, "Saved profile %s (credentials remain in %s).\n", name, profile.Credential.Provider)
	}
	return r.render.success(command, namedProfile{Name: name, Default: savedAsDefault, Profile: profile})
}

func (r commandRuntime) commitProfileAddition(name string, profile config.Profile, setDefault bool, password string) (bool, error) {
	keyringStored := false
	savedAsDefault := false
	err := r.store.Update(func(cfg *config.Config) error {
		if cfg.Profiles == nil {
			cfg.Profiles = make(map[string]config.Profile)
		}
		if _, exists := cfg.Profiles[name]; exists {
			return fmt.Errorf("profile %q already exists", name)
		}
		if profile.Credential.Provider == config.ProviderKeyring {
			if err := config.SetFreshKeyringPassword(profile.Credential, password, r.providerOptions); err != nil {
				return err
			}
			keyringStored = true
		}
		cfg.Profiles[name] = profile
		if setDefault || cfg.DefaultProfile == "" {
			cfg.DefaultProfile = name
		}
		savedAsDefault = cfg.DefaultProfile == name
		return nil
	})
	if err != nil {
		return false, r.resolveProfileCommitError(profile, keyringStored, err)
	}
	return savedAsDefault, nil
}

func (r commandRuntime) resolveProfileCommitError(profile config.Profile, keyringStored bool, commitErr error) error {
	if !keyringStored || errors.Is(commitErr, config.ErrConfigPublishedDurabilityUnknown) {
		return commitErr
	}
	if cleanupErr := config.DeleteKeyringPassword(profile.Credential, r.providerOptions); cleanupErr != nil {
		return errKeyringCleanupIncomplete
	}
	return commitErr
}

func isProfileValueFlag(argument string) bool {
	switch argument {
	case "--username", "--credential-provider", "--credential-ref", "--network-bind-ip", "--switch":
		return true
	default:
		return false
	}
}

func (r commandRuntime) profileRemove(name string) int {
	if err := r.store.Update(func(cfg *config.Config) error {
		if _, ok := cfg.Profiles[name]; !ok {
			return fmt.Errorf("profile %q does not exist", name)
		}
		delete(cfg.Profiles, name)
		if cfg.DefaultProfile == name {
			cfg.DefaultProfile = ""
		}
		return nil
	}); err != nil {
		return r.render.failure("profile.remove", configFailure(err))
	}
	if r.render.mode == outputHuman {
		_, _ = fmt.Fprintf(r.render.out, "Removed profile %s; provider-owned credentials were not deleted.\n", name)
	}
	return r.render.success("profile.remove", map[string]string{"removed": name})
}
