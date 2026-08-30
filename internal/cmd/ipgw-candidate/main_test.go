package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestRunRejectsInvalidCommandsWithFixedDiagnostics(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		exit       int
		diagnostic string
	}{
		{name: "empty", exit: 2, diagnostic: "ipgw-candidate: invalid invocation\n"},
		{name: "unknown", args: []string{"unknown", "PRIVATE-CANARY"}, exit: 2, diagnostic: "ipgw-candidate: invalid invocation\n"},
		{name: "bad verify", args: []string{"verify", "--candidate-set", "relative/PRIVATE-CANARY"}, exit: 1, diagnostic: "ipgw-candidate: operation failed\n"},
		{name: "missing assemble", args: []string{"assemble"}, exit: 1, diagnostic: "ipgw-candidate: operation failed\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer
			if exit := run(context.Background(), test.args, &stdout, &stderr); exit != test.exit {
				t.Fatalf("run() = %d, want %d", exit, test.exit)
			}
			if stdout.Len() != 0 || stderr.String() != test.diagnostic {
				t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if strings.Contains(stderr.String(), "PRIVATE-CANARY") {
				t.Fatal("diagnostics leaked an argument")
			}
		})
	}
}

func TestRunNilContextIsInvalid(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if exit := run(nil, []string{"verify"}, &stdout, &stderr); exit != 2 {
		t.Fatalf("run(nil) = %d", exit)
	}
}
