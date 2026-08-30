package candidate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateGoBinaryAcceptsFrozenBuildRecipe(t *testing.T) {
	module := newBuildInfoModule(t)
	for _, target := range targetOrder {
		t.Run(target, func(t *testing.T) {
			binary := buildInfoFixture(t, module, target, "ipgw-meta", false, "")
			if err := validateGoBinary(binary, target, "ipgw-meta", false, runtime.Version()); err != nil {
				t.Fatalf("validateGoBinary() error = %v", err)
			}
		})
	}
	for _, command := range []string{"ipgw", "ipgw-legacy"} {
		t.Run(command, func(t *testing.T) {
			binary := buildInfoFixture(t, module, "linux-amd64", command, false, "")
			if err := validateGoBinary(binary, "linux-amd64", command, false, runtime.Version()); err != nil {
				t.Fatalf("validateGoBinary() error = %v", err)
			}
		})
	}
	for _, target := range []string{"linux-amd64", "windows-amd64"} {
		t.Run("helper-"+target, func(t *testing.T) {
			binary := buildInfoFixture(t, module, target, "ipgw-live-gate", true, "")
			if err := validateGoBinary(binary, target, "ipgw-live-gate", true, runtime.Version()); err != nil {
				t.Fatalf("validateGoBinary() error = %v", err)
			}
		})
	}
}

func TestValidateGoBinaryRejectsRecipeDrift(t *testing.T) {
	module := newBuildInfoModule(t)
	valid := buildInfoFixture(t, module, "linux-amd64", "ipgw-meta", false, "")
	for name, check := range map[string]func() error{
		"wrong version": func() error { return validateGoBinary(valid, "linux-amd64", "ipgw-meta", false, "go0.0") },
		"wrong target":  func() error { return validateGoBinary(valid, "linux-arm64", "ipgw-meta", false, runtime.Version()) },
		"wrong command": func() error { return validateGoBinary(valid, "linux-amd64", "ipgw", false, runtime.Version()) },
		"helper path":   func() error { return validateGoBinary(valid, "linux-amd64", "ipgw-live-gate", true, runtime.Version()) },
	} {
		t.Run(name, func(t *testing.T) {
			if err := check(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("validateGoBinary() error = %v", err)
			}
		})
	}
	unstripped := buildInfoFixture(t, module, "linux-amd64", "ipgw-meta", false, "unstripped")
	if err := validateGoBinary(unstripped, "linux-amd64", "ipgw-meta", false, runtime.Version()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("unstripped error = %v", err)
	}
	drift := buildInfoFixture(t, module, "linux-amd64", "ipgw-meta", false, "goamd64")
	if err := validateGoBinary(drift, "linux-amd64", "ipgw-meta", false, runtime.Version()); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("GOAMD64 drift error = %v", err)
	}
}

func newBuildInfoModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTestFile(t, root, "go.mod", []byte("module "+candidateModulePath+"\n\ngo 1.25.0\n"), 0o644)
	for _, command := range productOrder {
		writeTestFile(t, root, filepath.Join("cmd", command, "main.go"), []byte("package main\nvar version = \"dev\"\nfunc main() { println(version) }\n"), 0o644)
	}
	writeTestFile(t, root, filepath.Join("internal", "cmd", "ipgw-live-gate", "main.go"), []byte("package main\nfunc main() {}\n"), 0o644)
	return root
}

func buildInfoFixture(t *testing.T, module, target, command string, helper bool, drift string) []byte {
	t.Helper()
	goos, goarch, ok := splitBuildTarget(target)
	if !ok {
		t.Fatalf("invalid fixture target %q", target)
	}
	packagePath := "./cmd/" + command
	ldflags := "-s -w -X main.version=" + Version
	if helper {
		packagePath = "./internal/cmd/ipgw-live-gate"
		ldflags = "-s -w"
	}
	output := filepath.Join(t.TempDir(), strings.ReplaceAll(target+"-"+command, "/", "-"))
	environment := []string{"CGO_ENABLED=0", "GOOS=" + goos, "GOARCH=" + goarch, "GOTOOLCHAIN=local"}
	if goarch == "amd64" {
		environment = append(environment, "GOAMD64="+GOAMD64)
	} else {
		environment = append(environment, "GOARM64="+GOARM64)
	}
	args := []string{"build", "-trimpath", "-buildvcs=false", "-ldflags", ldflags, "-o", output, packagePath}
	if drift == "unstripped" {
		args = []string{"build", "-trimpath", "-buildvcs=false", "-o", output, packagePath}
	}
	if drift == "goamd64" {
		environment = append(environment, "GOAMD64=v3")
	}
	runTestCommand(t, module, environment, "go", args...)
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
