package launcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	ModeEnvironment       = "IPGW_MODE"
	launcherSchemaVersion = 1
	maxLauncherBytes      = 64 << 10
)

type Mode string

const (
	ModeMeta   Mode = "meta"
	ModeLegacy Mode = "legacy"
)

type Options struct {
	Args           []string
	InstallDefault string
	Version        string
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer

	lookupEnv     func(string) (string, bool)
	userConfigDir func() (string, error)
	executable    func() (string, error)
	runChild      childRunner
}

type childRunner func(path string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error)

type failureKind uint8

const (
	failureInvalidArguments failureKind = iota + 1
	failureConfiguration
	failureStartup
)

type launcherFailure struct{ kind failureKind }

type launcherState struct {
	SchemaVersion int       `yaml:"schema_version"`
	Mode          string    `yaml:"mode"`
	Cohort        string    `yaml:"cohort,omitempty"`
	ChosenAt      time.Time `yaml:"chosen_at,omitempty"`
}

type modeResolution struct {
	Mode Mode
	Args []string
}

// Execute resolves the launcher mode and runs the exact sibling binary. It
// contains no gateway, credential, profile, or protocol logic.
func Execute(options Options) int {
	options = withDefaults(options)
	versionRequest, versionFailure := parseVersionRequest(options.Args)
	if versionRequest.requested {
		if versionFailure != nil {
			return renderFailure(versionRequest.failureJSON, options.Stdout, options.Stderr, versionFailure.kind)
		}
		return renderVersion(versionRequest.jsonOutput, options.Version, options.Stdout, options.Stderr)
	}
	jsonOutput := preparseJSON(options.Args)

	resolution, failure := resolveMode(options)
	if failure != nil {
		return renderFailure(jsonOutput, options.Stdout, options.Stderr, failure.kind)
	}
	executablePath, err := options.executable()
	if err != nil {
		return renderFailure(jsonOutput, options.Stdout, options.Stderr, failureStartup)
	}
	target, err := siblingExecutable(executablePath, resolution.Mode)
	if err != nil {
		return renderFailure(jsonOutput, options.Stdout, options.Stderr, failureStartup)
	}
	exitCode, err := options.runChild(target, resolution.Args, options.Stdin, options.Stdout, options.Stderr)
	if err != nil {
		return renderFailure(jsonOutput, options.Stdout, options.Stderr, failureStartup)
	}
	return exitCode
}

type versionRequest struct {
	requested   bool
	jsonOutput  bool
	failureJSON bool
}

// parseVersionRequest recognizes the side-effect-free global version path
// without consulting launcher environment, configuration, or sibling files.
// The normal child CLI remains authoritative for all non-version invocations.
func parseVersionRequest(args []string) (versionRequest, *launcherFailure) {
	request := versionRequest{failureJSON: preparseJSON(args)}
	for _, argument := range args {
		if argument == "--" {
			break
		}
		name, _, _ := splitLauncherLongFlag(argument)
		if name == "--version" {
			request.requested = true
			break
		}
	}
	if !request.requested {
		return request, nil
	}

	outputSeen := false
	modeSeen := false
	versionSeen := false
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			if index+1 != len(args) {
				return request, &launcherFailure{kind: failureInvalidArguments}
			}
			break
		}
		name, value, inline := splitLauncherLongFlag(argument)
		switch name {
		case "--version":
			if inline || versionSeen {
				return request, &launcherFailure{kind: failureInvalidArguments}
			}
			versionSeen = true
		case "--json":
			if inline || outputSeen {
				return request, &launcherFailure{kind: failureInvalidArguments}
			}
			outputSeen = true
			request.jsonOutput = true
		case "--output", "--mode", "--config", "--profile", "--bind-ip":
			if !inline {
				if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
					return request, &launcherFailure{kind: failureInvalidArguments}
				}
				index++
				value = args[index]
			}
			if isLauncherPasswordArgument(value) {
				return request, &launcherFailure{kind: failureInvalidArguments}
			}
			switch name {
			case "--output":
				if outputSeen || (value != "human" && value != "json") {
					return request, &launcherFailure{kind: failureInvalidArguments}
				}
				outputSeen = true
				request.jsonOutput = value == "json"
			case "--mode":
				if modeSeen {
					return request, &launcherFailure{kind: failureInvalidArguments}
				}
				if _, valid := parseMode(value); !valid {
					return request, &launcherFailure{kind: failureInvalidArguments}
				}
				modeSeen = true
			}
		default:
			return request, &launcherFailure{kind: failureInvalidArguments}
		}
	}
	return request, nil
}

func isLauncherPasswordArgument(argument string) bool {
	return argument == "--password" || strings.HasPrefix(argument, "--password=") ||
		argument == "-p" || (strings.HasPrefix(argument, "-p") && len(argument) > 2)
}

func splitLauncherLongFlag(argument string) (name, value string, inline bool) {
	if split := strings.IndexByte(argument, '='); strings.HasPrefix(argument, "--") && split > 2 {
		return argument[:split], argument[split+1:], true
	}
	return argument, "", false
}

func renderVersion(jsonOutput bool, version string, stdout, stderr io.Writer) int {
	if !jsonOutput {
		_, _ = fmt.Fprintf(stdout, "IPGW-Meta %s\n", version)
		return 0
	}
	value := struct {
		SchemaVersion int               `json:"schema_version"`
		Command       string            `json:"command"`
		OK            bool              `json:"ok"`
		Data          map[string]string `json:"data"`
	}{SchemaVersion: 1, Command: "version", OK: true, Data: map[string]string{"version": version}}
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		_, _ = fmt.Fprintln(stderr, "Error: unable to write JSON output")
		return 1
	}
	return 0
}

func withDefaults(options Options) Options {
	if options.Stdin == nil {
		options.Stdin = strings.NewReader("")
	}
	if options.Stdout == nil {
		options.Stdout = io.Discard
	}
	if options.Stderr == nil {
		options.Stderr = io.Discard
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	if options.lookupEnv == nil {
		options.lookupEnv = os.LookupEnv
	}
	if options.userConfigDir == nil {
		options.userConfigDir = os.UserConfigDir
	}
	if options.executable == nil {
		options.executable = os.Executable
	}
	if options.runChild == nil {
		options.runChild = runProcess
	}
	return options
}

func resolveMode(options Options) (modeResolution, *launcherFailure) {
	explicit, remaining, found, failure := extractMode(options.Args)
	if failure != nil {
		return modeResolution{}, failure
	}
	if found {
		return modeResolution{Mode: explicit, Args: remaining}, nil
	}

	if raw, ok := options.lookupEnv(ModeEnvironment); ok && strings.TrimSpace(raw) != "" {
		mode, valid := parseMode(raw)
		if !valid {
			return modeResolution{}, &launcherFailure{kind: failureInvalidArguments}
		}
		return modeResolution{Mode: mode, Args: remaining}, nil
	}

	configDir, err := options.userConfigDir()
	if err != nil || configDir == "" {
		return modeResolution{}, &launcherFailure{kind: failureConfiguration}
	}
	configured, found, failure := loadConfiguredMode(filepath.Join(configDir, "ipgw-meta", "launcher.yaml"))
	if failure != nil {
		return modeResolution{}, failure
	}
	if found {
		return modeResolution{Mode: configured, Args: remaining}, nil
	}

	fallback, valid := parseMode(options.InstallDefault)
	if !valid {
		return modeResolution{}, &launcherFailure{kind: failureStartup}
	}
	return modeResolution{Mode: fallback, Args: remaining}, nil
}

func extractMode(args []string) (Mode, []string, bool, *launcherFailure) {
	remaining := make([]string, 0, len(args))
	var selected Mode
	found := false
	for i := 0; i < len(args); i++ {
		argument := args[i]
		if argument == "--" {
			remaining = append(remaining, args[i:]...)
			break
		}

		var raw string
		switch {
		case argument == "--mode":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return "", nil, false, &launcherFailure{kind: failureInvalidArguments}
			}
			i++
			raw = args[i]
		case strings.HasPrefix(argument, "--mode="):
			raw = strings.TrimPrefix(argument, "--mode=")
		default:
			remaining = append(remaining, argument)
			continue
		}

		mode, valid := parseMode(raw)
		if !valid || found {
			return "", nil, false, &launcherFailure{kind: failureInvalidArguments}
		}
		selected = mode
		found = true
	}
	return selected, remaining, found, nil
}

func parseMode(raw string) (Mode, bool) {
	switch Mode(strings.ToLower(strings.TrimSpace(raw))) {
	case ModeMeta:
		return ModeMeta, true
	case ModeLegacy:
		return ModeLegacy, true
	default:
		return "", false
	}
}

func loadConfiguredMode(path string) (Mode, bool, *launcherFailure) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, &launcherFailure{kind: failureConfiguration}
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxLauncherBytes+1))
	if err != nil || len(data) > maxLauncherBytes {
		return "", false, &launcherFailure{kind: failureConfiguration}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	var state launcherState
	if err := decoder.Decode(&state); err != nil {
		return "", false, &launcherFailure{kind: failureConfiguration}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", false, &launcherFailure{kind: failureConfiguration}
	}
	mode := Mode(state.Mode)
	if state.SchemaVersion != launcherSchemaVersion || (mode != ModeMeta && mode != ModeLegacy) {
		return "", false, &launcherFailure{kind: failureConfiguration}
	}
	return mode, true, nil
}

func siblingExecutable(executablePath string, mode Mode) (string, error) {
	if executablePath == "" || (mode != ModeMeta && mode != ModeLegacy) {
		return "", errors.New("invalid launcher target")
	}
	absolute, err := filepath.Abs(executablePath)
	if err != nil {
		return "", err
	}
	suffix := ""
	if strings.EqualFold(filepath.Ext(absolute), ".exe") {
		suffix = ".exe"
	}
	return filepath.Join(filepath.Dir(absolute), "ipgw-"+string(mode)+suffix), nil
}

func runProcess(path string, args []string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	command := exec.Command(path, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}
	return 0, err
}

func preparseJSON(args []string) bool {
	for i := 0; i < len(args); i++ {
		argument := args[i]
		if argument == "--" {
			break
		}
		if argument == "--json" || argument == "--output=json" {
			return true
		}
		if argument == "--output" && i+1 < len(args) && args[i+1] == "json" {
			return true
		}
	}
	return false
}

func renderFailure(jsonOutput bool, stdout, stderr io.Writer, kind failureKind) int {
	code, message, exitCode := failureContract(kind)
	if jsonOutput {
		value := struct {
			SchemaVersion int    `json:"schema_version"`
			Command       string `json:"command"`
			OK            bool   `json:"ok"`
			Error         struct {
				Code      string         `json:"code"`
				Message   string         `json:"message"`
				Retryable bool           `json:"retryable"`
				Details   map[string]any `json:"details"`
			} `json:"error"`
		}{SchemaVersion: 1, Command: "cli", OK: false}
		value.Error.Code = code
		value.Error.Message = message
		value.Error.Retryable = false
		value.Error.Details = map[string]any{}
		if err := json.NewEncoder(stdout).Encode(value); err != nil {
			_, _ = fmt.Fprintln(stderr, "Error: unable to write JSON output")
			return 1
		}
		return exitCode
	}
	_, _ = fmt.Fprintf(stderr, "Error: %s\n", message)
	return exitCode
}

func failureContract(kind failureKind) (code, message string, exitCode int) {
	switch kind {
	case failureInvalidArguments:
		return "invalid_argument", "invalid launcher arguments", 2
	case failureConfiguration:
		return "config", "launcher configuration error", 2
	default:
		return "internal", "launcher could not start selected mode", 1
	}
}
