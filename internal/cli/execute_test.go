package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
	"github.com/UnbalancedCat/ipgw-meta/internal/app"
	"github.com/UnbalancedCat/ipgw-meta/internal/config"
)

const wireCanary = "WIRE-CANARY-MUST-NOT-LEAK"

func TestExecuteUsesJSONForPostfixedParseErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "missing value before inline json", args: []string{"--config", "--output=json"}},
		{name: "missing value after spaced json", args: []string{"--output", "json", "--profile"}},
		{name: "conflicting output", args: []string{"--output=human", "--json"}},
		{name: "duplicate output", args: []string{"--output=json", "--output=json"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := Execute(context.Background(), Options{
				Mode: ModeMeta, Args: test.args, Out: &stdout, Err: &stderr,
			})
			if exit != 2 {
				t.Fatalf("exit = %d, want 2", exit)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			object := decodeSingleEnvelope(t, stdout.Bytes())
			if got := object["command"]; got != "cli" {
				t.Fatalf("command = %#v, want cli", got)
			}
			if got := object["ok"]; got != false {
				t.Fatalf("ok = %#v, want false", got)
			}
			assertEnvelopeXOR(t, object, false)
			errorObject := objectField(t, object, "error")
			if got := errorObject["code"]; got != string(ipgw.CodeInvalidArgument) {
				t.Fatalf("code = %#v, want %q", got, ipgw.CodeInvalidArgument)
			}
		})
	}
}

func TestOutputPreparseAndExtraction(t *testing.T) {
	if got := preparseOutputMode([]string{"--config", "--output=json"}); got != outputJSON {
		t.Fatalf("postfixed output mode = %q, want json", got)
	}
	if got := preparseOutputMode([]string{"--", "--json"}); got != outputHuman {
		t.Fatalf("output mode after -- = %q, want human", got)
	}

	globals, remaining, err := extractGlobals([]string{"status", "--json"})
	if err != nil {
		t.Fatalf("extract --json: %v", err)
	}
	if globals.output != outputJSON {
		t.Fatalf("output = %q, want json", globals.output)
	}
	if len(remaining) != 1 || remaining[0] != "status" {
		t.Fatalf("remaining = %#v, want [status]", remaining)
	}

	for _, args := range [][]string{
		{"--json", "--json"},
		{"--json", "--output=json"},
		{"--output=human", "--json"},
		{"--output=json", "--output=json"},
		{"--json=value"},
	} {
		if _, _, err := extractGlobals(args); err == nil {
			t.Errorf("extractGlobals(%#v) succeeded, want duplicate/conflict error", args)
		}
	}

	globals, remaining, err = extractGlobals([]string{"--", "--json"})
	if err != nil {
		t.Fatalf("extract after --: %v", err)
	}
	if globals.output != outputHuman || len(remaining) != 1 || remaining[0] != "--json" {
		t.Fatalf("after --: output=%q remaining=%#v", globals.output, remaining)
	}
}

func TestEnvelopeAlwaysHasDataErrorXOR(t *testing.T) {
	t.Run("success includes null data", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		exit := (renderer{mode: outputJSON, out: &stdout, err: &stderr}).success("status", nil)
		if exit != 0 {
			t.Fatalf("exit = %d, want 0", exit)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr = %q, want empty", stderr.String())
		}
		object := decodeSingleEnvelope(t, stdout.Bytes())
		assertEnvelopeXOR(t, object, true)
		if value, exists := object["data"]; !exists || value != nil {
			t.Fatalf("data = %#v, exists=%v; want explicit null", value, exists)
		}
	})

	t.Run("failure includes only error", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		err := &ipgw.Error{Code: ipgw.CodeInvalidArgument, Message: wireCanary}
		exit := (renderer{mode: outputJSON, out: &stdout, err: &stderr}).failure("cli", err)
		if exit != 2 {
			t.Fatalf("exit = %d, want 2", exit)
		}
		object := decodeSingleEnvelope(t, stdout.Bytes())
		assertEnvelopeXOR(t, object, false)
		if strings.Contains(stdout.String(), wireCanary) {
			t.Fatal("arbitrary error message leaked into JSON")
		}
	})
}

func TestAllStableExitCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "internal", err: errors.New(wireCanary), want: 1},
		{name: "invalid argument", err: &ipgw.Error{Code: ipgw.CodeInvalidArgument}, want: 2},
		{name: "config", err: &ipgw.Error{Code: ipgw.CodeConfig}, want: 2},
		{name: "unsupported", err: &ipgw.Error{Code: ipgw.CodeUnsupported}, want: 2},
		{name: "network", err: &ipgw.Error{Code: ipgw.CodeNetwork}, want: 3},
		{name: "deadline", err: context.DeadlineExceeded, want: 3},
		{name: "authentication", err: &ipgw.Error{Code: ipgw.CodeAuthentication}, want: 4},
		{name: "session conflict", err: &ipgw.Error{Code: ipgw.CodeSessionConflict}, want: 5},
		{name: "protocol changed", err: &ipgw.Error{Code: ipgw.CodeProtocolChanged}, want: 6},
		{name: "interaction required", err: &ipgw.Error{Code: ipgw.CodeInteractionRequired}, want: 7},
		{name: "canceled", err: context.Canceled, want: 130},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			exit := (renderer{mode: outputHuman, out: io.Discard, err: &stderr}).failure("test", test.err)
			if exit != test.want {
				t.Fatalf("exit = %d, want %d", exit, test.want)
			}
			if strings.Contains(stderr.String(), wireCanary) {
				t.Fatal("arbitrary error text leaked into human output")
			}
		})
	}
}

func TestWireMessagesAreFixedAndInteractionIsAllowlisted(t *testing.T) {
	messages := map[ipgw.ErrorCode]string{
		ipgw.CodeInvalidArgument:     "invalid arguments",
		ipgw.CodeConfig:              "configuration error",
		ipgw.CodeNetwork:             "network request failed",
		ipgw.CodeAuthentication:      "authentication failed",
		ipgw.CodeSessionConflict:     "another account is already online",
		ipgw.CodeProtocolChanged:     "gateway protocol changed",
		ipgw.CodeInteractionRequired: "login requires human verification",
		ipgw.CodeUnsupported:         "operation is unsupported",
		ipgw.CodeInternal:            "internal error",
	}
	for code, expected := range messages {
		t.Run(string(code), func(t *testing.T) {
			var stdout bytes.Buffer
			err := &ipgw.Error{Code: code, Message: wireCanary, Cause: errors.New(wireCanary)}
			(renderer{mode: outputJSON, out: &stdout, err: io.Discard}).failure("test", err)
			if strings.Contains(stdout.String(), wireCanary) {
				t.Fatal("arbitrary error text leaked into JSON")
			}
			wired := objectField(t, decodeSingleEnvelope(t, stdout.Bytes()), "error")
			if got := wired["message"]; got != expected {
				t.Fatalf("message = %#v, want %q", got, expected)
			}
		})
	}

	interaction := &ipgw.InteractionDetails{
		Challenge:      ipgw.ChallengeSMSOTP,
		OriginMethod:   ipgw.AuthMethodPassword,
		Capability:     []ipgw.CapabilityStatus{ipgw.CapabilityObservedAnonymous, ipgw.CapabilityStatus(wireCanary), ipgw.CapabilityDetectedOnly},
		SessionBinding: "cas_session",
		ResumeMode:     "official_portal",
		TTYRequired:    true,
		HelpID:         "AUTH-SMS-001",
	}
	setOptionalStringField(interaction, "UserAction", wireCanary)
	setOptionalStringField(interaction, "DeliveryHint", wireCanary)
	err := &ipgw.Error{
		Code: ipgw.CodeInteractionRequired, Message: wireCanary, Cause: errors.New(wireCanary),
		Details: ipgw.ErrorDetails{Interaction: interaction},
	}
	var stdout, humanStderr bytes.Buffer
	(renderer{mode: outputJSON, out: &stdout, err: io.Discard}).failure("login", err)
	(renderer{mode: outputHuman, out: io.Discard, err: &humanStderr}).failure("login", err)
	if strings.Contains(stdout.String(), wireCanary) || strings.Contains(humanStderr.String(), wireCanary) {
		t.Fatal("interaction canary leaked")
	}
	wiredError := objectField(t, decodeSingleEnvelope(t, stdout.Bytes()), "error")
	details := objectField(t, wiredError, "details")
	wiredInteraction := objectField(t, details, "interaction")
	if got := wiredInteraction["challenge_kind"]; got != string(ipgw.ChallengeSMSOTP) {
		t.Fatalf("challenge_kind = %#v", got)
	}
	if got := wiredInteraction["origin_method"]; got != string(ipgw.AuthMethodPassword) {
		t.Fatalf("origin_method = %#v", got)
	}
	if got := wiredInteraction["session_binding"]; got != "cas_session" {
		t.Fatalf("session_binding = %#v", got)
	}
	if got := wiredInteraction["resume_mode"]; got != "official_portal" {
		t.Fatalf("resume_mode = %#v", got)
	}
	if got := wiredInteraction["help_id"]; got != "AUTH-SMS-001" {
		t.Fatalf("help_id = %#v", got)
	}
	capabilities, ok := wiredInteraction["capability_status"].([]any)
	if !ok || len(capabilities) != 2 {
		t.Fatalf("capability_status = %#v, want two allowlisted values", wiredInteraction["capability_status"])
	}

	invalidInteraction := &ipgw.Error{
		Code: ipgw.CodeInteractionRequired, Message: wireCanary,
		Details: ipgw.ErrorDetails{Interaction: &ipgw.InteractionDetails{
			Challenge: ipgw.ChallengeKind(wireCanary), OriginMethod: ipgw.AuthMethodPassword,
			Capability:     []ipgw.CapabilityStatus{ipgw.CapabilityStatus(wireCanary)},
			SessionBinding: wireCanary, ResumeMode: wireCanary, HelpID: wireCanary,
		}},
	}
	stdout.Reset()
	(renderer{mode: outputJSON, out: &stdout, err: io.Discard}).failure("login", invalidInteraction)
	if strings.Contains(stdout.String(), wireCanary) {
		t.Fatal("invalid interaction fields leaked")
	}
	wiredError = objectField(t, decodeSingleEnvelope(t, stdout.Bytes()), "error")
	details = objectField(t, wiredError, "details")
	wiredInteraction = objectField(t, details, "interaction")
	if got := wiredInteraction["challenge_kind"]; got != string(ipgw.ChallengeUnknown) {
		t.Fatalf("invalid challenge normalized to %#v, want unknown", got)
	}
	for _, field := range []string{"session_binding", "resume_mode", "help_id"} {
		if _, exists := wiredInteraction[field]; exists {
			t.Errorf("invalid %s was not omitted", field)
		}
	}
}

func TestJSONEncodeFailureReturnsInternalExitWithoutRetry(t *testing.T) {
	for _, test := range []struct {
		name string
		run  func(renderer) int
	}{
		{name: "success", run: func(r renderer) int { return r.success("status", map[string]any{"ok": true}) }},
		{name: "failure", run: func(r renderer) int {
			return r.failure("status", &ipgw.Error{Code: ipgw.CodeNetwork, Message: wireCanary})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &countingFailWriter{}
			var stderr bytes.Buffer
			exit := test.run(renderer{mode: outputJSON, out: writer, err: &stderr})
			if exit != 1 {
				t.Fatalf("exit = %d, want 1", exit)
			}
			if writer.writes != 1 {
				t.Fatalf("writes = %d, want exactly one encode attempt", writer.writes)
			}
			if got := stderr.String(); got != "Error: unable to write JSON output\n" {
				t.Fatalf("stderr = %q", got)
			}
			if strings.Contains(stderr.String(), wireCanary) {
				t.Fatal("encoder error leaked")
			}
		})
	}
}

func TestModernTreeRejectsRemovedCommandsWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "top-level migrate", args: []string{"migrate", "--yes"}},
		{name: "top-level version", args: []string{"version"}},
		{name: "profile set", args: []string{"profile", "set", "work"}},
		{name: "profile update", args: []string{"profile", "update", "work"}},
		{name: "profile use", args: []string{"profile", "use", "work"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := t.TempDir()
			configPath := filepath.Join(base, "config.yaml")
			gateway := &recordingGateway{}
			var stdout, stderr bytes.Buffer
			exit := Execute(context.Background(), Options{
				Mode: ModeMeta,
				Args: test.args,
				Paths: config.Paths{
					BaseDir:         base,
					ConfigFile:      configPath,
					MigrationMarker: filepath.Join(base, "migration.yaml"),
					LegacyMetaYAML:  filepath.Join(base, "legacy-meta.yaml"),
					LegacyUpstream:  filepath.Join(base, "legacy-upstream.json"),
				},
				NewGateway: func(config.Paths) Gateway { return gateway },
				Out:        &stdout,
				Err:        &stderr,
			})
			if exit != 2 {
				t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
			if gateway.totalCalls() != 0 {
				t.Fatalf("removed command called gateway %d time(s)", gateway.totalCalls())
			}
			if _, err := os.Stat(configPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("removed command changed config: stat err=%v", err)
			}
		})
	}
}

func TestModernHelpShowsOnlyFixedTree(t *testing.T) {
	gateway := &recordingGateway{}
	var stdout, stderr bytes.Buffer
	exit := Execute(context.Background(), Options{
		Mode:       ModeMeta,
		Args:       []string{"help"},
		NewGateway: func(config.Paths) Gateway { return gateway },
		Out:        &stdout,
		Err:        &stderr,
	})
	if exit != 0 {
		t.Fatalf("exit = %d; stderr=%q", exit, stderr.String())
	}
	for _, expected := range []string{
		"<status|login|logout|network|profile>",
		"login [--method password|qr] [--switch]",
		"network <list|scan>",
		"profile <list|show|add|remove|migrate>",
	} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("help missing %q:\n%s", expected, stdout.String())
		}
	}
	for _, removed := range []string{"profile <list|show|add|update", "profile <list|show|add|set", "profile use", "<status|login|logout|network|profile|migrate>", "version"} {
		if strings.Contains(stdout.String(), removed) {
			t.Errorf("help still contains removed path %q:\n%s", removed, stdout.String())
		}
	}
	if gateway.totalCalls() != 0 {
		t.Fatalf("help called gateway %d time(s)", gateway.totalCalls())
	}
}

func TestModernLoginMethodContract(t *testing.T) {
	valid := []struct {
		name           string
		args           []string
		method         ipgw.AuthMethod
		switchExisting bool
	}{
		{name: "default password", args: []string{"login"}, method: ipgw.AuthMethodPassword},
		{name: "spaced password", args: []string{"login", "--method", "password"}, method: ipgw.AuthMethodPassword},
		{name: "inline password", args: []string{"login", "--method=password"}, method: ipgw.AuthMethodPassword},
		{name: "spaced qr", args: []string{"login", "--method", "qr"}, method: ipgw.AuthMethodTerminalQR},
		{name: "inline qr and switch", args: []string{"login", "--method=qr", "--switch"}, method: ipgw.AuthMethodTerminalQR, switchExisting: true},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			gateway := &recordingGateway{}
			var stdout, stderr bytes.Buffer
			exit := Execute(context.Background(), Options{
				Mode:       ModeMeta,
				Args:       test.args,
				NewGateway: func(config.Paths) Gateway { return gateway },
				Out:        &stdout,
				Err:        &stderr,
				IsTTY:      test.method == ipgw.AuthMethodTerminalQR,
			})
			if exit != 0 {
				t.Fatalf("exit = %d; stderr=%q", exit, stderr.String())
			}
			if gateway.loginCalls != 1 {
				t.Fatalf("login calls = %d, want 1", gateway.loginCalls)
			}
			if gateway.lastLogin.Method != test.method {
				t.Fatalf("method = %q, want %q", gateway.lastLogin.Method, test.method)
			}
			if gateway.lastLogin.Switch != test.switchExisting {
				t.Fatalf("switch = %v, want %v", gateway.lastLogin.Switch, test.switchExisting)
			}
		})
	}

	invalid := []struct {
		name string
		args []string
	}{
		{name: "auth alias", args: []string{"login", "--auth", "qr"}},
		{name: "terminal qr alias spaced", args: []string{"login", "--method", "terminal-qr"}},
		{name: "terminal qr alias inline", args: []string{"login", "--method=terminal-qr"}},
		{name: "missing method", args: []string{"login", "--method"}},
		{name: "empty inline method", args: []string{"login", "--method="}},
		{name: "duplicate method", args: []string{"login", "--method=qr", "--method", "qr"}},
		{name: "duplicate switch", args: []string{"login", "--switch", "--switch"}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			gateway := &recordingGateway{}
			var stdout, stderr bytes.Buffer
			exit := Execute(context.Background(), Options{
				Mode:       ModeMeta,
				Args:       test.args,
				NewGateway: func(config.Paths) Gateway { return gateway },
				Out:        &stdout,
				Err:        &stderr,
			})
			if exit != 2 {
				t.Fatalf("exit = %d, want 2; stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
			if gateway.loginCalls != 0 {
				t.Fatalf("invalid method called login %d time(s)", gateway.loginCalls)
			}
		})
	}
}

func TestLegacyTopLevelCredentialFlags(t *testing.T) {
	const (
		username = "synthetic-user"
		password = "synthetic-credential"
	)
	tests := []struct {
		name string
		args []string
	}{
		{name: "short separated", args: []string{"-u", username, "-p", password}},
		{name: "short attached", args: []string{"-u" + username, "-p" + password}},
		{name: "long separated", args: []string{"--username", username, "--password", password}},
		{name: "long equals", args: []string{"--username=" + username, "--password=" + password}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &recordingGateway{}
			exit, _, stderr := executeWithRecordingGateway(context.Background(), ModeLegacy, test.args, false, gateway)
			if exit != 0 {
				t.Fatalf("exit = %d; stderr=%q", exit, stderr)
			}
			if gateway.loginCalls != 1 {
				t.Fatalf("login calls = %d, want 1", gateway.loginCalls)
			}
			if gateway.lastLogin.ExpectedUsername != username {
				t.Fatalf("expected username = %q", gateway.lastLogin.ExpectedUsername)
			}
			if gateway.lastLogin.Credentials == nil {
				t.Fatal("direct credential provider is nil")
			}
			credential, err := gateway.lastLogin.Credentials.Credential(context.Background(), ipgw.CredentialRequest{Username: username})
			if err != nil {
				t.Fatalf("read synthetic credential: %v", err)
			}
			if credential.Password != password {
				t.Fatal("direct credential provider returned the wrong synthetic value")
			}
			if strings.Contains(stderr, password) {
				t.Fatal("legacy warning leaked the credential")
			}
			if !strings.Contains(stderr, "shell history") {
				t.Fatalf("missing shell-history warning: %q", stderr)
			}
		})
	}

	t.Run("password only defers username to selected profile", func(t *testing.T) {
		gateway := &recordingGateway{}
		exit, _, stderr := executeWithRecordingGateway(context.Background(), ModeLegacy,
			[]string{"--profile", "selected", "-p" + password}, false, gateway)
		if exit != 0 {
			t.Fatalf("exit = %d; stderr=%q", exit, stderr)
		}
		if gateway.lastLogin.ExpectedUsername != "" {
			t.Fatalf("expected username = %q, want app/profile resolution", gateway.lastLogin.ExpectedUsername)
		}
		if gateway.lastLogin.Profile != "selected" || gateway.lastLogin.Credentials == nil {
			t.Fatalf("login options = %#v", gateway.lastLogin)
		}
	})

	t.Run("username only is a hint not a profile name", func(t *testing.T) {
		gateway := &recordingGateway{}
		exit, _, stderr := executeWithRecordingGateway(context.Background(), ModeLegacy,
			[]string{"--username=" + username}, false, gateway)
		if exit != 0 {
			t.Fatalf("exit = %d; stderr=%q", exit, stderr)
		}
		if gateway.lastLogin.ExpectedUsername != username {
			t.Fatalf("expected username = %q", gateway.lastLogin.ExpectedUsername)
		}
		if gateway.lastLogin.Profile != "" {
			t.Fatalf("username was treated as profile name %q", gateway.lastLogin.Profile)
		}
		if gateway.lastLogin.Credentials != nil {
			t.Fatal("username-only login unexpectedly installed direct credentials")
		}
	})

	for _, args := range [][]string{{"--username="}, {"--password="}} {
		gateway := &recordingGateway{}
		exit, _, _ := executeWithRecordingGateway(context.Background(), ModeLegacy, args, false, gateway)
		if exit != 2 || gateway.loginCalls != 0 {
			t.Errorf("empty flag %#v: exit=%d login calls=%d", args, exit, gateway.loginCalls)
		}
	}
}

func TestLegacyStatusAndLogoutRejectExtraArguments(t *testing.T) {
	for _, args := range [][]string{{"logout", "extra"}, {"status", "extra"}, {"test", "extra"}} {
		gateway := &recordingGateway{}
		exit, _, _ := executeWithRecordingGateway(context.Background(), ModeLegacy, args, false, gateway)
		if exit != 2 {
			t.Errorf("args %#v: exit=%d, want 2", args, exit)
		}
		if gateway.totalCalls() != 0 {
			t.Errorf("args %#v called gateway %d time(s)", args, gateway.totalCalls())
		}
	}
}

func TestQRFailsBeforeGatewayWithoutSafeHumanTTY(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		isTTY bool
		json  bool
	}{
		{name: "headless human", args: []string{"login", "--method=qr"}, isTTY: false},
		{name: "json even with tty", args: []string{"login", "--method=qr", "--json"}, isTTY: true, json: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &recordingGateway{}
			exit, stdout, stderr := executeWithRecordingGateway(context.Background(), ModeMeta, test.args, test.isTTY, gateway)
			if exit != 7 {
				t.Fatalf("exit = %d, want 7; stdout=%q stderr=%q", exit, stdout, stderr)
			}
			if gateway.totalCalls() != 0 {
				t.Fatalf("QR preflight called gateway %d time(s)", gateway.totalCalls())
			}
			if strings.Contains(stdout, wireCanary) || strings.Contains(stderr, wireCanary) {
				t.Fatal("QR payload canary leaked")
			}
			if test.json {
				if stderr != "" {
					t.Fatalf("JSON stderr = %q, want empty", stderr)
				}
				object := decodeSingleEnvelope(t, []byte(stdout))
				wiredError := objectField(t, object, "error")
				if wiredError["code"] != string(ipgw.CodeInteractionRequired) {
					t.Fatalf("error = %#v", wiredError)
				}
				details := objectField(t, wiredError, "details")
				interaction := objectField(t, details, "interaction")
				if interaction["resume_mode"] != "retry_in_tty" || interaction["help_id"] != "AUTH-QR-001" {
					t.Fatalf("interaction = %#v", interaction)
				}
			} else {
				if !strings.Contains(stderr, "SSH with a TTY") || !strings.Contains(stderr, "no desktop GUI is required") {
					t.Fatalf("headless guidance is incomplete: %q", stderr)
				}
			}
		})
	}

	t.Run("human tty reaches gateway with presenter", func(t *testing.T) {
		gateway := &recordingGateway{}
		exit, _, stderr := executeWithRecordingGateway(context.Background(), ModeMeta,
			[]string{"login", "--method=qr"}, true, gateway)
		if exit != 0 {
			t.Fatalf("exit = %d; stderr=%q", exit, stderr)
		}
		if gateway.loginCalls != 1 || gateway.lastLogin.Interactions == nil {
			t.Fatalf("login calls=%d interactions=%#v", gateway.loginCalls, gateway.lastLogin.Interactions)
		}
	})
}

func TestNetworkScanStopsOnCancellationAndDeadline(t *testing.T) {
	interfaces := syntheticInterfaces()
	tests := []struct {
		name      string
		statusErr error
		wantExit  int
		wantCode  ipgw.ErrorCode
	}{
		{name: "canceled status", statusErr: context.Canceled, wantExit: 130, wantCode: ipgw.CodeNetwork},
		{name: "deadline status", statusErr: context.DeadlineExceeded, wantExit: 3, wantCode: ipgw.CodeNetwork},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &recordingGateway{
				interfaces: interfaces,
				statusFunc: func(context.Context, app.RequestOptions) (ipgw.Status, error) {
					return ipgw.Status{}, test.statusErr
				},
			}
			exit, stdout, stderr := executeWithRecordingGateway(context.Background(), ModeMeta,
				[]string{"network", "scan", "--json"}, false, gateway)
			if exit != test.wantExit {
				t.Fatalf("exit = %d, want %d", exit, test.wantExit)
			}
			if gateway.statusCalls != 1 {
				t.Fatalf("status calls = %d, want 1", gateway.statusCalls)
			}
			if stderr != "" {
				t.Fatalf("stderr = %q, want empty", stderr)
			}
			object := decodeSingleEnvelope(t, []byte(stdout))
			if _, hasData := object["data"]; hasData {
				t.Fatal("canceled scan returned partial data")
			}
			wiredError := objectField(t, object, "error")
			if wiredError["code"] != string(test.wantCode) {
				t.Fatalf("error = %#v", wiredError)
			}
		})
	}

	t.Run("already canceled does not enumerate", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		gateway := &recordingGateway{interfaces: interfaces}
		exit, _, _ := executeWithRecordingGateway(ctx, ModeMeta, []string{"network", "scan", "--json"}, false, gateway)
		if exit != 130 || gateway.listInterfaceCalls != 0 || gateway.statusCalls != 0 {
			t.Fatalf("exit=%d list=%d status=%d", exit, gateway.listInterfaceCalls, gateway.statusCalls)
		}
	})

	t.Run("context canceled by status overrides nested error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		gateway := &recordingGateway{interfaces: interfaces}
		gateway.statusFunc = func(context.Context, app.RequestOptions) (ipgw.Status, error) {
			cancel()
			return ipgw.Status{}, errors.New(wireCanary)
		}
		exit, stdout, _ := executeWithRecordingGateway(ctx, ModeMeta, []string{"network", "scan", "--json"}, false, gateway)
		if exit != 130 || gateway.statusCalls != 1 {
			t.Fatalf("exit=%d status calls=%d", exit, gateway.statusCalls)
		}
		if strings.Contains(stdout, wireCanary) {
			t.Fatal("canceled scan leaked nested error")
		}
	})
}

func TestNetworkScanNestedErrorsUseFixedMessages(t *testing.T) {
	gateway := &recordingGateway{
		interfaces: syntheticInterfaces(),
		statusFunc: func(context.Context, app.RequestOptions) (ipgw.Status, error) {
			return ipgw.Status{}, errors.New(wireCanary)
		},
	}
	exit, stdout, stderr := executeWithRecordingGateway(context.Background(), ModeMeta,
		[]string{"network", "scan", "--json"}, false, gateway)
	if exit != 0 {
		t.Fatalf("exit = %d; stderr=%q", exit, stderr)
	}
	if gateway.statusCalls != len(gateway.interfaces) {
		t.Fatalf("status calls = %d, want %d", gateway.statusCalls, len(gateway.interfaces))
	}
	if strings.Contains(stdout, wireCanary) {
		t.Fatal("nested scan error leaked")
	}
	object := decodeSingleEnvelope(t, []byte(stdout))
	data := objectField(t, object, "data")
	results, ok := data["results"].([]any)
	if !ok || len(results) != len(gateway.interfaces) {
		t.Fatalf("results = %#v", data["results"])
	}
	first, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("first result = %#v", results[0])
	}
	wiredError := objectField(t, first, "error")
	if wiredError["message"] != "internal error" {
		t.Fatalf("nested error = %#v", wiredError)
	}
}

func executeWithRecordingGateway(ctx context.Context, mode Mode, args []string, isTTY bool, gateway *recordingGateway) (int, string, string) {
	var stdout, stderr bytes.Buffer
	exit := Execute(ctx, Options{
		Mode:       mode,
		Args:       args,
		NewGateway: func(config.Paths) Gateway { return gateway },
		Out:        &stdout,
		Err:        &stderr,
		IsTTY:      isTTY,
	})
	return exit, stdout.String(), stderr.String()
}

func syntheticInterfaces() []ipgw.Interface {
	return []ipgw.Interface{
		{Name: "synthetic-a", Index: 1, IP: netip.MustParseAddr("192.0.2.10")},
		{Name: "synthetic-b", Index: 2, IP: netip.MustParseAddr("198.51.100.20")},
		{Name: "synthetic-c", Index: 3, IP: netip.MustParseAddr("203.0.113.30")},
	}
}

type recordingGateway struct {
	statusCalls        int
	loginCalls         int
	logoutCalls        int
	listInterfaceCalls int
	lastLogin          app.LoginOptions
	interfaces         []ipgw.Interface
	statusFunc         func(context.Context, app.RequestOptions) (ipgw.Status, error)
	listInterfacesErr  error
}

func (g *recordingGateway) Status(ctx context.Context, options app.RequestOptions) (ipgw.Status, error) {
	g.statusCalls++
	if g.statusFunc != nil {
		return g.statusFunc(ctx, options)
	}
	return testOfflineStatus(), nil
}

func (g *recordingGateway) Login(_ context.Context, options app.LoginOptions) (ipgw.LoginResult, error) {
	g.loginCalls++
	g.lastLogin = options
	return ipgw.LoginResult{Outcome: ipgw.LoginLoggedIn, Status: testOfflineStatus()}, nil
}

func (g *recordingGateway) Logout(context.Context, app.RequestOptions) (ipgw.LogoutResult, error) {
	g.logoutCalls++
	return ipgw.LogoutResult{Outcome: ipgw.LogoutAlreadyOffline, Status: testOfflineStatus()}, nil
}

func (g *recordingGateway) ListInterfaces(context.Context) ([]ipgw.Interface, error) {
	g.listInterfaceCalls++
	return append([]ipgw.Interface(nil), g.interfaces...), g.listInterfacesErr
}

func (g *recordingGateway) totalCalls() int {
	return g.statusCalls + g.loginCalls + g.logoutCalls + g.listInterfaceCalls
}

func testOfflineStatus() ipgw.Status {
	return ipgw.Status{
		Network: ipgw.NetworkReachable, Session: ipgw.SessionOffline,
		ObservedAt: time.Unix(0, 0).UTC(),
	}
}

type countingFailWriter struct{ writes int }

func (w *countingFailWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New(wireCanary)
}

func decodeSingleEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		t.Fatalf("JSON output must end in one newline: %q", raw)
	}
	if bytes.Count(raw, []byte{'\n'}) != 1 {
		t.Fatalf("JSON output contains multiple lines: %q", raw)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatalf("decode envelope: %v; raw=%q", err, raw)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("JSON output contains another value: err=%v value=%#v", err, trailing)
	}
	return object
}

func assertEnvelopeXOR(t *testing.T, object map[string]any, success bool) {
	t.Helper()
	_, hasData := object["data"]
	_, hasError := object["error"]
	if hasData == hasError {
		t.Fatalf("data/error XOR violated: data=%v error=%v object=%#v", hasData, hasError, object)
	}
	if success != hasData {
		t.Fatalf("success=%v but data=%v error=%v", success, hasData, hasError)
	}
}

func objectField(t *testing.T, object map[string]any, name string) map[string]any {
	t.Helper()
	value, ok := object[name].(map[string]any)
	if !ok {
		t.Fatalf("field %q = %#v, want object", name, object[name])
	}
	return value
}

func setOptionalStringField(target any, name, value string) {
	field := reflect.ValueOf(target).Elem().FieldByName(name)
	if field.IsValid() && field.CanSet() && field.Kind() == reflect.String {
		field.SetString(value)
	}
}
