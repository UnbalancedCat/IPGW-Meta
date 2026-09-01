package main

import (
	"bytes"
	"context"
	"path/filepath"
	"runtime"
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

func TestNormalizeVerifyRoot(t *testing.T) {
	absolute := filepath.Join(t.TempDir(), "candidate-set")
	tests := []struct {
		name  string
		input string
		ok    bool
	}{
		{name: "empty"},
		{name: "relative", input: filepath.Join("relative", "candidate-set")},
		{name: "clean absolute", input: absolute, ok: true},
		{name: "absolute dot", input: absolute + string(filepath.Separator) + ".", ok: true},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests, struct {
			name  string
			input string
			ok    bool
		}{
			name:  "windows mixed separators",
			input: filepath.Dir(absolute) + "/" + filepath.Base(absolute),
			ok:    true,
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := normalizeVerifyRoot(test.input)
			if ok != test.ok {
				t.Fatalf("normalizeVerifyRoot() ok = %v, want %v", ok, test.ok)
			}
			if ok && got != filepath.Clean(test.input) {
				t.Fatalf("normalizeVerifyRoot() = %q, want %q", got, filepath.Clean(test.input))
			}
		})
	}
}
