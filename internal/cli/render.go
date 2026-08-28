package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
)

type renderer struct {
	mode outputMode
	out  io.Writer
	err  io.Writer
}

type envelope struct {
	SchemaVersion int        `json:"schema_version"`
	Command       string     `json:"command"`
	OK            bool       `json:"ok"`
	Data          any        `json:"-"`
	Error         *wireError `json:"-"`
}

// MarshalJSON makes the data/error exclusive-or invariant structural instead
// of relying on omitempty. A successful envelope always contains data (even
// when it is null), while a failed envelope always contains exactly one error.
func (e envelope) MarshalJSON() ([]byte, error) {
	if e.OK {
		return json.Marshal(struct {
			SchemaVersion int    `json:"schema_version"`
			Command       string `json:"command"`
			OK            bool   `json:"ok"`
			Data          any    `json:"data"`
		}{e.SchemaVersion, e.Command, true, e.Data})
	}
	wiredError := e.Error
	if wiredError == nil {
		wiredError = &wireError{Code: string(ipgw.CodeInternal), Details: wireErrorDetails{}}
	}
	return json.Marshal(struct {
		SchemaVersion int        `json:"schema_version"`
		Command       string     `json:"command"`
		OK            bool       `json:"ok"`
		Error         *wireError `json:"error"`
	}{e.SchemaVersion, e.Command, false, wiredError})
}

type wireError struct {
	Code      string           `json:"code"`
	Message   string           `json:"message"`
	Retryable bool             `json:"retryable"`
	Details   wireErrorDetails `json:"details"`
}

type wireErrorDetails struct {
	Interaction  *wireInteractionDetails `json:"interaction,omitempty"`
	ProtocolPart string                  `json:"protocol_part,omitempty"`
	Migration    *wireMigrationReport    `json:"migration,omitempty"`
}

type wireMigrationReport struct {
	SourceCount int                     `json:"source_count"`
	Profiles    []wireMigrationProfile  `json:"profiles"`
	Conflicts   []wireMigrationConflict `json:"conflicts"`
	Warnings    []string                `json:"warnings"`
}

type wireMigrationProfile struct {
	Name               string `json:"name"`
	Source             string `json:"source"`
	CredentialStatus   string `json:"credential_status"`
	UsernameConfigured bool   `json:"username_configured"`
	Default            bool   `json:"default"`
}

type wireMigrationConflict struct {
	Profile string `json:"profile"`
	Reason  string `json:"reason"`
}

type wireInteractionDetails struct {
	ChallengeKind  ipgw.ChallengeKind      `json:"challenge_kind"`
	OriginMethod   ipgw.AuthMethod         `json:"origin_method"`
	Capability     []ipgw.CapabilityStatus `json:"capability_status"`
	SessionBinding string                  `json:"session_binding,omitempty"`
	ResumeMode     string                  `json:"resume_mode,omitempty"`
	TTYRequired    bool                    `json:"tty_required"`
	HelpID         string                  `json:"help_id,omitempty"`
}

// MarshalJSON is a final guard at the public wire boundary. Some command paths
// construct a wireError directly, so normalize the code and sensitive messages
// here as well as at the point where an SDK error is classified.
func (e wireError) MarshalJSON() ([]byte, error) {
	code := e.Code
	retryable := e.Retryable
	details := e.Details
	if !isPublicErrorCode(code) {
		code = string(ipgw.CodeInternal)
		retryable = false
		details = wireErrorDetails{}
	}
	details = sanitizeWireDetails(code, details)
	type encodedWireError struct {
		Code      string           `json:"code"`
		Message   string           `json:"message"`
		Retryable bool             `json:"retryable"`
		Details   wireErrorDetails `json:"details"`
	}
	return json.Marshal(encodedWireError{
		Code: code, Message: safeWireMessage(code), Retryable: retryable, Details: details,
	})
}

type wireStatus struct {
	Network    string              `json:"network"`
	Session    string              `json:"session"`
	Username   *string             `json:"username"`
	OnlineIP   *string             `json:"online_ip"`
	ObservedAt time.Time           `json:"observed_at"`
	Summary    *ipgw.OnlineSummary `json:"summary"`
}

type wireLoginResult struct {
	Outcome string     `json:"outcome"`
	Status  wireStatus `json:"status"`
}

type wireLogoutResult struct {
	Outcome string     `json:"outcome"`
	Status  wireStatus `json:"status"`
}

type wireInterface struct {
	Name  string `json:"name"`
	Index int    `json:"index"`
	IP    string `json:"ip"`
}

func (r renderer) success(command string, data any) int {
	if r.mode == outputJSON {
		if !r.encode(envelope{SchemaVersion: 1, Command: command, OK: true, Data: data}) {
			return 1
		}
	}
	return 0
}

func (r renderer) failure(command string, err error) int {
	return r.failureWithDetails(command, err, wireErrorDetails{})
}

func (r renderer) failureWithDetails(command string, err error, supplied wireErrorDetails) int {
	code, retryable, details := wireErrorOf(err)
	if code == string(ipgw.CodeConfig) && supplied.Migration != nil {
		details.Migration = supplied.Migration
	}
	message := wireErrorMessage(err, code)
	if r.mode == outputJSON {
		if !r.encode(envelope{
			SchemaVersion: 1, Command: command, OK: false,
			Error: &wireError{Code: code, Message: message, Retryable: retryable, Details: details},
		}) {
			return 1
		}
	} else {
		_, _ = fmt.Fprintf(r.err, "Error: %s\n", message)
		if code == string(ipgw.CodeInteractionRequired) {
			r.interactionHelp(details.Interaction)
		}
	}
	return exitCode(err)
}

func (r renderer) encode(value any) bool {
	if err := json.NewEncoder(r.out).Encode(value); err != nil {
		_, _ = fmt.Fprintln(r.err, "Error: unable to write JSON output")
		return false
	}
	return true
}

func (r renderer) interactionHelp(interaction *wireInteractionDetails) {
	if interaction == nil {
		_, _ = fmt.Fprintln(r.err, "Complete the required verification in the official NEU portal, then rerun login.")
		return
	}
	switch interaction.ResumeMode {
	case "retry_in_tty":
		_, _ = fmt.Fprintln(r.err, "Retry from a terminal connected to both stdin and stderr. SSH with a TTY is supported; no desktop GUI is required, and a terminal QR code can be scanned from another device.")
	case "restart":
		_, _ = fmt.Fprintln(r.err, "Restart the login command to request a new verification session.")
	case "official_portal":
		_, _ = fmt.Fprintln(r.err, "Complete the required verification in the official NEU portal, then rerun login.")
	default:
		if interaction.TTYRequired {
			_, _ = fmt.Fprintln(r.err, "Retry from a terminal connected to both stdin and stderr. SSH with a TTY is supported; no desktop GUI is required.")
		} else {
			_, _ = fmt.Fprintln(r.err, "Complete the required verification in the official NEU portal, then rerun login.")
		}
	}
	if interaction.HelpID != "" {
		_, _ = fmt.Fprintf(r.err, "Help ID: %s\n", interaction.HelpID)
	}
}

func wireErrorOf(err error) (string, bool, wireErrorDetails) {
	if errors.Is(err, context.Canceled) {
		return string(ipgw.CodeNetwork), false, wireErrorDetails{}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return string(ipgw.CodeNetwork), true, wireErrorDetails{}
	}
	var sdkErr *ipgw.Error
	if errors.As(err, &sdkErr) {
		code := string(sdkErr.Code)
		if !isPublicErrorCode(code) {
			return string(ipgw.CodeInternal), false, wireErrorDetails{}
		}
		return code, sdkErr.Retryable, wireErrorDetailsOf(sdkErr.Code, sdkErr.Details)
	}
	return string(ipgw.CodeInternal), false, wireErrorDetails{}
}

func wireErrorDetailsOf(code ipgw.ErrorCode, details ipgw.ErrorDetails) wireErrorDetails {
	switch code {
	case ipgw.CodeInteractionRequired:
		if details.Interaction == nil {
			return wireErrorDetails{}
		}
		interaction := details.Interaction
		candidate := wireErrorDetails{Interaction: &wireInteractionDetails{
			ChallengeKind: interaction.Challenge, OriginMethod: interaction.OriginMethod,
			Capability: interaction.Capability, SessionBinding: interaction.SessionBinding,
			ResumeMode: interaction.ResumeMode, TTYRequired: interaction.TTYRequired,
			HelpID: interaction.HelpID,
		}}
		return sanitizeWireDetails(string(code), candidate)
	case ipgw.CodeProtocolChanged:
		return sanitizeWireDetails(string(code), wireErrorDetails{ProtocolPart: details.ProtocolPart})
	}
	return wireErrorDetails{}
}

func wireErrorMessage(err error, code string) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "operation canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "operation timed out"
	default:
		return safeWireMessage(code)
	}
}

func safeWireMessage(code string) string {
	switch code {
	case string(ipgw.CodeInvalidArgument):
		return "invalid arguments"
	case string(ipgw.CodeConfig):
		return "configuration error"
	case string(ipgw.CodeNetwork):
		return "network request failed"
	case string(ipgw.CodeAuthentication):
		return "authentication failed"
	case string(ipgw.CodeSessionConflict):
		return "another account is already online"
	case string(ipgw.CodeProtocolChanged):
		return "gateway protocol changed"
	case string(ipgw.CodeInteractionRequired):
		return "login requires human verification"
	case string(ipgw.CodeUnsupported):
		return "operation is unsupported"
	case string(ipgw.CodeInternal):
		return "internal error"
	default:
		return "internal error"
	}
}

func sanitizeWireDetails(code string, details wireErrorDetails) wireErrorDetails {
	switch ipgw.ErrorCode(code) {
	case ipgw.CodeInteractionRequired:
		return wireErrorDetails{Interaction: sanitizeInteraction(details.Interaction)}
	case ipgw.CodeProtocolChanged:
		return wireErrorDetails{ProtocolPart: sanitizeProtocolPart(details.ProtocolPart)}
	case ipgw.CodeConfig:
		if details.Migration != nil {
			return wireErrorDetails{Migration: details.Migration}
		}
	default:
		return wireErrorDetails{}
	}
	return wireErrorDetails{}
}

func sanitizeInteraction(value *wireInteractionDetails) *wireInteractionDetails {
	if value == nil || !isAuthMethod(value.OriginMethod) {
		return nil
	}
	challenge := value.ChallengeKind
	if !isChallengeKind(challenge) {
		challenge = ipgw.ChallengeUnknown
	}
	capabilities := make([]ipgw.CapabilityStatus, 0, len(value.Capability))
	seen := make(map[ipgw.CapabilityStatus]struct{}, len(value.Capability))
	for _, capability := range value.Capability {
		if !isCapabilityStatus(capability) {
			continue
		}
		if _, duplicate := seen[capability]; duplicate {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	return &wireInteractionDetails{
		ChallengeKind:  challenge,
		OriginMethod:   value.OriginMethod,
		Capability:     capabilities,
		SessionBinding: sanitizeSessionBinding(value.SessionBinding),
		ResumeMode:     sanitizeResumeMode(value.ResumeMode),
		TTYRequired:    value.TTYRequired,
		HelpID:         sanitizeHelpID(value.HelpID),
	}
}

func isAuthMethod(method ipgw.AuthMethod) bool {
	return method == ipgw.AuthMethodPassword || method == ipgw.AuthMethodTerminalQR
}

func isChallengeKind(challenge ipgw.ChallengeKind) bool {
	switch challenge {
	case ipgw.ChallengeSMSOTP, ipgw.ChallengeDeviceVerification, ipgw.ChallengeAccountSetup,
		ipgw.ChallengeQRApproval, ipgw.ChallengeUnknown:
		return true
	default:
		return false
	}
}

func isCapabilityStatus(status ipgw.CapabilityStatus) bool {
	switch status {
	case ipgw.CapabilityObservedAnonymous, ipgw.CapabilitySyntheticCovered,
		ipgw.CapabilityLiveUnverified, ipgw.CapabilityLiveVerified,
		ipgw.CapabilitySupported, ipgw.CapabilityDetectedOnly, ipgw.CapabilityUnknown:
		return true
	default:
		return false
	}
}

func sanitizeSessionBinding(value string) string {
	if value == "cas_session" {
		return value
	}
	return ""
}

func sanitizeResumeMode(value string) string {
	switch value {
	case "retry_in_tty", "restart", "official_portal":
		return value
	default:
		return ""
	}
}

func sanitizeHelpID(value string) string {
	switch value {
	case "AUTH-QR-001", "AUTH-SMS-001", "AUTH-DEVICE-001", "AUTH-UNKNOWN-001", "AUTH-CHALLENGE-001":
		return value
	default:
		return ""
	}
}

func sanitizeProtocolPart(value string) string {
	switch value {
	case "cas_login", "cas_qr", "gateway_status", "gateway_activation", "gateway_logout", "redirect":
		return value
	default:
		return ""
	}
}

func isPublicErrorCode(code string) bool {
	switch ipgw.ErrorCode(code) {
	case ipgw.CodeInvalidArgument, ipgw.CodeConfig, ipgw.CodeNetwork,
		ipgw.CodeAuthentication, ipgw.CodeSessionConflict, ipgw.CodeProtocolChanged,
		ipgw.CodeInteractionRequired, ipgw.CodeUnsupported, ipgw.CodeInternal:
		return true
	default:
		return false
	}
}

func exitCode(err error) int {
	if errors.Is(err, context.Canceled) {
		return 130
	}
	switch ipgw.CodeOf(err) {
	case ipgw.CodeInvalidArgument, ipgw.CodeConfig:
		return 2
	case ipgw.CodeNetwork:
		return 3
	case ipgw.CodeAuthentication:
		return 4
	case ipgw.CodeSessionConflict:
		return 5
	case ipgw.CodeProtocolChanged:
		return 6
	case ipgw.CodeInteractionRequired:
		return 7
	case ipgw.CodeUnsupported:
		return 2
	default:
		return 1
	}
}

func toWireStatus(status ipgw.Status) wireStatus {
	wired := wireStatus{
		Network: string(status.Network), Session: string(status.Session),
		ObservedAt: status.ObservedAt.UTC(), Summary: status.Summary,
	}
	if status.Username != "" {
		username := status.Username
		wired.Username = &username
	}
	if status.OnlineIP.IsValid() {
		ip := status.OnlineIP.String()
		wired.OnlineIP = &ip
	}
	return wired
}

func toWireLogin(result ipgw.LoginResult) wireLoginResult {
	return wireLoginResult{Outcome: string(result.Outcome), Status: toWireStatus(result.Status)}
}

func toWireLogout(result ipgw.LogoutResult) wireLogoutResult {
	return wireLogoutResult{Outcome: string(result.Outcome), Status: toWireStatus(result.Status)}
}

func toWireInterfaces(interfaces []ipgw.Interface) []wireInterface {
	result := make([]wireInterface, 0, len(interfaces))
	for _, iface := range interfaces {
		result = append(result, wireInterface{Name: iface.Name, Index: iface.Index, IP: iface.IP.String()})
	}
	return result
}
