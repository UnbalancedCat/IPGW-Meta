package cas

import (
	"errors"
	"strings"
	"testing"
)

func TestParsePollRecognizesSingleStates(t *testing.T) {
	tests := []struct {
		name      string
		input     []byte
		state     PollState
		challenge Challenge
		redirect  string
	}{
		{name: "exact empty pending", input: nil, state: PollPending},
		{name: "pending", input: []byte(`{"status":"waiting"}`), state: PollPending},
		{name: "confirmed", input: []byte(`{"redirect_url":"https://pass.example.test/tpass/complete"}`), state: PollConfirmed, redirect: "https://pass.example.test/tpass/complete"},
		{name: "expired", input: []byte(`{"status":"expired"}`), state: PollExpired},
		{name: "SMS challenge", input: []byte(`{"message":"需要短信验证码"}`), state: PollChallenge, challenge: ChallengeSMSOTP},
		{name: "JSONP", input: []byte(`callback_1({"status":"pending"});`), state: PollPending},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			poll, err := ParsePoll(testCase.input)
			if err != nil {
				t.Fatalf("ParsePoll() error = %v", err)
			}
			if poll.State != testCase.state || poll.Challenge != testCase.challenge || poll.RedirectURL != testCase.redirect {
				t.Fatalf("ParsePoll() = %#v", poll)
			}
		})
	}
}

func TestParsePollRejectsWhitespaceUnknownAndConflictingEvidence(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "whitespace", input: " \t\r\n"},
		{name: "unknown non-empty", input: `{"message":"unrecognized-state"}`},
		{name: "redirect and waiting", input: `{"redirect_url":"https://pass.example.test/tpass/complete","status":"waiting"}`},
		{name: "redirect and challenge", input: `{"redirect_url":"https://pass.example.test/tpass/complete","message":"需要短信验证码"}`},
		{name: "waiting and expired", input: `{"status":"waiting","message":"expired"}`},
		{name: "different redirect aliases", input: `{"redirect_url":"https://pass.example.test/tpass/a","url":"https://pass.example.test/tpass/b"}`},
		{name: "duplicate status", input: `{"status":"waiting","status":"expired"}`},
		{name: "message fields disagree", input: `{"message":"pending","msg":"expired"}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ParsePoll([]byte(testCase.input)); !errors.Is(err, ErrUnrecognized) {
				t.Fatalf("ParsePoll() error = %v", err)
			}
		})
	}
}

func TestParsePollAcceptsEquivalentAliasesAndEvidence(t *testing.T) {
	const redirect = "https://pass.example.test/tpass/complete"
	confirmed, err := ParsePoll([]byte(`{"redirect_url":"` + redirect + `","redirectUrl":"` + redirect + `","url":"` + redirect + `"}`))
	if err != nil || confirmed.State != PollConfirmed || confirmed.RedirectURL != redirect {
		t.Fatalf("equivalent redirect aliases = %#v, %v", confirmed, err)
	}

	pending, err := ParsePoll([]byte(`{"status":"waiting","message":"pending","msg":"等待扫码"}`))
	if err != nil || pending.State != PollPending {
		t.Fatalf("same-state evidence = %#v, %v", pending, err)
	}
}

func TestParsePollErrorDoesNotLeakBody(t *testing.T) {
	const canary = "QR-POLL-RESPONSE-CANARY"
	_, err := ParsePoll([]byte(`{"message":"` + canary + `"}`))
	if !errors.Is(err, ErrUnrecognized) {
		t.Fatalf("ParsePoll() error = %v", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("QR response leaked through error: %v", err)
	}
}
