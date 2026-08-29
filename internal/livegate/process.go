package livegate

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	ErrProcessProfile     = errors.New("livegate: candidate profile preflight failed")
	ErrProcessRunnerFault = errors.New("livegate: candidate runner fault")
)

type processRunFunc func(
	context.Context,
	string,
	[]string,
	io.Reader,
	io.Writer,
	io.Writer,
) (int, error)

// ProcessExecutor runs the verified product candidate directly, without a
// shell, and projects bounded JSON into the closed Observation type.
type ProcessExecutor struct {
	CandidatePath string
	Profile       string
	Stdin         *os.File
	Stdout        *os.File
	Stderr        *os.File

	expectedUsername string
	run              processRunFunc
}

// PrepareProfile validates the selected profile and retains only the expected
// username needed for in-memory identity comparison.
func (executor *ProcessExecutor) PrepareProfile(ctx context.Context, suite Suite) error {
	if executor != nil {
		executor.expectedUsername = ""
	}
	if executor == nil ||
		ctx == nil ||
		!validProcessConfiguration(executor.CandidatePath, executor.Profile) ||
		executor.Stderr == nil ||
		(suite != SuitePasswordCore && suite != SuiteTerminalQR) {
		return ErrProcessProfile
	}

	projection, exit, runErr, parseErr := executor.runJSON(
		ctx,
		[]string{"--json", "--profile", executor.Profile, "profile", "show"},
	)
	if ctx.Err() != nil {
		return ErrProcessRunnerFault
	}
	exitCode, validExit := processCommandExit(exit)
	if !validExit || parseErr != nil || (runErr != nil && exit == 0) {
		return ErrProcessRunnerFault
	}

	if exit != 0 {
		if !projection.validEnvelope("profile.show", false) ||
			projection.errorCodeInvalid ||
			!projection.errorCodeSeen ||
			projection.errorCode == "" {
			return ErrProcessRunnerFault
		}
		code := ErrorCode(projection.errorCode)
		if !code.valid() || !validExitError(exitCode, &code) {
			return ErrProcessRunnerFault
		}
		return ErrProcessProfile
	}

	if !projection.validEnvelope("profile.show", true) ||
		!projection.validProfileShape(suite) {
		return ErrProcessRunnerFault
	}
	if projection.profileUsername == "" {
		return ErrProcessProfile
	}
	if suite == SuitePasswordCore &&
		(projection.provider != "prompt" ||
			(projection.referenceSeen && projection.reference != "")) {
		return ErrProcessProfile
	}
	executor.expectedUsername = projection.profileUsername
	return nil
}

// Execute maps a state-machine action to one direct candidate invocation.
// Candidate stderr is always connected directly to the private TTY and is
// never captured by the runner.
func (executor *ProcessExecutor) Execute(ctx context.Context, action CommandAction) Observation {
	if executor == nil ||
		ctx == nil ||
		executor.expectedUsername == "" ||
		!validProcessConfiguration(executor.CandidatePath, executor.Profile) ||
		executor.Stderr == nil {
		return runnerFaultObservation()
	}
	if action == ActionQRLogin {
		return executor.executeQR(ctx)
	}

	args, command, stdin, ok := executor.jsonAction(action)
	if !ok {
		return runnerFaultObservation()
	}
	projection, exit, runErr, parseErr := executor.runJSONWithInput(ctx, args, stdin)
	if ctx.Err() != nil {
		return canceledProcessObservation()
	}
	exitCode, validExit := processCommandExit(exit)
	if !validExit || parseErr != nil || (runErr != nil && exit == 0) {
		return runnerFaultObservation()
	}
	if exit == 0 {
		if !projection.validEnvelope(command, true) {
			return runnerFaultObservation()
		}
		observation, ok := projection.successObservation(command, executor.expectedUsername)
		if !ok {
			return runnerFaultObservation()
		}
		return observation
	}
	if !projection.validEnvelope(command, false) ||
		projection.errorCodeInvalid ||
		!projection.errorCodeSeen ||
		projection.errorCode == "" {
		return runnerFaultObservation()
	}
	code := ErrorCode(projection.errorCode)
	if !code.valid() || !validExitError(exitCode, &code) {
		return runnerFaultObservation()
	}
	return Observation{ExitCode: exitCode, ErrorCode: &code}
}

func (executor *ProcessExecutor) jsonAction(action CommandAction) ([]string, string, io.Reader, bool) {
	prefix := []string{"--json", "--profile", executor.Profile}
	switch action {
	case ActionInitialStatus, ActionStatusOnline, ActionFinalStatus, ActionCleanupStatus:
		return append(prefix, "status"), "status", nil, true
	case ActionPasswordLogin:
		return append(prefix, "login", "--method", "password"), "login", executor.Stdin, executor.Stdin != nil
	case ActionSecondPasswordLogin:
		return append(prefix, "login", "--method", "password"), "login", nil, true
	case ActionLogout, ActionSecondLogout, ActionCleanupLogout:
		return append(prefix, "logout"), "logout", nil, true
	default:
		return nil, "", nil, false
	}
}

func (executor *ProcessExecutor) executeQR(ctx context.Context) Observation {
	if executor.Stdin == nil || executor.Stdout == nil || executor.Stderr == nil {
		return runnerFaultObservation()
	}
	runner := executor.run
	if runner == nil {
		runner = runCandidateProcess
	}
	exit, runErr := runner(
		ctx,
		executor.CandidatePath,
		[]string{"--profile", executor.Profile, "login", "--method", "qr"},
		executor.Stdin,
		executor.Stdout,
		executor.Stderr,
	)
	if ctx.Err() != nil {
		return canceledProcessObservation()
	}
	exitCode, ok := processCommandExit(exit)
	if !ok || (runErr != nil && exit == 0) {
		return runnerFaultObservation()
	}
	if exitCode == CommandExitSuccess {
		// Human-mode success is only a transport observation. The state
		// machine must pair it with the immediately following JSON status
		// before synthesizing qr_login_logged_in.
		return Observation{}
	}
	code := defaultProcessErrorCode(exitCode)
	if code == "" {
		return runnerFaultObservation()
	}
	return Observation{ExitCode: exitCode, ErrorCode: &code}
}

type processRunResult struct {
	exit int
	err  error
}

func (executor *ProcessExecutor) runJSON(
	ctx context.Context,
	args []string,
) (candidateProjection, int, error, error) {
	return executor.runJSONWithInput(ctx, args, nil)
}

func (executor *ProcessExecutor) runJSONWithInput(
	ctx context.Context,
	args []string,
	stdin io.Reader,
) (candidateProjection, int, error, error) {
	var projection candidateProjection
	runner := executor.run
	if runner == nil {
		runner = runCandidateProcess
	}
	reader, writer := io.Pipe()
	done := make(chan processRunResult, 1)
	go func() {
		exit, err := runner(ctx, executor.CandidatePath, args, stdin, writer, executor.Stderr)
		_ = writer.Close()
		done <- processRunResult{exit: exit, err: err}
	}()

	parseErr := projectCandidateJSONNodes(reader, projection.project)
	_ = reader.Close()
	result := <-done
	if parseErr != nil || projection.invalid {
		parseErr = ErrInvalidCandidateJSON
	}
	return projection, result.exit, result.err, parseErr
}

func runCandidateProcess(
	ctx context.Context,
	candidatePath string,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	command := exec.CommandContext(ctx, candidatePath, args...)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	if ctx.Err() != nil {
		return int(CommandExitCanceled), ctx.Err()
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), err
	}
	return -1, err
}

type candidateProjection struct {
	rootObject bool
	invalid    bool

	schemaSeen bool
	schema     string
	command    string
	commandSet bool
	ok         bool
	okSet      bool

	dataSeen    bool
	dataObject  bool
	errorSeen   bool
	errorObject bool

	errorCode        string
	errorCodeSeen    bool
	errorCodeInvalid bool
	outcome          string
	outcomeSeen      bool
	outcomeInvalid   bool

	statusSession         string
	statusSessionSeen     bool
	statusSessionInvalid  bool
	statusUsername        string
	statusUsernameSeen    bool
	statusUsernameInvalid bool

	finalStatusSeen      bool
	finalStatusObject    bool
	finalSession         string
	finalSessionSeen     bool
	finalSessionInvalid  bool
	finalUsername        string
	finalUsernameSeen    bool
	finalUsernameInvalid bool

	profileSeen            bool
	profileObject          bool
	profileUsername        string
	profileUsernameSeen    bool
	profileUsernameInvalid bool
	credentialSeen         bool
	credentialObject       bool
	provider               string
	providerSeen           bool
	providerInvalid        bool
	reference              string
	referenceSeen          bool
	referenceInvalid       bool
}

func (projection *candidateProjection) project(path []string, node candidateJSONNode) error {
	if len(path) == 0 {
		projection.rootObject = node.kind == candidateJSONObject
		projection.invalid = projection.invalid || !projection.rootObject
		return nil
	}

	switch {
	case candidatePathEquals(path, "schema_version"):
		projection.schemaSeen = true
		if node.kind != candidateJSONScalar {
			projection.invalid = true
			return nil
		}
		value, invalid := candidateNumberText(node.scalar)
		projection.schema = value
		projection.invalid = projection.invalid || invalid
	case candidatePathEquals(path, "command"):
		projection.commandSet = true
		if node.kind != candidateJSONScalar {
			projection.invalid = true
			return nil
		}
		value, invalid := candidateStringValue(node.scalar)
		projection.command = value
		projection.invalid = projection.invalid || invalid
	case candidatePathEquals(path, "ok"):
		projection.okSet = true
		if node.kind != candidateJSONScalar {
			projection.invalid = true
			return nil
		}
		value, invalid := candidateBoolValue(node.scalar)
		projection.ok = value
		projection.invalid = projection.invalid || invalid
	case candidatePathEquals(path, "data"):
		projection.dataSeen = true
		projection.dataObject = node.kind == candidateJSONObject
	case candidatePathEquals(path, "error"):
		projection.errorSeen = true
		projection.errorObject = node.kind == candidateJSONObject
	case candidatePathEquals(path, "error", "code"):
		projection.errorCodeSeen = true
		if node.kind != candidateJSONScalar {
			projection.errorCodeInvalid = true
			return nil
		}
		value, invalid := candidateStringValue(node.scalar)
		projection.errorCode = value
		projection.errorCodeInvalid = projection.errorCodeInvalid || invalid
	case candidatePathEquals(path, "data", "outcome"):
		projection.outcomeSeen = true
		if node.kind != candidateJSONScalar {
			projection.outcomeInvalid = true
			return nil
		}
		value, invalid := candidateStringValue(node.scalar)
		projection.outcome = value
		projection.outcomeInvalid = projection.outcomeInvalid || invalid
	case candidatePathEquals(path, "data", "session"):
		projection.statusSessionSeen = true
		if node.kind != candidateJSONScalar {
			projection.statusSessionInvalid = true
			return nil
		}
		value, invalid := candidateStringValue(node.scalar)
		projection.statusSession = value
		projection.statusSessionInvalid = projection.statusSessionInvalid || invalid
	case candidatePathEquals(path, "data", "username"):
		projection.statusUsernameSeen = true
		if node.kind != candidateJSONScalar {
			projection.statusUsernameInvalid = true
			return nil
		}
		value, invalid := candidateNullableStringValue(node.scalar)
		projection.statusUsername = value
		projection.statusUsernameInvalid = projection.statusUsernameInvalid || invalid
	case candidatePathEquals(path, "data", "status"):
		projection.finalStatusSeen = true
		projection.finalStatusObject = node.kind == candidateJSONObject
	case candidatePathEquals(path, "data", "status", "session"):
		projection.finalSessionSeen = true
		if node.kind != candidateJSONScalar {
			projection.finalSessionInvalid = true
			return nil
		}
		value, invalid := candidateStringValue(node.scalar)
		projection.finalSession = value
		projection.finalSessionInvalid = projection.finalSessionInvalid || invalid
	case candidatePathEquals(path, "data", "status", "username"):
		projection.finalUsernameSeen = true
		if node.kind != candidateJSONScalar {
			projection.finalUsernameInvalid = true
			return nil
		}
		value, invalid := candidateNullableStringValue(node.scalar)
		projection.finalUsername = value
		projection.finalUsernameInvalid = projection.finalUsernameInvalid || invalid
	case candidatePathEquals(path, "data", "profile"):
		projection.profileSeen = true
		projection.profileObject = node.kind == candidateJSONObject
	case candidatePathEquals(path, "data", "profile", "username"):
		projection.profileUsernameSeen = true
		if node.kind != candidateJSONScalar {
			projection.profileUsernameInvalid = true
			return nil
		}
		value, invalid := candidateStringValue(node.scalar)
		projection.profileUsername = value
		projection.profileUsernameInvalid = projection.profileUsernameInvalid || invalid
	case candidatePathEquals(path, "data", "profile", "credential"):
		projection.credentialSeen = true
		projection.credentialObject = node.kind == candidateJSONObject
	case candidatePathEquals(path, "data", "profile", "credential", "provider"):
		projection.providerSeen = true
		if node.kind != candidateJSONScalar {
			projection.providerInvalid = true
			return nil
		}
		value, invalid := candidateStringValue(node.scalar)
		projection.provider = value
		projection.providerInvalid = projection.providerInvalid || invalid
	case candidatePathEquals(path, "data", "profile", "credential", "reference"):
		projection.referenceSeen = true
		if node.kind != candidateJSONScalar {
			projection.referenceInvalid = true
			return nil
		}
		value, invalid := candidateStringValue(node.scalar)
		projection.reference = value
		projection.referenceInvalid = projection.referenceInvalid || invalid
	}
	return nil
}

func candidatePathEquals(path []string, segments ...string) bool {
	if len(path) != len(segments) {
		return false
	}
	for index := range segments {
		if path[index] != segments[index] {
			return false
		}
	}
	return true
}

func (projection candidateProjection) validEnvelope(command string, success bool) bool {
	if projection.invalid ||
		!projection.rootObject ||
		!projection.schemaSeen ||
		projection.schema != "1" ||
		!projection.commandSet ||
		projection.command != command ||
		!projection.okSet ||
		projection.ok != success {
		return false
	}
	if success {
		return projection.dataSeen &&
			projection.dataObject &&
			!projection.errorSeen
	}
	return !projection.dataSeen &&
		projection.errorSeen &&
		projection.errorObject
}

func (projection candidateProjection) validProfileShape(suite Suite) bool {
	if !projection.profileSeen ||
		!projection.profileObject ||
		!projection.profileUsernameSeen ||
		projection.profileUsernameInvalid {
		return false
	}
	if suite != SuitePasswordCore {
		return suite == SuiteTerminalQR
	}
	return projection.credentialSeen &&
		projection.credentialObject &&
		projection.providerSeen &&
		!projection.providerInvalid &&
		!projection.referenceInvalid
}

func (projection candidateProjection) successObservation(command, expectedUsername string) (Observation, bool) {
	observation := Observation{}
	switch command {
	case "status":
		if !projection.statusSessionSeen ||
			!projection.statusUsernameSeen ||
			projection.statusSessionInvalid ||
			projection.statusUsernameInvalid {
			return Observation{}, false
		}
		observation.Session = SessionState(projection.statusSession)
		if !validProcessSession(observation.Session) {
			return Observation{}, false
		}
		observation.IdentityMatches = observation.Session == SessionOnline &&
			projection.statusUsername != "" &&
			projection.statusUsername == expectedUsername
	case "login":
		if !projection.outcomeSeen ||
			projection.outcomeInvalid ||
			!projection.finalStatusSeen ||
			!projection.finalStatusObject ||
			!projection.finalSessionSeen ||
			projection.finalSessionInvalid ||
			!projection.finalUsernameSeen ||
			projection.finalUsernameInvalid {
			return Observation{}, false
		}
		observation.Outcome = CommandOutcome(projection.outcome)
		observation.Session = SessionState(projection.finalSession)
		if !validProcessOutcome(observation.Outcome) ||
			!validProcessSession(observation.Session) {
			return Observation{}, false
		}
		observation.IdentityMatches = observation.Session == SessionOnline &&
			projection.finalUsername != "" &&
			projection.finalUsername == expectedUsername
	case "logout":
		if !projection.outcomeSeen ||
			projection.outcomeInvalid ||
			!projection.finalStatusSeen ||
			!projection.finalStatusObject ||
			!projection.finalSessionSeen ||
			projection.finalSessionInvalid ||
			!projection.finalUsernameSeen ||
			projection.finalUsernameInvalid {
			return Observation{}, false
		}
		observation.Outcome = CommandOutcome(projection.outcome)
		observation.Session = SessionState(projection.finalSession)
		if !validProcessOutcome(observation.Outcome) ||
			!validProcessSession(observation.Session) {
			return Observation{}, false
		}
	default:
		return Observation{}, false
	}
	return observation, true
}

func candidateNumberText(value any) (string, bool) {
	number, ok := value.(interface{ String() string })
	if !ok {
		return "", true
	}
	return number.String(), false
}

func candidateStringValue(value any) (string, bool) {
	text, ok := value.(string)
	return text, !ok
}

func candidateNullableStringValue(value any) (string, bool) {
	if value == nil {
		return "", false
	}
	return candidateStringValue(value)
}

func candidateBoolValue(value any) (bool, bool) {
	boolean, ok := value.(bool)
	return boolean, !ok
}

func validProcessSession(session SessionState) bool {
	switch session {
	case SessionOffline, SessionOnline, SessionUnknown:
		return true
	default:
		return false
	}
}

func validProcessOutcome(outcome CommandOutcome) bool {
	switch outcome {
	case OutcomeLoggedIn, OutcomeAlreadyOnline, OutcomeLoggedOut, OutcomeAlreadyOffline:
		return true
	default:
		return false
	}
}

func processCommandExit(exit int) (CommandExitCode, bool) {
	switch CommandExitCode(exit) {
	case CommandExitSuccess,
		CommandExitInternal,
		CommandExitInvalid,
		CommandExitNetwork,
		CommandExitAuthentication,
		CommandExitSessionConflict,
		CommandExitProtocolChanged,
		CommandExitInteractionRequired,
		CommandExitCanceled:
		return CommandExitCode(exit), true
	default:
		return CommandExitInternal, false
	}
}

func defaultProcessErrorCode(exit CommandExitCode) ErrorCode {
	switch exit {
	case CommandExitInternal:
		return ErrorInternal
	case CommandExitInvalid:
		return ErrorUnsupported
	case CommandExitNetwork, CommandExitCanceled:
		return ErrorNetwork
	case CommandExitAuthentication:
		return ErrorAuthentication
	case CommandExitSessionConflict:
		return ErrorSessionConflict
	case CommandExitProtocolChanged:
		return ErrorProtocolChanged
	case CommandExitInteractionRequired:
		return ErrorInteractionRequired
	default:
		return ""
	}
}

func runnerFaultObservation() Observation {
	return Observation{RunnerFault: true}
}

func internalProcessObservation() Observation {
	return runnerFaultObservation()
}

func canceledProcessObservation() Observation {
	code := ErrorNetwork
	return Observation{ExitCode: CommandExitCanceled, ErrorCode: &code}
}

func validProcessConfiguration(candidatePath, profile string) bool {
	return candidatePath != "" &&
		filepath.IsAbs(candidatePath) &&
		filepath.Clean(candidatePath) == candidatePath &&
		profile != "" &&
		strings.TrimSpace(profile) == profile
}
