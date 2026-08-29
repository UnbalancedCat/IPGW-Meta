package livegate

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type scriptedCall struct {
	action      CommandAction
	observation Observation
	before      func(context.Context)
	check       func(context.Context)
}

type scriptedExecutor struct {
	t      *testing.T
	script []scriptedCall
	seen   []CommandAction
}

func (e *scriptedExecutor) Execute(ctx context.Context, action CommandAction) Observation {
	e.t.Helper()
	index := len(e.seen)
	e.seen = append(e.seen, action)
	if index >= len(e.script) {
		e.t.Fatalf("unexpected action %q", action)
	}
	call := e.script[index]
	if action != call.action {
		e.t.Fatalf("action %d = %q, want %q", index, action, call.action)
	}
	if call.check != nil {
		call.check(ctx)
	}
	if call.before != nil {
		call.before(ctx)
	}
	return call.observation
}

func (e *scriptedExecutor) requireConsumed() {
	e.t.Helper()
	if len(e.seen) != len(e.script) {
		e.t.Fatalf("executed %d actions, want %d", len(e.seen), len(e.script))
	}
}

func TestStateMachineHappyPaths(t *testing.T) {
	for _, suite := range []Suite{SuitePasswordCore, SuiteTerminalQR} {
		t.Run(string(suite), func(t *testing.T) {
			actions := testPrimaryActions(suite)
			executor := &scriptedExecutor{t: t, script: happyScript(actions)}
			steps, result, canceled, err := (StateMachine{Executor: executor}).Run(context.Background(), suite)
			if err != nil || canceled || result != ResultPass {
				t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
			}
			executor.requireConsumed()
			if got, want := stepNames(steps), testPrimaryStepNames(suite); !reflect.DeepEqual(got, want) {
				t.Fatalf("step names = %#v, want %#v", got, want)
			}
			for _, step := range steps {
				if step.Result != ResultPass || step.ExitCode != CommandExitSuccess || step.ErrorCode != nil {
					t.Fatalf("non-pass happy step: %#v", step)
				}
			}
			if err := validateStepSequence(suite, result, steps); err != nil {
				t.Fatalf("schema rejected happy sequence: %v", err)
			}
		})
	}
}

func TestStateMachineStopsAtEveryPrimaryFailure(t *testing.T) {
	for _, suite := range []Suite{SuitePasswordCore, SuiteTerminalQR} {
		actions := testPrimaryActions(suite)
		statusIndex := indexOfAction(actions, ActionStatusOnline)
		finalIndex := indexOfAction(actions, ActionFinalStatus)
		for failureIndex, failedAction := range actions {
			t.Run(string(suite)+"/"+string(failedAction), func(t *testing.T) {
				script := happyScript(actions[:failureIndex+1])
				script[failureIndex].observation = failedObservation()
				cleanupExpected := failureIndex > statusIndex && failureIndex <= finalIndex
				if cleanupExpected {
					script = append(script, happyCleanupScript()...)
				}

				executor := &scriptedExecutor{t: t, script: script}
				steps, result, canceled, err := (StateMachine{Executor: executor}).Run(context.Background(), suite)
				if err != nil || canceled || result != ResultFail {
					t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
				}
				executor.requireConsumed()
				wantCount := failureIndex + 1
				recordedFailureIndex := failureIndex
				if suite == SuiteTerminalQR && failedAction == ActionStatusOnline {
					wantCount = 2
					recordedFailureIndex = 1
				}
				if cleanupExpected {
					wantCount += 2
				}
				if len(steps) != wantCount || steps[recordedFailureIndex].Result != ResultFail {
					t.Fatalf("failure prefix = %#v, want %d steps", steps, wantCount)
				}
				if err := validateStepSequence(suite, result, steps); err != nil {
					t.Fatalf("schema rejected failure prefix: %v", err)
				}
			})
		}
	}
}

func TestStateMachineExitClassification(t *testing.T) {
	tests := []struct {
		name        string
		observation Observation
		want        Result
	}{
		{"network", commandFailure(CommandExitNetwork, ErrorNetwork), ResultBlocked},
		{"interaction", commandFailure(CommandExitInteractionRequired, ErrorInteractionRequired), ResultBlocked},
		{"config", commandFailure(CommandExitInvalid, ErrorConfig), ResultBlocked},
		{"candidate 130", commandFailure(CommandExitCanceled, ErrorNetwork), ResultBlocked},
		{"authentication", commandFailure(CommandExitAuthentication, ErrorAuthentication), ResultFail},
		{"invalid argument", commandFailure(CommandExitInvalid, ErrorInvalidArgument), ResultFail},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &scriptedExecutor{t: t, script: []scriptedCall{{
				action: ActionInitialStatus, observation: test.observation,
			}}}
			steps, result, canceled, err := (StateMachine{Executor: executor}).Run(context.Background(), SuitePasswordCore)
			if err != nil || canceled || result != test.want || len(steps) != 1 {
				t.Fatalf("Run() steps=%#v result=%q canceled=%v err=%v", steps, result, canceled, err)
			}
			if steps[0].Result != test.want {
				t.Fatalf("step result = %q, want %q", steps[0].Result, test.want)
			}
			if err := validateStepSequence(SuitePasswordCore, result, steps); err != nil {
				t.Fatalf("schema rejected classified exit: %v", err)
			}
		})
	}
}

func TestStateMachineSuccessfulSemanticClassification(t *testing.T) {
	actions := testPrimaryActions(SuitePasswordCore)
	tests := []struct {
		name        string
		index       int
		observation Observation
		want        Result
		cleanup     bool
	}{
		{"initial online blocks", 0, Observation{Session: SessionOnline}, ResultBlocked, false},
		{"login opposite fails", 1, Observation{Outcome: OutcomeAlreadyOnline}, ResultFail, false},
		{"login identity mismatch fails", 1, Observation{Outcome: OutcomeLoggedIn, Session: SessionOnline}, ResultFail, false},
		{"login offline fails", 1, Observation{Outcome: OutcomeLoggedIn, Session: SessionOffline}, ResultFail, false},
		{"login unknown session blocks", 1, Observation{Outcome: OutcomeLoggedIn, Session: SessionUnknown}, ResultBlocked, false},
		{"identity mismatch fails", 2, Observation{Session: SessionOnline}, ResultFail, false},
		{"second login opposite fails", 3, Observation{
			Outcome: OutcomeLoggedIn, Session: SessionOnline, IdentityMatches: true,
		}, ResultFail, true},
		{"logout opposite fails", 4, Observation{Outcome: OutcomeAlreadyOffline}, ResultFail, true},
		{"logout unknown blocks", 4, Observation{Outcome: OutcomeLoggedOut, Session: SessionUnknown}, ResultBlocked, true},
		{"logout online fails", 4, Observation{Outcome: OutcomeLoggedOut, Session: SessionOnline}, ResultFail, true},
		{"final unknown blocks", 5, Observation{Session: SessionUnknown}, ResultBlocked, true},
		{"final online fails", 5, Observation{Session: SessionOnline}, ResultFail, true},
		{"second logout opposite fails", 6, Observation{Outcome: OutcomeLoggedOut}, ResultFail, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			script := happyScript(actions[:test.index+1])
			script[test.index].observation = test.observation
			if test.cleanup {
				script = append(script, happyCleanupScript()...)
			}
			executor := &scriptedExecutor{t: t, script: script}
			steps, result, canceled, err := (StateMachine{Executor: executor}).Run(context.Background(), SuitePasswordCore)
			if err != nil || canceled || result != test.want {
				t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
			}
			executor.requireConsumed()
			if steps[test.index].Result != test.want {
				t.Fatalf("semantic result = %q, want %q", steps[test.index].Result, test.want)
			}
			if err := validateStepSequence(SuitePasswordCore, result, steps); err != nil {
				t.Fatalf("schema rejected semantic sequence: %v", err)
			}
		})
	}
}

func TestStateMachineCleanupRunsBothStepsAndAggregatesFailure(t *testing.T) {
	actions := testPrimaryActions(SuitePasswordCore)[:4]
	script := happyScript(actions)
	script[3].observation = commandFailure(CommandExitNetwork, ErrorNetwork)

	checkCleanupContext := func(ctx context.Context) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("cleanup context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 25*time.Second || remaining > defaultCleanupTimeout {
			t.Fatalf("cleanup deadline remaining = %v", remaining)
		}
	}
	cleanup := happyCleanupScript()
	cleanup[0].observation = failedObservation()
	cleanup[0].check = checkCleanupContext
	cleanup[1].check = checkCleanupContext
	script = append(script, cleanup...)

	executor := &scriptedExecutor{t: t, script: script}
	steps, result, canceled, err := (StateMachine{Executor: executor}).Run(context.Background(), SuitePasswordCore)
	if err != nil || canceled || result != ResultFail {
		t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
	}
	executor.requireConsumed()
	if len(steps) != 6 ||
		steps[3].Result != ResultBlocked ||
		steps[4].Result != ResultFail ||
		steps[5].Result != ResultPass {
		t.Fatalf("unexpected cleanup aggregation: %#v", steps)
	}
	if err := validateStepSequence(SuitePasswordCore, result, steps); err != nil {
		t.Fatalf("schema rejected cleanup sequence: %v", err)
	}
}

func TestStateMachineCleanupDeadlinesRecordBothBlockedSteps(t *testing.T) {
	actions := testPrimaryActions(SuitePasswordCore)[:4]
	script := happyScript(actions)
	script[3].observation = commandFailure(CommandExitNetwork, ErrorNetwork)

	var cleanupDeadlines []time.Time
	waitForCleanupDeadline := func(ctx context.Context) {
		t.Helper()
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("cleanup context has no deadline")
		}
		cleanupDeadlines = append(cleanupDeadlines, deadline)
		<-ctx.Done()
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("cleanup context error = %v", ctx.Err())
		}
	}
	cleanup := happyCleanupScript()
	cleanup[0].check = waitForCleanupDeadline
	cleanup[1].check = waitForCleanupDeadline
	script = append(script, cleanup...)

	base := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	executor := &scriptedExecutor{t: t, script: script}
	steps, result, canceled, err := (StateMachine{
		Executor:       executor,
		Now:            func() time.Time { return base },
		CleanupTimeout: 5 * time.Millisecond,
	}).Run(context.Background(), SuitePasswordCore)
	if err != nil || canceled || result != ResultBlocked {
		t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
	}
	executor.requireConsumed()
	if len(cleanupDeadlines) != 2 || !cleanupDeadlines[1].After(cleanupDeadlines[0]) {
		t.Fatalf("cleanup deadlines = %#v, want two independent increasing deadlines", cleanupDeadlines)
	}

	network := ErrorNetwork
	wantCleanup := []Step{
		{
			Name:      StepCleanupLogout,
			Result:    ResultBlocked,
			ExitCode:  CommandExitCanceled,
			ErrorCode: &network,
		},
		{
			Name:      StepCleanupStatusOffline,
			Result:    ResultBlocked,
			ExitCode:  CommandExitCanceled,
			ErrorCode: &network,
		},
	}
	if got := steps[len(steps)-2:]; !reflect.DeepEqual(got, wantCleanup) {
		t.Fatalf("cleanup timeout steps = %#v, want %#v", got, wantCleanup)
	}
	if err := validateStepSequence(SuitePasswordCore, result, steps); err != nil {
		t.Fatalf("schema rejected cleanup timeout sequence: %v", err)
	}
}

func TestStateMachineCleanupDeadlineAtObservationBoundaries(t *testing.T) {
	base := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	network := ErrorNetwork
	want := Step{
		Name:      StepCleanupLogout,
		Result:    ResultBlocked,
		ExitCode:  CommandExitCanceled,
		ErrorCode: &network,
	}

	t.Run("before execute", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		<-ctx.Done()

		executor := &scriptedExecutor{t: t}
		got, err := (StateMachine{Executor: executor}).executeCleanupStep(
			ctx,
			func() time.Time { return base },
			cleanupActions[0],
		)
		if err != nil || !reflect.DeepEqual(got, want) || len(executor.seen) != 0 {
			t.Fatalf("step=%#v seen=%#v err=%v", got, executor.seen, err)
		}
	})

	t.Run("during finished clock", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
		defer cancel()
		executor := &scriptedExecutor{t: t, script: []scriptedCall{{
			action:      ActionCleanupLogout,
			observation: happyObservation(ActionCleanupLogout),
		}}}
		nowCalls := 0
		now := func() time.Time {
			nowCalls++
			if nowCalls == 2 {
				<-ctx.Done()
			}
			return base
		}
		got, err := (StateMachine{Executor: executor}).executeCleanupStep(
			ctx,
			now,
			cleanupActions[0],
		)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("step=%#v err=%v", got, err)
		}
		executor.requireConsumed()
	})
}

func TestStateMachineParentCancellationUsesFreshCleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	actions := testPrimaryActions(SuitePasswordCore)[:4]
	script := happyScript(actions)
	script[3].observation = commandFailure(CommandExitCanceled, ErrorNetwork)
	script[3].before = func(context.Context) { cancel() }

	cleanup := happyCleanupScript()
	for index := range cleanup {
		cleanup[index].check = func(cleanupCtx context.Context) {
			if cleanupCtx.Err() != nil {
				t.Fatalf("cleanup inherited canceled parent: %v", cleanupCtx.Err())
			}
			if _, ok := cleanupCtx.Deadline(); !ok {
				t.Fatal("cleanup context has no deadline")
			}
		}
	}
	script = append(script, cleanup...)

	executor := &scriptedExecutor{t: t, script: script}
	steps, result, canceled, err := (StateMachine{Executor: executor}).Run(ctx, SuitePasswordCore)
	if err != nil || !canceled || result != ResultBlocked {
		t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
	}
	executor.requireConsumed()
	want := []StepName{
		StepInitialStatusOffline,
		StepLoginLoggedIn,
		StepStatusOnline,
		StepCleanupLogout,
		StepCleanupStatusOffline,
	}
	if got := stepNames(steps); !reflect.DeepEqual(got, want) {
		t.Fatalf("canceled step names = %#v, want %#v", got, want)
	}
}

func TestStateMachineAlreadyCanceledDoesNotExecute(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	executor := &scriptedExecutor{t: t}
	steps, result, canceled, err := (StateMachine{Executor: executor}).Run(ctx, SuitePasswordCore)
	if err != nil || !canceled || result != ResultBlocked || len(steps) != 0 {
		t.Fatalf("Run() steps=%#v result=%q canceled=%v err=%v", steps, result, canceled, err)
	}
	if len(executor.seen) != 0 {
		t.Fatalf("executed actions after cancellation: %#v", executor.seen)
	}
}

func TestStateMachineClockCancellationStopsBeforeExecutorCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	nowCalls := 0
	now := func() time.Time {
		nowCalls++
		cancel()
		return time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	}
	executor := &scriptedExecutor{t: t}
	steps, result, canceled, err := (StateMachine{
		Executor: executor,
		Now:      now,
	}).Run(ctx, SuitePasswordCore)
	if err != nil || !canceled || result != ResultBlocked || len(steps) != 0 {
		t.Fatalf("Run() steps=%#v result=%q canceled=%v err=%v", steps, result, canceled, err)
	}
	if nowCalls != 1 || len(executor.seen) != 0 {
		t.Fatalf("nowCalls=%d executed=%#v", nowCalls, executor.seen)
	}
}

func TestStateMachineDurationsAreFlooredAndNonnegative(t *testing.T) {
	base := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	tests := []struct {
		name  string
		times []time.Time
		want  int64
	}{
		{"floor fractional seconds", []time.Time{base, base.Add(2900 * time.Millisecond)}, 2},
		{"clock reversal", []time.Time{base, base.Add(-time.Second)}, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index := 0
			now := func() time.Time {
				value := test.times[index]
				index++
				return value
			}
			executor := &scriptedExecutor{t: t, script: []scriptedCall{{
				action:      ActionInitialStatus,
				observation: commandFailure(CommandExitNetwork, ErrorNetwork),
			}}}
			steps, result, canceled, err := (StateMachine{Executor: executor, Now: now}).Run(context.Background(), SuitePasswordCore)
			if err != nil || canceled || result != ResultBlocked || len(steps) != 1 {
				t.Fatalf("Run() steps=%#v result=%q canceled=%v err=%v", steps, result, canceled, err)
			}
			if steps[0].DurationSeconds != test.want {
				t.Fatalf("duration = %d, want %d", steps[0].DurationSeconds, test.want)
			}
		})
	}
}

func TestStateMachineRejectsMalformedObservationWithFixedError(t *testing.T) {
	code := ErrorAuthentication
	executor := &scriptedExecutor{t: t, script: []scriptedCall{{
		action:      ActionInitialStatus,
		observation: Observation{ExitCode: CommandExitInvalid, ErrorCode: &code},
	}}}
	steps, result, canceled, err := (StateMachine{Executor: executor}).Run(context.Background(), SuitePasswordCore)
	if !errors.Is(err, ErrInvalidObservation) || err.Error() != "livegate: invalid command observation" {
		t.Fatalf("error = %v", err)
	}
	if canceled || result != ResultBlocked || len(steps) != 0 {
		t.Fatalf("Run() steps=%#v result=%q canceled=%v", steps, result, canceled)
	}
}

func TestStateMachineRejectsInvalidConfiguration(t *testing.T) {
	if _, _, _, err := (StateMachine{}).Run(context.Background(), SuitePasswordCore); !errors.Is(err, ErrInvalidStateMachine) {
		t.Fatalf("nil executor error = %v", err)
	}
	executor := &scriptedExecutor{t: t}
	if _, _, _, err := (StateMachine{Executor: executor}).Run(context.Background(), Suite("future")); !errors.Is(err, ErrInvalidSuite) {
		t.Fatalf("invalid suite error = %v", err)
	}
}

func testPrimaryActions(suite Suite) []CommandAction {
	switch suite {
	case SuitePasswordCore:
		return []CommandAction{
			ActionInitialStatus,
			ActionPasswordLogin,
			ActionStatusOnline,
			ActionSecondPasswordLogin,
			ActionLogout,
			ActionFinalStatus,
			ActionSecondLogout,
		}
	case SuiteTerminalQR:
		return []CommandAction{
			ActionInitialStatus,
			ActionQRLogin,
			ActionStatusOnline,
			ActionLogout,
			ActionFinalStatus,
		}
	default:
		panic("unsupported suite")
	}
}

func testPrimaryStepNames(suite Suite) []StepName {
	switch suite {
	case SuitePasswordCore:
		return []StepName{
			StepInitialStatusOffline,
			StepLoginLoggedIn,
			StepStatusOnline,
			StepSecondLoginAlreadyOnline,
			StepLogoutLoggedOut,
			StepFinalStatusOffline,
			StepSecondLogoutAlreadyOffline,
		}
	case SuiteTerminalQR:
		return []StepName{
			StepInitialStatusOffline,
			StepQRLoginLoggedIn,
			StepStatusOnline,
			StepLogoutLoggedOut,
			StepFinalStatusOffline,
		}
	default:
		panic("unsupported suite")
	}
}

func happyScript(actions []CommandAction) []scriptedCall {
	script := make([]scriptedCall, 0, len(actions))
	for _, action := range actions {
		script = append(script, scriptedCall{action: action, observation: happyObservation(action)})
	}
	return script
}

func happyCleanupScript() []scriptedCall {
	return happyScript([]CommandAction{ActionCleanupLogout, ActionCleanupStatus})
}

func happyObservation(action CommandAction) Observation {
	switch action {
	case ActionInitialStatus, ActionFinalStatus, ActionCleanupStatus:
		return Observation{Session: SessionOffline}
	case ActionPasswordLogin:
		return Observation{Outcome: OutcomeLoggedIn, Session: SessionOnline, IdentityMatches: true}
	case ActionQRLogin:
		return Observation{}
	case ActionStatusOnline:
		return Observation{Session: SessionOnline, IdentityMatches: true}
	case ActionSecondPasswordLogin:
		return Observation{
			Outcome:         OutcomeAlreadyOnline,
			Session:         SessionOnline,
			IdentityMatches: true,
		}
	case ActionLogout, ActionCleanupLogout:
		return Observation{Outcome: OutcomeLoggedOut, Session: SessionOffline}
	case ActionSecondLogout:
		return Observation{Outcome: OutcomeAlreadyOffline, Session: SessionOffline}
	default:
		panic("unsupported action")
	}
}

func failedObservation() Observation {
	return commandFailure(CommandExitAuthentication, ErrorAuthentication)
}

func commandFailure(exit CommandExitCode, code ErrorCode) Observation {
	cloned := code
	return Observation{ExitCode: exit, ErrorCode: &cloned}
}

func stepNames(steps []Step) []StepName {
	names := make([]StepName, len(steps))
	for index, step := range steps {
		names[index] = step.Name
	}
	return names
}

func indexOfAction(actions []CommandAction, target CommandAction) int {
	for index, action := range actions {
		if action == target {
			return index
		}
	}
	return -1
}
func TestStateMachineQRProofPairing(t *testing.T) {
	network := commandFailure(CommandExitNetwork, ErrorNetwork)
	tests := []struct {
		name        string
		status      Observation
		want        Result
		wantExit    CommandExitCode
		wantErrCode *ErrorCode
	}{
		{"offline fails", Observation{Session: SessionOffline}, ResultFail, CommandExitSuccess, nil},
		{"identity drift blocks", Observation{Session: SessionOnline}, ResultBlocked, CommandExitSuccess, nil},
		{"unknown blocks", Observation{Session: SessionUnknown}, ResultBlocked, CommandExitSuccess, nil},
		{"network blocks", network, ResultBlocked, CommandExitNetwork, network.ErrorCode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor := &scriptedExecutor{t: t, script: []scriptedCall{
				{action: ActionInitialStatus, observation: Observation{Session: SessionOffline}},
				{action: ActionQRLogin, observation: Observation{}},
				{action: ActionStatusOnline, observation: test.status},
			}}
			steps, result, canceled, err := (StateMachine{Executor: executor}).Run(
				context.Background(),
				SuiteTerminalQR,
			)
			if err != nil || canceled || result != test.want {
				t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
			}
			executor.requireConsumed()
			if got := stepNames(steps); !reflect.DeepEqual(got, []StepName{
				StepInitialStatusOffline,
				StepQRLoginLoggedIn,
			}) {
				t.Fatalf("step names = %#v", got)
			}
			qrStep := steps[1]
			if qrStep.Result != test.want || qrStep.ExitCode != test.wantExit {
				t.Fatalf("QR proof step = %#v", qrStep)
			}
			if (qrStep.ErrorCode == nil) != (test.wantErrCode == nil) {
				t.Fatalf("QR proof error code = %v, want %v", qrStep.ErrorCode, test.wantErrCode)
			}
			if qrStep.ErrorCode != nil && *qrStep.ErrorCode != *test.wantErrCode {
				t.Fatalf("QR proof error code = %v, want %v", qrStep.ErrorCode, test.wantErrCode)
			}
			if err := validateStepSequence(SuiteTerminalQR, result, steps); err != nil {
				t.Fatalf("schema rejected folded QR proof: %v", err)
			}
		})
	}
}

func TestStateMachineQRStatusRunnerFaultDoesNotSynthesizePass(t *testing.T) {
	executor := &scriptedExecutor{t: t, script: []scriptedCall{
		{action: ActionInitialStatus, observation: Observation{Session: SessionOffline}},
		{action: ActionQRLogin, observation: Observation{}},
		{action: ActionStatusOnline, observation: Observation{RunnerFault: true}},
	}}
	steps, result, canceled, err := (StateMachine{Executor: executor}).Run(
		context.Background(),
		SuiteTerminalQR,
	)
	if !errors.Is(err, ErrCommandRunnerFault) || canceled || result != ResultBlocked {
		t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
	}
	executor.requireConsumed()
	if got := stepNames(steps); !reflect.DeepEqual(got, []StepName{StepInitialStatusOffline}) {
		t.Fatalf("fault synthesized QR evidence: %#v", got)
	}
}

func TestStateMachineRunnerFaultCleanupOwnership(t *testing.T) {
	t.Run("before ownership", func(t *testing.T) {
		executor := &scriptedExecutor{t: t, script: []scriptedCall{{
			action: ActionInitialStatus, observation: Observation{RunnerFault: true},
		}}}
		steps, result, canceled, err := (StateMachine{Executor: executor}).Run(
			context.Background(),
			SuitePasswordCore,
		)
		if !errors.Is(err, ErrCommandRunnerFault) ||
			canceled ||
			result != ResultBlocked ||
			len(steps) != 0 {
			t.Fatalf("Run() steps=%#v result=%q canceled=%v err=%v", steps, result, canceled, err)
		}
		executor.requireConsumed()
	})

	t.Run("primary fault after ownership", func(t *testing.T) {
		script := happyScript(testPrimaryActions(SuitePasswordCore)[:4])
		script[3].observation = Observation{RunnerFault: true}
		script = append(script, happyCleanupScript()...)
		executor := &scriptedExecutor{t: t, script: script}
		steps, result, canceled, err := (StateMachine{Executor: executor}).Run(
			context.Background(),
			SuitePasswordCore,
		)
		if !errors.Is(err, ErrCommandRunnerFault) || canceled || result != ResultBlocked {
			t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
		}
		executor.requireConsumed()
		want := []StepName{
			StepInitialStatusOffline,
			StepLoginLoggedIn,
			StepStatusOnline,
			StepCleanupLogout,
			StepCleanupStatusOffline,
		}
		if got := stepNames(steps); !reflect.DeepEqual(got, want) {
			t.Fatalf("fault/cleanup steps = %#v, want %#v", got, want)
		}
	})

	for _, faultAction := range []CommandAction{ActionCleanupLogout, ActionCleanupStatus} {
		t.Run(string(faultAction), func(t *testing.T) {
			script := happyScript(testPrimaryActions(SuitePasswordCore)[:4])
			script[3].observation = commandFailure(CommandExitNetwork, ErrorNetwork)
			cleanup := happyCleanupScript()
			if faultAction == ActionCleanupLogout {
				cleanup[0].observation = Observation{RunnerFault: true}
			} else {
				cleanup[1].observation = Observation{RunnerFault: true}
			}
			script = append(script, cleanup...)
			executor := &scriptedExecutor{t: t, script: script}
			steps, result, canceled, err := (StateMachine{Executor: executor}).Run(
				context.Background(),
				SuitePasswordCore,
			)
			if !errors.Is(err, ErrCommandRunnerFault) || canceled || result != ResultBlocked {
				t.Fatalf("Run() result=%q canceled=%v err=%v", result, canceled, err)
			}
			executor.requireConsumed()
			if faultAction == ActionCleanupLogout &&
				steps[len(steps)-1].Name != StepCleanupStatusOffline {
				t.Fatalf("cleanup status did not run after cleanup logout fault: %#v", steps)
			}
		})
	}
}

func TestStateMachineRealCandidateExitInternalIsEvidenceFailure(t *testing.T) {
	internal := commandFailure(CommandExitInternal, ErrorInternal)
	executor := &scriptedExecutor{t: t, script: []scriptedCall{{
		action: ActionInitialStatus, observation: internal,
	}}}
	steps, result, canceled, err := (StateMachine{Executor: executor}).Run(
		context.Background(),
		SuitePasswordCore,
	)
	if err != nil || canceled || result != ResultFail || len(steps) != 1 {
		t.Fatalf("Run() steps=%#v result=%q canceled=%v err=%v", steps, result, canceled, err)
	}
	if steps[0].ExitCode != CommandExitInternal ||
		steps[0].ErrorCode == nil ||
		*steps[0].ErrorCode != ErrorInternal {
		t.Fatalf("candidate exit1 was not retained: %#v", steps[0])
	}
}
