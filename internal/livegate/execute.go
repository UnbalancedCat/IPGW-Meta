package livegate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/term"
)

var liveGateProfilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Options contains the maintainer-only live-gate process boundary. IsTTY must
// be computed from Stdin and Stderr immediately before Execute is called.
type Options struct {
	Args   []string
	Stdin  *os.File
	Stdout *os.File
	Stderr *os.File
	IsTTY  bool

	dependencies *executeDependencies
}

type preparedCommandExecutor interface {
	CommandExecutor
	PrepareProfile(context.Context, Suite) error
}

type executeDependencies struct {
	platform     func() (Platform, bool)
	isTerminal   func(*os.File) bool
	verifyBefore func(string, string, string, Platform) (VerifiedCandidate, error)
	verifyAfter  func(VerifiedCandidate) error
	newExecutor  func(VerifiedCandidate, liveGateInvocation, Options) preparedCommandExecutor
	publish      func(context.Context, string, Evidence) (PublishedBundle, error)
	discard      func(PublishedBundle)
	now          func() time.Time
}

func defaultExecuteDependencies() executeDependencies {
	return executeDependencies{
		platform: currentLiveGatePlatform,
		isTerminal: func(file *os.File) bool {
			return file != nil && term.IsTerminal(int(file.Fd()))
		},
		verifyBefore: VerifyCandidateBefore,
		verifyAfter: func(candidate VerifiedCandidate) error {
			return candidate.VerifyAfter()
		},
		newExecutor: func(candidate VerifiedCandidate, invocation liveGateInvocation, options Options) preparedCommandExecutor {
			return &ProcessExecutor{
				CandidatePath: candidate.CandidatePath,
				Profile:       invocation.profile,
				Stdin:         options.Stdin,
				Stdout:        options.Stdout,
				Stderr:        options.Stderr,
			}
		},
		publish: func(ctx context.Context, outputDir string, evidence Evidence) (PublishedBundle, error) {
			return (BundlePublisher{OutputDir: outputDir}).PublishContext(ctx, evidence)
		},
		discard: discardPublishedBundle,
		now:     time.Now,
	}
}

func (options Options) effectiveDependencies() executeDependencies {
	defaults := defaultExecuteDependencies()
	if options.dependencies == nil {
		return defaults
	}
	provided := options.dependencies
	if provided.platform != nil {
		defaults.platform = provided.platform
	}
	if provided.isTerminal != nil {
		defaults.isTerminal = provided.isTerminal
	}
	if provided.verifyBefore != nil {
		defaults.verifyBefore = provided.verifyBefore
	}
	if provided.verifyAfter != nil {
		defaults.verifyAfter = provided.verifyAfter
	}
	if provided.newExecutor != nil {
		defaults.newExecutor = provided.newExecutor
	}
	if provided.publish != nil {
		defaults.publish = provided.publish
	}
	if provided.discard != nil {
		defaults.discard = provided.discard
	}
	if provided.now != nil {
		defaults.now = provided.now
	}
	return defaults
}

type liveGateInvocation struct {
	candidatePath     string
	candidateSHA256   string
	candidateManifest string
	suite             Suite
	testbed           Testbed
	network           NetworkType
	profile           string
	outputDir         string
}

// Execute runs the closed maintainer-only live gate. It never renders caller,
// candidate, profile, path, or process error text.
func Execute(ctx context.Context, options Options) GateExitCode {
	if ctx == nil {
		return renderGateExit(options, GateExitInternal, "", "")
	}
	finish := func(exit GateExitCode, result Result, evidenceID string) GateExitCode {
		if ctx.Err() != nil {
			return renderGateExit(options, GateExitCanceled, "", "")
		}
		return renderGateExit(options, exit, result, evidenceID)
	}

	invocation, ok := parseLiveGateInvocation(options.Args)
	if !ok {
		return finish(GateExitSecurityReject, "", "")
	}
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}

	dependencies := options.effectiveDependencies()
	terminalOK, terminalCanceled := validTerminalBoundary(ctx, options, invocation.suite, dependencies.isTerminal)
	if terminalCanceled {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	if !terminalOK {
		return finish(GateExitSecurityReject, "", "")
	}
	platform, ok := dependencies.platform()
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	if !ok || !validLiveGatePreflight(invocation, platform) {
		return finish(GateExitSecurityReject, "", "")
	}
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}

	verified, err := dependencies.verifyBefore(
		invocation.candidatePath,
		invocation.candidateManifest,
		invocation.candidateSHA256,
		platform,
	)
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	if err != nil {
		return finish(GateExitSecurityReject, "", "")
	}

	executor := dependencies.newExecutor(verified, invocation, options)
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	if executor == nil {
		return finish(GateExitInternal, "", "")
	}
	profileErr := executor.PrepareProfile(ctx, invocation.suite)
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	if profileErr != nil {
		afterErr := dependencies.verifyAfter(verified)
		if ctx.Err() != nil {
			return renderGateExit(options, GateExitCanceled, "", "")
		}
		if afterErr != nil {
			return finish(GateExitEvidenceDurability, "", "")
		}
		if errors.Is(profileErr, ErrProcessRunnerFault) {
			return finish(GateExitInternal, "", "")
		}
		return finish(GateExitSecurityReject, "", "")
	}

	started := dependencies.now().UTC()
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	steps, result, canceled, runErr := (StateMachine{
		Executor: executor,
		Now:      dependencies.now,
	}).Run(ctx, invocation.suite)
	if canceled || ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	afterErr := dependencies.verifyAfter(verified)
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	if afterErr != nil {
		return finish(GateExitEvidenceDurability, "", "")
	}
	if runErr != nil {
		return finish(GateExitInternal, "", "")
	}

	finished := dependencies.now().UTC()
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}
	if finished.Before(started) {
		return finish(GateExitEvidenceDurability, "", "")
	}
	evidence := buildLiveGateEvidence(verified.Binding, invocation, started, finished, result, steps)
	probe := evidence
	probe.EvidenceID = fmt.Sprintf("EVID-%s-001", started.Format("20060102"))
	if err := probe.Validate(); err != nil {
		return finish(GateExitEvidenceDurability, "", "")
	}
	if ctx.Err() != nil {
		return renderGateExit(options, GateExitCanceled, "", "")
	}

	published, err := dependencies.publish(ctx, invocation.outputDir, evidence)
	if err != nil {
		if ctx.Err() != nil ||
			errors.Is(err, ErrEvidencePublishCanceled) ||
			errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return renderGateExit(options, GateExitCanceled, "", "")
		}
		return renderGateExit(options, GateExitEvidenceDurability, "", "")
	}
	if err := published.Evidence.Validate(); err != nil ||
		published.Evidence.Result != result ||
		published.Evidence.CandidateID != verified.Binding.CandidateID {
		dependencies.discard(published)
		return renderGateExit(options, GateExitEvidenceDurability, "", "")
	}

	exit := gateExitForResult(result)
	return renderGateExit(options, exit, result, published.Evidence.EvidenceID)
}

func parseLiveGateInvocation(args []string) (liveGateInvocation, bool) {
	if len(args) != 17 || args[0] != "run" {
		return liveGateInvocation{}, false
	}
	values := make(map[string]string, 8)
	for index := 1; index < len(args); index += 2 {
		name, value := args[index], args[index+1]
		switch name {
		case "--candidate", "--candidate-sha256", "--candidate-manifest",
			"--suite", "--testbed", "--network", "--profile", "--output-dir":
		default:
			return liveGateInvocation{}, false
		}
		if value == "" {
			return liveGateInvocation{}, false
		}
		if _, duplicate := values[name]; duplicate {
			return liveGateInvocation{}, false
		}
		values[name] = value
	}
	if len(values) != 8 {
		return liveGateInvocation{}, false
	}

	suite, ok := parseLiveGateSuite(values["--suite"])
	if !ok {
		return liveGateInvocation{}, false
	}
	invocation := liveGateInvocation{
		candidatePath:     values["--candidate"],
		candidateSHA256:   values["--candidate-sha256"],
		candidateManifest: values["--candidate-manifest"],
		suite:             suite,
		testbed:           Testbed(values["--testbed"]),
		network:           NetworkType(values["--network"]),
		profile:           values["--profile"],
		outputDir:         values["--output-dir"],
	}
	return invocation, true
}

func parseLiveGateSuite(value string) (Suite, bool) {
	switch value {
	case "password-core":
		return SuitePasswordCore, true
	case "terminal-qr":
		return SuiteTerminalQR, true
	default:
		return "", false
	}
}

func validTerminalBoundary(
	ctx context.Context,
	options Options,
	suite Suite,
	isTerminal func(*os.File) bool,
) (valid bool, canceled bool) {
	if ctx.Err() != nil {
		return false, true
	}
	if !options.IsTTY ||
		options.Stdin == nil ||
		options.Stdout == nil ||
		options.Stderr == nil ||
		isTerminal == nil {
		return false, false
	}
	stdinTTY := isTerminal(options.Stdin)
	if ctx.Err() != nil {
		return false, true
	}
	if !stdinTTY {
		return false, false
	}
	stderrTTY := isTerminal(options.Stderr)
	if ctx.Err() != nil {
		return false, true
	}
	if !stderrTTY {
		return false, false
	}
	switch suite {
	case SuitePasswordCore:
		return true, false
	case SuiteTerminalQR:
		stdoutTTY := isTerminal(options.Stdout)
		if ctx.Err() != nil {
			return false, true
		}
		return stdoutTTY, false
	default:
		return false, false
	}
}

func validLiveGatePreflight(invocation liveGateInvocation, platform Platform) bool {
	if !platform.valid() ||
		!liveGateProfilePattern.MatchString(invocation.profile) ||
		!validLiveGatePath(invocation.candidatePath) ||
		!validLiveGatePath(invocation.candidateManifest) ||
		!validLiveGatePath(invocation.outputDir) ||
		validateBundleOutputDir(invocation.outputDir) != nil ||
		liveGatePathsOverlap(invocation.outputDir, invocation.candidatePath) ||
		liveGatePathsOverlap(invocation.outputDir, invocation.candidateManifest) {
		return false
	}
	auth := AuthMethodPassword
	if invocation.suite == SuiteTerminalQR {
		auth = AuthMethodTerminalQR
	}
	return validMatrix(platform, invocation.testbed, invocation.network, auth, invocation.suite)
}

func validLiveGatePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func liveGatePathsOverlap(left, right string) bool {
	return liveGatePathContains(left, right) || liveGatePathContains(right, left)
}

func liveGatePathContains(base, target string) bool {
	relative, err := filepath.Rel(base, target)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	if relative == "." {
		return true
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func currentLiveGatePlatform() (Platform, bool) {
	if runtime.GOARCH != "amd64" {
		return "", false
	}
	switch runtime.GOOS {
	case "linux":
		return PlatformLinuxAMD64, true
	case "windows":
		return PlatformWindowsAMD64, true
	default:
		return "", false
	}
}

func discardPublishedBundle(bundle PublishedBundle) {
	if bundle.Path == "" ||
		!filepath.IsAbs(bundle.Path) ||
		filepath.Clean(bundle.Path) != bundle.Path ||
		bundle.Evidence.Validate() != nil {
		return
	}
	candidateDir := filepath.Dir(bundle.Path)
	outputDir := filepath.Dir(candidateDir)
	expected := filepath.Join(outputDir, bundle.Evidence.CandidateID, bundle.Evidence.EvidenceID)
	if validateBundleOutputDir(outputDir) != nil || expected != bundle.Path {
		return
	}
	invalidatePublishedBundle(bundle.Path)
}

func buildLiveGateEvidence(
	binding CandidateBinding,
	invocation liveGateInvocation,
	started time.Time,
	finished time.Time,
	result Result,
	steps []Step,
) Evidence {
	auth := AuthMethodPassword
	before := passwordCapabilitiesBefore[:]
	after := passwordCapabilitiesBefore[:]
	if result == ResultPass {
		after = passwordCapabilitiesAfter[:]
	}
	if invocation.suite == SuiteTerminalQR {
		auth = AuthMethodTerminalQR
		before = terminalQRCapabilitiesBefore[:]
		after = terminalQRCapabilitiesBefore[:]
		if result == ResultPass {
			after = terminalQRCapabilitiesAfter[:]
		}
	}
	return Evidence{
		SchemaVersion:      SchemaVersion,
		PlanID:             PlanID,
		Revision:           Revision,
		CandidateID:        binding.CandidateID,
		CandidateSetSHA256: binding.CandidateSetSHA256,
		SourceCommit:       binding.SourceCommit,
		Platform:           binding.Platform,
		Testbed:            invocation.testbed,
		NetworkType:        invocation.network,
		AuthMethod:         auth,
		Suite:              invocation.suite,
		CapabilityBefore:   append([]CapabilityStatus(nil), before...),
		Result:             result,
		CapabilityAfter:    append([]CapabilityStatus(nil), after...),
		StartedAt:          Timestamp(started.Format(time.RFC3339Nano)),
		FinishedAt:         Timestamp(finished.Format(time.RFC3339Nano)),
		Steps:              append([]Step(nil), steps...),
	}
}

func gateExitForResult(result Result) GateExitCode {
	switch result {
	case ResultPass:
		return GateExitPass
	case ResultFail:
		return GateExitFail
	case ResultBlocked:
		return GateExitBlocked
	default:
		return GateExitInternal
	}
}

func renderGateExit(options Options, exit GateExitCode, result Result, evidenceID string) GateExitCode {
	var output io.Writer = io.Discard
	if options.Stdout != nil {
		output = options.Stdout
	}
	var diagnostics io.Writer = io.Discard
	if options.Stderr != nil {
		diagnostics = options.Stderr
	}
	switch exit {
	case GateExitPass, GateExitFail, GateExitBlocked:
		_, _ = fmt.Fprintf(output, "ipgw-live-gate: result=%s evidence_id=%s\n", result, evidenceID)
	case GateExitSecurityReject:
		_, _ = fmt.Fprintln(diagnostics, "ipgw-live-gate: security reject")
	case GateExitEvidenceDurability:
		_, _ = fmt.Fprintln(diagnostics, "ipgw-live-gate: evidence durability failure")
	case GateExitCanceled:
		_, _ = fmt.Fprintln(diagnostics, "ipgw-live-gate: canceled")
	default:
		_, _ = fmt.Fprintln(diagnostics, "ipgw-live-gate: internal failure")
	}
	return exit
}
