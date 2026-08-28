// Package cas parses the untrusted CAS login, challenge, and QR wire formats.
// It does not own HTTP clients, credentials, or public SDK state.
package cas

import (
	"errors"
	"net/url"
)

// ErrUnrecognized reports a CAS response that cannot be classified safely.
// It intentionally contains no response content.
var ErrUnrecognized = errors.New("unrecognized CAS response")

type Challenge string

const (
	ChallengeNone    Challenge = ""
	ChallengeSMSOTP  Challenge = "sms_otp"
	ChallengeDevice  Challenge = "device_verification"
	ChallengeSetup   Challenge = "account_setup"
	ChallengeUnknown Challenge = "unknown"
)

// Page contains transient values discovered from one CAS page. Hidden values
// and QR identifiers must remain inside the authentication operation.
type Page struct {
	Action      *url.URL
	Hidden      url.Values
	Scripts     []*url.URL
	PublicKey   string
	Challenge   Challenge
	QRUUID      string
	QRSupported bool
}
