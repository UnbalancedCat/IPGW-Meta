package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	ipgw "github.com/UnbalancedCat/ipgw-meta"
)

func TestIsSafeTerminalHelperRequiresBothEnds(t *testing.T) {
	stdin := os.Stdin
	stderr := os.Stderr
	if isSafeTerminal(nil, stderr, func(int) bool { return true }) {
		t.Fatal("nil stdin was considered safe")
	}
	if isSafeTerminal(stdin, nil, func(int) bool { return true }) {
		t.Fatal("nil stderr was considered safe")
	}
	if isSafeTerminal(stdin, stderr, nil) {
		t.Fatal("nil checker was considered safe")
	}

	tests := []struct {
		name         string
		stdinResult  bool
		stderrResult bool
		want         bool
		wantCalls    int
	}{
		{name: "neither", want: false, wantCalls: 1},
		{name: "stdin only", stdinResult: true, want: false, wantCalls: 2},
		{name: "stderr only", stderrResult: true, want: false, wantCalls: 1},
		{name: "both", stdinResult: true, stderrResult: true, want: true, wantCalls: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			got := isSafeTerminal(stdin, stderr, func(fd int) bool {
				calls++
				switch fd {
				case int(stdin.Fd()):
					return test.stdinResult
				case int(stderr.Fd()):
					return test.stderrResult
				default:
					t.Fatalf("unexpected fd %d", fd)
					return false
				}
			})
			if got != test.want {
				t.Fatalf("IsSafeTerminal = %v, want %v", got, test.want)
			}
			if calls != test.wantCalls {
				t.Fatalf("checker calls = %d, want %d", calls, test.wantCalls)
			}
		})
	}
}

func TestIsSafeTerminalRejectsOrdinaryFiles(t *testing.T) {
	stdin, err := os.CreateTemp(t.TempDir(), "stdin-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stderr, err := os.CreateTemp(t.TempDir(), "stderr-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()

	if IsSafeTerminal(stdin, stderr) {
		t.Fatal("ordinary files were considered safe terminals")
	}
	if IsSafeTerminal(nil, stderr) || IsSafeTerminal(stdin, nil) {
		t.Fatal("a nil terminal endpoint was considered safe")
	}
}

func TestStartupFailureHumanIsFixedConfigFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := StartupFailure(nil, &stdout, &stderr, errors.New(wireCanary))
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if got := stderr.String(); got != "Error: configuration error\n" {
		t.Fatalf("stderr = %q", got)
	}
	if bytes.Contains(stderr.Bytes(), []byte(wireCanary)) {
		t.Fatal("startup cause leaked")
	}

	// A context-shaped cause is still a startup/config error, not exit 130.
	if got := StartupFailure(nil, io.Discard, io.Discard, context.Canceled); got != 2 {
		t.Fatalf("context-shaped startup cause exit = %d, want 2", got)
	}
}

func TestStartupFailurePostfixedJSONIsSingleEnvelope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := StartupFailure([]string{"--config", "--output=json"}, &stdout, &stderr, errors.New(wireCanary))
	if exit != 2 {
		t.Fatalf("exit = %d, want 2", exit)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte(wireCanary)) {
		t.Fatal("startup cause leaked into JSON")
	}
	object := decodeSingleEnvelope(t, stdout.Bytes())
	assertEnvelopeXOR(t, object, false)
	if object["command"] != "cli" || object["ok"] != false {
		t.Fatalf("envelope = %#v", object)
	}
	wiredError := objectField(t, object, "error")
	if wiredError["code"] != string(ipgw.CodeConfig) || wiredError["message"] != "configuration error" {
		t.Fatalf("error = %#v", wiredError)
	}
}

func TestStartupFailureEncodeErrorReturnsOne(t *testing.T) {
	writer := &startupFailWriter{}
	var stderr bytes.Buffer
	exit := StartupFailure([]string{"--json"}, writer, &stderr, errors.New(wireCanary))
	if exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if writer.writes != 1 {
		t.Fatalf("writes = %d, want one encode attempt", writer.writes)
	}
	if got := stderr.String(); got != "Error: unable to write JSON output\n" {
		t.Fatalf("stderr = %q", got)
	}
	if bytes.Contains(stderr.Bytes(), []byte(wireCanary)) {
		t.Fatal("startup/encoder cause leaked")
	}
}

type startupFailWriter struct{ writes int }

func (w *startupFailWriter) Write([]byte) (int, error) {
	w.writes++
	return 0, errors.New(wireCanary)
}
