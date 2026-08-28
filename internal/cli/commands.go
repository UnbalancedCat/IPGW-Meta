package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
	"github.com/UnbalancedCat/ipgw-meta/internal/app"
	"github.com/UnbalancedCat/ipgw-meta/internal/config"
	"github.com/mdp/qrterminal/v3"
)

type commandRuntime struct {
	gateway         Gateway
	store           *config.Store
	paths           config.Paths
	render          renderer
	input           *os.File
	isTTY           bool
	profile         string
	bindIP          string
	version         string
	providerOptions config.ProviderOptions
}

func (r commandRuntime) runStatus(ctx context.Context, noArgs bool) int {
	status, err := r.gateway.Status(ctx, app.RequestOptions{Profile: r.profile, BindIP: r.bindIP})
	if err != nil {
		return r.render.failure("status", err)
	}
	wired := toWireStatus(status)
	if r.render.mode == outputHuman {
		r.printStatus(wired)
		if noArgs {
			_, _ = fmt.Fprintln(r.render.out, "Next: configure a profile with 'ipgw-meta profile add ...', then run 'ipgw-meta login'.")
		}
	}
	return r.render.success("status", wired)
}

func (r commandRuntime) runLogin(ctx context.Context, args []string, legacy bool) int {
	switchExisting := false
	method := ipgw.AuthMethodPassword
	var legacyUsername, legacyPassword string
	usernameSet := false
	passwordSet := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-u" || arg == "--username":
			if !legacy {
				return r.render.failure("login", invalidArgument(fmt.Errorf("legacy username flags are only available in ipgw-legacy")))
			}
			if i+1 >= len(args) {
				return r.render.failure("login", invalidArgument(fmt.Errorf("username flag requires a value")))
			}
			i++
			legacyUsername = args[i]
			usernameSet = true
		case strings.HasPrefix(arg, "--username="):
			if !legacy {
				return r.render.failure("login", invalidArgument(fmt.Errorf("legacy username flags are only available in ipgw-legacy")))
			}
			legacyUsername = strings.TrimPrefix(arg, "--username=")
			usernameSet = true
		case legacy && strings.HasPrefix(arg, "-u") && !strings.HasPrefix(arg, "--") && len(arg) > 2:
			legacyUsername = strings.TrimPrefix(arg, "-u")
			usernameSet = true
		case arg == "-p" || arg == "--password":
			if !legacy {
				return r.render.failure("login", invalidArgument(fmt.Errorf("password flags are only available in ipgw-legacy")))
			}
			if i+1 >= len(args) {
				return r.render.failure("login", invalidArgument(fmt.Errorf("password flag requires a value")))
			}
			i++
			legacyPassword = args[i]
			passwordSet = true
		case strings.HasPrefix(arg, "--password="):
			if !legacy {
				return r.render.failure("login", invalidArgument(fmt.Errorf("password flags are only available in ipgw-legacy")))
			}
			legacyPassword = strings.TrimPrefix(arg, "--password=")
			passwordSet = true
		case legacy && strings.HasPrefix(arg, "-p") && !strings.HasPrefix(arg, "--") && len(arg) > 2:
			legacyPassword = strings.TrimPrefix(arg, "-p")
			passwordSet = true
		case arg == "--switch":
			switchExisting = true
		case arg == "--method" || arg == "--auth":
			if i+1 >= len(args) {
				return r.render.failure("login", invalidArgument(fmt.Errorf("--method requires password or qr")))
			}
			i++
			switch args[i] {
			case "password":
				method = ipgw.AuthMethodPassword
			case "qr", "terminal-qr":
				method = ipgw.AuthMethodTerminalQR
			default:
				return r.render.failure("login", invalidArgument(fmt.Errorf("unsupported auth method %q", args[i])))
			}
		default:
			return r.render.failure("login", invalidArgument(fmt.Errorf("unknown login argument %q", arg)))
		}
	}
	if usernameSet && legacyUsername == "" {
		return r.render.failure("login", invalidArgument(fmt.Errorf("username must not be empty")))
	}
	if passwordSet {
		_, _ = fmt.Fprintln(r.render.err, "Warning: --password may be retained in shell history. Migrate this account to keyring, env, file, or prompt credentials.")
	}
	if passwordSet && legacyPassword == "" {
		return r.render.failure("login", invalidArgument(fmt.Errorf("password must not be empty")))
	}
	if method != ipgw.AuthMethodPassword && passwordSet {
		return r.render.failure("login", invalidArgument(fmt.Errorf("legacy password flags cannot be combined with QR login")))
	}
	if method == ipgw.AuthMethodTerminalQR && (r.render.mode == outputJSON || !r.isTTY) {
		return r.render.failure("login", terminalQRInteractionRequired())
	}
	options := app.LoginOptions{
		RequestOptions: app.RequestOptions{Profile: r.profile, BindIP: r.bindIP},
		Switch:         switchExisting, Method: method,
	}
	if usernameSet {
		options.ExpectedUsername = legacyUsername
	}
	if passwordSet {
		password := legacyPassword
		options.Credentials = ipgw.CredentialProviderFunc(func(ctx context.Context, _ ipgw.CredentialRequest) (ipgw.Credential, error) {
			if err := ctx.Err(); err != nil {
				return ipgw.Credential{}, err
			}
			return ipgw.Credential{Password: password}, nil
		})
	}
	if method == ipgw.AuthMethodTerminalQR && r.isTTY && r.render.mode == outputHuman {
		options.Interactions = ipgw.InteractionHandlerFunc(func(ctx context.Context, prompt ipgw.QRCodePrompt) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(r.render.err, "请使用企业微信扫描下方一次性二维码；等待期间可按 Ctrl-C 取消。")
			if !prompt.ExpiresAt.IsZero() {
				_, _ = fmt.Fprintf(r.render.err, "二维码过期时间：%s\n", prompt.ExpiresAt.Local().Format("2006-01-02 15:04:05 MST"))
			}
			qrterminal.GenerateHalfBlock(prompt.Payload, qrterminal.L, r.render.err)
			return nil
		})
	}
	result, err := r.gateway.Login(ctx, options)
	if err != nil {
		return r.render.failure("login", err)
	}
	wired := toWireLogin(result)
	if r.render.mode == outputHuman {
		_, _ = fmt.Fprintf(r.render.out, "Login: %s\n", wired.Outcome)
		r.printStatus(wired.Status)
	}
	return r.render.success("login", wired)
}

func terminalQRInteractionRequired() error {
	return &ipgw.Error{
		Code:      ipgw.CodeInteractionRequired,
		Message:   "login requires human verification",
		Retryable: false,
		Details: ipgw.ErrorDetails{Interaction: &ipgw.InteractionDetails{
			Challenge:      ipgw.ChallengeQRApproval,
			OriginMethod:   ipgw.AuthMethodTerminalQR,
			Capability:     []ipgw.CapabilityStatus{ipgw.CapabilityObservedAnonymous, ipgw.CapabilitySyntheticCovered, ipgw.CapabilityLiveUnverified},
			SessionBinding: "cas_session",
			ResumeMode:     "retry_in_tty",
			TTYRequired:    true,
			HelpID:         "AUTH-QR-001",
		}},
	}
}

type scanResult struct {
	Interface wireInterface `json:"interface"`
	Status    *wireStatus   `json:"status,omitempty"`
	Error     *wireError    `json:"error,omitempty"`
}

func (r commandRuntime) runNetworkScan(ctx context.Context) int {
	if err := ctx.Err(); err != nil {
		return r.render.failure("network.scan", err)
	}
	interfaces, err := r.gateway.ListInterfaces(ctx)
	if err != nil {
		return r.render.failure("network.scan", err)
	}
	if err := ctx.Err(); err != nil {
		return r.render.failure("network.scan", err)
	}
	results := make([]scanResult, 0, len(interfaces))
	for _, iface := range interfaces {
		if err := ctx.Err(); err != nil {
			return r.render.failure("network.scan", err)
		}
		item := scanResult{Interface: wireInterface{Name: iface.Name, Index: iface.Index, IP: iface.IP.String()}}
		status, statusErr := r.gateway.Status(ctx, app.RequestOptions{Profile: r.profile, BindIP: iface.IP.String()})
		if errors.Is(statusErr, context.Canceled) || errors.Is(statusErr, context.DeadlineExceeded) {
			return r.render.failure("network.scan", statusErr)
		}
		if err := ctx.Err(); err != nil {
			return r.render.failure("network.scan", err)
		}
		if statusErr != nil {
			code, retryable, details := wireErrorOf(statusErr)
			item.Error = &wireError{Code: code, Message: wireErrorMessage(statusErr, code), Retryable: retryable, Details: details}
		} else {
			wired := toWireStatus(status)
			item.Status = &wired
		}
		results = append(results, item)
		if r.render.mode == outputHuman {
			if item.Error != nil {
				_, _ = fmt.Fprintf(r.render.out, "%d\t%s\t%s\terror=%s\n", iface.Index, iface.Name, iface.IP, item.Error.Code)
			} else {
				_, _ = fmt.Fprintf(r.render.out, "%d\t%s\t%s\t%s/%s\n", iface.Index, iface.Name, iface.IP, item.Status.Network, item.Status.Session)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return r.render.failure("network.scan", err)
	}
	return r.render.success("network.scan", map[string]any{"results": results})
}

func (r commandRuntime) runLogout(ctx context.Context) int {
	result, err := r.gateway.Logout(ctx, app.RequestOptions{Profile: r.profile, BindIP: r.bindIP})
	if err != nil {
		return r.render.failure("logout", err)
	}
	wired := toWireLogout(result)
	if r.render.mode == outputHuman {
		_, _ = fmt.Fprintf(r.render.out, "Logout: %s\n", wired.Outcome)
		r.printStatus(wired.Status)
	}
	return r.render.success("logout", wired)
}

func (r commandRuntime) runNetworkList(ctx context.Context) int {
	interfaces, err := r.gateway.ListInterfaces(ctx)
	if err != nil {
		return r.render.failure("network.list", err)
	}
	wired := toWireInterfaces(interfaces)
	if r.render.mode == outputHuman {
		if len(wired) == 0 {
			_, _ = fmt.Fprintln(r.render.out, "No usable IPv4 interfaces found.")
		}
		for _, iface := range wired {
			_, _ = fmt.Fprintf(r.render.out, "%d\t%s\t%s\n", iface.Index, iface.Name, iface.IP)
		}
	}
	return r.render.success("network.list", map[string]any{"interfaces": wired})
}

func (r commandRuntime) runMigrate(args []string) int {
	const command = "profile.migrate"
	request, err := parseMigrationArguments(args)
	if err != nil {
		return r.render.failure(command, invalidArgument(err))
	}
	applyOptions := config.MigrationApplyOptions{
		ToolVersion: r.version, ProviderOptions: r.providerOptions,
	}
	// Recovery is the first migration-state operation. In particular, a stale
	// marker must never bypass rollback or committed-transaction cleanup.
	if err := config.RecoverPendingMigration(r.paths, applyOptions); err != nil {
		return r.render.failure(command, configFailure(err))
	}
	cfg, _, err := r.store.Load()
	if err != nil {
		return r.render.failure(command, configFailure(err))
	}
	plan, err := config.BuildMigrationPlan(r.paths, cfg)
	if err != nil {
		return r.render.failure(command, configFailure(err))
	}
	defer plan.Close()
	failPlan := func(err error) int {
		return r.render.failureWithDetails(command, configFailure(err), wireErrorDetails{Migration: migrationReport(plan)})
	}
	_, alreadyApplied, err := config.MigrationAlreadyApplied(r.paths.MigrationMarker)
	if err != nil {
		return failPlan(err)
	}
	if alreadyApplied {
		// A marker is only a hint. ApplyMigrationWithOptions takes its verified
		// fast path and checks the current config digest and source stamps.
		result, err := config.ApplyMigrationWithOptions(r.paths, r.store, cfg, plan, applyOptions)
		if err != nil {
			return failPlan(err)
		}
		if r.render.mode == outputHuman {
			_, _ = fmt.Fprintln(r.render.out, "Migration was already applied and verified; no changes were made.")
		}
		return r.render.success(command, map[string]any{"applied": false, "already_applied": true, "result": result})
	}
	if r.render.mode == outputHuman {
		r.printMigrationPlan(plan)
	}
	if len(plan.Sources) == 0 {
		if len(request.credentials) != 0 {
			return r.render.failure(command, invalidArgument(fmt.Errorf("credential mappings do not match a migration profile")))
		}
		return r.render.success(command, map[string]any{"applied": false, "plan": plan})
	}
	interactive := r.isTTY && r.render.mode == outputHuman
	var reader *bufio.Reader
	if interactive {
		if r.input == nil {
			return r.render.failure(command, configFailure(fmt.Errorf("migration input is unavailable")))
		}
		reader = bufio.NewReader(r.input)
	}
	if len(plan.Conflicts) > 0 {
		if !interactive {
			return failPlan(fmt.Errorf("migration has unresolved conflicts; rerun in a TTY to choose replace or skip"))
		}
		if err := r.resolveMigrationConflicts(&plan, reader); err != nil {
			return failPlan(err)
		}
	}
	if !interactive {
		if !request.yes {
			return failPlan(fmt.Errorf("non-interactive migration requires --yes and explicit credential mappings"))
		}
		if err := validateMigrationCredentialDecisions(plan, request.credentials, true, true); err != nil {
			return failPlan(err)
		}
		if err := resolveMigrationCredentialDecisions(&plan, request.credentials); err != nil {
			return failPlan(err)
		}
	} else {
		if err := validateMigrationCredentialDecisions(plan, request.credentials, false, false); err != nil {
			return failPlan(err)
		}
		if err := resolveMigrationCredentialDecisions(&plan, request.credentials); err != nil {
			return failPlan(err)
		}
		if err := r.resolveInteractiveMigrationCredentials(&plan, reader); err != nil {
			return failPlan(err)
		}
		if r.render.mode == outputHuman {
			_, _ = fmt.Fprintln(r.render.out, "Final credential decisions:")
			r.printMigrationProfiles(plan)
		}
		if !request.yes {
			confirmed, err := r.confirmMigration(reader)
			if err != nil {
				return failPlan(err)
			}
			if !confirmed {
				return r.render.success(command, map[string]any{"applied": false, "plan": plan})
			}
		}
	}
	result, err := config.ApplyMigrationWithOptions(r.paths, r.store, cfg, plan, applyOptions)
	if err != nil {
		return failPlan(err)
	}
	if r.render.mode == outputHuman {
		_, _ = fmt.Fprintf(r.render.out, "Migrated %d profile(s); private source backups were preserved.\n", len(result.AppliedProfiles))
	}
	return r.render.success(command, map[string]any{"applied": true, "already_applied": false, "result": result})
}

func migrationReport(plan config.MigrationPlan) *wireMigrationReport {
	report := &wireMigrationReport{
		SourceCount: len(plan.Sources),
		Profiles:    make([]wireMigrationProfile, 0, len(plan.Profiles)),
		Conflicts:   make([]wireMigrationConflict, 0, len(plan.Conflicts)),
		Warnings:    append([]string(nil), plan.Warnings...),
	}
	for _, profile := range plan.Profiles {
		report.Profiles = append(report.Profiles, wireMigrationProfile{
			Name: profile.Name, Source: string(profile.Source),
			CredentialStatus:   string(profile.CredentialStatus),
			UsernameConfigured: profile.Username != "", Default: profile.Default,
		})
	}
	for _, conflict := range plan.Conflicts {
		report.Conflicts = append(report.Conflicts, wireMigrationConflict{
			Profile: conflict.Profile, Reason: conflict.Reason,
		})
	}
	return report
}

type migrationArguments struct {
	yes         bool
	credentials map[string]config.CredentialRef
}

func parseMigrationArguments(args []string) (migrationArguments, error) {
	result := migrationArguments{credentials: make(map[string]config.CredentialRef)}
	for index := 0; index < len(args); index++ {
		name, value, inline := splitLongFlag(args[index])
		switch name {
		case "--yes":
			if inline || result.yes {
				return migrationArguments{}, fmt.Errorf("--yes must be specified at most once without a value")
			}
			result.yes = true
		case "--credential":
			if !inline {
				if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
					return migrationArguments{}, fmt.Errorf("--credential requires PROFILE=PROVIDER")
				}
				index++
				value = args[index]
			}
			profile, reference, err := parseMigrationCredential(value)
			if err != nil {
				return migrationArguments{}, err
			}
			if _, duplicate := result.credentials[profile]; duplicate {
				return migrationArguments{}, fmt.Errorf("a migration profile may have only one --credential mapping")
			}
			result.credentials[profile] = reference
		default:
			return migrationArguments{}, fmt.Errorf("unknown profile migrate argument")
		}
	}
	return result, nil
}

func parseMigrationCredential(value string) (string, config.CredentialRef, error) {
	separator := strings.IndexByte(value, '=')
	if separator <= 0 || separator == len(value)-1 {
		return "", config.CredentialRef{}, fmt.Errorf("--credential must use PROFILE=keyring|env:VAR|file:ABS|prompt")
	}
	profile := value[:separator]
	if strings.TrimSpace(profile) != profile {
		return "", config.CredentialRef{}, fmt.Errorf("--credential contains an invalid profile name")
	}
	decision := value[separator+1:]
	var reference config.CredentialRef
	switch {
	case decision == "keyring":
		reference.Provider = config.ProviderKeyring
	case decision == "prompt":
		reference.Provider = config.ProviderPrompt
	case strings.HasPrefix(decision, "env:"):
		reference = config.CredentialRef{Provider: config.ProviderEnv, Reference: strings.TrimPrefix(decision, "env:")}
	case strings.HasPrefix(decision, "file:"):
		reference = config.CredentialRef{Provider: config.ProviderFile, Reference: strings.TrimPrefix(decision, "file:")}
	default:
		return "", config.CredentialRef{}, fmt.Errorf("--credential uses an unsupported provider")
	}
	if reference.Provider != config.ProviderKeyring {
		if err := reference.Validate(); err != nil {
			return "", config.CredentialRef{}, fmt.Errorf("--credential contains an invalid provider reference")
		}
	}
	if reference.Provider == config.ProviderFile {
		if !filepath.IsAbs(reference.Reference) {
			return "", config.CredentialRef{}, fmt.Errorf("--credential file reference must be an absolute path")
		}
		reference.Reference = filepath.Clean(reference.Reference)
	}
	return profile, reference, nil
}

func validateMigrationCredentialDecisions(plan config.MigrationPlan, decisions map[string]config.CredentialRef, requireComplete, automated bool) error {
	profiles := make(map[string]config.MigratedProfile, len(plan.Profiles))
	for _, profile := range plan.Profiles {
		profiles[profile.Name] = profile
	}
	for name, reference := range decisions {
		profile, exists := profiles[name]
		if !exists {
			return fmt.Errorf("a credential mapping does not match a migration profile")
		}
		if profile.CredentialStatus != config.MigrationCredentialPendingImportable && profile.CredentialStatus != config.MigrationCredentialPendingManual {
			return fmt.Errorf("a credential mapping targets an already resolved profile")
		}
		if automated && reference.Provider != config.ProviderEnv && reference.Provider != config.ProviderFile {
			return fmt.Errorf("non-interactive migration permits only env or file credential mappings")
		}
		if reference.Provider == config.ProviderKeyring && profile.CredentialStatus != config.MigrationCredentialPendingImportable {
			return fmt.Errorf("a manual legacy credential cannot be imported into keyring")
		}
	}
	if requireComplete {
		for _, profile := range plan.Profiles {
			if profile.CredentialStatus != config.MigrationCredentialPendingImportable && profile.CredentialStatus != config.MigrationCredentialPendingManual {
				continue
			}
			if _, exists := decisions[profile.Name]; !exists {
				return fmt.Errorf("every pending migration profile requires one explicit --credential mapping")
			}
		}
	}
	return nil
}

func resolveMigrationCredentialDecisions(plan *config.MigrationPlan, decisions map[string]config.CredentialRef) error {
	for _, profile := range plan.Profiles {
		reference, exists := decisions[profile.Name]
		if !exists {
			continue
		}
		if err := config.ResolveMigrationCredential(plan, profile.Name, reference); err != nil {
			return err
		}
	}
	return nil
}

func (r commandRuntime) resolveInteractiveMigrationCredentials(plan *config.MigrationPlan, reader *bufio.Reader) error {
	for index := range plan.Profiles {
		profile := plan.Profiles[index]
		if profile.CredentialStatus != config.MigrationCredentialPendingImportable && profile.CredentialStatus != config.MigrationCredentialPendingManual {
			continue
		}
		reference, err := r.promptMigrationCredential(reader, profile)
		if err != nil {
			return err
		}
		if err := config.ResolveMigrationCredential(plan, profile.Name, reference); err != nil {
			return err
		}
	}
	return nil
}

func (r commandRuntime) promptMigrationCredential(reader *bufio.Reader, profile config.MigratedProfile) (config.CredentialRef, error) {
	if reader == nil {
		return config.CredentialRef{}, fmt.Errorf("credential decision input is unavailable")
	}
	for {
		if profile.CredentialStatus == config.MigrationCredentialPendingImportable {
			_, _ = fmt.Fprintf(r.render.err, "Credential provider for %s [K]eyring/[e]nv/[f]ile/[p]rompt/[a]bort (default keyring): ", profile.Name)
		} else {
			_, _ = fmt.Fprintf(r.render.err, "Credential provider for %s [E]nv/[f]ile/[p]rompt/[a]bort (default prompt): ", profile.Name)
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return config.CredentialRef{}, err
		}
		choice := strings.ToLower(strings.TrimSpace(line))
		if choice == "" {
			if profile.CredentialStatus == config.MigrationCredentialPendingImportable {
				choice = "k"
			} else {
				choice = "p"
			}
		}
		switch choice {
		case "k", "keyring":
			if profile.CredentialStatus == config.MigrationCredentialPendingImportable {
				return config.CredentialRef{Provider: config.ProviderKeyring}, nil
			}
			_, _ = fmt.Fprintln(r.render.err, "This source has no safely importable credential; choose env, file, or prompt.")
		case "e", "env":
			return r.promptMigrationReference(reader, config.ProviderEnv, "Environment variable name")
		case "f", "file":
			return r.promptMigrationReference(reader, config.ProviderFile, "Absolute credential file path")
		case "p", "prompt":
			return config.CredentialRef{Provider: config.ProviderPrompt}, nil
		case "a", "abort":
			return config.CredentialRef{}, fmt.Errorf("migration aborted before credentials were changed")
		default:
			_, _ = fmt.Fprintln(r.render.err, "Choose one of the listed providers.")
		}
	}
}

func (r commandRuntime) promptMigrationReference(reader *bufio.Reader, provider config.CredentialProvider, label string) (config.CredentialRef, error) {
	_, _ = fmt.Fprintf(r.render.err, "%s: ", label)
	line, err := reader.ReadString('\n')
	if err != nil {
		return config.CredentialRef{}, err
	}
	reference := config.CredentialRef{Provider: provider, Reference: strings.TrimSpace(line)}
	if provider == config.ProviderFile {
		if !filepath.IsAbs(reference.Reference) {
			return config.CredentialRef{}, fmt.Errorf("credential file path must be absolute")
		}
		reference.Reference = filepath.Clean(reference.Reference)
	}
	if err := reference.Validate(); err != nil {
		return config.CredentialRef{}, err
	}
	return reference, nil
}

func (r commandRuntime) resolveMigrationConflicts(plan *config.MigrationPlan, reader *bufio.Reader) error {
	if reader == nil {
		return fmt.Errorf("conflict input is unavailable")
	}
	reasons := make(map[string][]string)
	var names []string
	for _, conflict := range plan.Conflicts {
		if _, exists := reasons[conflict.Profile]; !exists {
			names = append(names, conflict.Profile)
		}
		reasons[conflict.Profile] = append(reasons[conflict.Profile], conflict.Reason)
	}
	for _, name := range names {
		_, _ = fmt.Fprintf(r.render.err, "Migration conflict for %s (%s): [r]eplace/[s]kip/[a]bort: ", name, strings.Join(reasons[name], "; "))
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "r", "replace":
			config.ResolveMigrationConflict(plan, name, true)
		case "s", "skip":
			config.ResolveMigrationConflict(plan, name, false)
		default:
			return fmt.Errorf("migration aborted with unresolved conflict %q", name)
		}
	}
	return nil
}

func (r commandRuntime) runVersion() int {
	if r.render.mode == outputHuman {
		_, _ = fmt.Fprintf(r.render.out, "IPGW-Meta %s\n", r.version)
	}
	return r.render.success("version", map[string]string{"version": r.version})
}

func (r commandRuntime) printStatus(status wireStatus) {
	_, _ = fmt.Fprintf(r.render.out, "Network: %s\nSession: %s\n", status.Network, status.Session)
	if status.Username != nil {
		_, _ = fmt.Fprintf(r.render.out, "Username: %s\n", *status.Username)
	}
	if status.OnlineIP != nil {
		_, _ = fmt.Fprintf(r.render.out, "Online IP: %s\n", *status.OnlineIP)
	}
}

func (r commandRuntime) printMigrationPlan(plan config.MigrationPlan) {
	_, _ = fmt.Fprintf(r.render.out, "Migration preview: %d source(s), %d profile(s), %d conflict(s)\n", len(plan.Sources), len(plan.Profiles), len(plan.Conflicts))
	r.printMigrationProfiles(plan)
	for _, warning := range plan.Warnings {
		_, _ = fmt.Fprintf(r.render.out, "  warning: %s\n", warning)
	}
}

func (r commandRuntime) printMigrationProfiles(plan config.MigrationPlan) {
	for _, profile := range plan.Profiles {
		provider := string(profile.Credential.Provider)
		if provider == "" {
			provider = string(profile.CredentialStatus)
		}
		_, _ = fmt.Fprintf(r.render.out, "  %s -> %s (%s)\n", profile.Source, profile.Name, provider)
	}
}

func (r commandRuntime) confirmMigration(reader *bufio.Reader) (bool, error) {
	if reader == nil {
		return false, fmt.Errorf("confirmation input is unavailable")
	}
	_, _ = fmt.Fprint(r.render.err, "Apply this migration and create backups? [y/N]: ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(line), "y") || strings.EqualFold(strings.TrimSpace(line), "yes"), nil
}

func (r commandRuntime) printMetaHelp() {
	_, _ = fmt.Fprintln(r.render.out, "Usage: ipgw-meta [--output human|json] [--profile NAME] [--bind-ip IP] <status|login|logout|network|profile>")
	_, _ = fmt.Fprintln(r.render.out, "  login [--method password|qr] [--switch]")
	_, _ = fmt.Fprintln(r.render.out, "  network <list|scan>")
	_, _ = fmt.Fprintln(r.render.out, "  profile <list|show|add|update|remove|migrate>")
}

func (r commandRuntime) printLegacyHelp() {
	_, _ = fmt.Fprintln(r.render.out, "Usage: ipgw-legacy <login|logout|test|info|config>; no arguments performs legacy login")
}
