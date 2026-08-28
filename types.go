package ipgw

import (
	"context"
	"errors"
	"net/netip"
	"time"
)

// NetworkState describes whether the NEU gateway was reached. A failed
// network request is returned as an error instead of being reported as
// NetworkUnreachable.
type NetworkState string

const (
	NetworkReachable   NetworkState = "reachable"
	NetworkUnreachable NetworkState = "unreachable"
)

// SessionState describes the gateway's view of the current device.
type SessionState string

const (
	SessionOnline  SessionState = "online"
	SessionOffline SessionState = "offline"
	SessionUnknown SessionState = "unknown"
)

// Money represents an amount in the smallest unit of Currency.
type Money struct {
	Currency   string `json:"currency"`
	MinorUnits int64  `json:"minor_units"`
}

// OnlineSummary contains the optional accounting fields returned by Srun.
type OnlineSummary struct {
	TrafficBytes    int64  `json:"traffic_bytes"`
	DurationSeconds int64  `json:"duration_seconds"`
	Balance         *Money `json:"balance"`
}

// Status is a single, time-stamped gateway observation.
type Status struct {
	Network    NetworkState   `json:"network"`
	Session    SessionState   `json:"session"`
	Username   string         `json:"username,omitempty"`
	OnlineIP   netip.Addr     `json:"online_ip,omitempty"`
	ObservedAt time.Time      `json:"observed_at"`
	Summary    *OnlineSummary `json:"summary,omitempty"`
}

// Interface is a usable, local IPv4 interface address. ListInterfaces never
// performs a network request.
type Interface struct {
	Name  string     `json:"name"`
	Index int        `json:"index"`
	IP    netip.Addr `json:"ip"`
}

// AuthMethod selects the explicit authentication flow.
type AuthMethod string

const (
	AuthMethodPassword   AuthMethod = "password"
	AuthMethodTerminalQR AuthMethod = "terminal_qr"
)

// SwitchPolicy controls what Login does when another account is online.
type SwitchPolicy string

const (
	SwitchRefuse         SwitchPolicy = "refuse"
	SwitchLogoutExisting SwitchPolicy = "logout_existing"
)

// CredentialPurpose gives providers enough context to select a secret without
// making them responsible for profiles or configuration.
type CredentialPurpose string

const CredentialPurposeLogin CredentialPurpose = "login"

type CredentialRequest struct {
	Username string
	Purpose  CredentialPurpose
}

// Credential intentionally has no JSON tags: credentials must never be
// marshalled into CLI output or diagnostics.
type Credential struct {
	Password string
}

// MarshalJSON rejects serialization so a generic renderer cannot turn a
// credential into an accidental secret-bearing wire format.
func (Credential) MarshalJSON() ([]byte, error) {
	return nil, errors.New("ipgw: credentials cannot be serialized")
}

func (Credential) String() string   { return "Credential{Password:<redacted>}" }
func (Credential) GoString() string { return "Credential{Password:<redacted>}" }

type CredentialProvider interface {
	Credential(context.Context, CredentialRequest) (Credential, error)
}

type CredentialProviderFunc func(context.Context, CredentialRequest) (Credential, error)

func (f CredentialProviderFunc) Credential(ctx context.Context, req CredentialRequest) (Credential, error) {
	return f(ctx, req)
}

// CapabilityStatus records what kind of evidence exists for an authentication
// capability. Multiple values may apply to a capability.
type CapabilityStatus string

const (
	CapabilityObservedAnonymous CapabilityStatus = "observed_anonymous"
	CapabilitySyntheticCovered  CapabilityStatus = "synthetic_covered"
	CapabilityLiveUnverified    CapabilityStatus = "live_unverified"
	CapabilityLiveVerified      CapabilityStatus = "live_verified"
	CapabilitySupported         CapabilityStatus = "supported"
	CapabilityDetectedOnly      CapabilityStatus = "detected_only"
	CapabilityUnknown           CapabilityStatus = "unknown"
)

type ChallengeKind string

const (
	ChallengeSMSOTP             ChallengeKind = "sms_otp"
	ChallengeDeviceVerification ChallengeKind = "device_verification"
	ChallengeAccountSetup       ChallengeKind = "account_setup"
	ChallengeQRApproval         ChallengeKind = "qr_approval"
	ChallengeUnknown            ChallengeKind = "unknown"
)

// QRCodePrompt is transient. Payload must only be sent to an
// InteractionHandler and must never be persisted, logged, or returned in an
// Error.
type QRCodePrompt struct {
	Payload      string
	ExpiresAt    time.Time
	PollInterval time.Duration
}

// MarshalJSON rejects serialization because Payload is an ephemeral
// authentication secret that may only be handed to InteractionHandler.
func (QRCodePrompt) MarshalJSON() ([]byte, error) {
	return nil, errors.New("ipgw: QR code prompts cannot be serialized")
}

func (QRCodePrompt) String() string   { return "QRCodePrompt{Payload:<redacted>}" }
func (QRCodePrompt) GoString() string { return "QRCodePrompt{Payload:<redacted>}" }

type InteractionHandler interface {
	PromptQRCode(context.Context, QRCodePrompt) error
}

type InteractionHandlerFunc func(context.Context, QRCodePrompt) error

func (f InteractionHandlerFunc) PromptQRCode(ctx context.Context, prompt QRCodePrompt) error {
	return f(ctx, prompt)
}

type LoginRequest struct {
	Method           AuthMethod
	ExpectedUsername string
	Credentials      CredentialProvider
	Switch           SwitchPolicy
	Interactions     InteractionHandler
}

type LoginOutcome string

const (
	LoginLoggedIn      LoginOutcome = "logged_in"
	LoginAlreadyOnline LoginOutcome = "already_online"
)

type LoginResult struct {
	Outcome LoginOutcome `json:"outcome"`
	Status  Status       `json:"status"`
}

type LogoutOutcome string

const (
	LogoutLoggedOut      LogoutOutcome = "logged_out"
	LogoutAlreadyOffline LogoutOutcome = "already_offline"
)

type LogoutResult struct {
	Outcome LogoutOutcome `json:"outcome"`
	Status  Status        `json:"status"`
}

// EventName is deliberately closed over a small set of redacted lifecycle
// events. Event contains no arbitrary fields or URLs.
type EventName string

const (
	EventOperationStarted   EventName = "operation_started"
	EventOperationFinished  EventName = "operation_finished"
	EventProtocolDiscovered EventName = "protocol_discovered"
	EventInteractionNeeded  EventName = "interaction_required"
)

type Event struct {
	Name      EventName `json:"name"`
	Operation string    `json:"operation"`
	Phase     string    `json:"phase,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
	At        time.Time `json:"at"`
}

type Observer interface {
	Observe(context.Context, Event)
}

type ObserverFunc func(context.Context, Event)

func (f ObserverFunc) Observe(ctx context.Context, event Event) { f(ctx, event) }

// ProtocolState is a secret-free, previously verified discovery result.
type ProtocolState struct {
	NetworkKey string    `json:"network_key"`
	ACID       string    `json:"ac_id"`
	VerifiedAt time.Time `json:"verified_at"`
}

type ProtocolStateStore interface {
	Load(context.Context, string) (ProtocolState, bool, error)
	Save(context.Context, ProtocolState) error
}
