package ipgw

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/UnbalancedCat/ipgw-meta/internal/cas"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func testResponse(request *http.Request, status int, body string, location string) *http.Response {
	header := make(http.Header)
	if location != "" {
		header.Set("Location", location)
	}
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d test", status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func assertOperationLifecycle(t *testing.T, events []Event, operation, outcome string, forbidden ...string) {
	t.Helper()
	startIndex := -1
	finishIndex := -1
	var start, finish Event
	for index, event := range events {
		if event.Name != EventOperationStarted && event.Name != EventOperationFinished {
			continue
		}
		if event.Operation != operation {
			t.Fatalf("unexpected lifecycle event for %q: %#v", event.Operation, event)
		}
		switch event.Name {
		case EventOperationStarted:
			if startIndex >= 0 {
				t.Fatalf("duplicate operation_started events: %#v", events)
			}
			startIndex, start = index, event
		case EventOperationFinished:
			if finishIndex >= 0 {
				t.Fatalf("duplicate operation_finished events: %#v", events)
			}
			finishIndex, finish = index, event
		}
	}
	if startIndex < 0 || finishIndex < 0 || startIndex >= finishIndex {
		t.Fatalf("invalid lifecycle event pair: %#v", events)
	}
	if start.Outcome != "" || finish.Outcome != outcome {
		t.Fatalf("lifecycle outcomes = start %q, finish %q; want empty/%q", start.Outcome, finish.Outcome, outcome)
	}
	stableOutcomes := map[string]bool{
		"ok": true, string(CodeInvalidArgument): true, string(CodeConfig): true,
		string(CodeNetwork): true, string(CodeAuthentication): true,
		string(CodeSessionConflict): true, string(CodeProtocolChanged): true,
		string(CodeInteractionRequired): true, string(CodeUnsupported): true,
		string(CodeInternal): true,
	}
	if !stableOutcomes[finish.Outcome] {
		t.Fatalf("operation_finished used unstable outcome %q", finish.Outcome)
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range forbidden {
		if value != "" && strings.Contains(string(encoded), value) {
			t.Fatalf("observer events leaked %q: %s", value, encoded)
		}
	}
}

func TestNewClientRejectsDisabledTLSVerification(t *testing.T) {
	_, err := NewClient(WithRoundTripper(&http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}))
	if !IsCode(err, CodeInvalidArgument) {
		t.Fatalf("NewClient() error = %v, code = %q", err, CodeOf(err))
	}
}

func TestNewClientRejectsTypedNilExtensions(t *testing.T) {
	var roundTripper *http.Transport
	var observer ObserverFunc
	var store *memoryProtocolStore
	for name, option := range map[string]Option{
		"round tripper": WithRoundTripper(roundTripper),
		"observer":      WithObserver(observer),
		"state store":   WithProtocolStateStore(store),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewClient(option); !IsCode(err, CodeInvalidArgument) {
				t.Fatalf("NewClient() error = %v, code=%q", err, CodeOf(err))
			}
		})
	}
}

func TestStandardTransportIsClonedBeforeUse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`not_online_error`))
	}))
	defer server.Close()
	original := &http.Transport{TLSClientConfig: &tls.Config{}}
	client, err := NewClient(WithRoundTripper(original))
	if err != nil {
		t.Fatal(err)
	}
	original.TLSClientConfig.InsecureSkipVerify = true
	client.endpoints.status, _ = url.Parse(server.URL)
	if _, err := client.Status(context.Background()); !IsCode(err, CodeNetwork) {
		t.Fatalf("mutating caller transport weakened TLS: %v, code=%q", err, CodeOf(err))
	}
}

func TestSecretBearingTypesRejectGenericSerializationAndFormatting(t *testing.T) {
	for name, value := range map[string]any{
		"credential": Credential{Password: "PASSWORD-CANARY"},
		"QR prompt":  QRCodePrompt{Payload: "QR-PAYLOAD-CANARY"},
	} {
		t.Run(name, func(t *testing.T) {
			if encoded, err := json.Marshal(value); err == nil {
				t.Fatalf("json.Marshal unexpectedly succeeded: %s", encoded)
			}
			formatted := fmt.Sprintf("%v %#v", value, value)
			if strings.Contains(formatted, "CANARY") {
				t.Fatalf("secret leaked through formatting: %s", formatted)
			}
		})
	}
}

func TestDefaultTransportUsesSystemTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`not_online_error`))
	}))
	defer server.Close()
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	client.endpoints.status, err = url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Status(context.Background()); !IsCode(err, CodeNetwork) {
		t.Fatalf("self-signed TLS endpoint error = %v, code=%q", err, CodeOf(err))
	}
}

func TestPasswordLoginCapturesHTTPServiceTicketWithoutSendingIt(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := base64.StdEncoding.EncodeToString(der)

	statusCalls := 0
	credentialCalls := 0
	var requests []string
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.String())
		switch {
		case request.URL.Host == "ipgw.neu.edu.cn" && request.URL.Path == "/cgi-bin/rad_user_info":
			statusCalls++
			if statusCalls == 1 {
				return testResponse(request, http.StatusOK, `not_online_error`, ""), nil
			}
			return testResponse(request, http.StatusOK, `{"error":"ok","user_name":"alice","online_ip":"10.0.0.8"}`, ""), nil
		case request.URL.Scheme == "http" && request.URL.Host == "ipgw.neu.edu.cn" && request.URL.Path == "/":
			return testResponse(request, http.StatusFound, "", "http://ipgw.neu.edu.cn/srun_portal_pc?ac_id=15"), nil
		case request.URL.Host == "pass.neu.edu.cn" && request.URL.Path == "/tpass/login" && request.Method == http.MethodGet:
			html := `<form id="loginForm" action="/tpass/auth"><input name="lt" value="LT-1"><input name="execution" value="e1s1"></form><script>var publicKeyStr='` + publicKey + `';</script>`
			return testResponse(request, http.StatusOK, html, ""), nil
		case request.URL.Host == "pass.neu.edu.cn" && request.URL.Path == "/tpass/auth" && request.Method == http.MethodPost:
			return testResponse(request, http.StatusFound, "", "http://ipgw.neu.edu.cn/srun_portal_sso?ac_id=15&ticket=SECRET-TICKET"), nil
		case request.URL.Scheme == "https" && request.URL.Host == "ipgw.neu.edu.cn" && request.URL.Path == "/v1/srun_portal_sso":
			if request.URL.Query().Get("ticket") != "SECRET-TICKET" || request.URL.Query().Get("ac_id") != "15" {
				return nil, fmt.Errorf("activation missing captured values")
			}
			return testResponse(request, http.StatusOK, `{"code":0,"message":"success"}`, ""), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", request.URL.Redacted())
		}
	})
	client, err := NewClient(WithRoundTripper(roundTripper))
	if err != nil {
		t.Fatal(err)
	}
	client.verifyDelay = 0
	result, err := client.Login(context.Background(), LoginRequest{
		Method:           AuthMethodPassword,
		ExpectedUsername: "alice",
		Credentials: CredentialProviderFunc(func(context.Context, CredentialRequest) (Credential, error) {
			credentialCalls++
			return Credential{Password: "password"}, nil
		}),
	})
	if err != nil {
		t.Fatalf("Login() error = %v (%s)", err, CodeOf(err))
	}
	if result.Outcome != LoginLoggedIn || result.Status.Username != "alice" || credentialCalls != 1 {
		t.Fatalf("unexpected result: %#v, credential calls: %d", result, credentialCalls)
	}
	for _, rawURL := range requests {
		if strings.HasPrefix(rawURL, "http://") && strings.Contains(rawURL, "ticket=") {
			t.Fatalf("ticket-bearing HTTP redirect was sent: %s", rawURL)
		}
	}
}

func TestEncryptCredentialAcceptsPKIXAndPKCS1RSAKeys(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkixDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, der := range map[string][]byte{
		"PKIX":   pkixDER,
		"PKCS#1": x509.MarshalPKCS1PublicKey(&privateKey.PublicKey),
	} {
		t.Run(name, func(t *testing.T) {
			encodedKey := base64.StdEncoding.EncodeToString(der)
			ciphertext, err := encryptCredential("alice", "password", encodedKey)
			if err != nil {
				t.Fatal(err)
			}
			cipherBytes, err := base64.StdEncoding.DecodeString(ciphertext)
			if err != nil {
				t.Fatal(err)
			}
			plaintext, err := rsa.DecryptPKCS1v15(rand.Reader, privateKey, cipherBytes)
			if err != nil {
				t.Fatal(err)
			}
			if string(plaintext) != "alicepassword" {
				t.Fatalf("decrypted credential = %q", plaintext)
			}
		})
	}
}

func TestEncryptCredentialRejectsWeakRSAForBothEncodings(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pkixDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, der := range map[string][]byte{
		"PKIX":   pkixDER,
		"PKCS#1": x509.MarshalPKCS1PublicKey(&privateKey.PublicKey),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := encryptCredential("alice", "password", base64.StdEncoding.EncodeToString(der)); err == nil {
				t.Fatal("weak RSA key was accepted")
			}
		})
	}
}

func TestEncryptCredentialRejectsOversizedEncodedKeyWithoutEcho(t *testing.T) {
	const canary = "ENCODED-KEY-LIMIT-CANARY"
	encodedKey := " \n" + strings.Repeat("A", casEncodedKeyLimit+1) + canary + "\t"
	_, err := encryptCredential("alice", "password", encodedKey)
	if err == nil {
		t.Fatal("oversized encoded public key was accepted")
	}
	assertErrorDoesNotContain(t, err, canary)
	assertErrorDoesNotContain(t, err, strings.TrimSpace(encodedKey))
}

func TestEncryptCredentialRejectsOversizedDERWithoutEcho(t *testing.T) {
	const canary = "DER-KEY-LIMIT-CANARY"
	der := []byte(canary + strings.Repeat("D", casDERKeyLimit+1-len(canary)))
	encodedKey := base64.StdEncoding.EncodeToString(der)
	if len(encodedKey) > casEncodedKeyLimit {
		t.Fatalf("test DER encoded length = %d, exceeds encoded limit", len(encodedKey))
	}
	_, err := encryptCredential("alice", "password", encodedKey)
	if err == nil {
		t.Fatal("oversized DER public key was accepted")
	}
	assertErrorDoesNotContain(t, err, canary)
	assertErrorDoesNotContain(t, err, encodedKey)
}

func TestEncryptCredentialRejectsRSAAboveMaximumWithoutEcho(t *testing.T) {
	encodedKey := syntheticEncodedRSAKey(t, casRSAMaxBits+1)
	_, err := encryptCredential("USERNAME-CANARY", "PASSWORD-CANARY", encodedKey)
	if err == nil {
		t.Fatal("RSA modulus above the maximum was accepted")
	}
	assertErrorDoesNotContain(t, err, "USERNAME-CANARY")
	assertErrorDoesNotContain(t, err, "PASSWORD-CANARY")
	assertErrorDoesNotContain(t, err, encodedKey)
}

func TestEncryptCredentialAcceptsMaximumRSAModulus(t *testing.T) {
	encodedKey := syntheticEncodedRSAKey(t, casRSAMaxBits)
	ciphertext, err := encryptCredential("alice", "password", encodedKey)
	if err != nil {
		t.Fatalf("encryptCredential with %d-bit RSA modulus: %v", casRSAMaxBits, err)
	}
	ciphertextDER, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	if got, want := len(ciphertextDER), casRSAMaxBits/8; got != want {
		t.Fatalf("ciphertext length = %d, want %d", got, want)
	}
}

func syntheticEncodedRSAKey(t *testing.T, bits int) string {
	t.Helper()
	modulus := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	modulus.SetBit(modulus, 0, 1)
	der, err := x509.MarshalPKIXPublicKey(&rsa.PublicKey{N: modulus, E: 65537})
	if err != nil {
		t.Fatalf("marshal synthetic %d-bit RSA public key: %v", bits, err)
	}
	if len(der) > casDERKeyLimit {
		t.Fatalf("synthetic RSA DER length = %d, exceeds DER limit", len(der))
	}
	encodedKey := base64.StdEncoding.EncodeToString(der)
	if len(encodedKey) > casEncodedKeyLimit {
		t.Fatalf("synthetic RSA encoded length = %d, exceeds encoded limit", len(encodedKey))
	}
	return encodedKey
}

func TestJavaScriptUTF16Length(t *testing.T) {
	for input, expected := range map[string]int{
		"":      0,
		"alice": 5,
		"a😀":    3,
		"密😀":    3,
		"😀😀":    4,
	} {
		if actual := javascriptUTF16Length(input); actual != expected {
			t.Fatalf("javascriptUTF16Length(%q) = %d, want %d", input, actual, expected)
		}
	}
}

func TestLoginDoesNotReadCredentialBeforeSessionDecision(t *testing.T) {
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return testResponse(request, http.StatusOK, `{"error":"ok","user_name":"alice","online_ip":"10.0.0.8"}`, ""), nil
	})
	client, _ := NewClient(WithRoundTripper(roundTripper))
	calls := 0
	provider := CredentialProviderFunc(func(context.Context, CredentialRequest) (Credential, error) {
		calls++
		return Credential{Password: "must-not-be-read"}, nil
	})
	result, err := client.Login(context.Background(), LoginRequest{ExpectedUsername: "alice", Credentials: provider})
	if err != nil || result.Outcome != LoginAlreadyOnline || calls != 0 {
		t.Fatalf("same-account preflight = %#v, %v, calls=%d", result, err, calls)
	}
	_, err = client.Login(context.Background(), LoginRequest{ExpectedUsername: "bob", Credentials: provider})
	if !IsCode(err, CodeSessionConflict) || calls != 0 {
		t.Fatalf("conflict error = %v, code=%q, calls=%d", err, CodeOf(err), calls)
	}
}

func TestLoginStopsWhenExplicitSwitchLogoutIsNotConfirmed(t *testing.T) {
	credentialCalls := 0
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/cgi-bin/rad_user_info" {
			return testResponse(request, http.StatusOK, `{"error":"ok","user_name":"other","online_ip":"10.0.0.9"}`, ""), nil
		}
		if request.URL.Path == "/cgi-bin/srun_portal" {
			return testResponse(request, http.StatusOK, `<html>unknown logout response</html>`, ""), nil
		}
		return nil, fmt.Errorf("authentication continued after logout failure")
	})
	client, _ := NewClient(WithRoundTripper(roundTripper))
	_, err := client.Login(context.Background(), LoginRequest{
		ExpectedUsername: "alice",
		Switch:           SwitchLogoutExisting,
		Credentials: CredentialProviderFunc(func(context.Context, CredentialRequest) (Credential, error) {
			credentialCalls++
			return Credential{Password: "password"}, nil
		}),
	})
	if !IsCode(err, CodeProtocolChanged) || credentialCalls != 0 {
		t.Fatalf("Login() error = %v, code=%q, credential calls=%d", err, CodeOf(err), credentialCalls)
	}
}

func TestLoginObserverLifecycleErrors(t *testing.T) {
	const (
		expectedUsername = "EXPECTED-USERNAME-CANARY"
		actualUsername   = "ACTUAL-USERNAME-CANARY"
		transportSecret  = "TRANSPORT-SECRET-CANARY"
	)
	for _, testCase := range []struct {
		name           string
		request        LoginRequest
		statusBody     string
		transportErr   error
		wantCode       ErrorCode
		wantRoundTrips int
		forbidden      []string
	}{
		{
			name: "invalid argument", request: LoginRequest{}, wantCode: CodeInvalidArgument,
			wantRoundTrips: 0,
		},
		{
			name: "network", request: LoginRequest{ExpectedUsername: expectedUsername},
			transportErr: errors.New("transport failed: " + transportSecret), wantCode: CodeNetwork,
			wantRoundTrips: 1, forbidden: []string{expectedUsername, transportSecret},
		},
		{
			name: "session conflict", request: LoginRequest{ExpectedUsername: expectedUsername},
			statusBody: `{"error":"ok","user_name":"` + actualUsername + `","online_ip":"10.0.0.9"}`,
			wantCode:   CodeSessionConflict, wantRoundTrips: 1,
			forbidden: []string{expectedUsername, actualUsername},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var observed []Event
			roundTrips := 0
			client, err := NewClient(
				WithRoundTripper(roundTripFunc(func(request *http.Request) (*http.Response, error) {
					roundTrips++
					if testCase.transportErr != nil {
						return nil, testCase.transportErr
					}
					return testResponse(request, http.StatusOK, testCase.statusBody, ""), nil
				})),
				WithObserver(ObserverFunc(func(_ context.Context, event Event) { observed = append(observed, event) })),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Login(context.Background(), testCase.request)
			if !IsCode(err, testCase.wantCode) || roundTrips != testCase.wantRoundTrips {
				t.Fatalf("Login() error = %v, code=%q, round trips=%d", err, CodeOf(err), roundTrips)
			}
			assertOperationLifecycle(t, observed, "login", string(testCase.wantCode), testCase.forbidden...)
		})
	}
}

func TestOTPAndHeadlessQRReturnRedactedInteractionErrors(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		page      string
		method    AuthMethod
		challenge ChallengeKind
		helpID    string
	}{
		{name: "otp", page: `<html>需要手机验证码</html>`, method: AuthMethodPassword, challenge: ChallengeSMSOTP, helpID: "AUTH-SMS-001"},
		{name: "headless qr", page: `<script>var uuid='ephemeral-qr-id';</script>`, method: AuthMethodTerminalQR, challenge: ChallengeQRApproval, helpID: "AUTH-QR-001"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var observed []Event
			roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/cgi-bin/rad_user_info":
					return testResponse(request, http.StatusOK, `not_online_error`, ""), nil
				case "/":
					return testResponse(request, http.StatusFound, "", "http://ipgw.neu.edu.cn/?ac_id=1"), nil
				case "/tpass/login":
					return testResponse(request, http.StatusOK, testCase.page, ""), nil
				default:
					return nil, fmt.Errorf("unexpected request: %s", request.URL.Redacted())
				}
			})
			client, _ := NewClient(
				WithRoundTripper(roundTripper),
				WithObserver(ObserverFunc(func(_ context.Context, event Event) { observed = append(observed, event) })),
			)
			_, err := client.Login(context.Background(), LoginRequest{Method: testCase.method, ExpectedUsername: "alice"})
			if !IsCode(err, CodeInteractionRequired) {
				t.Fatalf("Login() error = %v, code=%q", err, CodeOf(err))
			}
			var sdkErr *Error
			if !errors.As(err, &sdkErr) || sdkErr.Details.Interaction == nil || sdkErr.Details.Interaction.Challenge != testCase.challenge || sdkErr.Details.Interaction.HelpID != testCase.helpID {
				t.Fatalf("unexpected interaction details: %#v", sdkErr)
			}
			encoded, marshalErr := json.Marshal(sdkErr)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if strings.Contains(string(encoded), "ephemeral-qr-id") {
				t.Fatalf("QR payload leaked into error JSON: %s", encoded)
			}
			assertOperationLifecycle(t, observed, "login", string(CodeInteractionRequired), "alice", "ephemeral-qr-id")
		})
	}
}

func TestInteractionErrorJSONRestrictsMetadataToFixedValues(t *testing.T) {
	valid := interactionError(AuthMethodTerminalQR, InteractionDetails{
		Challenge:      ChallengeQRApproval,
		Capability:     []CapabilityStatus{CapabilityLiveUnverified},
		SessionBinding: interactionSessionCASSession,
		ResumeMode:     interactionResumeRetryInTTY,
		TTYRequired:    true,
		HelpID:         interactionHelpQR,
	}, "interaction is required")
	encoded, err := json.Marshal(*valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`"session_binding":"cas_session"`,
		`"resume_mode":"retry_in_tty"`,
		`"help_id":"AUTH-QR-001"`,
	} {
		if !strings.Contains(string(encoded), expected) {
			t.Fatalf("fixed interaction metadata %s is missing: %s", expected, encoded)
		}
	}

	const canary = "INTERACTION-FREE-TEXT-CANARY"
	valid.Details.Interaction.Challenge = ChallengeKind(canary)
	valid.Details.Interaction.Capability = []CapabilityStatus{CapabilityStatus(canary), CapabilityLiveUnverified}
	valid.Details.Interaction.SessionBinding = canary
	valid.Details.Interaction.ResumeMode = canary
	valid.Details.Interaction.HelpID = canary
	encoded, err = json.Marshal(*valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{canary, "user_action", "delivery_hint", "session_binding", "resume_mode", "help_id"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("interaction JSON leaked forbidden value %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(string(encoded), `"challenge_kind":"unknown"`) || !strings.Contains(string(encoded), `"capability_status":["live_unverified"]`) {
		t.Fatalf("unknown interaction enums were not normalized: %s", encoded)
	}

	valid.Details.Interaction.OriginMethod = AuthMethod(canary)
	encoded, err = json.Marshal(*valid)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), canary) || !strings.Contains(string(encoded), `"interaction":null`) {
		t.Fatalf("unknown origin method was not removed: %s", encoded)
	}
}

func TestInteractionHelpIDsUseDeclaredDocumentationIDs(t *testing.T) {
	client, _ := NewClient()
	for _, testCase := range []struct {
		name      string
		challenge cas.Challenge
		helpID    string
	}{
		{name: "sms", challenge: cas.ChallengeSMSOTP, helpID: "AUTH-SMS-001"},
		{name: "device", challenge: cas.ChallengeDevice, helpID: "AUTH-CHALLENGE-001"},
		{name: "setup", challenge: cas.ChallengeSetup, helpID: "AUTH-CHALLENGE-001"},
		{name: "unknown", challenge: cas.ChallengeUnknown, helpID: "AUTH-CHALLENGE-001"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var sdkErr *Error
			if err := client.challengeError(context.Background(), AuthMethodPassword, testCase.challenge); !errors.As(err, &sdkErr) || sdkErr.Details.Interaction == nil || sdkErr.Details.Interaction.HelpID != testCase.helpID {
				t.Fatalf("challenge error = %#v, want help ID %q", sdkErr, testCase.helpID)
			}
		})
	}
	var qrErr *Error
	if err := terminalQRExpiredError(AuthMethodTerminalQR); !errors.As(err, &qrErr) || qrErr.Details.Interaction == nil || qrErr.Details.Interaction.HelpID != "AUTH-QR-001" {
		t.Fatalf("expired QR error = %#v", qrErr)
	}
}

func TestTerminalQRGeneratedUUIDPendingConfirmedAndActivated(t *testing.T) {
	statusCalls := 0
	pollCalls := 0
	var prompt QRCodePrompt
	var observed []Event
	var pollUUID string
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch {
		case request.URL.Path == "/cgi-bin/rad_user_info":
			statusCalls++
			if statusCalls == 1 {
				return testResponse(request, http.StatusOK, `not_online_error`, ""), nil
			}
			return testResponse(request, http.StatusOK, `{"error":"ok","user_name":"alice","online_ip":"10.0.0.8"}`, ""), nil
		case request.URL.Scheme == "http" && request.URL.Path == "/":
			return testResponse(request, http.StatusFound, "", "http://ipgw.neu.edu.cn/?ac_id=1"), nil
		case request.URL.Host == "pass.neu.edu.cn" && request.URL.Path == "/tpass/login":
			return testResponse(request, http.StatusOK, `<script src="/tpass/comm/js/login-qrcode.js"></script>`, ""), nil
		case request.URL.Host == "pass.neu.edu.cn" && request.URL.Path == "/tpass/checkQRCodeScan":
			pollCalls++
			if request.URL.Query().Get("service") != "" || request.URL.Query().Get("random") == "" {
				return nil, fmt.Errorf("poll query must contain only UUID and cache buster")
			}
			if pollUUID == "" {
				pollUUID = request.URL.Query().Get("uuid")
			}
			if request.URL.Query().Get("uuid") != pollUUID || pollUUID == "" {
				return nil, fmt.Errorf("poll UUID is missing or changed")
			}
			if pollCalls == 1 {
				return testResponse(request, http.StatusOK, ``, ""), nil
			}
			return testResponse(request, http.StatusOK, `{"redirect_url":"http://ipgw.neu.edu.cn/srun_portal_sso?ac_id=1&ticket=QR-TICKET"}`, ""), nil
		case request.URL.Scheme == "https" && request.URL.Path == "/v1/srun_portal_sso":
			if request.URL.Query().Get("ticket") != "QR-TICKET" {
				return nil, fmt.Errorf("activation did not receive QR ticket")
			}
			return testResponse(request, http.StatusOK, `{"code":0,"message":"success"}`, ""), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", request.URL.Redacted())
		}
	})
	client, err := NewClient(
		WithRoundTripper(roundTripper),
		WithObserver(ObserverFunc(func(_ context.Context, event Event) { observed = append(observed, event) })),
	)
	if err != nil {
		t.Fatal(err)
	}
	client.pollInterval = 0
	client.verifyDelay = 0
	result, err := client.Login(context.Background(), LoginRequest{
		Method: AuthMethodTerminalQR, ExpectedUsername: "alice",
		Interactions: InteractionHandlerFunc(func(_ context.Context, value QRCodePrompt) error {
			prompt = value
			return nil
		}),
	})
	if err != nil {
		t.Fatalf("Login() error = %v, code=%q", err, CodeOf(err))
	}
	if result.Outcome != LoginLoggedIn || result.Status.Username != "alice" || pollCalls != 2 {
		t.Fatalf("unexpected QR result: %#v, polls=%d", result, pollCalls)
	}
	if prompt.Payload == "" || prompt.PollInterval != 0 || !strings.Contains(prompt.Payload, "qyQrLogin") {
		t.Fatalf("unexpected QR prompt: %#v", prompt)
	}
	parsedPrompt, parseErr := http.NewRequest(http.MethodGet, prompt.Payload, nil)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	promptUUID := parsedPrompt.URL.Query().Get("uuid")
	if promptUUID != pollUUID || !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(promptUUID) {
		t.Fatalf("prompt/poll UUID mismatch: %q, %q", promptUUID, pollUUID)
	}
	resultJSON, _ := json.Marshal(result)
	eventsJSON, _ := json.Marshal(observed)
	if strings.Contains(string(resultJSON), promptUUID) || strings.Contains(string(eventsJSON), promptUUID) {
		t.Fatalf("ephemeral QR UUID leaked: result=%s events=%s", resultJSON, eventsJSON)
	}
	assertOperationLifecycle(t, observed, "login", "ok", "alice", "QR-TICKET", promptUUID)
}

func TestLogoutObserverLifecycle(t *testing.T) {
	const username = "LOGOUT-USERNAME-CANARY"
	statusCalls := 0
	var observed []Event
	client, err := NewClient(
		WithRoundTripper(roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/cgi-bin/rad_user_info":
				statusCalls++
				if statusCalls == 1 {
					return testResponse(request, http.StatusOK, `{"error":"ok","user_name":"`+username+`","online_ip":"10.0.0.9"}`, ""), nil
				}
				return testResponse(request, http.StatusOK, `not_online_error`, ""), nil
			case "/cgi-bin/srun_portal":
				return testResponse(request, http.StatusOK, `callback({"ecode":0})`, ""), nil
			default:
				return nil, fmt.Errorf("unexpected request: %s", request.URL.Redacted())
			}
		})),
		WithObserver(ObserverFunc(func(_ context.Context, event Event) { observed = append(observed, event) })),
	)
	if err != nil {
		t.Fatal(err)
	}
	client.verifyDelay = 0
	result, err := client.Logout(context.Background())
	if err != nil || result.Outcome != LogoutLoggedOut || result.Status.Session != SessionOffline || statusCalls != 2 {
		t.Fatalf("Logout() = %#v, %v; status calls=%d", result, err, statusCalls)
	}
	assertOperationLifecycle(t, observed, "logout", "ok", username)
}

func TestTypedNilCredentialAndInteractionAreRejectedWithoutPanic(t *testing.T) {
	client, err := NewClient(WithRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network must not be reached")
	})))
	if err != nil {
		t.Fatal(err)
	}
	var provider CredentialProviderFunc
	if _, err := client.passwordLogin(context.Background(), nil, LoginRequest{Credentials: provider}, nil, nil, "", cas.Page{}, nil); !IsCode(err, CodeConfig) {
		t.Fatalf("typed-nil credential error = %v, code=%q", err, CodeOf(err))
	}
	var interaction InteractionHandlerFunc
	if _, err := client.qrLogin(context.Background(), nil, LoginRequest{Method: AuthMethodTerminalQR, Interactions: interaction}, nil, "", cas.Page{}); !IsCode(err, CodeInteractionRequired) {
		t.Fatalf("typed-nil interaction error = %v, code=%q", err, CodeOf(err))
	}
}

func TestCanceledMutationWaitReturnsWithoutNetwork(t *testing.T) {
	client, _ := NewClient(WithRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network must not be reached")
	})))
	client.mutating.Lock()
	defer client.mutating.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.Logout(ctx)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || CodeOf(err) != CodeNetwork {
			t.Fatalf("Logout() error = %v, code=%q", err, CodeOf(err))
		}
	case <-time.After(time.Second):
		t.Fatal("canceled Logout remained blocked on mutation lock")
	}
}

func TestQRFinalCASPageDetectsOTPWithoutActivation(t *testing.T) {
	statusCalls := 0
	activationCalls := 0
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/cgi-bin/rad_user_info":
			statusCalls++
			return testResponse(request, http.StatusOK, `not_online_error`, ""), nil
		case "/":
			return testResponse(request, http.StatusFound, "", "http://ipgw.neu.edu.cn/?ac_id=1"), nil
		case "/tpass/login":
			return testResponse(request, http.StatusOK, `<script src="/tpass/comm/js/login-qrcode.js"></script>`, ""), nil
		case "/tpass/checkQRCodeScan":
			return testResponse(request, http.StatusOK, `{"redirect_url":"https://pass.neu.edu.cn/tpass/qr-complete"}`, ""), nil
		case "/tpass/qr-complete":
			return testResponse(request, http.StatusOK, `<html>需要手机验证码</html>`, ""), nil
		case "/v1/srun_portal_sso":
			activationCalls++
			return testResponse(request, http.StatusOK, `{"code":0,"message":"success"}`, ""), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", request.URL.Redacted())
		}
	})
	client, _ := NewClient(WithRoundTripper(roundTripper))
	client.pollInterval = 0
	_, err := client.Login(context.Background(), LoginRequest{
		Method: AuthMethodTerminalQR, ExpectedUsername: "alice",
		Interactions: InteractionHandlerFunc(func(context.Context, QRCodePrompt) error { return nil }),
	})
	if !IsCode(err, CodeInteractionRequired) || activationCalls != 0 || statusCalls != 1 {
		t.Fatalf("final OTP result = %v, code=%q, activation=%d status=%d", err, CodeOf(err), activationCalls, statusCalls)
	}
	var sdkErr *Error
	if !errors.As(err, &sdkErr) || sdkErr.Details.Interaction == nil || sdkErr.Details.Interaction.Challenge != ChallengeSMSOTP {
		t.Fatalf("unexpected OTP details: %#v", sdkErr)
	}
}

func TestQRPresenterCannotExtendPromptDeadline(t *testing.T) {
	current := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	pollCalls := 0
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/cgi-bin/rad_user_info":
			return testResponse(request, http.StatusOK, `not_online_error`, ""), nil
		case "/":
			return testResponse(request, http.StatusFound, "", "http://ipgw.neu.edu.cn/?ac_id=1"), nil
		case "/tpass/login":
			return testResponse(request, http.StatusOK, `<script src="/tpass/comm/js/login-qrcode.js"></script>`, ""), nil
		case "/tpass/checkQRCodeScan":
			pollCalls++
			return testResponse(request, http.StatusOK, ``, ""), nil
		default:
			return nil, fmt.Errorf("unexpected request: %s", request.URL.Redacted())
		}
	})
	client, _ := NewClient(WithRoundTripper(roundTripper))
	client.now = func() time.Time { return current }
	_, err := client.Login(context.Background(), LoginRequest{
		Method: AuthMethodTerminalQR, ExpectedUsername: "alice",
		Interactions: InteractionHandlerFunc(func(_ context.Context, prompt QRCodePrompt) error {
			current = prompt.ExpiresAt.Add(time.Second)
			return nil
		}),
	})
	if !IsCode(err, CodeInteractionRequired) || pollCalls != 0 {
		t.Fatalf("expired QR result = %v, code=%q, polls=%d", err, CodeOf(err), pollCalls)
	}
}

func TestProtocolCacheIsDisabledWithoutVerifiedNetworkFingerprint(t *testing.T) {
	bindIP := netip.MustParseAddr("10.0.0.8")
	for _, networkKey := range []string{"bind:" + bindIP.String(), "bind:10.0.0.99"} {
		t.Run(networkKey, func(t *testing.T) {
			store := &memoryProtocolStore{state: ProtocolState{NetworkKey: networkKey, ACID: "15", VerifiedAt: time.Now().UTC()}}
			client, _ := NewClient(
				WithRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("offline") })),
				WithProtocolStateStore(store), WithBindIP(bindIP),
			)
			if _, err := client.discoverACID(context.Background()); !IsCode(err, CodeNetwork) {
				t.Fatalf("disabled cache error = %v, code=%q", err, CodeOf(err))
			}
			if store.loads != 0 || store.saves != 0 {
				t.Fatalf("unverified network cache was used: loads=%d saves=%d", store.loads, store.saves)
			}
		})
	}
}

func TestDiscoverACIDPreservesRequestCancellation(t *testing.T) {
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		client, err := NewClient(WithRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, sentinel
		})))
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.discoverACID(context.Background())
		if !errors.Is(err, sentinel) || CodeOf(err) != CodeNetwork {
			t.Fatalf("discoverACID(%v) error = %v, code=%q", sentinel, err, CodeOf(err))
		}
	}
}

func TestPasswordLoginCapsDynamicScriptCandidates(t *testing.T) {
	credentialCalls := 0
	scripts := strings.Repeat(`<script src="/tpass/login-bundle.js"></script>`, maxDynamicScriptCandidates+1)
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Path {
		case "/cgi-bin/rad_user_info":
			return testResponse(request, http.StatusOK, `not_online_error`, ""), nil
		case "/":
			return testResponse(request, http.StatusFound, "", "http://ipgw.neu.edu.cn/?ac_id=1"), nil
		case "/tpass/login":
			page := `<form id="loginForm" action="/tpass/auth"><input name="lt" value="LT"><input name="execution" value="e1s1"></form>` + scripts
			return testResponse(request, http.StatusOK, page, ""), nil
		default:
			return nil, fmt.Errorf("unexpected request after script cap: %s", request.URL.Redacted())
		}
	})
	client, _ := NewClient(WithRoundTripper(roundTripper))
	_, err := client.Login(context.Background(), LoginRequest{
		ExpectedUsername: "alice",
		Credentials: CredentialProviderFunc(func(context.Context, CredentialRequest) (Credential, error) {
			credentialCalls++
			return Credential{Password: "password"}, nil
		}),
	})
	if !IsCode(err, CodeProtocolChanged) || credentialCalls != 0 {
		t.Fatalf("script cap error = %v, code=%q, credential calls=%d", err, CodeOf(err), credentialCalls)
	}
}

func TestStatusRejectsOversizedAndRedirectedResponses(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		response func(*http.Request) *http.Response
		code     ErrorCode
	}{
		{name: "oversized", response: func(request *http.Request) *http.Response {
			return testResponse(request, http.StatusOK, strings.Repeat("x", int(statusResponseLimit)+1), "")
		}, code: CodeProtocolChanged},
		{name: "HTTPS to HTTP downgrade", response: func(request *http.Request) *http.Response {
			return testResponse(request, http.StatusFound, "", "http://attacker.invalid/")
		}, code: CodeProtocolChanged},
		{name: "HTTPS cross origin", response: func(request *http.Request) *http.Response {
			return testResponse(request, http.StatusFound, "", "https://attacker.invalid/")
		}, code: CodeProtocolChanged},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			roundTrips := 0
			client, _ := NewClient(WithRoundTripper(roundTripFunc(func(request *http.Request) (*http.Response, error) {
				roundTrips++
				return testCase.response(request), nil
			})))
			_, err := client.Status(context.Background())
			if !IsCode(err, testCase.code) {
				t.Fatalf("Status() error = %v, code=%q", err, CodeOf(err))
			}
			if roundTrips != 1 {
				t.Fatalf("redirect made %d RoundTrip calls; want exactly one", roundTrips)
			}
		})
	}
}

func TestStatusLegacyCSVRetainsIdentityWhenSummaryIsUnavailable(t *testing.T) {
	roundTrips := 0
	client, err := NewClient(WithRoundTripper(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		roundTrips++
		return testResponse(request, http.StatusOK, `alice,a,b,c,d,e,opaque,opaque,10.0.0.10`, ""), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	observedAt := time.Unix(123, 0).UTC()
	client.now = func() time.Time { return observedAt }

	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Network != NetworkReachable || status.Session != SessionOnline || status.Username != "alice" || status.OnlineIP != netip.MustParseAddr("10.0.0.10") || !status.ObservedAt.Equal(observedAt) || status.Summary != nil {
		t.Fatalf("Status() = %#v", status)
	}
	if roundTrips != 1 {
		t.Fatalf("round trips = %d, want 1", roundTrips)
	}
}

func TestStatusUnrecognizedResponseIdentifiesGatewayStatusProtocolPart(t *testing.T) {
	const responseCanary = "STATUS-DIAGNOSTIC-CANARY"
	roundTrips := 0
	client, err := NewClient(WithRoundTripper(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		roundTrips++
		return testResponse(request, http.StatusOK, "<html><body>"+responseCanary+"</body></html>", ""), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Status(context.Background())
	var sdkErr *Error
	if !errors.As(err, &sdkErr) || sdkErr.Code != CodeProtocolChanged {
		t.Fatalf("Status() error = %v, code=%q", err, CodeOf(err))
	}
	if got := sdkErr.Details.ProtocolPart; got != "gateway_status" {
		t.Fatalf("protocol part = %q, want gateway_status", got)
	}
	if roundTrips != 1 {
		t.Fatalf("round trips = %d, want 1", roundTrips)
	}
	encoded, marshalErr := json.Marshal(sdkErr)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), responseCanary) {
		t.Fatal("raw status response leaked through SDK error JSON")
	}
}

func TestContextCancellationIsPreserved(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client, _ := NewClient(WithRoundTripper(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return nil, request.Context().Err()
	})))
	_, err := client.Status(ctx)
	if !errors.Is(err, context.Canceled) || CodeOf(err) != CodeNetwork {
		t.Fatalf("Status() error = %v, code=%q", err, CodeOf(err))
	}
}

func TestPublicMethodsRejectNilContextAndUninitializedClient(t *testing.T) {
	client, err := NewClient(WithRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("network must not be reached")
	})))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		call func(*Client, context.Context) error
	}{
		{name: "Status", call: func(c *Client, ctx context.Context) error { _, err := c.Status(ctx); return err }},
		{name: "Login", call: func(c *Client, ctx context.Context) error { _, err := c.Login(ctx, LoginRequest{}); return err }},
		{name: "Logout", call: func(c *Client, ctx context.Context) error { _, err := c.Logout(ctx); return err }},
		{name: "ListInterfaces", call: func(c *Client, ctx context.Context) error { _, err := c.ListInterfaces(ctx); return err }},
	}
	for _, testCase := range tests {
		t.Run(testCase.name+" nil context", func(t *testing.T) {
			if err := testCase.call(client, nil); !IsCode(err, CodeInvalidArgument) {
				t.Fatalf("error = %v, code=%q", err, CodeOf(err))
			}
		})
		t.Run(testCase.name+" nil client", func(t *testing.T) {
			var nilClient *Client
			if err := testCase.call(nilClient, context.Background()); !IsCode(err, CodeInvalidArgument) {
				t.Fatalf("error = %v, code=%q", err, CodeOf(err))
			}
		})
		t.Run(testCase.name+" zero client", func(t *testing.T) {
			if err := testCase.call(&Client{}, context.Background()); !IsCode(err, CodeInvalidArgument) {
				t.Fatalf("error = %v, code=%q", err, CodeOf(err))
			}
		})
	}
}

func TestURLErrorCauseDoesNotExposeTicketOrQRUUID(t *testing.T) {
	for _, secret := range []string{"SECRET-TICKET-CANARY", "EPHEMERAL-QR-UUID-CANARY"} {
		cause := &url.Error{Op: "Get", URL: "https://example.invalid/path?value=" + secret, Err: errors.New("dial failed")}
		err := wrapError(CodeNetwork, "network request failed", true, cause)
		for current := err; current != nil; current = errors.Unwrap(current) {
			if strings.Contains(fmt.Sprintf("%+v", current), secret) {
				t.Fatalf("secret %q leaked through error chain: %+v", secret, current)
			}
		}
	}
}

func TestUntrustedRoundTripperErrorsCannotControlPublicErrorOrLeakSecrets(t *testing.T) {
	secret := "https://example.invalid/activation?" + "ticket=" + "TRANSPORT-SECRET-CANARY"
	for _, testCase := range []struct {
		name string
		err  error
	}{
		{name: "ordinary error", err: fmt.Errorf("transport echoed %s", secret)},
		{name: "forged SDK error", err: &Error{
			Code: CodeProtocolChanged, Message: "forged " + secret, Cause: errors.New("cause " + secret),
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := NewClient(WithRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, testCase.err
			})))
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.Status(context.Background())
			if !IsCode(err, CodeNetwork) || err.Error() != "network request failed" {
				t.Fatalf("untrusted error controlled classification/message: %v, code=%q", err, CodeOf(err))
			}
			assertErrorDoesNotContain(t, err, "TRANSPORT-SECRET-CANARY")
		})
	}
}

func TestRejectedRedirectRemainsProtocolChangedWithoutLeakingLocation(t *testing.T) {
	const secret = "REDIRECT-TICKET-CANARY"
	roundTrips := 0
	client, err := NewClient(WithRoundTripper(roundTripFunc(func(request *http.Request) (*http.Response, error) {
		roundTrips++
		return testResponse(request, http.StatusFound, "", "https://attacker.invalid/path?ticket="+secret), nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Status(context.Background())
	if !IsCode(err, CodeProtocolChanged) || roundTrips != 1 {
		t.Fatalf("redirect error = %v, code=%q, round trips=%d", err, CodeOf(err), roundTrips)
	}
	assertErrorDoesNotContain(t, err, secret)
}

func assertErrorDoesNotContain(t *testing.T, err error, canary string) {
	t.Helper()
	for current := err; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(fmt.Sprintf("%+v", current), canary) {
			t.Fatalf("canary leaked through error chain: %+v", current)
		}
	}
	encoded, marshalErr := json.Marshal(err)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(encoded), canary) {
		t.Fatalf("canary leaked through error JSON: %s", encoded)
	}
}

func TestRandomUUIDIsRFC4122AndNotReused(t *testing.T) {
	first, err := randomUUID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomUUID()
	if err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(first) || !pattern.MatchString(second) || first == second {
		t.Fatalf("invalid or reused UUIDs: %q, %q", first, second)
	}
}

func TestTicketRedirectRejectsExtraOrRepeatedParameters(t *testing.T) {
	client, _ := NewClient()
	service := cloneURL(client.endpoints.service)
	query := service.Query()
	query.Set("ac_id", "1")
	service.RawQuery = query.Encode()
	for _, raw := range []string{
		"http://ipgw.neu.edu.cn/srun_portal_sso?ac_id=1&ticket=x&extra=y",
		"http://ipgw.neu.edu.cn/srun_portal_sso?ac_id=1&ticket=x&ticket=y",
		"http://ipgw.neu.edu.cn/srun_portal_sso?ac_id=1&ticket=x&extra=%zz",
		"http://ipgw.neu.edu.cn/srun_portal_sso?ac_id=1&ticket=x#fragment",
	} {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil {
			parsed = &url.URL{Scheme: "http", Host: "ipgw.neu.edu.cn", Path: "/srun_portal_sso", RawQuery: "ac_id=1&ticket=x&extra=%zz"}
		}
		target := &http.Request{Method: http.MethodGet, URL: parsed, Header: make(http.Header)}
		err := client.casRedirectPolicy(service, &ticketCapture{})(target, nil)
		if !IsCode(err, CodeProtocolChanged) {
			t.Fatalf("redirect %q error = %v, code=%q", raw, err, CodeOf(err))
		}
	}
}

type memoryProtocolStore struct {
	state ProtocolState
	loads int
	saves int
}

func (s *memoryProtocolStore) Load(context.Context, string) (ProtocolState, bool, error) {
	s.loads++
	return s.state, s.state.ACID != "", nil
}

func (s *memoryProtocolStore) Save(_ context.Context, state ProtocolState) error {
	s.saves++
	s.state = state
	return nil
}
