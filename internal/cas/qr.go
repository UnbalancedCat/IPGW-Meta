package cas

import (
	"encoding/json"
	"strings"

	"github.com/UnbalancedCat/ipgw-meta/internal/wirejson"
)

type PollState string

const (
	PollPending   PollState = "pending"
	PollConfirmed PollState = "confirmed"
	PollExpired   PollState = "expired"
	PollChallenge PollState = "challenge"
)

// Poll is the single, non-conflicting state represented by one QR response.
// RedirectURL is transient and must not be logged or persisted.
type Poll struct {
	State       PollState
	RedirectURL string
	Challenge   Challenge
}

var pollMessageFields = []string{"message", "msg", "description", "status", "result"}

// ParsePoll classifies a QR polling response only when all recognized evidence
// agrees on one state. The exact zero-length response is the sole non-object
// pending representation observed from the anonymous official endpoint.
func ParsePoll(data []byte) (Poll, error) {
	if len(data) == 0 {
		return Poll{State: PollPending}, nil
	}
	object, err := wirejson.DecodeObjectOrJSONP(data)
	if err != nil {
		return Poll{}, ErrUnrecognized
	}

	states := make(map[PollState]struct{})
	redirect, redirectPresent, err := aliasedExactString(object, "redirect_url", "redirectUrl", "url")
	if err != nil {
		return Poll{}, err
	}
	if redirectPresent && redirect != "" {
		states[PollConfirmed] = struct{}{}
	}

	challenge, err := structuredChallenge(object)
	if err != nil {
		return Poll{}, err
	}
	if challenge != ChallengeNone {
		states[PollChallenge] = struct{}{}
	}

	for _, name := range pollMessageFields {
		raw, ok := object.Raw(name)
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return Poll{}, ErrUnrecognized
		}
		if value == "" {
			continue
		}
		recognized := false
		if challengeFromText(value) != ChallengeNone {
			recognized = true
		}
		if isExpiredPollWord(value) {
			states[PollExpired] = struct{}{}
			recognized = true
		}
		if isPendingPollWord(value) {
			states[PollPending] = struct{}{}
			recognized = true
		}
		if !recognized {
			return Poll{}, ErrUnrecognized
		}
	}

	if len(states) != 1 {
		return Poll{}, ErrUnrecognized
	}
	for state := range states {
		switch state {
		case PollConfirmed:
			return Poll{State: state, RedirectURL: redirect}, nil
		case PollChallenge:
			return Poll{State: state, Challenge: challenge}, nil
		case PollExpired, PollPending:
			return Poll{State: state}, nil
		default:
			return Poll{}, ErrUnrecognized
		}
	}
	return Poll{}, ErrUnrecognized
}

func aliasedExactString(object wirejson.Object, names ...string) (string, bool, error) {
	var result string
	present := false
	for _, name := range names {
		raw, ok := object.Raw(name)
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return "", false, ErrUnrecognized
		}
		if present && result != value {
			return "", false, ErrUnrecognized
		}
		result = value
		present = true
	}
	return result, present, nil
}

func isPendingPollWord(value string) bool {
	switch normalizePollWord(value) {
	case "pending", "waiting", "wait", "waitingscan", "awaitingscan", "等待", "等待扫码", "未扫描", "二维码未扫描", "已扫码", "二维码已扫码":
		return true
	default:
		return false
	}
}

func isExpiredPollWord(value string) bool {
	switch normalizePollWord(value) {
	case "expired", "timeout", "timedout", "qrcodeexpired", "过期", "二维码已过期", "超时", "二维码已超时":
		return true
	default:
		return false
	}
}

func normalizePollWord(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("_", "", "-", "", " ", "").Replace(lower)
}
