// Package livegate defines the closed evidence schema for the maintainer-only
// live validation gate.
package livegate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	SchemaVersion        = 1
	PlanID               = "IPGW-META-V1"
	Revision             = "2026-08-28-r2"
	MaxEvidenceJSONBytes = 64 * 1024
)

var ErrInvalidEvidence = errors.New("livegate: invalid evidence")

type Platform string

const (
	PlatformLinuxAMD64   Platform = "linux-amd64"
	PlatformWindowsAMD64 Platform = "windows-amd64"
)

type Testbed string

const (
	TestbedNASVM      Testbed = "nas_vm"
	TestbedBHKWindows Testbed = "bhk_windows"
)

type NetworkType string

const (
	NetworkCampusWired NetworkType = "campus_wired"
	NetworkCampusWiFi  NetworkType = "campus_wifi"
)

type AuthMethod string

const (
	AuthMethodPassword   AuthMethod = "password"
	AuthMethodTerminalQR AuthMethod = "terminal_qr"
)

type Suite string

const (
	SuitePasswordCore Suite = "password_core"
	SuiteTerminalQR   Suite = "terminal_qr"
)

type Result string

const (
	ResultPass    Result = "pass"
	ResultFail    Result = "fail"
	ResultBlocked Result = "blocked"
)

type StepName string

const (
	StepInitialStatusOffline       StepName = "initial_status_offline"
	StepLoginLoggedIn              StepName = "login_logged_in"
	StepQRLoginLoggedIn            StepName = "qr_login_logged_in"
	StepStatusOnline               StepName = "status_online"
	StepSecondLoginAlreadyOnline   StepName = "second_login_already_online"
	StepLogoutLoggedOut            StepName = "logout_logged_out"
	StepFinalStatusOffline         StepName = "final_status_offline"
	StepSecondLogoutAlreadyOffline StepName = "second_logout_already_offline"
	StepCleanupLogout              StepName = "cleanup_logout"
	StepCleanupStatusOffline       StepName = "cleanup_status_offline"
)

type ErrorCode string

const (
	ErrorInvalidArgument     ErrorCode = "invalid_argument"
	ErrorConfig              ErrorCode = "config"
	ErrorNetwork             ErrorCode = "network"
	ErrorAuthentication      ErrorCode = "authentication"
	ErrorSessionConflict     ErrorCode = "session_conflict"
	ErrorProtocolChanged     ErrorCode = "protocol_changed"
	ErrorInteractionRequired ErrorCode = "interaction_required"
	ErrorUnsupported         ErrorCode = "unsupported"
	ErrorInternal            ErrorCode = "internal"
)

type CapabilityStatus string

const (
	CapabilitySupported         CapabilityStatus = "supported"
	CapabilityDetectedOnly      CapabilityStatus = "detected_only"
	CapabilityObservedAnonymous CapabilityStatus = "observed_anonymous"
	CapabilitySyntheticCovered  CapabilityStatus = "synthetic_covered"
	CapabilityLiveUnverified    CapabilityStatus = "live_unverified"
	CapabilityLiveVerified      CapabilityStatus = "live_verified"
	CapabilityUnknown           CapabilityStatus = "unknown"
)

type GateExitCode int

const (
	GateExitPass               GateExitCode = 0
	GateExitFail               GateExitCode = 10
	GateExitBlocked            GateExitCode = 11
	GateExitSecurityReject     GateExitCode = 12
	GateExitEvidenceDurability GateExitCode = 13
	GateExitInternal           GateExitCode = 14
	GateExitCanceled           GateExitCode = 130
)

type CommandExitCode int

const (
	CommandExitSuccess             CommandExitCode = 0
	CommandExitInternal            CommandExitCode = 1
	CommandExitInvalid             CommandExitCode = 2
	CommandExitNetwork             CommandExitCode = 3
	CommandExitAuthentication      CommandExitCode = 4
	CommandExitSessionConflict     CommandExitCode = 5
	CommandExitProtocolChanged     CommandExitCode = 6
	CommandExitInteractionRequired CommandExitCode = 7
	CommandExitCanceled            CommandExitCode = 130
)

// Timestamp is a canonical UTC RFC3339Nano timestamp ending in Z.
type Timestamp string

// Evidence is the exact version 1 evidence object.
type Evidence struct {
	SchemaVersion      int                `json:"schema_version"`
	PlanID             string             `json:"plan_id"`
	Revision           string             `json:"revision"`
	EvidenceID         string             `json:"evidence_id"`
	CandidateID        string             `json:"candidate_id"`
	CandidateSetSHA256 string             `json:"candidate_set_sha256"`
	SourceCommit       string             `json:"source_commit"`
	Platform           Platform           `json:"platform"`
	Testbed            Testbed            `json:"testbed"`
	NetworkType        NetworkType        `json:"network_type"`
	AuthMethod         AuthMethod         `json:"auth_method"`
	Suite              Suite              `json:"suite"`
	CapabilityBefore   []CapabilityStatus `json:"capability_before"`
	Result             Result             `json:"result"`
	CapabilityAfter    []CapabilityStatus `json:"capability_after"`
	StartedAt          Timestamp          `json:"started_at"`
	FinishedAt         Timestamp          `json:"finished_at"`
	Steps              []Step             `json:"steps"`
}

// Step is the exact version 1 evidence step object.
type Step struct {
	Name            StepName        `json:"name"`
	Result          Result          `json:"result"`
	ExitCode        CommandExitCode `json:"exit_code"`
	ErrorCode       *ErrorCode      `json:"error_code"`
	DurationSeconds int64           `json:"duration_seconds"`
}

var (
	evidenceIDPattern  = regexp.MustCompile("^EVID-([0-9]{8})-([0-9]{3})$")
	candidateIDPattern = regexp.MustCompile("^v1\\.0\\.0-([0-9a-f]{12})-([1-9][0-9]*)\\.([1-9][0-9]*)$")
	lowerHex40Pattern  = regexp.MustCompile("^[0-9a-f]{40}$")
	lowerHex64Pattern  = regexp.MustCompile("^[0-9a-f]{64}$")
)

var passwordPrimarySteps = [...]StepName{
	StepInitialStatusOffline,
	StepLoginLoggedIn,
	StepStatusOnline,
	StepSecondLoginAlreadyOnline,
	StepLogoutLoggedOut,
	StepFinalStatusOffline,
	StepSecondLogoutAlreadyOffline,
}

var terminalQRPrimarySteps = [...]StepName{
	StepInitialStatusOffline,
	StepQRLoginLoggedIn,
	StepStatusOnline,
	StepLogoutLoggedOut,
	StepFinalStatusOffline,
}

var passwordCapabilitiesBefore = [...]CapabilityStatus{
	CapabilitySyntheticCovered,
	CapabilityLiveUnverified,
}

var passwordCapabilitiesAfter = [...]CapabilityStatus{
	CapabilitySyntheticCovered,
	CapabilityLiveVerified,
}

var terminalQRCapabilitiesBefore = [...]CapabilityStatus{
	CapabilityObservedAnonymous,
	CapabilitySyntheticCovered,
	CapabilityLiveUnverified,
}

var terminalQRCapabilitiesAfter = [...]CapabilityStatus{
	CapabilityObservedAnonymous,
	CapabilitySyntheticCovered,
	CapabilityLiveVerified,
}

func (e Evidence) Validate() error {
	if e.SchemaVersion != SchemaVersion {
		return invalidEvidence("schema version")
	}
	if e.PlanID != PlanID {
		return invalidEvidence("plan ID")
	}
	if e.Revision != Revision {
		return invalidEvidence("revision")
	}

	started, err := parseTimestamp(e.StartedAt)
	if err != nil {
		return err
	}
	finished, err := parseTimestamp(e.FinishedAt)
	if err != nil {
		return err
	}
	if finished.Before(started) {
		return invalidEvidence("timestamp order")
	}

	if err := validateEvidenceID(e.EvidenceID, started); err != nil {
		return err
	}
	if !lowerHex64Pattern.MatchString(e.CandidateSetSHA256) {
		return invalidEvidence("candidate set hash")
	}
	if !lowerHex40Pattern.MatchString(e.SourceCommit) {
		return invalidEvidence("source commit")
	}
	if err := validateCandidateID(e.CandidateID, e.SourceCommit); err != nil {
		return err
	}

	if !e.Platform.valid() {
		return invalidEvidence("platform")
	}
	if !e.Testbed.valid() {
		return invalidEvidence("testbed")
	}
	if !e.NetworkType.valid() {
		return invalidEvidence("network type")
	}
	if !e.AuthMethod.valid() {
		return invalidEvidence("authentication method")
	}
	if !e.Suite.valid() {
		return invalidEvidence("suite")
	}
	if !e.Result.valid() {
		return invalidEvidence("result")
	}
	if !validMatrix(e.Platform, e.Testbed, e.NetworkType, e.AuthMethod, e.Suite) {
		return invalidEvidence("validation matrix")
	}
	if err := validateCapabilities(e); err != nil {
		return err
	}
	return validateStepSequence(e.Suite, e.Result, e.Steps)
}

func (s Step) Validate() error {
	if !s.Name.valid() {
		return invalidEvidence("step name")
	}
	if !s.Result.valid() {
		return invalidEvidence("step result")
	}
	if s.DurationSeconds < 0 {
		return invalidEvidence("step duration")
	}
	if s.ErrorCode != nil && !s.ErrorCode.valid() {
		return invalidEvidence("step error code")
	}
	if s.ExitCode == CommandExitSuccess {
		if s.ErrorCode != nil {
			return invalidEvidence("step exit and error code")
		}
		return nil
	}
	if s.Result == ResultPass {
		return invalidEvidence("step pass exit code")
	}
	if !validExitError(s.ExitCode, s.ErrorCode) {
		return invalidEvidence("step exit and error code")
	}
	return nil
}

func invalidEvidence(field string) error {
	return fmt.Errorf("%w: invalid %s", ErrInvalidEvidence, field)
}

func parseTimestamp(value Timestamp) (time.Time, error) {
	s := string(value)
	if !strings.HasSuffix(s, "Z") {
		return time.Time{}, invalidEvidence("timestamp")
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || parsed.Format(time.RFC3339Nano) != s {
		return time.Time{}, invalidEvidence("timestamp")
	}
	return parsed, nil
}

func validateEvidenceID(id string, started time.Time) error {
	parts := evidenceIDPattern.FindStringSubmatch(id)
	if parts == nil || parts[2] == "000" {
		return invalidEvidence("evidence ID")
	}
	if _, err := time.Parse("20060102", parts[1]); err != nil {
		return invalidEvidence("evidence ID")
	}
	if parts[1] != started.UTC().Format("20060102") {
		return invalidEvidence("evidence ID date")
	}
	return nil
}

func validateCandidateID(id, sourceCommit string) error {
	parts := candidateIDPattern.FindStringSubmatch(id)
	if parts == nil {
		return invalidEvidence("candidate ID")
	}
	if len(sourceCommit) < 12 || parts[1] != sourceCommit[:12] {
		return invalidEvidence("candidate ID source commit")
	}
	return nil
}

func validMatrix(platform Platform, testbed Testbed, network NetworkType, auth AuthMethod, suite Suite) bool {
	switch {
	case platform == PlatformLinuxAMD64 &&
		testbed == TestbedNASVM &&
		network == NetworkCampusWired &&
		auth == AuthMethodPassword &&
		suite == SuitePasswordCore:
		return true
	case platform == PlatformLinuxAMD64 &&
		testbed == TestbedNASVM &&
		network == NetworkCampusWired &&
		auth == AuthMethodTerminalQR &&
		suite == SuiteTerminalQR:
		return true
	case platform == PlatformWindowsAMD64 &&
		testbed == TestbedBHKWindows &&
		network == NetworkCampusWired &&
		auth == AuthMethodPassword &&
		suite == SuitePasswordCore:
		return true
	case platform == PlatformWindowsAMD64 &&
		testbed == TestbedBHKWindows &&
		network == NetworkCampusWiFi &&
		auth == AuthMethodPassword &&
		suite == SuitePasswordCore:
		return true
	default:
		return false
	}
}

func validateCapabilities(e Evidence) error {
	for _, capability := range e.CapabilityBefore {
		if !capability.valid() {
			return invalidEvidence("capability before")
		}
	}
	for _, capability := range e.CapabilityAfter {
		if !capability.valid() {
			return invalidEvidence("capability after")
		}
	}

	var before, after []CapabilityStatus
	switch e.Suite {
	case SuitePasswordCore:
		before = passwordCapabilitiesBefore[:]
		if e.Result == ResultPass {
			after = passwordCapabilitiesAfter[:]
		} else {
			after = passwordCapabilitiesBefore[:]
		}
	case SuiteTerminalQR:
		before = terminalQRCapabilitiesBefore[:]
		if e.Result == ResultPass {
			after = terminalQRCapabilitiesAfter[:]
		} else {
			after = terminalQRCapabilitiesBefore[:]
		}
	default:
		return invalidEvidence("capability suite")
	}
	if !equalCapabilities(e.CapabilityBefore, before) {
		return invalidEvidence("capability before transition")
	}
	if !equalCapabilities(e.CapabilityAfter, after) {
		return invalidEvidence("capability after transition")
	}
	return nil
}

func equalCapabilities(got, want []CapabilityStatus) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func validateStepSequence(suite Suite, overall Result, steps []Step) error {
	for _, step := range steps {
		if err := step.Validate(); err != nil {
			return err
		}
	}

	var primary []StepName
	switch suite {
	case SuitePasswordCore:
		primary = passwordPrimarySteps[:]
	case SuiteTerminalQR:
		primary = terminalQRPrimarySteps[:]
	default:
		return invalidEvidence("step suite")
	}

	if overall == ResultPass {
		if len(steps) != len(primary) {
			return invalidEvidence("pass step count")
		}
		for i, name := range primary {
			if steps[i].Name != name || steps[i].Result != ResultPass {
				return invalidEvidence("pass step sequence")
			}
		}
		return nil
	}
	if overall != ResultFail && overall != ResultBlocked {
		return invalidEvidence("step overall result")
	}
	if len(steps) == 0 {
		return invalidEvidence("non-pass step count")
	}

	primaryCount := 0
	for primaryCount < len(primary) && primaryCount < len(steps) {
		step := steps[primaryCount]
		if step.Name != primary[primaryCount] {
			return invalidEvidence("non-pass primary sequence")
		}
		primaryCount++
		if step.Result != ResultPass {
			break
		}
	}
	if primaryCount == 0 || steps[primaryCount-1].Result == ResultPass {
		return invalidEvidence("non-pass primary result")
	}

	statusOnlinePassed := primaryStepPassed(steps[:primaryCount], StepStatusOnline)
	finalOfflinePassed := primaryStepPassed(steps[:primaryCount], StepFinalStatusOffline)
	cleanupRequired := statusOnlinePassed && !finalOfflinePassed
	expectedCount := primaryCount
	if cleanupRequired {
		expectedCount += 2
	}
	if len(steps) != expectedCount {
		return invalidEvidence("cleanup step count")
	}
	if cleanupRequired &&
		(steps[primaryCount].Name != StepCleanupLogout ||
			steps[primaryCount+1].Name != StepCleanupStatusOffline) {
		return invalidEvidence("cleanup step sequence")
	}

	derived := ResultBlocked
	for _, step := range steps {
		if step.Result == ResultFail {
			derived = ResultFail
			break
		}
	}
	if overall != derived {
		return invalidEvidence("overall step result")
	}
	return nil
}

func primaryStepPassed(steps []Step, name StepName) bool {
	for _, step := range steps {
		if step.Name == name {
			return step.Result == ResultPass
		}
	}
	return false
}

func validExitError(exit CommandExitCode, code *ErrorCode) bool {
	if code == nil {
		return false
	}
	switch exit {
	case CommandExitInternal:
		return *code == ErrorInternal
	case CommandExitInvalid:
		return *code == ErrorInvalidArgument ||
			*code == ErrorConfig ||
			*code == ErrorUnsupported
	case CommandExitNetwork, CommandExitCanceled:
		return *code == ErrorNetwork
	case CommandExitAuthentication:
		return *code == ErrorAuthentication
	case CommandExitSessionConflict:
		return *code == ErrorSessionConflict
	case CommandExitProtocolChanged:
		return *code == ErrorProtocolChanged
	case CommandExitInteractionRequired:
		return *code == ErrorInteractionRequired
	default:
		return false
	}
}

func (p Platform) valid() bool {
	return p == PlatformLinuxAMD64 || p == PlatformWindowsAMD64
}

func (t Testbed) valid() bool {
	return t == TestbedNASVM || t == TestbedBHKWindows
}

func (n NetworkType) valid() bool {
	return n == NetworkCampusWired || n == NetworkCampusWiFi
}

func (a AuthMethod) valid() bool {
	return a == AuthMethodPassword || a == AuthMethodTerminalQR
}

func (s Suite) valid() bool {
	return s == SuitePasswordCore || s == SuiteTerminalQR
}

func (r Result) valid() bool {
	return r == ResultPass || r == ResultFail || r == ResultBlocked
}

func (s StepName) valid() bool {
	switch s {
	case StepInitialStatusOffline,
		StepLoginLoggedIn,
		StepQRLoginLoggedIn,
		StepStatusOnline,
		StepSecondLoginAlreadyOnline,
		StepLogoutLoggedOut,
		StepFinalStatusOffline,
		StepSecondLogoutAlreadyOffline,
		StepCleanupLogout,
		StepCleanupStatusOffline:
		return true
	default:
		return false
	}
}

func (e ErrorCode) valid() bool {
	switch e {
	case ErrorInvalidArgument,
		ErrorConfig,
		ErrorNetwork,
		ErrorAuthentication,
		ErrorSessionConflict,
		ErrorProtocolChanged,
		ErrorInteractionRequired,
		ErrorUnsupported,
		ErrorInternal:
		return true
	default:
		return false
	}
}

func (c CapabilityStatus) valid() bool {
	switch c {
	case CapabilitySupported,
		CapabilityDetectedOnly,
		CapabilityObservedAnonymous,
		CapabilitySyntheticCovered,
		CapabilityLiveUnverified,
		CapabilityLiveVerified,
		CapabilityUnknown:
		return true
	default:
		return false
	}
}

func (c GateExitCode) valid() bool {
	switch c {
	case GateExitPass,
		GateExitFail,
		GateExitBlocked,
		GateExitSecurityReject,
		GateExitEvidenceDurability,
		GateExitInternal,
		GateExitCanceled:
		return true
	default:
		return false
	}
}
