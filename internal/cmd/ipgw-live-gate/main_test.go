package main

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/UnbalancedCat/ipgw-meta/internal/livegate"
)

func TestRunMainUsesFixedSafeDiagnostics(t *testing.T) {
	root := t.TempDir()
	stdin := createMainTestFile(t, root, "stdin")
	stdout := createMainTestFile(t, root, "stdout")
	stderr := createMainTestFile(t, root, "stderr")
	canary := "PRIVATE-CANARY"

	exit := runMain(
		context.Background(),
		[]string{"run", "--unknown", canary},
		stdin,
		stdout,
		stderr,
		true,
	)
	if exit != int(livegate.GateExitSecurityReject) {
		t.Fatalf("runMain() = %d, want %d", exit, livegate.GateExitSecurityReject)
	}
	diagnostics, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if got := string(diagnostics); got != "ipgw-live-gate: security reject\n" {
		t.Fatalf("diagnostics = %q", got)
	}
	if strings.Contains(string(diagnostics), canary) {
		t.Fatal("runner diagnostics leaked an argument")
	}
	output, err := os.ReadFile(stdout.Name())
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 0 {
		t.Fatalf("unexpected stdout: %q", output)
	}
}

func TestRunMainNilContextIsInternalWithoutPanic(t *testing.T) {
	root := t.TempDir()
	stdin := createMainTestFile(t, root, "stdin")
	stdout := createMainTestFile(t, root, "stdout")
	stderr := createMainTestFile(t, root, "stderr")
	if exit := runMain(nil, nil, stdin, stdout, stderr, false); exit != int(livegate.GateExitInternal) {
		t.Fatalf("runMain() = %d, want %d", exit, livegate.GateExitInternal)
	}
}

func createMainTestFile(t *testing.T, dir, pattern string) *os.File {
	t.Helper()
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = file.Close()
	})
	return file
}
