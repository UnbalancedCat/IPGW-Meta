package livegate

import (
	"context"
	"errors"
	"time"
)

const defaultCleanupTimeout = 30 * time.Second

var (
	ErrInvalidStateMachine = errors.New("livegate: invalid state machine")
	ErrInvalidSuite        = errors.New("livegate: invalid state machine suite")
	ErrInvalidObservation  = errors.New("livegate: invalid command observation")
	ErrCommandRunnerFault  = errors.New("livegate: command runner fault")
)

type CommandAction string

const (
	ActionInitialStatus       CommandAction = "initial_status"
	ActionPasswordLogin       CommandAction = "password_login"
	ActionQRLogin             CommandAction = "qr_login"
	ActionStatusOnline        CommandAction = "status_online"
	ActionSecondPasswordLogin CommandAction = "second_password_login"
	ActionLogout              CommandAction = "logout"
	ActionFinalStatus         CommandAction = "final_status"
	ActionSecondLogout        CommandAction = "second_logout"
	ActionCleanupLogout       CommandAction = "cleanup_logout"
	ActionCleanupStatus       CommandAction = "cleanup_status"
)

type SessionState string

const (
	SessionOffline SessionState = "offline"
	SessionOnline  SessionState = "online"
	SessionUnknown SessionState = "unknown"
)

type CommandOutcome string

const (
	OutcomeLoggedIn       CommandOutcome = "logged_in"
	OutcomeAlreadyOnline  CommandOutcome = "already_online"
	OutcomeLoggedOut      CommandOutcome = "logged_out"
	OutcomeAlreadyOffline CommandOutcome = "already_offline"
)

type Observation struct {
	ExitCode        CommandExitCode
	ErrorCode       *ErrorCode
	Session         SessionState
	Outcome         CommandOutcome
	IdentityMatches bool
	RunnerFault     bool
}

type CommandExecutor interface {
	Execute(context.Context, CommandAction) Observation
}

type StateMachine struct {
	Executor       CommandExecutor
	Now            func() time.Time
	CleanupTimeout time.Duration
}

type actionStep struct {
	action CommandAction
	name   StepName
}

var passwordPrimaryActions = [...]actionStep{
	{ActionInitialStatus, StepInitialStatusOffline},
	{ActionPasswordLogin, StepLoginLoggedIn},
	{ActionStatusOnline, StepStatusOnline},
	{ActionSecondPasswordLogin, StepSecondLoginAlreadyOnline},
	{ActionLogout, StepLogoutLoggedOut},
	{ActionFinalStatus, StepFinalStatusOffline},
	{ActionSecondLogout, StepSecondLogoutAlreadyOffline},
}

var terminalQRPrimaryActions = [...]actionStep{
	{ActionInitialStatus, StepInitialStatusOffline},
	{ActionQRLogin, StepQRLoginLoggedIn},
	{ActionStatusOnline, StepStatusOnline},
	{ActionLogout, StepLogoutLoggedOut},
	{ActionFinalStatus, StepFinalStatusOffline},
}

var cleanupActions = [...]actionStep{
	{ActionCleanupLogout, StepCleanupLogout},
	{ActionCleanupStatus, StepCleanupStatusOffline},
}

func (s StateMachine) Run(ctx context.Context, suite Suite) (steps []Step, result Result, canceled bool, err error) {
	if ctx == nil || s.Executor == nil {
		return nil, ResultBlocked, false, ErrInvalidStateMachine
	}
	primary, ok := primaryActions(suite)
	if !ok {
		return nil, ResultBlocked, false, ErrInvalidSuite
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	cleanupOwned := false
	finalOffline := false
	primaryComplete := true
	for index := 0; index < len(primary); {
		if ctx.Err() != nil {
			canceled = true
			primaryComplete = false
			break
		}

		spec := primary[index]
		if suite == SuiteTerminalQR && spec.action == ActionQRLogin {
			pair, pairPassed, pairCanceled, pairErr := s.executeQRPair(ctx, now)
			if pairCanceled || ctx.Err() != nil {
				canceled = true
				primaryComplete = false
				break
			}
			if pairErr != nil {
				primaryComplete = false
				err = pairErr
				break
			}
			steps = append(steps, pair...)
			if !pairPassed {
				primaryComplete = false
				break
			}
			cleanupOwned = true
			index += 2
			continue
		}

		step, stepErr := s.executeStep(ctx, now, spec)
		if ctx.Err() != nil {
			canceled = true
			primaryComplete = false
			break
		}
		if stepErr != nil {
			primaryComplete = false
			err = stepErr
			break
		}
		steps = append(steps, step)
		if spec.action == ActionStatusOnline && step.Result == ResultPass {
			cleanupOwned = true
		}
		if spec.action == ActionFinalStatus && step.Result == ResultPass {
			finalOffline = true
		}
		if step.Result != ResultPass {
			primaryComplete = false
			break
		}
		index++
	}

	if cleanupOwned && !finalOffline && (!primaryComplete || canceled || err != nil) {
		cleanupTimeout := s.CleanupTimeout
		if cleanupTimeout <= 0 {
			cleanupTimeout = defaultCleanupTimeout
		}
		for _, spec := range cleanupActions {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
			step, stepErr := s.executeCleanupStep(cleanupCtx, now, spec)
			cancel()
			if stepErr != nil {
				if err == nil {
					err = stepErr
				}
				continue
			}
			steps = append(steps, step)
		}
	}

	if primaryComplete && !canceled && err == nil {
		return steps, ResultPass, false, nil
	}
	return steps, aggregateNonPass(steps), canceled, err
}

func primaryActions(suite Suite) ([]actionStep, bool) {
	switch suite {
	case SuitePasswordCore:
		return passwordPrimaryActions[:], true
	case SuiteTerminalQR:
		return terminalQRPrimaryActions[:], true
	default:
		return nil, false
	}
}

func (s StateMachine) executeStep(ctx context.Context, now func() time.Time, spec actionStep) (Step, error) {
	observation, duration := s.observeAction(ctx, now, spec.action)
	if observation.RunnerFault {
		return Step{}, ErrCommandRunnerFault
	}
	return stepFromObservation(spec, observation, duration)
}

// executeCleanupStep preserves an independent cleanup deadline as evidence;
// primary cancellation remains suppressed by executeStep/observeAction.
func (s StateMachine) executeCleanupStep(ctx context.Context, now func() time.Time, spec actionStep) (Step, error) {
	observation, duration := s.observeCleanupAction(ctx, now, spec.action)
	if observation.RunnerFault {
		return Step{}, ErrCommandRunnerFault
	}
	return stepFromObservation(spec, observation, duration)
}

// executeQRPair keeps the human QR exit and its immediately following JSON
// status inseparable. qr_login_logged_in is emitted as pass only after that
// status proves the expected identity online.
func (s StateMachine) executeQRPair(
	ctx context.Context,
	now func() time.Time,
) (steps []Step, passed bool, canceled bool, err error) {
	qrSpec := actionStep{action: ActionQRLogin, name: StepQRLoginLoggedIn}
	statusSpec := actionStep{action: ActionStatusOnline, name: StepStatusOnline}

	qrObservation, qrDuration := s.observeAction(ctx, now, qrSpec.action)
	if ctx.Err() != nil {
		return nil, false, true, nil
	}
	if qrObservation.RunnerFault {
		return nil, false, false, ErrCommandRunnerFault
	}
	if qrObservation.ExitCode != CommandExitSuccess {
		qrStep, err := stepFromObservation(qrSpec, qrObservation, qrDuration)
		if err != nil {
			return nil, false, false, err
		}
		return []Step{qrStep}, false, false, nil
	}
	if qrObservation.ErrorCode != nil ||
		qrObservation.Session != "" ||
		qrObservation.Outcome != "" ||
		qrObservation.IdentityMatches {
		return nil, false, false, ErrInvalidObservation
	}
	qrStep := Step{
		Name:            StepQRLoginLoggedIn,
		ExitCode:        CommandExitSuccess,
		DurationSeconds: qrDuration,
	}

	statusObservation, statusDuration := s.observeAction(ctx, now, statusSpec.action)
	if ctx.Err() != nil {
		return nil, false, true, nil
	}
	if statusObservation.RunnerFault {
		return nil, false, false, ErrCommandRunnerFault
	}
	statusStep, err := stepFromObservation(statusSpec, statusObservation, statusDuration)
	if err != nil {
		return nil, false, false, err
	}
	if statusObservation.ExitCode == CommandExitSuccess &&
		statusObservation.Session == SessionOnline &&
		!statusObservation.IdentityMatches {
		statusStep.Result = ResultBlocked
	}
	if statusStep.Result == ResultPass {
		qrStep.Result = ResultPass
		return []Step{qrStep, statusStep}, true, false, nil
	}

	// The QR step is a synthesized claim over both invocations. If the status
	// proof fails, record that proof result at the QR position and stop the
	// primary prefix before status_online.
	qrStep.Result = statusStep.Result
	qrStep.DurationSeconds += statusStep.DurationSeconds
	if statusStep.ExitCode != CommandExitSuccess {
		qrStep.ExitCode = statusStep.ExitCode
		qrStep.ErrorCode = cloneErrorCode(statusStep.ErrorCode)
	}
	if validateErr := qrStep.Validate(); validateErr != nil {
		return nil, false, false, ErrInvalidObservation
	}
	return []Step{qrStep}, false, false, nil
}

func (s StateMachine) observeAction(
	ctx context.Context,
	now func() time.Time,
	action CommandAction,
) (Observation, int64) {
	started := now()
	if ctx.Err() != nil {
		return Observation{}, 0
	}
	observation := s.Executor.Execute(ctx, action)
	if ctx.Err() != nil {
		return Observation{}, 0
	}
	finished := now()
	if ctx.Err() != nil {
		return Observation{}, 0
	}
	return observation, elapsedSeconds(started, finished)
}

func (s StateMachine) observeCleanupAction(ctx context.Context, now func() time.Time, action CommandAction) (Observation, int64) {
	started := now()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return cleanupTimeoutObservation(), 0
	}
	observation := s.Executor.Execute(ctx, action)
	finished := now()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return cleanupTimeoutObservation(), elapsedSeconds(started, finished)
	}
	return observation, elapsedSeconds(started, finished)
}

func cleanupTimeoutObservation() Observation {
	code := ErrorNetwork
	return Observation{ExitCode: CommandExitCanceled, ErrorCode: &code}
}

func stepFromObservation(spec actionStep, observation Observation, duration int64) (Step, error) {
	step := Step{
		Name:            spec.name,
		ExitCode:        observation.ExitCode,
		ErrorCode:       cloneErrorCode(observation.ErrorCode),
		DurationSeconds: duration,
	}
	if observation.ExitCode != CommandExitSuccess {
		step.Result = classifyCommandFailure(observation)
	} else {
		semantic, ok := classifySuccessfulObservation(spec.action, observation)
		if !ok {
			return Step{}, ErrInvalidObservation
		}
		step.Result = semantic
	}
	if validateErr := step.Validate(); validateErr != nil {
		return Step{}, ErrInvalidObservation
	}
	return step, nil
}

func classifyCommandFailure(observation Observation) Result {
	if observation.ExitCode == CommandExitCanceled {
		return ResultBlocked
	}
	if observation.ErrorCode == nil {
		return ResultFail
	}
	switch *observation.ErrorCode {
	case ErrorNetwork, ErrorInteractionRequired, ErrorConfig:
		return ResultBlocked
	default:
		return ResultFail
	}
}

func classifySuccessfulObservation(action CommandAction, observation Observation) (Result, bool) {
	if observation.RunnerFault || observation.ErrorCode != nil || !validObservationEnums(observation) {
		return ResultBlocked, false
	}
	switch action {
	case ActionInitialStatus:
		switch observation.Session {
		case SessionOffline:
			return ResultPass, true
		case SessionOnline, SessionUnknown:
			return ResultBlocked, true
		}
	case ActionPasswordLogin:
		if observation.Outcome == "" || observation.Session == SessionUnknown {
			return ResultBlocked, true
		}
		if observation.Outcome == OutcomeLoggedIn &&
			observation.Session == SessionOnline && observation.IdentityMatches {
			return ResultPass, true
		}
		return ResultFail, true
	case ActionQRLogin:
		switch observation.Outcome {
		case OutcomeLoggedIn:
			return ResultPass, true
		case OutcomeAlreadyOnline, OutcomeLoggedOut, OutcomeAlreadyOffline:
			return ResultFail, true
		case "":
			return ResultBlocked, true
		}
	case ActionStatusOnline:
		switch observation.Session {
		case SessionUnknown:
			return ResultBlocked, true
		case SessionOffline:
			return ResultFail, true
		case SessionOnline:
			if observation.IdentityMatches {
				return ResultPass, true
			}
			return ResultFail, true
		}
	case ActionSecondPasswordLogin:
		if observation.Outcome == "" || observation.Session == SessionUnknown {
			return ResultBlocked, true
		}
		if observation.Outcome == OutcomeAlreadyOnline &&
			observation.Session == SessionOnline && observation.IdentityMatches {
			return ResultPass, true
		}
		return ResultFail, true
	case ActionLogout:
		switch observation.Outcome {
		case OutcomeLoggedOut:
			if observation.Session == SessionUnknown {
				return ResultBlocked, true
			}
			if observation.Session != SessionOffline {
				return ResultFail, true
			}
			return ResultPass, true
		case OutcomeAlreadyOffline, OutcomeLoggedIn, OutcomeAlreadyOnline:
			return ResultFail, true
		case "":
			return ResultBlocked, true
		}
	case ActionFinalStatus, ActionCleanupStatus:
		switch observation.Session {
		case SessionOffline:
			return ResultPass, true
		case SessionOnline:
			return ResultFail, true
		case SessionUnknown:
			return ResultBlocked, true
		}
	case ActionSecondLogout:
		switch observation.Outcome {
		case OutcomeAlreadyOffline:
			if observation.Session == SessionUnknown {
				return ResultBlocked, true
			}
			if observation.Session != SessionOffline {
				return ResultFail, true
			}
			return ResultPass, true
		case OutcomeLoggedOut, OutcomeLoggedIn, OutcomeAlreadyOnline:
			return ResultFail, true
		case "":
			return ResultBlocked, true
		}
	case ActionCleanupLogout:
		switch observation.Outcome {
		case OutcomeLoggedOut, OutcomeAlreadyOffline:
			if observation.Session == SessionUnknown {
				return ResultBlocked, true
			}
			if observation.Session != SessionOffline {
				return ResultFail, true
			}
			return ResultPass, true
		case OutcomeLoggedIn, OutcomeAlreadyOnline:
			return ResultFail, true
		case "":
			return ResultBlocked, true
		}
	}
	return ResultBlocked, false
}

func validObservationEnums(observation Observation) bool {
	switch observation.Session {
	case "", SessionOffline, SessionOnline, SessionUnknown:
	default:
		return false
	}
	switch observation.Outcome {
	case "", OutcomeLoggedIn, OutcomeAlreadyOnline, OutcomeLoggedOut, OutcomeAlreadyOffline:
		return true
	default:
		return false
	}
}

func cloneErrorCode(code *ErrorCode) *ErrorCode {
	if code == nil {
		return nil
	}
	cloned := *code
	return &cloned
}

func elapsedSeconds(started, finished time.Time) int64 {
	if finished.Before(started) {
		return 0
	}
	return int64(finished.Sub(started) / time.Second)
}

func aggregateNonPass(steps []Step) Result {
	for _, step := range steps {
		if step.Result == ResultFail {
			return ResultFail
		}
	}
	return ResultBlocked
}
