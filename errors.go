package ipgw

import (
	"context"
	"encoding/json"
	"errors"
)

type ErrorCode string

// errRedirectRejected is private so caller-provided transports cannot forge
// the protocol classification merely by returning a public *Error. It is only
// emitted by SDK-owned redirect policies and is mapped to a fixed public error.
var errRedirectRejected = errors.New("ipgw: redirect rejected")

const (
	CodeInvalidArgument     ErrorCode = "invalid_argument"
	CodeConfig              ErrorCode = "config"
	CodeNetwork             ErrorCode = "network"
	CodeAuthentication      ErrorCode = "authentication"
	CodeSessionConflict     ErrorCode = "session_conflict"
	CodeProtocolChanged     ErrorCode = "protocol_changed"
	CodeInteractionRequired ErrorCode = "interaction_required"
	CodeUnsupported         ErrorCode = "unsupported"
	CodeInternal            ErrorCode = "internal"
)

const (
	interactionSessionCASSession  = "cas_session"
	interactionResumeRetryInTTY   = "retry_in_tty"
	interactionResumeRestart      = "restart"
	interactionResumeOfficialSite = "official_portal"
	interactionHelpQR             = "AUTH-QR-001"
	interactionHelpChallenge      = "AUTH-CHALLENGE-001"
	interactionHelpSMS            = "AUTH-SMS-001"
)

// InteractionDetails contains only redacted, stable metadata. In particular it
// has no QR payload, phone number, cookie, ticket, raw response, or URL.
type InteractionDetails struct {
	Challenge      ChallengeKind      `json:"challenge_kind"`
	OriginMethod   AuthMethod         `json:"origin_method"`
	Capability     []CapabilityStatus `json:"capability_status"`
	SessionBinding string             `json:"session_binding,omitempty"`
	ResumeMode     string             `json:"resume_mode,omitempty"`
	TTYRequired    bool               `json:"tty_required"`
	HelpID         string             `json:"help_id,omitempty"`
}

func (details InteractionDetails) MarshalJSON() ([]byte, error) {
	type wireDetails InteractionDetails
	details = normalizeInteractionDetails(details)
	if !isInteractionAuthMethod(details.OriginMethod) {
		return []byte("null"), nil
	}
	return json.Marshal(wireDetails(details))
}

func normalizeInteractionDetails(details InteractionDetails) InteractionDetails {
	if !isInteractionAuthMethod(details.OriginMethod) {
		details.OriginMethod = ""
	}
	if !isInteractionChallenge(details.Challenge) {
		details.Challenge = ChallengeUnknown
	}
	capabilities := make([]CapabilityStatus, 0, len(details.Capability))
	seen := make(map[CapabilityStatus]struct{}, len(details.Capability))
	for _, capability := range details.Capability {
		if !isInteractionCapability(capability) {
			continue
		}
		if _, duplicate := seen[capability]; duplicate {
			continue
		}
		seen[capability] = struct{}{}
		capabilities = append(capabilities, capability)
	}
	details.Capability = capabilities
	if details.SessionBinding != "" && details.SessionBinding != interactionSessionCASSession {
		details.SessionBinding = ""
	}
	switch details.ResumeMode {
	case "", interactionResumeRetryInTTY, interactionResumeRestart, interactionResumeOfficialSite:
	default:
		details.ResumeMode = ""
	}
	switch details.HelpID {
	case "", interactionHelpQR, interactionHelpChallenge, interactionHelpSMS:
	default:
		details.HelpID = ""
	}
	return details
}

func isInteractionAuthMethod(method AuthMethod) bool {
	return method == AuthMethodPassword || method == AuthMethodTerminalQR
}

func isInteractionChallenge(challenge ChallengeKind) bool {
	switch challenge {
	case ChallengeSMSOTP, ChallengeDeviceVerification, ChallengeAccountSetup, ChallengeQRApproval, ChallengeUnknown:
		return true
	default:
		return false
	}
}

func isInteractionCapability(capability CapabilityStatus) bool {
	switch capability {
	case CapabilityObservedAnonymous, CapabilitySyntheticCovered, CapabilityLiveUnverified,
		CapabilityLiveVerified, CapabilitySupported, CapabilityDetectedOnly, CapabilityUnknown:
		return true
	default:
		return false
	}
}

type ErrorDetails struct {
	Interaction  *InteractionDetails `json:"interaction,omitempty"`
	ExpectedUser string              `json:"expected_username,omitempty"`
	ActualUser   string              `json:"actual_username,omitempty"`
	ProtocolPart string              `json:"protocol_part,omitempty"`
}

// Error is the stable SDK error contract. Cause is intentionally excluded
// from JSON so low-level errors cannot accidentally expose request data.
type Error struct {
	Code      ErrorCode    `json:"code"`
	Message   string       `json:"message"`
	Retryable bool         `json:"retryable"`
	Details   ErrorDetails `json:"details"`
	Cause     error        `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.Cause }

func newError(code ErrorCode, message string, retryable bool, cause error) *Error {
	return &Error{Code: code, Message: message, Retryable: retryable, Cause: contextSentinel(cause)}
}

func wrapError(code ErrorCode, message string, retryable bool, cause error) error {
	if cause == nil {
		return newError(code, message, retryable, nil)
	}
	if sentinel := contextSentinel(cause); sentinel != nil {
		return newError(CodeNetwork, message, true, sentinel)
	}
	if errors.Is(cause, errRedirectRejected) {
		return newError(CodeProtocolChanged, "gateway attempted an unexpected redirect", false, nil)
	}
	// Extension points and net/http errors are untrusted diagnostic input. In
	// particular, a RoundTripper error can echo a ticket- or QR-bearing URL.
	// Keep the stable classification but never retain an arbitrary public cause.
	return newError(code, message, retryable, nil)
}

func contextSentinel(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func CodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	if errors.Is(err, errRedirectRejected) {
		return CodeProtocolChanged
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return CodeNetwork
	}
	var sdkErr *Error
	if errors.As(err, &sdkErr) {
		return sdkErr.Code
	}
	return CodeInternal
}

func IsCode(err error, code ErrorCode) bool { return CodeOf(err) == code }

func interactionError(method AuthMethod, details InteractionDetails, message string) *Error {
	details.OriginMethod = method
	details = normalizeInteractionDetails(details)
	return &Error{
		Code:      CodeInteractionRequired,
		Message:   message,
		Retryable: false,
		Details: ErrorDetails{
			Interaction: &details,
		},
	}
}
