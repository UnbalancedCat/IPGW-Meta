package srun

import (
	"errors"
	"fmt"
	"testing"
)

func TestParseStatusOnlineJSONAndJSONP(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		username string
		ip       string
		traffic  int64
		duration int64
		balance  int64
	}{
		{
			name:     "JSON strings",
			input:    `{"error":"ok","user_name":"alice","online_ip":"10.0.0.8","sum_bytes":"1234","sum_seconds":"56","user_balance":"12.34"}`,
			username: "alice", ip: "10.0.0.8", traffic: 1234, duration: 56, balance: 1234,
		},
		{
			name:     "JSONP numeric username and equivalent aliases",
			input:    `callback_1({"error":"ok","error_code":"ok","user_name":20260001,"username":"20260001","online_ip":"10.0.0.9","sum_bytes":1234,"used_bytes":"1234","sum_seconds":56,"duration":"56","user_balance":"12.3400","balance":12.34});`,
			username: "20260001", ip: "10.0.0.9", traffic: 1234, duration: 56, balance: 1234,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			status, err := ParseStatus([]byte(testCase.input))
			if err != nil {
				t.Fatalf("ParseStatus() error = %v", err)
			}
			if !status.Online || status.Username != testCase.username || status.OnlineIP.String() != testCase.ip {
				t.Fatalf("identity = %#v", status)
			}
			if status.Summary == nil || status.Summary.TrafficBytes != testCase.traffic || status.Summary.DurationSeconds != testCase.duration || status.Summary.BalanceMinor == nil || *status.Summary.BalanceMinor != testCase.balance {
				t.Fatalf("summary = %#v", status.Summary)
			}
		})
	}

	withoutSummary, err := ParseStatus([]byte(`{"error":"ok","user_name":"alice","online_ip":"10.0.0.8"}`))
	if err != nil || !withoutSummary.Online || withoutSummary.Summary != nil {
		t.Fatalf("online status without summary = %#v, %v", withoutSummary, err)
	}
}

func TestParseStatusOfflineRequiresUnambiguousMarker(t *testing.T) {
	for _, input := range []string{
		`not_online_error`,
		`{"error":"not_online_error","res":"not_online_error","client_ip":"10.0.0.7","online_ip":"","user_name":""}`,
	} {
		status, err := ParseStatus([]byte(input))
		if err != nil || status.Online || status.OnlineIP.IsValid() || status.Summary != nil {
			t.Fatalf("offline status = %#v, %v", status, err)
		}
	}

	requireUnrecognizedStatus(t, `{"error":"not_online_error","user_name":"alice","online_ip":"10.0.0.8"}`)
	requireUnrecognizedStatus(t, `{"error":"not_online_error","sum_bytes":1,"sum_seconds":1}`)
}

func TestParseStatusRejectsAliasConflictsAndDuplicateKeys(t *testing.T) {
	for _, input := range []string{
		`{"error":"ok","res":"not_online_error","user_name":"alice","online_ip":"10.0.0.8"}`,
		`{"error":"ok","user_name":"alice","username":"bob","online_ip":"10.0.0.8"}`,
		`{"error":"ok","user_name":"alice","online_ip":"10.0.0.8","sum_bytes":1,"used_bytes":2,"sum_seconds":1}`,
		`{"error":"ok","user_name":"alice","online_ip":"10.0.0.8","sum_bytes":1,"sum_seconds":1,"duration":2}`,
		`{"error":"ok","user_name":"alice","online_ip":"10.0.0.8","sum_bytes":1,"sum_seconds":1,"user_balance":"1.00","balance":"2.00"}`,
		`{"error":"ok","error":"ok","user_name":"alice","online_ip":"10.0.0.8"}`,
		`{"error":"ok","err\u006fr":"ok","user_name":"alice","online_ip":"10.0.0.8"}`,
	} {
		requireUnrecognizedStatus(t, input)
	}
}

func TestParseStatusRejectsUnknownMissingAndInvalidIdentity(t *testing.T) {
	for _, input := range []string{
		`{"error":"unknown","user_name":"alice","online_ip":"10.0.0.8"}`,
		`<html><body>gateway status</body></html>`,
		`{"error":"ok","online_ip":"10.0.0.8"}`,
		`{"error":"ok","user_name":"alice"}`,
		`{"error":"ok","user_name":"alice","client_ip":"10.0.0.8"}`,
		`{"error":"ok","user_name":"bad user","online_ip":"10.0.0.8"}`,
		`{"error":"ok","user_name":"alice","online_ip":"not-an-ip"}`,
		`{"error":"ok","user_name":"alice","online_ip":"2001:db8::1"}`,
		`{"error":"ok","user_name":"alice","online_ip":"::ffff:10.0.0.8"}`,
		`{"error":"ok","user_name":"alice","online_ip":"127.0.0.1"}`,
		`{"error":"ok","user_name":"alice","online_ip":"169.254.1.2"}`,
		`{"error":"ok","user_name":"alice","online_ip":"224.0.0.1"}`,
		`{"error":"ok","user_name":"alice","online_ip":"0.0.0.0"}`,
		`{"error":"ok","user_name":"alice","online_ip":"10.0.0.8",`,
	} {
		requireUnrecognizedStatus(t, input)
	}
}

func TestParseStatusRejectsInvalidOrPartialSummary(t *testing.T) {
	for _, fields := range []string{
		`"sum_bytes":-1,"sum_seconds":1`,
		`"sum_bytes":1.5,"sum_seconds":1`,
		`"sum_bytes":1e2,"sum_seconds":1`,
		`"sum_bytes":"9223372036854775808","sum_seconds":1`,
		`"sum_bytes":1,"sum_seconds":-1`,
		`"sum_bytes":1,"sum_seconds":1.5`,
		`"sum_bytes":1,"sum_seconds":1e2`,
		`"sum_bytes":1,"sum_seconds":"9223372036854775808"`,
		`"sum_bytes":1`,
		`"sum_seconds":1`,
		`"user_balance":"12.34"`,
		`"sum_bytes":1,"sum_seconds":1,"user_balance":"12.3456"`,
		`"sum_bytes":1,"sum_seconds":1,"user_balance":"92233720368547758.08"`,
	} {
		input := `{"error":"ok","user_name":"alice","online_ip":"10.0.0.8",` + fields + `}`
		requireUnrecognizedStatus(t, input)
	}
}

func TestParseStatusBalanceIsExactMinorUnits(t *testing.T) {
	for _, balance := range []string{"12.34", "12.3400"} {
		input := `{"error":"ok","user_name":"alice","online_ip":"10.0.0.8","sum_bytes":1,"sum_seconds":2,"user_balance":"` + balance + `"}`
		status, err := ParseStatus([]byte(input))
		if err != nil || status.Summary == nil || status.Summary.BalanceMinor == nil || *status.Summary.BalanceMinor != 1234 {
			t.Fatalf("balance %q = %#v, %v", balance, status.Summary, err)
		}
	}
}

func TestParseStatusLegacyCSV(t *testing.T) {
	t.Run("complete summary", func(t *testing.T) {
		status, err := ParseStatus([]byte(`alice,a,b,c,d,e,2048,60,10.0.0.10,j,k,8.50`))
		if err != nil || !status.Online || status.Username != "alice" || status.OnlineIP.String() != "10.0.0.10" || status.Summary == nil || status.Summary.TrafficBytes != 2048 || status.Summary.DurationSeconds != 60 || status.Summary.BalanceMinor == nil || *status.Summary.BalanceMinor != 850 {
			t.Fatalf("CSV status = %#v, %v", status, err)
		}
	})

	t.Run("summary without balance", func(t *testing.T) {
		status, err := ParseStatus([]byte(`alice,a,b,c,d,e,2048,60,10.0.0.10`))
		if err != nil || !status.Online || status.Summary == nil || status.Summary.TrafficBytes != 2048 || status.Summary.DurationSeconds != 60 || status.Summary.BalanceMinor != nil {
			t.Fatalf("CSV status = %#v, %v", status, err)
		}
	})

	t.Run("summary with empty balance", func(t *testing.T) {
		status, err := ParseStatus([]byte(`alice,a,b,c,d,e,2048,60,10.0.0.10,j,k,`))
		if err != nil || !status.Online || status.Summary == nil || status.Summary.TrafficBytes != 2048 || status.Summary.DurationSeconds != 60 || status.Summary.BalanceMinor != nil {
			t.Fatalf("CSV status = %#v, %v", status, err)
		}
	})

	for _, testCase := range []struct {
		name  string
		input string
	}{
		{name: "empty summary", input: `alice,a,b,c,d,e,,,10.0.0.10`},
		{name: "opaque summary", input: `alice,a,b,c,d,e,opaque,opaque,10.0.0.10`},
		{name: "partial traffic", input: `alice,a,b,c,d,e,1,opaque,10.0.0.10`},
		{name: "partial duration", input: `alice,a,b,c,d,e,opaque,2,10.0.0.10`},
		{name: "negative traffic", input: `alice,a,b,c,d,e,-1,2,10.0.0.10`},
		{name: "fractional duration", input: `alice,a,b,c,d,e,1,2.5,10.0.0.10`},
		{name: "exponent duration", input: `alice,a,b,c,d,e,1,1e2,10.0.0.10`},
		{name: "traffic overflow", input: `alice,a,b,c,d,e,9223372036854775808,2,10.0.0.10`},
		{name: "invalid balance", input: `alice,a,b,c,d,e,1,2,10.0.0.10,j,k,12.3456`},
		{name: "balance overflow", input: `alice,a,b,c,d,e,1,2,10.0.0.10,j,k,92233720368547758.08`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			status, err := ParseStatus([]byte(testCase.input))
			if err != nil || !status.Online || status.Username != "alice" || status.OnlineIP.String() != "10.0.0.10" || status.Summary != nil {
				t.Fatalf("CSV status = %#v, %v", status, err)
			}
		})
	}

	for _, input := range []string{
		`alice,a,b,c,d,e,1,2`,
		`alice,a,b,c,d,e,1,2,127.0.0.1`,
		`alice,a,b,c,d,e,1,2,2001:db8::1`,
		`bad user,a,b,c,d,e,1,2,10.0.0.10`,
		`,a,b,c,d,e,1,2,10.0.0.10`,
		`alice,a,b,c,d,"unterminated,1,2,10.0.0.10`,
		"alice,a,b,c,\"line\nbreak\",e,1,2,10.0.0.10",
		"alice,a,b,c,d,e,1,2,10.0.0.10\nbob,a,b,c,d,e,1,2,10.0.0.11",
	} {
		requireUnrecognizedStatus(t, input)
	}
}

func requireUnrecognizedStatus(t *testing.T, input string) {
	t.Helper()
	if _, err := ParseStatus([]byte(input)); !errors.Is(err, ErrUnrecognized) {
		t.Fatalf("ParseStatus(%s) error = %v", fmt.Sprintf("%q", input), err)
	}
}
