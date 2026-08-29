package livegate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	processTestUsername = "student-id"
	processTestCanary   = "process-sensitive-canary"
)

type processTestStep struct {
	output string
	exit   int
	err    error
}

type processTestCall struct {
	path   string
	args   []string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

type processTestScript struct {
	steps []processTestStep
	calls []processTestCall
}

func (script *processTestScript) run(
	_ context.Context,
	path string,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) (int, error) {
	call := processTestCall{
		path:   path,
		args:   append([]string(nil), args...),
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}
	script.calls = append(script.calls, call)
	index := len(script.calls) - 1
	if index >= len(script.steps) {
		return -1, errors.New("unexpected process test invocation")
	}

	step := script.steps[index]
	if step.output != "" && stdout != nil {
		_, _ = io.WriteString(stdout, step.output)
	}
	return step.exit, step.err
}

func newProcessTestExecutor(
	t *testing.T,
	steps ...processTestStep,
) (*ProcessExecutor, *processTestScript) {
	t.Helper()

	directory := t.TempDir()
	stdin := openProcessTestFile(t, directory, "stdin")
	stdout := openProcessTestFile(t, directory, "stdout")
	stderr := openProcessTestFile(t, directory, "stderr")
	script := &processTestScript{steps: steps}
	return &ProcessExecutor{
		CandidatePath: filepath.Join(directory, "ipgw-meta"),
		Profile:       "campus",
		Stdin:         stdin,
		Stdout:        stdout,
		Stderr:        stderr,
		run:           script.run,
	}, script
}

func openProcessTestFile(t *testing.T, directory, name string) *os.File {
	t.Helper()

	file, err := os.OpenFile(
		filepath.Join(directory, name),
		os.O_CREATE|os.O_RDWR|os.O_EXCL,
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = file.Close()
	})
	return file
}

func processSuccessEnvelope(command, data string) string {
	return fmt.Sprintf(
		`{"schema_version":1,"command":%q,"ok":true,"data":%s}`,
		command,
		data,
	)
}

func processFailureEnvelope(command string, code ErrorCode) string {
	return fmt.Sprintf(
		`{"schema_version":1,"command":%q,"ok":false,"error":{"code":%q}}`,
		command,
		code,
	)
}

func processProfileEnvelope(username, credential string) string {
	return processSuccessEnvelope(
		"profile.show",
		fmt.Sprintf(
			`{"profile":{"username":%q,"credential":%s}}`,
			username,
			credential,
		),
	)
}

func requireProcessCall(
	t *testing.T,
	executor *ProcessExecutor,
	script *processTestScript,
	index int,
	args []string,
	stdin io.Reader,
	jsonMode bool,
) {
	t.Helper()

	if len(script.calls) <= index {
		t.Fatalf("call %d missing; got %d calls", index, len(script.calls))
	}
	call := script.calls[index]
	if call.path != executor.CandidatePath {
		t.Fatalf("candidate path = %q, want %q", call.path, executor.CandidatePath)
	}
	if !reflect.DeepEqual(call.args, args) {
		t.Fatalf("args = %#v, want %#v", call.args, args)
	}
	if call.stdin != stdin {
		t.Fatalf("stdin identity differs")
	}
	if call.stderr != executor.Stderr {
		t.Fatalf("stderr was not connected directly")
	}
	if jsonMode {
		if call.stdout == executor.Stdout {
			t.Fatalf("JSON stdout was connected directly")
		}
	} else if call.stdout != executor.Stdout {
		t.Fatalf("human stdout was not connected directly")
	}
}

func requireProcessFailure(
	t *testing.T,
	observation Observation,
	exit CommandExitCode,
	code ErrorCode,
) {
	t.Helper()
	if observation.RunnerFault {
		t.Fatal("product failure was marked as a runner fault")
	}

	if observation.ExitCode != exit {
		t.Fatalf("exit = %d, want %d", observation.ExitCode, exit)
	}
	if observation.ErrorCode == nil || *observation.ErrorCode != code {
		t.Fatalf("error code = %v, want %q", observation.ErrorCode, code)
	}
	if observation.Session != "" ||
		observation.Outcome != "" ||
		observation.IdentityMatches {
		t.Fatalf("failure retained success projection: %+v", observation)
	}
}

func requireInternalProcessFailure(t *testing.T, observation Observation) {
	t.Helper()
	if !observation.RunnerFault {
		t.Fatalf("observation = %+v, want fixed runner fault", observation)
	}
	if observation.ExitCode != CommandExitSuccess ||
		observation.ErrorCode != nil ||
		observation.Session != "" ||
		observation.Outcome != "" ||
		observation.IdentityMatches {
		t.Fatalf("runner fault retained product fields: %+v", observation)
	}
}

func TestProcessExecutorPreparePasswordProfile(t *testing.T) {
	for _, test := range []struct {
		name       string
		credential string
	}{
		{"reference_absent", `{"provider":"prompt"}`},
		{"reference_empty", `{"provider":"prompt","reference":""}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor, script := newProcessTestExecutor(t, processTestStep{
				output: processProfileEnvelope(processTestUsername, test.credential),
			})
			executor.expectedUsername = "stale-value"

			if err := executor.PrepareProfile(context.Background(), SuitePasswordCore); err != nil {
				t.Fatalf("PrepareProfile() error = %v", err)
			}
			if executor.expectedUsername != processTestUsername {
				t.Fatalf("expected username was not retained in memory")
			}
			if len(script.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(script.calls))
			}
			requireProcessCall(
				t,
				executor,
				script,
				0,
				[]string{"--json", "--profile", "campus", "profile", "show"},
				nil,
				true,
			)
		})
	}
}

func TestProcessExecutorPrepareTerminalQRProfileIgnoresPasswordProvider(t *testing.T) {
	executor, script := newProcessTestExecutor(t, processTestStep{
		output: processProfileEnvelope(
			processTestUsername,
			`{"provider":"env","reference":"IPGW_META_PASSWORD"}`,
		),
	})

	if err := executor.PrepareProfile(context.Background(), SuiteTerminalQR); err != nil {
		t.Fatalf("PrepareProfile() error = %v", err)
	}
	if executor.expectedUsername != processTestUsername {
		t.Fatalf("expected username was not retained")
	}
	if len(script.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(script.calls))
	}
}

func TestProcessExecutorPrepareProfileRejectsUnsafeOrInvalidProfile(t *testing.T) {
	tests := []struct {
		name        string
		output      string
		exit        int
		err         error
		runnerFault bool
	}{
		{
			name:   "password_provider",
			output: processProfileEnvelope(processTestUsername, `{"provider":"env"}`),
		},
		{
			name:   "password_reference",
			output: processProfileEnvelope(processTestUsername, `{"provider":"prompt","reference":"ref"}`),
		},
		{
			name:   "empty_username",
			output: processProfileEnvelope("", `{"provider":"prompt"}`),
		},
		{
			name:        "missing_provider",
			output:      processProfileEnvelope(processTestUsername, `{}`),
			runnerFault: true,
		},
		{
			name: "failure_envelope",
			output: fmt.Sprintf(
				`{"schema_version":1,"command":"profile.show","ok":false,"error":{"code":"config","message":%q,"details":{"value":%q}}}`,
				processTestCanary,
				processTestCanary,
			),
			exit: 2,
			err:  errors.New(processTestCanary),
		},
		{
			name:        "run_error_on_success_exit",
			output:      processProfileEnvelope(processTestUsername, `{"provider":"prompt"}`),
			err:         errors.New(processTestCanary),
			runnerFault: true,
		},
		{
			name:        "invalid_json",
			output:      `{"schema_version":1`,
			runnerFault: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, script := newProcessTestExecutor(t, processTestStep{
				output: test.output,
				exit:   test.exit,
				err:    test.err,
			})
			executor.expectedUsername = "stale-value"

			err := executor.PrepareProfile(context.Background(), SuitePasswordCore)
			want := ErrProcessProfile
			if test.runnerFault {
				want = ErrProcessRunnerFault
			}
			if !errors.Is(err, want) {
				t.Fatalf("PrepareProfile() error = %v, want %v", err, want)
			}
			if strings.Contains(err.Error(), processTestCanary) {
				t.Fatalf("profile error leaked candidate-derived detail")
			}
			if executor.expectedUsername != "" {
				t.Fatalf("failed preflight retained stale username")
			}
			if len(script.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(script.calls))
			}
		})
	}
}

func TestProcessExecutorPrepareProfileRejectsConfigurationBeforeRun(t *testing.T) {
	executor, script := newProcessTestExecutor(t)
	executor.expectedUsername = "stale-value"

	err := executor.PrepareProfile(context.Background(), Suite(""))
	if !errors.Is(err, ErrProcessProfile) {
		t.Fatalf("PrepareProfile() error = %v, want ErrProcessProfile", err)
	}
	if executor.expectedUsername != "" {
		t.Fatalf("invalid preflight retained stale username")
	}
	if len(script.calls) != 0 {
		t.Fatalf("invalid preflight invoked candidate")
	}
}

func TestProcessExecutorJSONActionArgumentsAndIO(t *testing.T) {
	tests := []struct {
		name      string
		action    CommandAction
		command   string
		data      string
		args      []string
		ttyStdin  bool
		wantState SessionState
		wantOut   CommandOutcome
	}{
		{
			"initial_status",
			ActionInitialStatus,
			"status",
			`{"session":"offline","username":null}`,
			[]string{"--json", "--profile", "campus", "status"},
			false,
			SessionOffline,
			"",
		},
		{
			"status_online",
			ActionStatusOnline,
			"status",
			fmt.Sprintf(`{"session":"online","username":%q}`, processTestUsername),
			[]string{"--json", "--profile", "campus", "status"},
			false,
			SessionOnline,
			"",
		},
		{
			"final_status",
			ActionFinalStatus,
			"status",
			`{"session":"offline","username":null}`,
			[]string{"--json", "--profile", "campus", "status"},
			false,
			SessionOffline,
			"",
		},
		{
			"cleanup_status",
			ActionCleanupStatus,
			"status",
			`{"session":"offline","username":null}`,
			[]string{"--json", "--profile", "campus", "status"},
			false,
			SessionOffline,
			"",
		},
		{
			"password_login",
			ActionPasswordLogin,
			"login",
			fmt.Sprintf(
				`{"outcome":"logged_in","status":{"session":"online","username":%q}}`,
				processTestUsername,
			),
			[]string{"--json", "--profile", "campus", "login", "--method", "password"},
			true,
			SessionOnline,
			OutcomeLoggedIn,
		},
		{
			"second_password_login_eof",
			ActionSecondPasswordLogin,
			"login",
			fmt.Sprintf(
				`{"outcome":"already_online","status":{"session":"online","username":%q}}`,
				processTestUsername,
			),
			[]string{"--json", "--profile", "campus", "login", "--method", "password"},
			false,
			SessionOnline,
			OutcomeAlreadyOnline,
		},
		{
			"logout",
			ActionLogout,
			"logout",
			`{"outcome":"logged_out","status":{"session":"offline","username":null}}`,
			[]string{"--json", "--profile", "campus", "logout"},
			false,
			SessionOffline,
			OutcomeLoggedOut,
		},
		{
			"second_logout",
			ActionSecondLogout,
			"logout",
			`{"outcome":"already_offline","status":{"session":"offline","username":null}}`,
			[]string{"--json", "--profile", "campus", "logout"},
			false,
			SessionOffline,
			OutcomeAlreadyOffline,
		},
		{
			"cleanup_logout",
			ActionCleanupLogout,
			"logout",
			`{"outcome":"logged_out","status":{"session":"offline","username":null}}`,
			[]string{"--json", "--profile", "campus", "logout"},
			false,
			SessionOffline,
			OutcomeLoggedOut,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, script := newProcessTestExecutor(t, processTestStep{
				output: processSuccessEnvelope(test.command, test.data),
			})
			executor.expectedUsername = processTestUsername

			observation := executor.Execute(context.Background(), test.action)
			if observation.ExitCode != CommandExitSuccess ||
				observation.ErrorCode != nil ||
				observation.Session != test.wantState ||
				observation.Outcome != test.wantOut {
				t.Fatalf("observation = %+v", observation)
			}
			if test.wantState == SessionOnline && !observation.IdentityMatches {
				t.Fatalf("online identity did not match")
			}
			if len(script.calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(script.calls))
			}
			var wantStdin io.Reader
			if test.ttyStdin {
				wantStdin = executor.Stdin
			}
			requireProcessCall(
				t,
				executor,
				script,
				0,
				test.args,
				wantStdin,
				true,
			)
		})
	}
}

func TestProcessExecutorStatusIdentityProjection(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		session SessionState
		matches bool
	}{
		{
			"offline",
			`{"session":"offline","username":null}`,
			SessionOffline,
			false,
		},
		{
			"online_match",
			fmt.Sprintf(`{"session":"online","username":%q}`, processTestUsername),
			SessionOnline,
			true,
		},
		{
			"online_mismatch",
			`{"session":"online","username":"different-user"}`,
			SessionOnline,
			false,
		},
		{
			"unknown",
			`{"session":"unknown","username":null}`,
			SessionUnknown,
			false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, _ := newProcessTestExecutor(t, processTestStep{
				output: processSuccessEnvelope("status", test.data),
			})
			executor.expectedUsername = processTestUsername

			observation := executor.Execute(context.Background(), ActionInitialStatus)
			if observation.ExitCode != CommandExitSuccess ||
				observation.ErrorCode != nil ||
				observation.Session != test.session ||
				observation.IdentityMatches != test.matches {
				t.Fatalf("observation = %+v", observation)
			}
		})
	}
}

func TestProcessExecutorFailureExitMapping(t *testing.T) {
	tests := []struct {
		name string
		exit CommandExitCode
		code ErrorCode
	}{
		{"internal", CommandExitInternal, ErrorInternal},
		{"invalid_argument", CommandExitInvalid, ErrorInvalidArgument},
		{"config", CommandExitInvalid, ErrorConfig},
		{"unsupported", CommandExitInvalid, ErrorUnsupported},
		{"network", CommandExitNetwork, ErrorNetwork},
		{"authentication", CommandExitAuthentication, ErrorAuthentication},
		{"session_conflict", CommandExitSessionConflict, ErrorSessionConflict},
		{"protocol_changed", CommandExitProtocolChanged, ErrorProtocolChanged},
		{"interaction_required", CommandExitInteractionRequired, ErrorInteractionRequired},
		{"candidate_canceled", CommandExitCanceled, ErrorNetwork},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, _ := newProcessTestExecutor(t, processTestStep{
				output: processFailureEnvelope("status", test.code),
				exit:   int(test.exit),
				err:    errors.New("candidate returned nonzero"),
			})
			executor.expectedUsername = processTestUsername

			observation := executor.Execute(context.Background(), ActionInitialStatus)
			requireProcessFailure(t, observation, test.exit, test.code)
		})
	}
}

func TestProcessExecutorRejectsInvalidJSONEnvelope(t *testing.T) {
	validData := `{"session":"offline","username":null}`
	oversized := processSuccessEnvelope("status", validData) +
		strings.Repeat(" ", MaxCandidateJSONBytes)
	invalidUTF8 := processSuccessEnvelope("status", validData) + string([]byte{0xff})

	tests := []struct {
		name   string
		output string
		exit   int
		err    error
	}{
		{"schema_string", `{"schema_version":"1","command":"status","ok":true,"data":{"session":"offline","username":null}}`, 0, nil},
		{"schema_decimal", `{"schema_version":1.0,"command":"status","ok":true,"data":{"session":"offline","username":null}}`, 0, nil},
		{"wrong_command", processSuccessEnvelope("login", validData), 0, nil},
		{"missing_ok", `{"schema_version":1,"command":"status","data":{"session":"offline","username":null}}`, 0, nil},
		{"failure_on_success_exit", processFailureEnvelope("status", ErrorInternal), 0, nil},
		{
			"data_and_error",
			`{"schema_version":1,"command":"status","ok":true,"data":{"session":"offline","username":null},"error":{"code":"internal"}}`,
			0,
			nil,
		},
		{
			"data_and_empty_error",
			`{"schema_version":1,"command":"status","ok":true,"data":{"session":"offline","username":null},"error":{}}`,
			0,
			nil,
		},
		{
			"data_and_escaped_empty_error",
			`{"schema_version":1,"command":"status","ok":true,"data":{"session":"offline","username":null},"\u0065rror":{}}`,
			0,
			nil,
		},
		{
			"failure_and_empty_data",
			`{"schema_version":1,"command":"status","ok":false,"data":{},"error":{"code":"network"}}`,
			int(CommandExitNetwork),
			errors.New(processTestCanary),
		},
		{"missing_session", processSuccessEnvelope("status", `{"future":true}`), 0, nil},
		{"invalid_session", processSuccessEnvelope("status", `{"session":"connected","username":null}`), 0, nil},
		{
			"nested_duplicate",
			`{"schema_version":1,"command":"status","ok":true,"data":{"session":"offline","username":null,"future":{"x":1,"x":2}}}`,
			0,
			nil,
		},
		{"trailing_value", processSuccessEnvelope("status", validData) + `{}`, 0, nil},
		{"oversized", oversized, 0, nil},
		{"invalid_utf8", invalidUTF8, 0, nil},
		{
			"exit_error_on_success",
			processSuccessEnvelope("status", validData),
			0,
			errors.New(processTestCanary),
		},
		{
			"mismatched_exit_error",
			processFailureEnvelope("status", ErrorNetwork),
			int(CommandExitInvalid),
			errors.New(processTestCanary),
		},
		{
			"unknown_exit",
			processFailureEnvelope("status", ErrorInternal),
			8,
			errors.New(processTestCanary),
		},
		{
			"unknown_error_code",
			`{"schema_version":1,"command":"status","ok":false,"error":{"code":"future_code"}}`,
			int(CommandExitInternal),
			errors.New(processTestCanary),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, _ := newProcessTestExecutor(t, processTestStep{
				output: test.output,
				exit:   test.exit,
				err:    test.err,
			})
			executor.expectedUsername = processTestUsername

			observation := executor.Execute(context.Background(), ActionInitialStatus)
			requireInternalProcessFailure(t, observation)
			if strings.Contains(fmt.Sprintf("%+v", observation), processTestCanary) {
				t.Fatalf("observation leaked candidate-derived detail")
			}
		})
	}
}

func TestProcessExecutorSkipsAdditiveFieldsWithoutRetainingThem(t *testing.T) {
	output := fmt.Sprintf(
		`{"schema_version":1,"future":{"value":%q},"command":"status","ok":true,"data":{"session":"offline","username":null,"ip":%q}}`,
		processTestCanary,
		processTestCanary,
	)
	executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
	executor.expectedUsername = processTestUsername

	observation := executor.Execute(context.Background(), ActionInitialStatus)
	if observation.ExitCode != CommandExitSuccess ||
		observation.ErrorCode != nil ||
		observation.Session != SessionOffline {
		t.Fatalf("observation = %+v", observation)
	}
	if strings.Contains(fmt.Sprintf("%+v", observation), processTestCanary) {
		t.Fatalf("observation retained an additive field")
	}
}

func TestProcessExecutorFailureProjectionDoesNotRetainMessageOrDetails(t *testing.T) {
	output := fmt.Sprintf(
		`{"schema_version":1,"command":"status","ok":false,"error":{"code":"network","message":%q,"details":{"response":%q}}}`,
		processTestCanary,
		processTestCanary,
	)
	executor, _ := newProcessTestExecutor(t, processTestStep{
		output: output,
		exit:   int(CommandExitNetwork),
		err:    errors.New(processTestCanary),
	})
	executor.expectedUsername = processTestUsername

	observation := executor.Execute(context.Background(), ActionInitialStatus)
	requireProcessFailure(t, observation, CommandExitNetwork, ErrorNetwork)
	if strings.Contains(fmt.Sprintf("%+v", observation), processTestCanary) {
		t.Fatalf("observation retained candidate message or details")
	}
}

func TestProcessExecutorStartErrorIsFixedInternalFailure(t *testing.T) {
	executor, _ := newProcessTestExecutor(t, processTestStep{
		exit: -1,
		err:  errors.New(processTestCanary),
	})
	executor.expectedUsername = processTestUsername

	observation := executor.Execute(context.Background(), ActionInitialStatus)
	requireInternalProcessFailure(t, observation)
	if strings.Contains(fmt.Sprintf("%+v", observation), processTestCanary) {
		t.Fatalf("observation leaked process start error")
	}
}

func TestProcessExecutorContextCancellationWins(t *testing.T) {
	executor, _ := newProcessTestExecutor(t, processTestStep{
		exit: int(CommandExitCanceled),
		err:  context.Canceled,
	})
	executor.expectedUsername = processTestUsername
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	observation := executor.Execute(ctx, ActionInitialStatus)
	requireProcessFailure(t, observation, CommandExitCanceled, ErrorNetwork)
}

func TestProcessExecutorQRUsesDirectPrivateTTY(t *testing.T) {
	executor, script := newProcessTestExecutor(t, processTestStep{})
	executor.expectedUsername = processTestUsername

	observation := executor.Execute(context.Background(), ActionQRLogin)
	if observation.RunnerFault ||
		observation.ExitCode != CommandExitSuccess ||
		observation.ErrorCode != nil ||
		observation.Outcome != "" {
		t.Fatalf("observation = %+v", observation)
	}
	requireProcessCall(
		t,
		executor,
		script,
		0,
		[]string{"--profile", "campus", "login", "--method", "qr"},
		executor.Stdin,
		false,
	)
}

func TestProcessExecutorQRExitMapping(t *testing.T) {
	tests := []struct {
		name string
		exit CommandExitCode
		code ErrorCode
	}{
		{"internal", CommandExitInternal, ErrorInternal},
		{"unsupported", CommandExitInvalid, ErrorUnsupported},
		{"network", CommandExitNetwork, ErrorNetwork},
		{"authentication", CommandExitAuthentication, ErrorAuthentication},
		{"session_conflict", CommandExitSessionConflict, ErrorSessionConflict},
		{"protocol_changed", CommandExitProtocolChanged, ErrorProtocolChanged},
		{"interaction_required", CommandExitInteractionRequired, ErrorInteractionRequired},
		{"candidate_canceled", CommandExitCanceled, ErrorNetwork},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, _ := newProcessTestExecutor(t, processTestStep{
				exit: int(test.exit),
				err:  errors.New(processTestCanary),
			})
			executor.expectedUsername = processTestUsername

			observation := executor.Execute(context.Background(), ActionQRLogin)
			requireProcessFailure(t, observation, test.exit, test.code)
		})
	}
}

func TestProcessExecutorQRCancellationWins(t *testing.T) {
	executor, _ := newProcessTestExecutor(t, processTestStep{
		exit: int(CommandExitCanceled),
		err:  context.Canceled,
	})
	executor.expectedUsername = processTestUsername
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	observation := executor.Execute(ctx, ActionQRLogin)
	requireProcessFailure(t, observation, CommandExitCanceled, ErrorNetwork)
}

func TestProcessExecutorRejectsUnpreparedOrUnknownActionWithoutRun(t *testing.T) {
	executor, script := newProcessTestExecutor(t)

	observation := executor.Execute(context.Background(), ActionInitialStatus)
	requireInternalProcessFailure(t, observation)
	if len(script.calls) != 0 {
		t.Fatalf("unprepared executor invoked candidate")
	}

	executor.expectedUsername = processTestUsername
	observation = executor.Execute(context.Background(), CommandAction("future_action"))
	requireInternalProcessFailure(t, observation)
	if len(script.calls) != 0 {
		t.Fatalf("unknown action invoked candidate")
	}
}

func TestProcessExecutorQRRequiresAllPrivateTTYStreams(t *testing.T) {
	executor, script := newProcessTestExecutor(t)
	executor.expectedUsername = processTestUsername
	executor.Stdout = nil

	observation := executor.Execute(context.Background(), ActionQRLogin)
	requireInternalProcessFailure(t, observation)
	if len(script.calls) != 0 {
		t.Fatalf("QR invocation ran without complete private TTY")
	}
}
func TestProcessExecutorCommandPayloadProjectionIsolation(t *testing.T) {
	t.Run("profile_uses_only_profile_payload", func(t *testing.T) {
		output := fmt.Sprintf(
			`{"schema_version":1,"command":"profile.show","ok":true,"data":{"username":7,"status":{"session":false,"username":false},"profile":{"username":%q,"credential":{"provider":"prompt"}}}}`,
			processTestUsername,
		)
		executor, _ := newProcessTestExecutor(t, processTestStep{output: output})

		if err := executor.PrepareProfile(context.Background(), SuitePasswordCore); err != nil {
			t.Fatalf("PrepareProfile() error = %v", err)
		}
		if executor.expectedUsername != processTestUsername {
			t.Fatalf("profile projection used a noncanonical username")
		}
	})

	t.Run("status_uses_only_root_status", func(t *testing.T) {
		output := processSuccessEnvelope(
			"status",
			`{"session":"offline","username":null,"status":{"session":7,"username":false}}`,
		)
		executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
		executor.expectedUsername = processTestUsername

		observation := executor.Execute(context.Background(), ActionInitialStatus)
		if observation.ExitCode != CommandExitSuccess ||
			observation.Session != SessionOffline ||
			observation.IdentityMatches {
			t.Fatalf("observation = %+v", observation)
		}
	})

	t.Run("login_uses_and_preserves_only_final_status", func(t *testing.T) {
		output := processSuccessEnvelope(
			"login",
			fmt.Sprintf(
				`{"outcome":"logged_in","session":7,"username":false,"status":{"session":"online","username":%q}}`,
				processTestUsername,
			),
		)
		executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
		executor.expectedUsername = processTestUsername

		observation := executor.Execute(context.Background(), ActionPasswordLogin)
		if observation.ExitCode != CommandExitSuccess ||
			observation.Outcome != OutcomeLoggedIn ||
			observation.Session != SessionOnline ||
			!observation.IdentityMatches {
			t.Fatalf("observation = %+v", observation)
		}
	})

	t.Run("logout_uses_only_final_status", func(t *testing.T) {
		output := processSuccessEnvelope(
			"logout",
			fmt.Sprintf(
				`{"outcome":"logged_out","session":"online","username":%q,"status":{"session":"offline","username":null}}`,
				processTestUsername,
			),
		)
		executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
		executor.expectedUsername = processTestUsername

		observation := executor.Execute(context.Background(), ActionLogout)
		if observation.ExitCode != CommandExitSuccess ||
			observation.Outcome != OutcomeLoggedOut ||
			observation.Session != SessionOffline {
			t.Fatalf("observation = %+v", observation)
		}
	})
}
func TestProcessExecutorDottedKeysCannotCollideWithCanonicalPaths(t *testing.T) {
	t.Run("top level dotted data key is ignored", func(t *testing.T) {
		output := `{"schema_version":1,"command":"status","ok":true,"data.session":"online","data.username":"wrong","data":{"session":"offline","username":null}}`
		executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
		executor.expectedUsername = processTestUsername

		observation := executor.Execute(context.Background(), ActionInitialStatus)
		if observation.RunnerFault ||
			observation.Session != SessionOffline ||
			observation.IdentityMatches {
			t.Fatalf("observation = %+v", observation)
		}
	})

	t.Run("nested dotted status key is ignored", func(t *testing.T) {
		output := processSuccessEnvelope(
			"login",
			fmt.Sprintf(
				`{"outcome":"logged_in","status.session":"offline","status.username":"wrong","status":{"session":"online","username":%q}}`,
				processTestUsername,
			),
		)
		executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
		executor.expectedUsername = processTestUsername

		observation := executor.Execute(context.Background(), ActionPasswordLogin)
		if observation.RunnerFault ||
			observation.Session != SessionOnline ||
			!observation.IdentityMatches {
			t.Fatalf("observation = %+v", observation)
		}
	})

	t.Run("dotted data key cannot replace data object", func(t *testing.T) {
		output := `{"schema_version":1,"command":"status","ok":true,"data.session":"offline","data.username":null}`
		executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
		executor.expectedUsername = processTestUsername
		requireInternalProcessFailure(t, executor.Execute(context.Background(), ActionInitialStatus))
	})

	t.Run("dotted error key cannot replace error object field", func(t *testing.T) {
		output := `{"schema_version":1,"command":"status","ok":false,"error.code":"network","error":{}}`
		executor, _ := newProcessTestExecutor(t, processTestStep{
			output: output,
			exit:   int(CommandExitNetwork),
			err:    errors.New("candidate returned nonzero"),
		})
		executor.expectedUsername = processTestUsername
		requireInternalProcessFailure(t, executor.Execute(context.Background(), ActionInitialStatus))
	})

	t.Run("dotted profile key cannot replace canonical username", func(t *testing.T) {
		output := processSuccessEnvelope(
			"profile.show",
			fmt.Sprintf(
				`{"profile.username":%q,"profile":{"credential":{"provider":"prompt"}}}`,
				processTestUsername,
			),
		)
		executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
		if err := executor.PrepareProfile(context.Background(), SuitePasswordCore); !errors.Is(err, ErrProcessRunnerFault) {
			t.Fatalf("PrepareProfile() error = %v, want runner fault", err)
		}
	})
}

func TestProcessExecutorRejectsCanonicalContainerTypeConfusion(t *testing.T) {
	tests := []struct {
		name    string
		command string
		data    string
		action  CommandAction
	}{
		{"data null", "status", `null`, ActionInitialStatus},
		{"data array", "status", `[]`, ActionInitialStatus},
		{"status username object", "status", `{"session":"offline","username":{}}`, ActionInitialStatus},
		{"status username array", "status", `{"session":"offline","username":[]}`, ActionInitialStatus},
		{"login status null", "login", `{"outcome":"logged_in","status":null}`, ActionPasswordLogin},
		{"login status array", "login", `{"outcome":"logged_in","status":[]}`, ActionPasswordLogin},
		{"login username object", "login", `{"outcome":"logged_in","status":{"session":"online","username":{}}}`, ActionPasswordLogin},
		{"logout username array", "logout", `{"outcome":"logged_out","status":{"session":"offline","username":[]}}`, ActionLogout},
		{"outcome object", "login", `{"outcome":{},"status":{"session":"online","username":"student-id"}}`, ActionPasswordLogin},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			executor, _ := newProcessTestExecutor(t, processTestStep{
				output: processSuccessEnvelope(test.command, test.data),
			})
			executor.expectedUsername = processTestUsername
			requireInternalProcessFailure(t, executor.Execute(context.Background(), test.action))
		})
	}

	for _, test := range []struct {
		name  string
		error string
	}{
		{"error null", `null`},
		{"error array", `[]`},
		{"code null", `{"code":null}`},
		{"code object", `{"code":{}}`},
		{"code array", `{"code":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := fmt.Sprintf(
				`{"schema_version":1,"command":"status","ok":false,"error":%s}`,
				test.error,
			)
			executor, _ := newProcessTestExecutor(t, processTestStep{
				output: output,
				exit:   int(CommandExitNetwork),
				err:    errors.New("candidate returned nonzero"),
			})
			executor.expectedUsername = processTestUsername
			requireInternalProcessFailure(t, executor.Execute(context.Background(), ActionInitialStatus))
		})
	}
}

func TestProcessExecutorRejectsProfileCredentialContainerTypeConfusion(t *testing.T) {
	credentials := map[string]string{
		"profile null":     `null`,
		"profile array":    `[]`,
		"credential null":  `{"username":"student-id","credential":null}`,
		"credential array": `{"username":"student-id","credential":[]}`,
		"provider object":  `{"username":"student-id","credential":{"provider":{}}}`,
		"reference null":   `{"username":"student-id","credential":{"provider":"prompt","reference":null}}`,
		"reference object": `{"username":"student-id","credential":{"provider":"prompt","reference":{}}}`,
		"reference array":  `{"username":"student-id","credential":{"provider":"prompt","reference":[]}}`,
	}
	for name, profile := range credentials {
		t.Run(name, func(t *testing.T) {
			output := processSuccessEnvelope("profile.show", fmt.Sprintf(`{"profile":%s}`, profile))
			executor, _ := newProcessTestExecutor(t, processTestStep{output: output})
			if err := executor.PrepareProfile(context.Background(), SuitePasswordCore); !errors.Is(err, ErrProcessRunnerFault) {
				t.Fatalf("PrepareProfile() error = %v, want runner fault", err)
			}
		})
	}
}
