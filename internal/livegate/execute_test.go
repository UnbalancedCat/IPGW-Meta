package livegate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type scriptedPreparedExecutor struct {
	prepareErr   error
	prepare      func()
	observations map[CommandAction]Observation
	actions      []CommandAction
	cancelAction CommandAction
	cancel       context.CancelFunc
}

func (executor *scriptedPreparedExecutor) PrepareProfile(context.Context, Suite) error {
	if executor.prepare != nil {
		executor.prepare()
	}
	return executor.prepareErr
}

func (executor *scriptedPreparedExecutor) Execute(_ context.Context, action CommandAction) Observation {
	executor.actions = append(executor.actions, action)
	if action == executor.cancelAction && executor.cancel != nil {
		executor.cancel()
	}
	return executor.observations[action]
}

type executeHarness struct {
	options Options
	input   *os.File
	output  *os.File
	errOut  *os.File

	executor *scriptedPreparedExecutor

	verifyBeforeErr  error
	verifyAfterErr   error
	publishErr       error
	malformedPublish bool

	verifyBeforeCalls int
	verifyAfterCalls  int
	publishCalls      int
	discardCalls      int
	published         Evidence
}

func newExecuteHarness(t *testing.T, platform Platform, suite Suite) *executeHarness {
	t.Helper()
	root := t.TempDir()
	input, err := os.CreateTemp(root, "input-")
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(root, "output-")
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := os.CreateTemp(root, "error-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = input.Close()
		_ = output.Close()
		_ = errOut.Close()
	})

	candidateName := "ipgw-meta"
	testbed := string(TestbedNASVM)
	network := string(NetworkCampusWired)
	suiteFlag := "password-core"
	if platform == PlatformWindowsAMD64 {
		candidateName = "ipgw-meta.exe"
		testbed = string(TestbedBHKWindows)
	}
	if suite == SuiteTerminalQR {
		suiteFlag = "terminal-qr"
	}
	candidatePath := filepath.Join(root, candidateName)
	manifestPath := filepath.Join(root, "candidate-manifest.json")
	outputDir := filepath.Join(root, "build", "live-evidence")
	explicitSHA := strings.Repeat("c", 64)

	harness := &executeHarness{
		input:  input,
		output: output,
		errOut: errOut,
		executor: &scriptedPreparedExecutor{
			observations: successfulObservations(suite),
		},
	}
	binding := CandidateBinding{
		CandidateID:        "v1.0.0-aaaaaaaaaaaa-1.1",
		CandidateSetSHA256: strings.Repeat("b", 64),
		SourceCommit:       strings.Repeat("a", 40),
		Platform:           platform,
		Name:               candidateName,
		Size:               1,
		SHA256:             explicitSHA,
	}
	current := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	dependencies := &executeDependencies{
		platform: func() (Platform, bool) {
			return platform, true
		},
		isTerminal: func(*os.File) bool {
			return true
		},
		verifyBefore: func(candidate, manifest, sha string, gotPlatform Platform) (VerifiedCandidate, error) {
			harness.verifyBeforeCalls++
			if harness.verifyBeforeErr != nil {
				return VerifiedCandidate{}, harness.verifyBeforeErr
			}
			if candidate != candidatePath || manifest != manifestPath ||
				sha != explicitSHA || gotPlatform != platform {
				t.Fatal("verification received a mutated invocation")
			}
			return VerifiedCandidate{
				Binding:       binding,
				CandidatePath: candidate,
				ManifestPath:  manifest,
			}, nil
		},
		verifyAfter: func(VerifiedCandidate) error {
			harness.verifyAfterCalls++
			return harness.verifyAfterErr
		},
		newExecutor: func(VerifiedCandidate, liveGateInvocation, Options) preparedCommandExecutor {
			return harness.executor
		},
		publish: func(_ context.Context, gotOutput string, evidence Evidence) (PublishedBundle, error) {
			harness.publishCalls++
			if harness.publishErr != nil {
				return PublishedBundle{}, harness.publishErr
			}
			if gotOutput != outputDir || evidence.EvidenceID != "" {
				t.Fatal("publisher received invalid output or preallocated evidence ID")
			}
			started, parseErr := time.Parse(time.RFC3339Nano, string(evidence.StartedAt))
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			evidence.EvidenceID = fmt.Sprintf("EVID-%s-001", started.Format("20060102"))
			if harness.malformedPublish {
				evidence.CandidateID = "invalid"
			}
			harness.published = evidence
			return PublishedBundle{
				Evidence: evidence,
				Path:     filepath.Join(outputDir, binding.CandidateID, evidence.EvidenceID),
			}, nil
		},
		discard: func(PublishedBundle) {
			harness.discardCalls++
		},
		now: func() time.Time {
			value := current
			current = current.Add(time.Second)
			return value
		},
	}
	harness.options = Options{
		Args: []string{
			"run",
			"--candidate", candidatePath,
			"--candidate-sha256", explicitSHA,
			"--candidate-manifest", manifestPath,
			"--suite", suiteFlag,
			"--testbed", testbed,
			"--network", network,
			"--profile", "live-profile",
			"--output-dir", outputDir,
		},
		Stdin:        input,
		Stdout:       output,
		Stderr:       errOut,
		IsTTY:        true,
		dependencies: dependencies,
	}
	return harness
}

func successfulObservations(suite Suite) map[CommandAction]Observation {
	observations := map[CommandAction]Observation{
		ActionInitialStatus: {Session: SessionOffline},
		ActionStatusOnline: {
			Session:         SessionOnline,
			IdentityMatches: true,
		},
		ActionLogout:        {Outcome: OutcomeLoggedOut, Session: SessionOffline},
		ActionFinalStatus:   {Session: SessionOffline},
		ActionCleanupLogout: {Outcome: OutcomeLoggedOut, Session: SessionOffline},
		ActionCleanupStatus: {Session: SessionOffline},
	}
	if suite == SuiteTerminalQR {
		observations[ActionQRLogin] = Observation{}
		return observations
	}
	observations[ActionPasswordLogin] = Observation{
		Outcome:         OutcomeLoggedIn,
		Session:         SessionOnline,
		IdentityMatches: true,
	}
	observations[ActionSecondPasswordLogin] = Observation{
		Outcome:         OutcomeAlreadyOnline,
		Session:         SessionOnline,
		IdentityMatches: true,
	}
	observations[ActionSecondLogout] = Observation{
		Outcome: OutcomeAlreadyOffline,
		Session: SessionOffline,
	}
	return observations
}

func (harness *executeHarness) outputText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(harness.output.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (harness *executeHarness) errorText(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(harness.errOut.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseLiveGateInvocationIsStrict(t *testing.T) {
	valid := []string{
		"run",
		"--candidate", "/private/ipgw-meta",
		"--candidate-sha256", strings.Repeat("a", 64),
		"--candidate-manifest", "/private/candidate-manifest.json",
		"--suite", "password-core",
		"--testbed", "nas_vm",
		"--network", "campus_wired",
		"--profile", "live-profile",
		"--output-dir", "/private/build/live-evidence",
	}
	if invocation, ok := parseLiveGateInvocation(valid); !ok || invocation.suite != SuitePasswordCore {
		t.Fatal("valid exact invocation was rejected")
	}

	tests := map[string][]string{
		"missing":      valid[:len(valid)-2],
		"extra":        append(append([]string(nil), valid...), "extra"),
		"wrong action": replaceArgument(valid, 0, "Run"),
		"equals form":  replaceArgument(valid, 1, "--candidate=/private/ipgw-meta"),
		"unknown flag": replaceArgument(valid, 1, "--binary"),
		"duplicate":    replaceArgument(valid, 13, "--candidate"),
		"suite alias":  replaceArgument(valid, 8, "password_core"),
		"suite case":   replaceArgument(valid, 8, "Password-Core"),
	}
	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			if _, ok := parseLiveGateInvocation(args); ok {
				t.Fatal("non-exact invocation was accepted")
			}
		})
	}
}

func replaceArgument(args []string, index int, replacement string) []string {
	cloned := append([]string(nil), args...)
	cloned[index] = replacement
	return cloned
}

func TestValidLiveGatePreflightClosesMatrixAndPaths(t *testing.T) {
	root := t.TempDir()
	base := liveGateInvocation{
		candidatePath:     filepath.Join(root, "ipgw-meta.exe"),
		candidateManifest: filepath.Join(root, "candidate-manifest.json"),
		suite:             SuitePasswordCore,
		testbed:           TestbedBHKWindows,
		network:           NetworkCampusWired,
		profile:           "live-profile",
		outputDir:         filepath.Join(root, "build", "live-evidence"),
	}
	if !validLiveGatePreflight(base, PlatformWindowsAMD64) {
		t.Fatal("documented Windows wired tuple was rejected")
	}
	wifi := base
	wifi.network = NetworkCampusWiFi
	if !validLiveGatePreflight(wifi, PlatformWindowsAMD64) {
		t.Fatal("documented Windows Wi-Fi tuple was rejected")
	}
	windowsQR := base
	windowsQR.suite = SuiteTerminalQR
	if validLiveGatePreflight(windowsQR, PlatformWindowsAMD64) {
		t.Fatal("undocumented Windows QR tuple was accepted")
	}

	linux := base
	linux.candidatePath = filepath.Join(root, "ipgw-meta")
	linux.testbed = TestbedNASVM
	linux.network = NetworkCampusWired
	if !validLiveGatePreflight(linux, PlatformLinuxAMD64) {
		t.Fatal("documented Linux password tuple was rejected")
	}
	linuxQR := linux
	linuxQR.suite = SuiteTerminalQR
	if !validLiveGatePreflight(linuxQR, PlatformLinuxAMD64) {
		t.Fatal("documented Linux QR tuple was rejected")
	}
	linuxWiFi := linux
	linuxWiFi.network = NetworkCampusWiFi
	if validLiveGatePreflight(linuxWiFi, PlatformLinuxAMD64) {
		t.Fatal("undocumented Linux Wi-Fi tuple was accepted")
	}

	badOutput := base
	badOutput.outputDir = filepath.Join(root, "evidence")
	if validLiveGatePreflight(badOutput, PlatformWindowsAMD64) {
		t.Fatal("non build/live-evidence output was accepted")
	}
	overlap := base
	overlap.candidatePath = filepath.Join(overlap.outputDir, "ipgw-meta.exe")
	if validLiveGatePreflight(overlap, PlatformWindowsAMD64) {
		t.Fatal("candidate inside output directory was accepted")
	}
	badProfile := base
	badProfile.profile = "../profile"
	if validLiveGatePreflight(badProfile, PlatformWindowsAMD64) {
		t.Fatal("invalid profile argument was accepted")
	}
}

func TestExecutePublishesOnlyClosedGateOutcomes(t *testing.T) {
	network := ErrorNetwork
	tests := []struct {
		name       string
		platform   Platform
		suite      Suite
		mutate     func(*scriptedPreparedExecutor)
		wantExit   GateExitCode
		wantResult Result
	}{
		{
			name: "password pass", platform: PlatformWindowsAMD64,
			suite: SuitePasswordCore, wantExit: GateExitPass, wantResult: ResultPass,
		},
		{
			name: "password fail", platform: PlatformWindowsAMD64,
			suite: SuitePasswordCore, wantExit: GateExitFail, wantResult: ResultFail,
			mutate: func(executor *scriptedPreparedExecutor) {
				executor.observations[ActionPasswordLogin] = Observation{
					Outcome: OutcomeAlreadyOnline, Session: SessionOnline, IdentityMatches: true,
				}
			},
		},
		{
			name: "password blocked", platform: PlatformWindowsAMD64,
			suite: SuitePasswordCore, wantExit: GateExitBlocked, wantResult: ResultBlocked,
			mutate: func(executor *scriptedPreparedExecutor) {
				executor.observations[ActionPasswordLogin] = Observation{
					ExitCode: CommandExitNetwork, ErrorCode: &network,
				}
			},
		},
		{
			name: "terminal QR pass", platform: PlatformLinuxAMD64,
			suite: SuiteTerminalQR, wantExit: GateExitPass, wantResult: ResultPass,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newExecuteHarness(t, test.platform, test.suite)
			if test.mutate != nil {
				test.mutate(harness.executor)
			}
			got := Execute(context.Background(), harness.options)
			if got != test.wantExit {
				t.Fatalf("Execute() = %d, want %d", got, test.wantExit)
			}
			if harness.publishCalls != 1 || harness.discardCalls != 0 {
				t.Fatalf("publish=%d discard=%d", harness.publishCalls, harness.discardCalls)
			}
			if harness.published.Result != test.wantResult {
				t.Fatalf("result = %q, want %q", harness.published.Result, test.wantResult)
			}
			if err := harness.published.Validate(); err != nil {
				t.Fatalf("published evidence invalid: %v", err)
			}
			wantOutput := fmt.Sprintf(
				"ipgw-live-gate: result=%s evidence_id=%s\n",
				test.wantResult,
				harness.published.EvidenceID,
			)
			if gotOutput := harness.outputText(t); gotOutput != wantOutput {
				t.Fatalf("output = %q, want %q", gotOutput, wantOutput)
			}
			if gotError := harness.errorText(t); gotError != "" {
				t.Fatalf("unexpected diagnostics: %q", gotError)
			}
			if strings.Contains(harness.outputText(t), "live-profile") ||
				strings.Contains(harness.outputText(t), string(os.PathSeparator)) {
				t.Fatal("safe output leaked a profile or path")
			}
		})
	}
}

func TestExecuteTerminalBoundaryChecksRealFileDescriptors(t *testing.T) {
	t.Run("QR stdout must be terminal", func(t *testing.T) {
		harness := newExecuteHarness(t, PlatformLinuxAMD64, SuiteTerminalQR)
		harness.options.dependencies.isTerminal = func(file *os.File) bool {
			return file != harness.output
		}
		if got := Execute(context.Background(), harness.options); got != GateExitSecurityReject {
			t.Fatalf("Execute() = %d, want %d", got, GateExitSecurityReject)
		}
		if harness.verifyBeforeCalls != 0 || len(harness.executor.actions) != 0 {
			t.Fatal("QR stdout redirection started candidate work")
		}
	})

	t.Run("password does not use stdout for interaction", func(t *testing.T) {
		harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
		harness.options.dependencies.isTerminal = func(file *os.File) bool {
			return file != harness.output
		}
		if got := Execute(context.Background(), harness.options); got != GateExitPass {
			t.Fatalf("Execute() = %d, want %d", got, GateExitPass)
		}
		if harness.publishCalls != 1 {
			t.Fatal("password suite did not complete with non-interactive stdout")
		}
	})
}

func TestExecutePreflightRejectsWithoutPublishing(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*executeHarness)
	}{
		{
			name: "no private TTY",
			mutate: func(harness *executeHarness) {
				harness.options.IsTTY = false
			},
		},
		{
			name: "matrix mismatch",
			mutate: func(harness *executeHarness) {
				harness.options.Args = replaceArgument(harness.options.Args, 10, "nas_vm")
			},
		},
		{
			name: "relative candidate",
			mutate: func(harness *executeHarness) {
				harness.options.Args = replaceArgument(harness.options.Args, 2, "ipgw-meta.exe")
			},
		},
		{
			name: "candidate security",
			mutate: func(harness *executeHarness) {
				harness.verifyBeforeErr = ErrCandidateSecurity
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
			test.mutate(harness)
			if got := Execute(context.Background(), harness.options); got != GateExitSecurityReject {
				t.Fatalf("Execute() = %d, want %d", got, GateExitSecurityReject)
			}
			if harness.publishCalls != 0 {
				t.Fatal("security reject published evidence")
			}
			if got := harness.errorText(t); got != "ipgw-live-gate: security reject\n" {
				t.Fatalf("diagnostics = %q", got)
			}
		})
	}
}

func TestExecuteFailureBoundariesNeverPublishAValidBundle(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*executeHarness)
		want        GateExitCode
		wantPublish int
		wantDiscard int
	}{
		{
			name: "profile preflight",
			mutate: func(harness *executeHarness) {
				harness.executor.prepareErr = ErrProcessProfile
			},
			want: GateExitSecurityReject,
		},
		{
			name: "profile runner fault",
			mutate: func(harness *executeHarness) {
				harness.executor.prepareErr = ErrProcessRunnerFault
			},
			want: GateExitInternal,
		},
		{
			name: "profile preflight and drift",
			mutate: func(harness *executeHarness) {
				harness.executor.prepareErr = ErrProcessProfile
				harness.verifyAfterErr = ErrCandidateDrift
			},
			want: GateExitEvidenceDurability,
		},
		{
			name: "post-run drift",
			mutate: func(harness *executeHarness) {
				harness.verifyAfterErr = ErrCandidateDrift
			},
			want: GateExitEvidenceDurability,
		},
		{
			name: "invalid candidate projection",
			mutate: func(harness *executeHarness) {
				delete(harness.executor.observations, ActionInitialStatus)
			},
			want: GateExitInternal,
		},
		{
			name: "publisher durability",
			mutate: func(harness *executeHarness) {
				harness.publishErr = ErrEvidenceDurability
			},
			want: GateExitEvidenceDurability, wantPublish: 1,
		},
		{
			name: "malformed publisher result",
			mutate: func(harness *executeHarness) {
				harness.malformedPublish = true
			},
			want: GateExitEvidenceDurability, wantPublish: 1, wantDiscard: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
			test.mutate(harness)
			if got := Execute(context.Background(), harness.options); got != test.want {
				t.Fatalf("Execute() = %d, want %d", got, test.want)
			}
			if harness.publishCalls != test.wantPublish ||
				harness.discardCalls != test.wantDiscard {
				t.Fatalf(
					"publish=%d discard=%d, want %d/%d",
					harness.publishCalls,
					harness.discardCalls,
					test.wantPublish,
					test.wantDiscard,
				)
			}
		})
	}
}

func TestExecuteCancellationHasPriorityAndNoBundle(t *testing.T) {
	t.Run("before candidate", func(t *testing.T) {
		harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if got := Execute(ctx, harness.options); got != GateExitCanceled {
			t.Fatalf("Execute() = %d, want %d", got, GateExitCanceled)
		}
		if harness.verifyBeforeCalls != 0 || harness.publishCalls != 0 {
			t.Fatal("pre-canceled execution crossed a side-effect boundary")
		}
	})

	t.Run("state machine cleanup", func(t *testing.T) {
		harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
		ctx, cancel := context.WithCancel(context.Background())
		harness.executor.cancelAction = ActionSecondPasswordLogin
		harness.executor.cancel = cancel
		harness.executor.observations[ActionSecondPasswordLogin] = Observation{RunnerFault: true}
		if got := Execute(ctx, harness.options); got != GateExitCanceled {
			t.Fatalf("Execute() = %d, want %d", got, GateExitCanceled)
		}
		wantTail := []CommandAction{ActionCleanupLogout, ActionCleanupStatus}
		if len(harness.executor.actions) < len(wantTail) {
			t.Fatalf("actions = %#v", harness.executor.actions)
		}
		gotTail := harness.executor.actions[len(harness.executor.actions)-len(wantTail):]
		for index := range wantTail {
			if gotTail[index] != wantTail[index] {
				t.Fatalf("cleanup tail = %#v, want %#v", gotTail, wantTail)
			}
		}
		if harness.publishCalls != 0 {
			t.Fatal("canceled state machine published evidence")
		}
	})

	t.Run("publish cancellation sentinel", func(t *testing.T) {
		harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
		harness.publishErr = ErrEvidencePublishCanceled
		if got := Execute(context.Background(), harness.options); got != GateExitCanceled {
			t.Fatalf("Execute() = %d, want %d", got, GateExitCanceled)
		}
		if harness.publishCalls != 1 || harness.discardCalls != 0 {
			t.Fatalf("publish=%d discard=%d", harness.publishCalls, harness.discardCalls)
		}
	})

	t.Run("ordinary publish error is durability", func(t *testing.T) {
		harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
		harness.publishErr = ErrEvidenceDurability
		if got := Execute(context.Background(), harness.options); got != GateExitEvidenceDurability {
			t.Fatalf("Execute() = %d, want %d", got, GateExitEvidenceDurability)
		}
		if harness.publishCalls != 1 || harness.discardCalls != 0 {
			t.Fatalf("publish=%d discard=%d", harness.publishCalls, harness.discardCalls)
		}
	})

	t.Run("ordinary publish error with cancellation is canceled", func(t *testing.T) {
		harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
		ctx, cancel := context.WithCancel(context.Background())
		harness.options.dependencies.publish = func(
			context.Context,
			string,
			Evidence,
		) (PublishedBundle, error) {
			harness.publishCalls++
			cancel()
			return PublishedBundle{}, ErrEvidenceDurability
		}
		if got := Execute(ctx, harness.options); got != GateExitCanceled {
			t.Fatalf("Execute() = %d, want %d", got, GateExitCanceled)
		}
		if harness.publishCalls != 1 || harness.discardCalls != 0 {
			t.Fatalf("publish=%d discard=%d", harness.publishCalls, harness.discardCalls)
		}
	})

	t.Run("after publish commit", func(t *testing.T) {
		harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
		ctx, cancel := context.WithCancel(context.Background())
		publishedPath := ""
		harness.options.dependencies.publish = func(publishCtx context.Context, outputDir string, evidence Evidence) (PublishedBundle, error) {
			harness.publishCalls++
			bundle, err := (BundlePublisher{OutputDir: outputDir}).PublishContext(publishCtx, evidence)
			if err != nil {
				return PublishedBundle{}, err
			}
			publishedPath = bundle.Path
			cancel()
			return bundle, nil
		}
		harness.options.dependencies.discard = discardPublishedBundle
		if got := Execute(ctx, harness.options); got != GateExitPass {
			t.Fatalf("Execute() = %d, want %d", got, GateExitPass)
		}
		if harness.publishCalls != 1 || publishedPath == "" {
			t.Fatalf("publish=%d path=%q", harness.publishCalls, publishedPath)
		}
		if _, err := os.Lstat(filepath.Join(publishedPath, bundleChecksumsName)); err != nil {
			t.Fatalf("committed run lost its checksum marker: %v", err)
		}
		if _, err := os.Lstat(publishedPath); err != nil {
			t.Fatalf("committed run lost its published bundle: %v", err)
		}
		if harness.discardCalls != 0 {
			t.Fatal("late cancellation discarded a committed bundle")
		}
	})
}

func TestExecuteInjectedCallbackCancellationPriority(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*executeHarness, context.CancelFunc)
	}{
		{
			name: "terminal detector",
			mutate: func(harness *executeHarness, cancel context.CancelFunc) {
				harness.options.dependencies.isTerminal = func(*os.File) bool {
					cancel()
					return false
				}
			},
		},
		{
			name: "platform detector",
			mutate: func(harness *executeHarness, cancel context.CancelFunc) {
				harness.options.dependencies.platform = func() (Platform, bool) {
					cancel()
					return "", false
				}
			},
		},
		{
			name: "verification with error",
			mutate: func(harness *executeHarness, cancel context.CancelFunc) {
				harness.options.dependencies.verifyBefore = func(string, string, string, Platform) (VerifiedCandidate, error) {
					cancel()
					return VerifiedCandidate{}, ErrCandidateSecurity
				}
			},
		},
		{
			name: "executor factory",
			mutate: func(harness *executeHarness, cancel context.CancelFunc) {
				harness.options.dependencies.newExecutor = func(VerifiedCandidate, liveGateInvocation, Options) preparedCommandExecutor {
					cancel()
					return nil
				}
			},
		},
		{
			name: "profile preparation with error",
			mutate: func(harness *executeHarness, cancel context.CancelFunc) {
				harness.executor.prepare = cancel
				harness.executor.prepareErr = ErrProcessRunnerFault
			},
		},
		{
			name: "profile failure verification with error",
			mutate: func(harness *executeHarness, cancel context.CancelFunc) {
				harness.executor.prepareErr = ErrProcessProfile
				harness.options.dependencies.verifyAfter = func(VerifiedCandidate) error {
					cancel()
					return ErrCandidateDrift
				}
			},
		},
		{
			name: "post-run verification with error",
			mutate: func(harness *executeHarness, cancel context.CancelFunc) {
				harness.options.dependencies.verifyAfter = func(VerifiedCandidate) error {
					cancel()
					return ErrCandidateDrift
				}
			},
		},
		{
			name: "start clock",
			mutate: func(harness *executeHarness, cancel context.CancelFunc) {
				harness.options.dependencies.now = func() time.Time {
					cancel()
					return time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
			ctx, cancel := context.WithCancel(context.Background())
			test.mutate(harness, cancel)
			if got := Execute(ctx, harness.options); got != GateExitCanceled {
				t.Fatalf("Execute() = %d, want %d", got, GateExitCanceled)
			}
			if harness.publishCalls != 0 {
				t.Fatal("canceled callback published evidence")
			}
		})
	}
}

func TestExecuteRunnerFaultAfterOwnershipCleansWithoutPublishing(t *testing.T) {
	harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
	harness.executor.observations[ActionSecondPasswordLogin] = Observation{RunnerFault: true}
	if got := Execute(context.Background(), harness.options); got != GateExitInternal {
		t.Fatalf("Execute() = %d, want %d", got, GateExitInternal)
	}
	wantActions := []CommandAction{
		ActionInitialStatus,
		ActionPasswordLogin,
		ActionStatusOnline,
		ActionSecondPasswordLogin,
		ActionCleanupLogout,
		ActionCleanupStatus,
	}
	if fmt.Sprint(harness.executor.actions) != fmt.Sprint(wantActions) {
		t.Fatalf("actions = %#v, want %#v", harness.executor.actions, wantActions)
	}
	if harness.verifyAfterCalls != 1 || harness.publishCalls != 0 {
		t.Fatalf("verifyAfter=%d publish=%d", harness.verifyAfterCalls, harness.publishCalls)
	}
}

func TestExecuteNilContextAndWritersIsFixedInternalFailure(t *testing.T) {
	if got := Execute(nil, Options{}); got != GateExitInternal {
		t.Fatalf("Execute() = %d, want %d", got, GateExitInternal)
	}
}

func TestExecutePublisherErrorTextIsNeverRendered(t *testing.T) {
	harness := newExecuteHarness(t, PlatformWindowsAMD64, SuitePasswordCore)
	secretCanary := "PRIVATE-CANARY"
	harness.publishErr = errors.New(secretCanary)
	if got := Execute(context.Background(), harness.options); got != GateExitEvidenceDurability {
		t.Fatalf("Execute() = %d, want %d", got, GateExitEvidenceDurability)
	}
	if strings.Contains(harness.errorText(t), secretCanary) {
		t.Fatal("untrusted error text reached diagnostics")
	}
}
