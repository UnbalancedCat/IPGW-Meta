package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
	"github.com/UnbalancedCat/ipgw-meta/internal/config"
)

func Execute(ctx context.Context, options Options) int {
	if options.Out == nil {
		options.Out = io.Discard
	}
	if options.Err == nil {
		options.Err = io.Discard
	}
	render := renderer{mode: preparseOutputMode(options.Args), out: options.Out, err: options.Err}
	globals, args, err := extractGlobals(options.Args)
	if options.Mode == ModeMeta && hasPasswordArgument(options.Args) {
		return render.failure("cli", passwordArgumentError())
	}
	if err != nil {
		return render.failure("cli", invalidArgument(err))
	}
	render.mode = globals.output
	if options.NewGateway == nil {
		return render.failure("cli", internalError("gateway factory is unavailable"))
	}
	paths := config.WithConfigPath(options.Paths, globals.configPath)
	providerOptions := config.ProviderOptions{}
	if options.ProviderOptions != nil {
		providerOptions = *options.ProviderOptions
	}
	if providerOptions.BaseDir == "" {
		providerOptions.BaseDir = paths.BaseDir
	}
	if providerOptions.Input == nil {
		providerOptions.Input = options.Input
	}
	if providerOptions.Output == nil {
		providerOptions.Output = options.Err
	}
	version := options.Version
	if version == "" {
		version = "dev"
	}
	runtime := commandRuntime{
		gateway: options.NewGateway(paths),
		store:   &config.Store{Path: paths.ConfigFile, MigrationJournal: paths.MigrationJournal},
		paths:   paths, render: render, input: options.Input, isTTY: options.IsTTY,
		profile: globals.profile, bindIP: globals.bindIP, version: version,
		providerOptions: providerOptions,
	}
	switch options.Mode {
	case ModeMeta:
		return runtime.runMeta(ctx, args)
	case ModeLegacy:
		return runtime.runLegacy(ctx, args)
	default:
		return render.failure("cli", invalidArgument(fmt.Errorf("unknown CLI mode")))
	}
}

func (r commandRuntime) runMeta(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return r.runStatus(ctx, true)
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return r.render.failure("status", invalidArgument(fmt.Errorf("status takes no positional arguments")))
		}
		return r.runStatus(ctx, false)
	case "login":
		loginArgs, err := normalizeMetaLoginArgs(args[1:])
		if err != nil {
			return r.render.failure("login", invalidArgument(err))
		}
		return r.runLogin(ctx, loginArgs, false)
	case "logout":
		if len(args) != 1 {
			return r.render.failure("logout", invalidArgument(fmt.Errorf("logout takes no positional arguments")))
		}
		return r.runLogout(ctx)
	case "network":
		if len(args) == 2 && args[1] == "list" {
			return r.runNetworkList(ctx)
		}
		if len(args) == 2 && args[1] == "scan" {
			return r.runNetworkScan(ctx)
		}
		return r.render.failure("network", invalidArgument(fmt.Errorf("usage: ipgw-meta network <list|scan>")))
	case "profile":
		return r.runProfile(ctx, args[1:])
	case "help", "--help", "-h":
		if r.render.mode == outputHuman {
			r.printMetaCommandHelp()
		}
		return r.render.success("help", map[string]any{"mode": "meta"})
	default:
		return r.render.failure("cli", invalidArgument(fmt.Errorf("unknown command")))
	}
}

func normalizeMetaLoginArgs(args []string) ([]string, error) {
	normalized := make([]string, 0, len(args)+1)
	methodSeen := false
	switchSeen := false
	for i := 0; i < len(args); i++ {
		argument := args[i]
		switch {
		case argument == "--switch":
			if switchSeen {
				return nil, fmt.Errorf("--switch was specified more than once")
			}
			switchSeen = true
			normalized = append(normalized, argument)
		case argument == "--method":
			if methodSeen {
				return nil, fmt.Errorf("--method was specified more than once")
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, fmt.Errorf("--method requires password or qr")
			}
			i++
			method := args[i]
			if method != "password" && method != "qr" {
				return nil, fmt.Errorf("--method must be password or qr")
			}
			methodSeen = true
			normalized = append(normalized, "--method", method)
		case strings.HasPrefix(argument, "--method="):
			if methodSeen {
				return nil, fmt.Errorf("--method was specified more than once")
			}
			method := strings.TrimPrefix(argument, "--method=")
			if method != "password" && method != "qr" {
				return nil, fmt.Errorf("--method must be password or qr")
			}
			methodSeen = true
			normalized = append(normalized, "--method", method)
		default:
			return nil, fmt.Errorf("unknown login argument")
		}
	}
	return normalized, nil
}

func (r commandRuntime) printMetaCommandHelp() {
	_, _ = fmt.Fprintln(r.render.out, "Usage: ipgw-meta [--json|--output human|json] [--profile NAME] [--bind-ip IP] <status|login|logout|network|profile>")
	_, _ = fmt.Fprintln(r.render.out, "  login [--method password|qr] [--switch]")
	_, _ = fmt.Fprintln(r.render.out, "  network <list|scan>")
	_, _ = fmt.Fprintln(r.render.out, "  profile <list|show|add|remove|migrate>")
}

func (r commandRuntime) runLegacy(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return r.runLogin(ctx, nil, true)
	}
	if isLegacyTopLevelLoginFlag(args[0]) {
		return r.runLogin(ctx, args, true)
	}
	switch args[0] {
	case "login":
		return r.runLogin(ctx, args[1:], true)
	case "logout":
		if len(args) != 1 {
			return r.render.failure("logout", invalidArgument(fmt.Errorf("logout takes no positional arguments")))
		}
		return r.runLogout(ctx)
	case "test", "status":
		if len(args) != 1 {
			return r.render.failure("status", invalidArgument(fmt.Errorf("status takes no positional arguments")))
		}
		return r.runStatus(ctx, false)
	case "info":
		if len(args) == 2 && (args[1] == "-a" || args[1] == "--all") {
			return r.runNetworkList(ctx)
		}
		if len(args) == 1 {
			return r.runStatus(ctx, false)
		}
		return r.render.failure("info", invalidArgument(fmt.Errorf("this legacy info flag has not migrated yet")))
	case "config":
		if len(args) == 3 && args[1] == "account" && (args[2] == "list" || args[2] == "ls" || args[2] == "show") {
			return r.runProfile(ctx, []string{"list"})
		}
		return r.render.failure("config", invalidArgument(fmt.Errorf("use ipgw-meta profile commands for the new secret-free configuration")))
	case "version":
		return r.runVersion()
	case "help", "--help", "-h":
		if r.render.mode == outputHuman {
			r.printLegacyHelp()
		}
		return r.render.success("help", map[string]any{"mode": "legacy"})
	default:
		return r.render.failure("cli", invalidArgument(fmt.Errorf("unsupported legacy command")))
	}
}

func isLegacyTopLevelLoginFlag(argument string) bool {
	switch argument {
	case "-u", "--username", "-p", "--password":
		return true
	}
	return (strings.HasPrefix(argument, "-u") && !strings.HasPrefix(argument, "--") && len(argument) > 2) ||
		(strings.HasPrefix(argument, "-p") && !strings.HasPrefix(argument, "--") && len(argument) > 2) ||
		strings.HasPrefix(argument, "--username=") || strings.HasPrefix(argument, "--password=")
}

func extractGlobals(args []string) (globalOptions, []string, error) {
	options := globalOptions{output: outputHuman}
	remaining := make([]string, 0, len(args))
	outputSeen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			remaining = append(remaining, args[i+1:]...)
			break
		}
		name, value, inline := splitLongFlag(arg)
		var destination *string
		switch name {
		case "--json":
			if inline {
				return options, nil, fmt.Errorf("--json does not take a value")
			}
			if outputSeen {
				return options, nil, fmt.Errorf("output mode was specified more than once")
			}
			options.output = outputJSON
			outputSeen = true
			continue
		case "--output":
			if outputSeen {
				return options, nil, fmt.Errorf("output mode was specified more than once")
			}
		case "--config":
			destination = &options.configPath
		case "--profile":
			destination = &options.profile
		case "--bind-ip":
			destination = &options.bindIP
		default:
			remaining = append(remaining, arg)
			continue
		}
		if !inline {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return options, nil, fmt.Errorf("%s requires a value", name)
			}
			i++
			value = args[i]
		}
		if isPasswordArgument(value) {
			return options, nil, fmt.Errorf("%s requires a non-secret value", name)
		}
		if name == "--output" {
			switch outputMode(value) {
			case outputHuman:
				options.output = outputHuman
			case outputJSON:
				options.output = outputJSON
			default:
				return options, nil, fmt.Errorf("--output must be human or json")
			}
			outputSeen = true
			continue
		}
		*destination = value
	}
	return options, remaining, nil
}

// preparseOutputMode is deliberately non-validating. It only discovers an
// explicit request for JSON so that later parse/startup failures can still use
// the one-envelope contract. extractGlobals remains the authority for syntax,
// duplicates, and conflicts.
func preparseOutputMode(args []string) outputMode {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--output=json" {
			return outputJSON
		}
		if arg == "--output" && i+1 < len(args) && args[i+1] == "json" {
			return outputJSON
		}
	}
	return outputHuman
}

func splitLongFlag(arg string) (name, value string, inline bool) {
	if index := strings.IndexByte(arg, '='); strings.HasPrefix(arg, "--") && index > 2 {
		return arg[:index], arg[index+1:], true
	}
	return arg, "", false
}

func invalidArgument(err error) error {
	if err == nil {
		return &ipgw.Error{Code: ipgw.CodeInvalidArgument, Message: "invalid arguments"}
	}
	message := err.Error()
	cause := err
	// Argument errors built with %q may contain an arbitrary command-line
	// token. Do not retain or expose that token: it can be --password=SECRET.
	if strings.Contains(message, `"`) {
		message = "invalid arguments"
		cause = nil
	}
	return &ipgw.Error{Code: ipgw.CodeInvalidArgument, Message: message, Cause: cause}
}

func passwordArgumentError() error {
	return &ipgw.Error{
		Code:    ipgw.CodeInvalidArgument,
		Message: "passwords may not be supplied on the command line; use a credential provider",
	}
}

func hasPasswordArgument(args []string) bool {
	for _, arg := range args {
		if isPasswordArgument(arg) {
			return true
		}
	}
	return false
}

func isPasswordArgument(arg string) bool {
	return arg == "--password" || strings.HasPrefix(arg, "--password=") ||
		arg == "-p" || (strings.HasPrefix(arg, "-p") && len(arg) > 2)
}

func configFailure(err error) error {
	return &ipgw.Error{Code: ipgw.CodeConfig, Message: err.Error(), Cause: err}
}

func internalError(message string) error {
	return &ipgw.Error{Code: ipgw.CodeInternal, Message: message}
}
