package cli

import (
	"bytes"
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
)

func TestJSONGoldenEnvelopes(t *testing.T) {
	tests := []struct {
		name       string
		goldenFile string
		wantExit   int
		run        func(renderer) int
	}{
		{
			name:       "status success",
			goldenFile: "status-success.golden.json",
			wantExit:   0,
			run: func(r renderer) int {
				return r.success("status", toWireStatus(ipgw.Status{
					Network:    ipgw.NetworkReachable,
					Session:    ipgw.SessionOnline,
					Username:   "synthetic-user",
					OnlineIP:   netip.MustParseAddr("192.0.2.44"),
					ObservedAt: time.Date(2026, 8, 27, 3, 4, 5, 0, time.UTC),
				}))
			},
		},
		{
			name:       "sms interaction",
			goldenFile: "login-sms-interaction.golden.json",
			wantExit:   7,
			run: func(r renderer) int {
				return r.failure("login", &ipgw.Error{
					Code: ipgw.CodeInteractionRequired,
					Details: ipgw.ErrorDetails{Interaction: &ipgw.InteractionDetails{
						Challenge:    ipgw.ChallengeSMSOTP,
						OriginMethod: ipgw.AuthMethodPassword,
						Capability: []ipgw.CapabilityStatus{
							ipgw.CapabilityObservedAnonymous,
							ipgw.CapabilityDetectedOnly,
						},
						SessionBinding: "cas_session",
						ResumeMode:     "official_portal",
						TTYRequired:    true,
						HelpID:         "AUTH-SMS-001",
					}},
				})
			},
		},
		{
			name:       "parse error",
			goldenFile: "cli-parse-error.golden.json",
			wantExit:   2,
			run: func(r renderer) int {
				return r.failure("cli", &ipgw.Error{Code: ipgw.CodeInvalidArgument})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := test.run(renderer{mode: outputJSON, out: &stdout, err: &stderr})
			if exit != test.wantExit {
				t.Fatalf("exit = %d, want %d", exit, test.wantExit)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			want, err := os.ReadFile(filepath.Join("testdata", test.goldenFile))
			if err != nil {
				t.Fatalf("read golden file: %v", err)
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("JSON contract changed\n got: %s\nwant: %s", stdout.Bytes(), want)
			}
			decodeSingleEnvelope(t, stdout.Bytes())
		})
	}
}

func TestJSONGoldenConsumerIgnoresAdditiveFields(t *testing.T) {
	raw := []byte("{\"schema_version\":1,\"command\":\"status\",\"ok\":true,\"data\":null,\"future_field\":{\"ignored\":true}}\n")
	object := decodeSingleEnvelope(t, raw)
	assertEnvelopeXOR(t, object, true)
	if object["future_field"] == nil {
		t.Fatal("test fixture is missing its additive field")
	}

	// A canceled operation remains a transport-level cancellation even in JSON.
	var stdout bytes.Buffer
	exit := (renderer{mode: outputJSON, out: &stdout, err: &bytes.Buffer{}}).failure("status", context.Canceled)
	if exit != 130 {
		t.Fatalf("canceled JSON exit = %d, want 130", exit)
	}
}
