package livegate

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const (
	testSourceCommit  = "0123456789abcdef0123456789abcdef01234567"
	testCandidateHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

func validPasswordEvidence() Evidence {
	return Evidence{
		SchemaVersion:      SchemaVersion,
		PlanID:             PlanID,
		Revision:           Revision,
		EvidenceID:         "EVID-20260829-001",
		CandidateID:        "v1.0.0-0123456789ab-12345.1",
		CandidateSetSHA256: testCandidateHash,
		SourceCommit:       testSourceCommit,
		Platform:           PlatformLinuxAMD64,
		Testbed:            TestbedNASVM,
		NetworkType:        NetworkCampusWired,
		AuthMethod:         AuthMethodPassword,
		Suite:              SuitePasswordCore,
		CapabilityBefore: []CapabilityStatus{
			CapabilitySyntheticCovered,
			CapabilityLiveUnverified,
		},
		Result: ResultPass,
		CapabilityAfter: []CapabilityStatus{
			CapabilitySyntheticCovered,
			CapabilityLiveVerified,
		},
		StartedAt:  "2026-08-29T01:02:03Z",
		FinishedAt: "2026-08-29T01:02:10Z",
		Steps: []Step{
			passStep(StepInitialStatusOffline),
			passStep(StepLoginLoggedIn),
			passStep(StepStatusOnline),
			passStep(StepSecondLoginAlreadyOnline),
			passStep(StepLogoutLoggedOut),
			passStep(StepFinalStatusOffline),
			passStep(StepSecondLogoutAlreadyOffline),
		},
	}
}

func validTerminalQREvidence() Evidence {
	e := validPasswordEvidence()
	e.AuthMethod = AuthMethodTerminalQR
	e.Suite = SuiteTerminalQR
	e.CapabilityBefore = []CapabilityStatus{
		CapabilityObservedAnonymous,
		CapabilitySyntheticCovered,
		CapabilityLiveUnverified,
	}
	e.CapabilityAfter = []CapabilityStatus{
		CapabilityObservedAnonymous,
		CapabilitySyntheticCovered,
		CapabilityLiveVerified,
	}
	e.Steps = []Step{
		passStep(StepInitialStatusOffline),
		passStep(StepQRLoginLoggedIn),
		passStep(StepStatusOnline),
		passStep(StepLogoutLoggedOut),
		passStep(StepFinalStatusOffline),
	}
	return e
}

func passStep(name StepName) Step {
	return Step{
		Name:            name,
		Result:          ResultPass,
		ExitCode:        CommandExitSuccess,
		ErrorCode:       nil,
		DurationSeconds: 1,
	}
}

func errorCodePointer(code ErrorCode) *ErrorCode {
	return &code
}

func cloneEvidence(e Evidence) Evidence {
	e.CapabilityBefore = append([]CapabilityStatus(nil), e.CapabilityBefore...)
	e.CapabilityAfter = append([]CapabilityStatus(nil), e.CapabilityAfter...)
	e.Steps = append([]Step(nil), e.Steps...)
	for i := range e.Steps {
		if e.Steps[i].ErrorCode != nil {
			code := *e.Steps[i].ErrorCode
			e.Steps[i].ErrorCode = &code
		}
	}
	return e
}

func nonPassAt(base Evidence, index int, result Result, exit CommandExitCode, code *ErrorCode) Evidence {
	e := cloneEvidence(base)
	e.Result = result
	e.CapabilityAfter = append([]CapabilityStatus(nil), e.CapabilityBefore...)
	e.Steps = e.Steps[:index+1]
	e.Steps[index].Result = result
	e.Steps[index].ExitCode = exit
	e.Steps[index].ErrorCode = code

	statusOnlinePassed := false
	finalOfflinePassed := false
	for _, step := range e.Steps {
		if step.Name == StepStatusOnline && step.Result == ResultPass {
			statusOnlinePassed = true
		}
		if step.Name == StepFinalStatusOffline && step.Result == ResultPass {
			finalOfflinePassed = true
		}
	}
	if statusOnlinePassed && !finalOfflinePassed {
		e.Steps = append(e.Steps,
			passStep(StepCleanupLogout),
			passStep(StepCleanupStatusOffline),
		)
	}
	return e
}

func requireInvalidEvidence(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected invalid evidence error")
	}
	if !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("error = %v, want ErrInvalidEvidence", err)
	}
}

func TestPassFixturesValidate(t *testing.T) {
	tests := []struct {
		name     string
		evidence Evidence
	}{
		{name: "password_core", evidence: validPasswordEvidence()},
		{name: "terminal_qr", evidence: validTerminalQREvidence()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.evidence.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			for i, step := range test.evidence.Steps {
				if err := step.Validate(); err != nil {
					t.Fatalf("Steps[%d].Validate() error = %v", i, err)
				}
			}
		})
	}
}

func TestClosedEnumsAndExitCodes(t *testing.T) {
	platforms := []struct {
		got  Platform
		want string
	}{
		{PlatformLinuxAMD64, "linux-amd64"},
		{PlatformWindowsAMD64, "windows-amd64"},
	}
	for _, test := range platforms {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("platform %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []Platform{"", "Linux-amd64", "linux_amd64"} {
		if value.valid() {
			t.Errorf("unexpected valid platform %q", value)
		}
	}

	testbeds := []struct {
		got  Testbed
		want string
	}{
		{TestbedNASVM, "nas_vm"},
		{TestbedBHKWindows, "bhk_windows"},
	}
	for _, test := range testbeds {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("testbed %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []Testbed{"", "NAS_VM", "nas-vm"} {
		if value.valid() {
			t.Errorf("unexpected valid testbed %q", value)
		}
	}

	networks := []struct {
		got  NetworkType
		want string
	}{
		{NetworkCampusWired, "campus_wired"},
		{NetworkCampusWiFi, "campus_wifi"},
	}
	for _, test := range networks {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("network type %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []NetworkType{"", "campus_WiFi", "campus-wired"} {
		if value.valid() {
			t.Errorf("unexpected valid network type %q", value)
		}
	}

	authMethods := []struct {
		got  AuthMethod
		want string
	}{
		{AuthMethodPassword, "password"},
		{AuthMethodTerminalQR, "terminal_qr"},
	}
	for _, test := range authMethods {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("auth method %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []AuthMethod{"", "Password", "terminal-qr"} {
		if value.valid() {
			t.Errorf("unexpected valid auth method %q", value)
		}
	}

	suites := []struct {
		got  Suite
		want string
	}{
		{SuitePasswordCore, "password_core"},
		{SuiteTerminalQR, "terminal_qr"},
	}
	for _, test := range suites {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("suite %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []Suite{"", "password-core", "Terminal_QR"} {
		if value.valid() {
			t.Errorf("unexpected valid suite %q", value)
		}
	}

	results := []struct {
		got  Result
		want string
	}{
		{ResultPass, "pass"},
		{ResultFail, "fail"},
		{ResultBlocked, "blocked"},
	}
	for _, test := range results {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("result %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []Result{"", "PASS", "block"} {
		if value.valid() {
			t.Errorf("unexpected valid result %q", value)
		}
	}

	stepNames := []struct {
		got  StepName
		want string
	}{
		{StepInitialStatusOffline, "initial_status_offline"},
		{StepLoginLoggedIn, "login_logged_in"},
		{StepQRLoginLoggedIn, "qr_login_logged_in"},
		{StepStatusOnline, "status_online"},
		{StepSecondLoginAlreadyOnline, "second_login_already_online"},
		{StepLogoutLoggedOut, "logout_logged_out"},
		{StepFinalStatusOffline, "final_status_offline"},
		{StepSecondLogoutAlreadyOffline, "second_logout_already_offline"},
		{StepCleanupLogout, "cleanup_logout"},
		{StepCleanupStatusOffline, "cleanup_status_offline"},
	}
	for _, test := range stepNames {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("step name %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []StepName{"", "Status_online", "cleanup"} {
		if value.valid() {
			t.Errorf("unexpected valid step name %q", value)
		}
	}

	errorCodes := []struct {
		got  ErrorCode
		want string
	}{
		{ErrorInvalidArgument, "invalid_argument"},
		{ErrorConfig, "config"},
		{ErrorNetwork, "network"},
		{ErrorAuthentication, "authentication"},
		{ErrorSessionConflict, "session_conflict"},
		{ErrorProtocolChanged, "protocol_changed"},
		{ErrorInteractionRequired, "interaction_required"},
		{ErrorUnsupported, "unsupported"},
		{ErrorInternal, "internal"},
	}
	for _, test := range errorCodes {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("error code %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []ErrorCode{"", "Network", "cancelled"} {
		if value.valid() {
			t.Errorf("unexpected valid error code %q", value)
		}
	}

	capabilities := []struct {
		got  CapabilityStatus
		want string
	}{
		{CapabilitySupported, "supported"},
		{CapabilityDetectedOnly, "detected_only"},
		{CapabilityObservedAnonymous, "observed_anonymous"},
		{CapabilitySyntheticCovered, "synthetic_covered"},
		{CapabilityLiveUnverified, "live_unverified"},
		{CapabilityLiveVerified, "live_verified"},
		{CapabilityUnknown, "unknown"},
	}
	for _, test := range capabilities {
		if string(test.got) != test.want || !test.got.valid() {
			t.Errorf("capability %q is not the expected valid enum", test.got)
		}
	}
	for _, value := range []CapabilityStatus{"", "Supported", "live-verified"} {
		if value.valid() {
			t.Errorf("unexpected valid capability %q", value)
		}
	}

	gateExitCodes := []struct {
		got  GateExitCode
		want int
	}{
		{GateExitPass, 0},
		{GateExitFail, 10},
		{GateExitBlocked, 11},
		{GateExitSecurityReject, 12},
		{GateExitEvidenceDurability, 13},
		{GateExitInternal, 14},
		{GateExitCanceled, 130},
	}
	for _, test := range gateExitCodes {
		if int(test.got) != test.want || !test.got.valid() {
			t.Errorf("runner exit %d is not the expected valid enum", test.got)
		}
	}
	for _, value := range []GateExitCode{-1, 1, 9, 15, 129, 131} {
		if value.valid() {
			t.Errorf("unexpected valid runner exit %d", value)
		}
	}

	commandExitCodes := []struct {
		got  CommandExitCode
		want int
	}{
		{CommandExitSuccess, 0},
		{CommandExitInternal, 1},
		{CommandExitInvalid, 2},
		{CommandExitNetwork, 3},
		{CommandExitAuthentication, 4},
		{CommandExitSessionConflict, 5},
		{CommandExitProtocolChanged, 6},
		{CommandExitInteractionRequired, 7},
		{CommandExitCanceled, 130},
	}
	for _, test := range commandExitCodes {
		if int(test.got) != test.want {
			t.Errorf("candidate command exit = %d, want %d", test.got, test.want)
		}
	}
}

func TestValidationMatrix(t *testing.T) {
	valid := []struct {
		name       string
		platform   Platform
		testbed    Testbed
		network    NetworkType
		authMethod AuthMethod
		suite      Suite
	}{
		{"linux_nas_wired_password", PlatformLinuxAMD64, TestbedNASVM, NetworkCampusWired, AuthMethodPassword, SuitePasswordCore},
		{"linux_nas_wired_terminal_qr", PlatformLinuxAMD64, TestbedNASVM, NetworkCampusWired, AuthMethodTerminalQR, SuiteTerminalQR},
		{"windows_bhk_wired_password", PlatformWindowsAMD64, TestbedBHKWindows, NetworkCampusWired, AuthMethodPassword, SuitePasswordCore},
		{"windows_bhk_wifi_password", PlatformWindowsAMD64, TestbedBHKWindows, NetworkCampusWiFi, AuthMethodPassword, SuitePasswordCore},
	}
	for _, test := range valid {
		t.Run("valid_"+test.name, func(t *testing.T) {
			e := validPasswordEvidence()
			if test.suite == SuiteTerminalQR {
				e = validTerminalQREvidence()
			}
			e.Platform = test.platform
			e.Testbed = test.testbed
			e.NetworkType = test.network
			e.AuthMethod = test.authMethod
			e.Suite = test.suite
			if err := e.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}

	invalid := []struct {
		name       string
		platform   Platform
		testbed    Testbed
		network    NetworkType
		authMethod AuthMethod
		suite      Suite
	}{
		{"linux_wifi_password", PlatformLinuxAMD64, TestbedNASVM, NetworkCampusWiFi, AuthMethodPassword, SuitePasswordCore},
		{"linux_bhk_password", PlatformLinuxAMD64, TestbedBHKWindows, NetworkCampusWired, AuthMethodPassword, SuitePasswordCore},
		{"windows_nas_password", PlatformWindowsAMD64, TestbedNASVM, NetworkCampusWired, AuthMethodPassword, SuitePasswordCore},
		{"windows_wired_terminal_qr", PlatformWindowsAMD64, TestbedBHKWindows, NetworkCampusWired, AuthMethodTerminalQR, SuiteTerminalQR},
		{"windows_wifi_terminal_qr", PlatformWindowsAMD64, TestbedBHKWindows, NetworkCampusWiFi, AuthMethodTerminalQR, SuiteTerminalQR},
		{"auth_suite_mismatch", PlatformLinuxAMD64, TestbedNASVM, NetworkCampusWired, AuthMethodPassword, SuiteTerminalQR},
	}
	for _, test := range invalid {
		t.Run("invalid_"+test.name, func(t *testing.T) {
			e := validPasswordEvidence()
			e.Platform = test.platform
			e.Testbed = test.testbed
			e.NetworkType = test.network
			e.AuthMethod = test.authMethod
			e.Suite = test.suite
			requireInvalidEvidence(t, e.Validate())
		})
	}
}

func TestIdentifiersHashesAndTimestamps(t *testing.T) {
	valid := validPasswordEvidence()
	valid.FinishedAt = valid.StartedAt
	if err := valid.Validate(); err != nil {
		t.Fatalf("equal timestamps should validate: %v", err)
	}
	valid.StartedAt = "2026-08-29T01:02:03.123456789Z"
	valid.FinishedAt = "2026-08-29T01:02:03.123456789Z"
	if err := valid.Validate(); err != nil {
		t.Fatalf("canonical RFC3339Nano should validate: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Evidence)
	}{
		{"schema_version", func(e *Evidence) { e.SchemaVersion = 2 }},
		{"plan_id", func(e *Evidence) { e.PlanID = "ipgw-meta-v1" }},
		{"revision", func(e *Evidence) { e.Revision = "2026-08-28-R2" }},
		{"evidence_id_shape", func(e *Evidence) { e.EvidenceID = "EVID-20260829-1" }},
		{"evidence_id_zero", func(e *Evidence) { e.EvidenceID = "EVID-20260829-000" }},
		{"evidence_id_too_large", func(e *Evidence) { e.EvidenceID = "EVID-20260829-1000" }},
		{"evidence_id_invalid_date", func(e *Evidence) { e.EvidenceID = "EVID-20260230-001" }},
		{"evidence_id_date_mismatch", func(e *Evidence) { e.EvidenceID = "EVID-20260828-001" }},
		{"candidate_id_shape", func(e *Evidence) { e.CandidateID = "v1.0.0-0123456789ab-12345" }},
		{"candidate_id_source_mismatch", func(e *Evidence) { e.CandidateID = "v1.0.0-ffffffffffff-12345.1" }},
		{"candidate_id_run_leading_zero", func(e *Evidence) { e.CandidateID = "v1.0.0-0123456789ab-012345.1" }},
		{"candidate_id_zero_run", func(e *Evidence) { e.CandidateID = "v1.0.0-0123456789ab-0.1" }},
		{"candidate_id_zero_attempt", func(e *Evidence) { e.CandidateID = "v1.0.0-0123456789ab-12345.0" }},
		{"candidate_id_attempt_leading_zero", func(e *Evidence) { e.CandidateID = "v1.0.0-0123456789ab-12345.01" }},
		{"candidate_hash_short", func(e *Evidence) { e.CandidateSetSHA256 = strings.Repeat("a", 63) }},
		{"candidate_hash_uppercase", func(e *Evidence) { e.CandidateSetSHA256 = strings.Repeat("A", 64) }},
		{"source_commit_short", func(e *Evidence) { e.SourceCommit = strings.Repeat("0", 39) }},
		{"source_commit_uppercase", func(e *Evidence) { e.SourceCommit = strings.Repeat("A", 40) }},
		{"started_offset", func(e *Evidence) { e.StartedAt = "2026-08-29T01:02:03+00:00" }},
		{"finished_offset", func(e *Evidence) { e.FinishedAt = "2026-08-29T01:02:10+00:00" }},
		{"started_noncanonical_fraction", func(e *Evidence) { e.StartedAt = "2026-08-29T01:02:03.0Z" }},
		{"finished_noncanonical_fraction", func(e *Evidence) { e.FinishedAt = "2026-08-29T01:02:10.000Z" }},
		{"timestamp_lowercase", func(e *Evidence) { e.StartedAt = "2026-08-29t01:02:03z" }},
		{"timestamp_order", func(e *Evidence) { e.FinishedAt = "2026-08-29T01:02:02Z" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := validPasswordEvidence()
			test.mutate(&e)
			requireInvalidEvidence(t, e.Validate())
		})
	}
}

func TestExactCapabilityTransitions(t *testing.T) {
	for _, evidence := range []Evidence{validPasswordEvidence(), validTerminalQREvidence()} {
		if err := evidence.Validate(); err != nil {
			t.Fatalf("pass transition for %s: %v", evidence.Suite, err)
		}
		for _, result := range []Result{ResultFail, ResultBlocked} {
			nonPass := nonPassAt(evidence, 0, result, CommandExitSuccess, nil)
			if err := nonPass.Validate(); err != nil {
				t.Fatalf("non-pass transition for %s/%s: %v", evidence.Suite, result, err)
			}
		}
	}

	tests := []struct {
		name   string
		mutate func(*Evidence)
	}{
		{"password_before_order", func(e *Evidence) {
			e.CapabilityBefore[0], e.CapabilityBefore[1] = e.CapabilityBefore[1], e.CapabilityBefore[0]
		}},
		{"password_before_extra", func(e *Evidence) {
			e.CapabilityBefore = append(e.CapabilityBefore, CapabilitySupported)
		}},
		{"password_after_unverified", func(e *Evidence) {
			e.CapabilityAfter[1] = CapabilityLiveUnverified
		}},
		{"password_after_unknown", func(e *Evidence) {
			e.CapabilityAfter[1] = CapabilityUnknown
		}},
		{"unknown_before_enum", func(e *Evidence) {
			e.CapabilityBefore[0] = CapabilityStatus("Synthetic_Covered")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e := validPasswordEvidence()
			test.mutate(&e)
			requireInvalidEvidence(t, e.Validate())
		})
	}

	qr := validTerminalQREvidence()
	qr.CapabilityBefore = qr.CapabilityBefore[1:]
	requireInvalidEvidence(t, qr.Validate())

	nonPass := nonPassAt(validPasswordEvidence(), 0, ResultFail, CommandExitSuccess, nil)
	nonPass.CapabilityAfter = []CapabilityStatus{CapabilitySyntheticCovered, CapabilityLiveVerified}
	requireInvalidEvidence(t, nonPass.Validate())
}

func TestValidNonPassPrefixesAndCleanupOwnership(t *testing.T) {
	tests := []struct {
		name     string
		evidence Evidence
	}{
		{
			name:     "fail_at_initial_no_cleanup",
			evidence: nonPassAt(validPasswordEvidence(), 0, ResultFail, CommandExitAuthentication, errorCodePointer(ErrorAuthentication)),
		},
		{
			name:     "blocked_at_login_no_cleanup",
			evidence: nonPassAt(validPasswordEvidence(), 1, ResultBlocked, CommandExitInteractionRequired, errorCodePointer(ErrorInteractionRequired)),
		},
		{
			name:     "fail_after_online_with_cleanup",
			evidence: nonPassAt(validPasswordEvidence(), 3, ResultFail, CommandExitSuccess, nil),
		},
		{
			name:     "blocked_at_logout_with_cleanup",
			evidence: nonPassAt(validPasswordEvidence(), 4, ResultBlocked, CommandExitSuccess, nil),
		},
		{
			name:     "fail_at_final_offline_with_cleanup",
			evidence: nonPassAt(validPasswordEvidence(), 5, ResultFail, CommandExitNetwork, errorCodePointer(ErrorNetwork)),
		},
		{
			name:     "fail_after_final_offline_no_cleanup",
			evidence: nonPassAt(validPasswordEvidence(), 6, ResultFail, CommandExitSuccess, nil),
		},
		{
			name:     "terminal_qr_blocked_before_online",
			evidence: nonPassAt(validTerminalQREvidence(), 1, ResultBlocked, CommandExitInteractionRequired, errorCodePointer(ErrorInteractionRequired)),
		},
		{
			name:     "terminal_qr_fail_after_online_with_cleanup",
			evidence: nonPassAt(validTerminalQREvidence(), 3, ResultFail, CommandExitAuthentication, errorCodePointer(ErrorAuthentication)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.evidence.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestInvalidPassAndNonPassSequences(t *testing.T) {
	passMissing := validPasswordEvidence()
	passMissing.Steps = passMissing.Steps[:len(passMissing.Steps)-1]

	passWrongOrder := validPasswordEvidence()
	passWrongOrder.Steps[1], passWrongOrder.Steps[2] = passWrongOrder.Steps[2], passWrongOrder.Steps[1]

	passWithCleanup := validPasswordEvidence()
	passWithCleanup.Steps = append(passWithCleanup.Steps, passStep(StepCleanupLogout), passStep(StepCleanupStatusOffline))

	passWithFailedStep := validPasswordEvidence()
	passWithFailedStep.Steps[2].Result = ResultFail

	emptyNonPass := nonPassAt(validPasswordEvidence(), 0, ResultFail, CommandExitSuccess, nil)
	emptyNonPass.Steps = nil

	allPassPrefix := nonPassAt(validPasswordEvidence(), 0, ResultBlocked, CommandExitSuccess, nil)
	allPassPrefix.Steps[0].Result = ResultPass

	wrongStart := nonPassAt(validPasswordEvidence(), 0, ResultFail, CommandExitSuccess, nil)
	wrongStart.Steps[0].Name = StepLoginLoggedIn

	continuedAfterFailure := nonPassAt(validPasswordEvidence(), 0, ResultFail, CommandExitSuccess, nil)
	continuedAfterFailure.Steps = append(continuedAfterFailure.Steps, passStep(StepLoginLoggedIn))

	cleanupWithoutOwnership := nonPassAt(validPasswordEvidence(), 0, ResultFail, CommandExitSuccess, nil)
	cleanupWithoutOwnership.Steps = append(cleanupWithoutOwnership.Steps, passStep(StepCleanupLogout), passStep(StepCleanupStatusOffline))

	missingCleanup := nonPassAt(validPasswordEvidence(), 3, ResultFail, CommandExitSuccess, nil)
	missingCleanup.Steps = missingCleanup.Steps[:len(missingCleanup.Steps)-2]

	shortCleanup := nonPassAt(validPasswordEvidence(), 3, ResultFail, CommandExitSuccess, nil)
	shortCleanup.Steps = shortCleanup.Steps[:len(shortCleanup.Steps)-1]

	reversedCleanup := nonPassAt(validPasswordEvidence(), 3, ResultFail, CommandExitSuccess, nil)
	last := len(reversedCleanup.Steps) - 1
	reversedCleanup.Steps[last-1], reversedCleanup.Steps[last] = reversedCleanup.Steps[last], reversedCleanup.Steps[last-1]

	cleanupAfterFinalOffline := nonPassAt(validPasswordEvidence(), 6, ResultFail, CommandExitSuccess, nil)
	cleanupAfterFinalOffline.Steps = append(cleanupAfterFinalOffline.Steps, passStep(StepCleanupLogout), passStep(StepCleanupStatusOffline))

	extraCleanup := nonPassAt(validPasswordEvidence(), 3, ResultFail, CommandExitSuccess, nil)
	extraCleanup.Steps = append(extraCleanup.Steps, passStep(StepCleanupStatusOffline))

	tests := []struct {
		name     string
		evidence Evidence
	}{
		{"pass_missing_step", passMissing},
		{"pass_wrong_order", passWrongOrder},
		{"pass_with_cleanup", passWithCleanup},
		{"pass_with_failed_step", passWithFailedStep},
		{"nonpass_empty", emptyNonPass},
		{"nonpass_all_pass_prefix", allPassPrefix},
		{"nonpass_wrong_start", wrongStart},
		{"nonpass_continues_after_failure", continuedAfterFailure},
		{"cleanup_without_ownership", cleanupWithoutOwnership},
		{"missing_cleanup", missingCleanup},
		{"short_cleanup", shortCleanup},
		{"reversed_cleanup", reversedCleanup},
		{"cleanup_after_final_offline", cleanupAfterFinalOffline},
		{"extra_cleanup", extraCleanup},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireInvalidEvidence(t, test.evidence.Validate())
		})
	}
}

func TestOverallResultAggregation(t *testing.T) {
	blocked := nonPassAt(validPasswordEvidence(), 3, ResultBlocked, CommandExitSuccess, nil)
	if err := blocked.Validate(); err != nil {
		t.Fatalf("all-blocked cleanup path should aggregate to blocked: %v", err)
	}

	failInCleanup := cloneEvidence(blocked)
	failInCleanup.Result = ResultFail
	failInCleanup.Steps[len(failInCleanup.Steps)-2].Result = ResultFail
	if err := failInCleanup.Validate(); err != nil {
		t.Fatalf("cleanup failure should aggregate to fail: %v", err)
	}

	wrongBlocked := cloneEvidence(failInCleanup)
	wrongBlocked.Result = ResultBlocked
	requireInvalidEvidence(t, wrongBlocked.Validate())

	wrongFail := cloneEvidence(blocked)
	wrongFail.Result = ResultFail
	requireInvalidEvidence(t, wrongFail.Validate())

	wrongPass := cloneEvidence(blocked)
	wrongPass.Result = ResultPass
	wrongPass.CapabilityAfter = []CapabilityStatus{CapabilitySyntheticCovered, CapabilityLiveVerified}
	requireInvalidEvidence(t, wrongPass.Validate())
}

func TestStepExitErrorMapping(t *testing.T) {
	tests := []struct {
		name  string
		exit  CommandExitCode
		codes []ErrorCode
	}{
		{"internal", CommandExitInternal, []ErrorCode{ErrorInternal}},
		{"invalid", CommandExitInvalid, []ErrorCode{ErrorInvalidArgument, ErrorConfig, ErrorUnsupported}},
		{"network", CommandExitNetwork, []ErrorCode{ErrorNetwork}},
		{"authentication", CommandExitAuthentication, []ErrorCode{ErrorAuthentication}},
		{"session_conflict", CommandExitSessionConflict, []ErrorCode{ErrorSessionConflict}},
		{"protocol_changed", CommandExitProtocolChanged, []ErrorCode{ErrorProtocolChanged}},
		{"interaction_required", CommandExitInteractionRequired, []ErrorCode{ErrorInteractionRequired}},
		{"candidate_canceled_maps_to_network", CommandExitCanceled, []ErrorCode{ErrorNetwork}},
	}
	for _, test := range tests {
		for _, code := range test.codes {
			t.Run(test.name+"_"+string(code), func(t *testing.T) {
				for _, result := range []Result{ResultFail, ResultBlocked} {
					step := Step{
						Name:            StepInitialStatusOffline,
						Result:          result,
						ExitCode:        test.exit,
						ErrorCode:       errorCodePointer(code),
						DurationSeconds: 0,
					}
					if err := step.Validate(); err != nil {
						t.Errorf("%s step Validate() error = %v", result, err)
					}
				}
			})
		}
	}

	for _, result := range []Result{ResultPass, ResultFail, ResultBlocked} {
		step := Step{Name: StepInitialStatusOffline, Result: result, ExitCode: CommandExitSuccess}
		if err := step.Validate(); err != nil {
			t.Errorf("exit 0/null with semantic result %s should validate: %v", result, err)
		}
	}

	invalid := []Step{
		{Name: StepInitialStatusOffline, Result: ResultFail, ExitCode: CommandExitSuccess, ErrorCode: errorCodePointer(ErrorInternal)},
		{Name: StepInitialStatusOffline, Result: ResultFail, ExitCode: CommandExitInternal},
		{Name: StepInitialStatusOffline, Result: ResultFail, ExitCode: CommandExitInternal, ErrorCode: errorCodePointer(ErrorNetwork)},
		{Name: StepInitialStatusOffline, Result: ResultFail, ExitCode: CommandExitInvalid, ErrorCode: errorCodePointer(ErrorNetwork)},
		{Name: StepInitialStatusOffline, Result: ResultFail, ExitCode: CommandExitCanceled, ErrorCode: errorCodePointer(ErrorInternal)},
		{Name: StepInitialStatusOffline, Result: ResultFail, ExitCode: 8, ErrorCode: errorCodePointer(ErrorInternal)},
		{Name: StepInitialStatusOffline, Result: ResultPass, ExitCode: CommandExitNetwork, ErrorCode: errorCodePointer(ErrorNetwork)},
		{Name: StepName("Initial_Status_Offline"), Result: ResultFail, ExitCode: CommandExitSuccess},
		{Name: StepInitialStatusOffline, Result: Result("FAIL"), ExitCode: CommandExitSuccess},
		{Name: StepInitialStatusOffline, Result: ResultFail, ExitCode: CommandExitNetwork, ErrorCode: errorCodePointer(ErrorCode("Network"))},
		{Name: StepInitialStatusOffline, Result: ResultFail, ExitCode: CommandExitSuccess, DurationSeconds: -1},
	}
	for i, step := range invalid {
		t.Run(fmt.Sprintf("invalid_%02d", i), func(t *testing.T) {
			requireInvalidEvidence(t, step.Validate())
		})
	}
}

func TestValidationErrorsDoNotLeakUntrustedValues(t *testing.T) {
	const canary = "CANARY_password_ticket_9f91?token=do-not-echo"
	e := validPasswordEvidence()
	e.CandidateID = canary
	err := e.Validate()
	requireInvalidEvidence(t, err)
	assertErrorTreeDoesNotContain(t, err, canary)

	e = validPasswordEvidence()
	e.Platform = Platform(canary)
	err = e.Validate()
	requireInvalidEvidence(t, err)
	assertErrorTreeDoesNotContain(t, err, canary)
}

func assertErrorTreeDoesNotContain(t *testing.T, err error, canary string) {
	t.Helper()
	var walk func(error, int)
	walk = func(current error, depth int) {
		if current == nil {
			return
		}
		if depth > 64 {
			t.Fatal("error unwrap tree exceeds 64 levels")
		}
		if strings.Contains(current.Error(), canary) {
			t.Fatalf("error leaked canary at unwrap depth %d: %q", depth, current.Error())
		}
		switch unwrapped := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range unwrapped.Unwrap() {
				walk(child, depth+1)
			}
		case interface{ Unwrap() error }:
			walk(unwrapped.Unwrap(), depth+1)
		}
	}
	walk(err, 0)
}
