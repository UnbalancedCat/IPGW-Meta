package launcher

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const launcherCanary = "LAUNCHER-CANARY-MUST-NOT-LEAK"

func TestModeResolutionPriority(t *testing.T) {
	t.Run("explicit skips environment and config", func(t *testing.T) {
		envCalls := 0
		configCalls := 0
		resolution, failure := resolveMode(withDefaults(Options{
			Args:           []string{"status", "--mode=legacy", "--json"},
			InstallDefault: "meta",
			lookupEnv: func(string) (string, bool) {
				envCalls++
				return "meta", true
			},
			userConfigDir: func() (string, error) {
				configCalls++
				return "", errors.New(launcherCanary)
			},
		}))
		if failure != nil || resolution.Mode != ModeLegacy {
			t.Fatalf("resolution=%#v failure=%#v", resolution, failure)
		}
		if !reflect.DeepEqual(resolution.Args, []string{"status", "--json"}) {
			t.Fatalf("args = %#v", resolution.Args)
		}
		if envCalls != 0 || configCalls != 0 {
			t.Fatalf("lower-priority sources were read: env=%d config=%d", envCalls, configCalls)
		}
	})

	t.Run("environment skips config", func(t *testing.T) {
		configCalls := 0
		resolution, failure := resolveMode(withDefaults(Options{
			Args:           []string{"status"},
			InstallDefault: "meta",
			lookupEnv:      func(string) (string, bool) { return "legacy", true },
			userConfigDir: func() (string, error) {
				configCalls++
				return "", errors.New(launcherCanary)
			},
		}))
		if failure != nil || resolution.Mode != ModeLegacy || configCalls != 0 {
			t.Fatalf("resolution=%#v failure=%#v config calls=%d", resolution, failure, configCalls)
		}
	})

	t.Run("config precedes install default", func(t *testing.T) {
		configDir := t.TempDir()
		writeLauncherConfig(t, configDir, "schema_version: 1\nmode: legacy\ncohort: existing-install\n")
		resolution, failure := resolveMode(withDefaults(Options{
			Args:           []string{"status"},
			InstallDefault: "meta",
			lookupEnv:      func(string) (string, bool) { return "", false },
			userConfigDir:  func() (string, error) { return configDir, nil },
		}))
		if failure != nil || resolution.Mode != ModeLegacy {
			t.Fatalf("resolution=%#v failure=%#v", resolution, failure)
		}
	})

	t.Run("missing config uses default without writing", func(t *testing.T) {
		configDir := t.TempDir()
		resolution, failure := resolveMode(withDefaults(Options{
			Args:           []string{"status"},
			InstallDefault: "meta",
			lookupEnv:      func(string) (string, bool) { return "", false },
			userConfigDir:  func() (string, error) { return configDir, nil },
		}))
		if failure != nil || resolution.Mode != ModeMeta {
			t.Fatalf("resolution=%#v failure=%#v", resolution, failure)
		}
		path := filepath.Join(configDir, "ipgw-meta", "launcher.yaml")
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("launcher wrote missing config: %v", err)
		}
	})
}

func TestLauncherConfigIsStrictAndBounded(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "unknown field", content: "schema_version: 1\nmode: meta\nunknown_field: " + launcherCanary + "\n"},
		{name: "multiple documents", content: "schema_version: 1\nmode: meta\n---\nschema_version: 1\nmode: legacy\n"},
		{name: "wrong schema", content: "schema_version: 2\nmode: meta\n"},
		{name: "invalid mode", content: "schema_version: 1\nmode: other\n"},
		{name: "noncanonical mode", content: "schema_version: 1\nmode: META\n"},
		{name: "duplicate field", content: "schema_version: 1\nmode: meta\nmode: legacy\n"},
		{name: "oversized", content: strings.Repeat("x", maxLauncherBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "launcher.yaml")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, found, failure := loadConfiguredMode(path)
			if found || failure == nil || failure.kind != failureConfiguration {
				t.Fatalf("found=%v failure=%#v", found, failure)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "launcher.yaml")
	if err := os.WriteFile(path, []byte("schema_version: 1\nmode: meta\nchosen_at: 2026-08-27T00:00:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mode, found, failure := loadConfiguredMode(path)
	if failure != nil || !found || mode != ModeMeta {
		t.Fatalf("valid config: mode=%q found=%v failure=%#v", mode, found, failure)
	}
}

func TestExecuteForwardsArgsAndStreamsToExactSibling(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	executable := filepath.Join(bundle, "ipgw.exe")
	stdin := strings.NewReader("synthetic input")
	var stdout, stderr bytes.Buffer
	var capturedPath string
	var capturedArgs []string
	var capturedStdin io.Reader
	var capturedStdout, capturedStderr io.Writer

	exit := Execute(Options{
		Args:           []string{"status", "--mode=legacy", "--json", "--", "--mode", "meta"},
		InstallDefault: "meta",
		Stdin:          stdin,
		Stdout:         &stdout,
		Stderr:         &stderr,
		lookupEnv:      func(string) (string, bool) { t.Fatal("environment must not be read"); return "", false },
		userConfigDir:  func() (string, error) { t.Fatal("config must not be read"); return "", nil },
		executable:     func() (string, error) { return executable, nil },
		runChild: func(path string, args []string, in io.Reader, out, errOut io.Writer) (int, error) {
			capturedPath = path
			capturedArgs = append([]string(nil), args...)
			capturedStdin, capturedStdout, capturedStderr = in, out, errOut
			return 130, nil
		},
	})
	if exit != 130 {
		t.Fatalf("exit = %d, want 130", exit)
	}
	wantPath := filepath.Join(bundle, "ipgw-legacy.exe")
	if capturedPath != wantPath || !filepath.IsAbs(capturedPath) {
		t.Fatalf("target = %q, want exact absolute sibling %q", capturedPath, wantPath)
	}
	wantArgs := []string{"status", "--json", "--", "--mode", "meta"}
	if !reflect.DeepEqual(capturedArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", capturedArgs, wantArgs)
	}
	if capturedStdin != stdin || capturedStdout != &stdout || capturedStderr != &stderr {
		t.Fatal("child did not inherit launcher streams")
	}
}

func TestSiblingExecutableNeverUsesPATH(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "bundle")
	for _, test := range []struct {
		executable string
		mode       Mode
		want       string
	}{
		{executable: filepath.Join(bundle, "ipgw"), mode: ModeMeta, want: filepath.Join(bundle, "ipgw-meta")},
		{executable: filepath.Join(bundle, "renamed.exe"), mode: ModeLegacy, want: filepath.Join(bundle, "ipgw-legacy.exe")},
	} {
		got, err := siblingExecutable(test.executable, test.mode)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want || !filepath.IsAbs(got) {
			t.Errorf("siblingExecutable(%q, %q) = %q, want %q", test.executable, test.mode, got, test.want)
		}
	}
}

func TestChildExitCodesArePropagated(t *testing.T) {
	for _, want := range []int{0, 2, 3, 7, 130} {
		t.Run(string(rune(want)), func(t *testing.T) {
			exit := Execute(Options{
				Args:           []string{"--mode=meta", "status"},
				InstallDefault: "legacy",
				Stdout:         io.Discard,
				Stderr:         io.Discard,
				executable:     func() (string, error) { return filepath.Join(t.TempDir(), "ipgw"), nil },
				runChild: func(string, []string, io.Reader, io.Writer, io.Writer) (int, error) {
					return want, nil
				},
			})
			if exit != want {
				t.Fatalf("exit = %d, want %d", exit, want)
			}
		})
	}
}

func TestLauncherFailuresAreFixedAndJSONSafe(t *testing.T) {
	tests := []struct {
		name     string
		options  func(*testing.T, *bytes.Buffer, *bytes.Buffer) Options
		wantCode string
		wantExit int
	}{
		{
			name: "invalid mode with postfixed json",
			options: func(t *testing.T, stdout, stderr *bytes.Buffer) Options {
				return Options{Args: []string{"--mode", "--json"}, InstallDefault: "meta", Stdout: stdout, Stderr: stderr}
			},
			wantCode: "invalid_argument", wantExit: 2,
		},
		{
			name: "config error",
			options: func(t *testing.T, stdout, stderr *bytes.Buffer) Options {
				return Options{
					Args: []string{"--output=json"}, InstallDefault: "meta", Stdout: stdout, Stderr: stderr,
					lookupEnv:     func(string) (string, bool) { return "", false },
					userConfigDir: func() (string, error) { return "", errors.New(launcherCanary) },
				}
			},
			wantCode: "config", wantExit: 2,
		},
		{
			name: "child startup error",
			options: func(t *testing.T, stdout, stderr *bytes.Buffer) Options {
				return Options{
					Args: []string{"--json", "--mode=meta"}, InstallDefault: "legacy", Stdout: stdout, Stderr: stderr,
					executable: func() (string, error) { return filepath.Join(t.TempDir(), "ipgw"), nil },
					runChild: func(string, []string, io.Reader, io.Writer, io.Writer) (int, error) {
						return 0, errors.New(launcherCanary)
					},
				}
			},
			wantCode: "internal", wantExit: 1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := Execute(test.options(t, &stdout, &stderr))
			if exit != test.wantExit {
				t.Fatalf("exit = %d, want %d", exit, test.wantExit)
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q, want empty", stderr.String())
			}
			if bytes.Contains(stdout.Bytes(), []byte(launcherCanary)) {
				t.Fatal("launcher cause leaked")
			}
			object := decodeLauncherEnvelope(t, stdout.Bytes())
			if object["command"] != "cli" || object["ok"] != false {
				t.Fatalf("envelope = %#v", object)
			}
			wiredError := launcherObjectField(t, object, "error")
			if wiredError["code"] != test.wantCode {
				t.Fatalf("error = %#v", wiredError)
			}
			if _, data := object["data"]; data {
				t.Fatal("failure envelope contains data")
			}
		})
	}

	if preparseJSON([]string{"--", "--json"}) {
		t.Fatal("JSON flag after -- changed launcher diagnostics")
	}
}

func TestLauncherHumanStartupErrorIsFixed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := Execute(Options{
		Args:           []string{"--mode=meta"},
		InstallDefault: "meta",
		Stdout:         &stdout,
		Stderr:         &stderr,
		executable:     func() (string, error) { return "", errors.New(launcherCanary) },
	})
	if exit != 1 || stdout.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q", exit, stdout.String())
	}
	if got := stderr.String(); got != "Error: launcher could not start selected mode\n" {
		t.Fatalf("stderr = %q", got)
	}
	if strings.Contains(stderr.String(), launcherCanary) {
		t.Fatal("startup canary leaked")
	}
}

func writeLauncherConfig(t *testing.T, configDir, content string) {
	t.Helper()
	dir := filepath.Join(configDir, "ipgw-meta")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "launcher.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func decodeLauncherEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	if len(raw) == 0 || raw[len(raw)-1] != '\n' || bytes.Count(raw, []byte{'\n'}) != 1 {
		t.Fatalf("launcher JSON is not one newline-terminated object: %q", raw)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing JSON: err=%v value=%#v", err, trailing)
	}
	return object
}

func launcherObjectField(t *testing.T, object map[string]any, name string) map[string]any {
	t.Helper()
	value, ok := object[name].(map[string]any)
	if !ok {
		t.Fatalf("field %q = %#v, want object", name, object[name])
	}
	return value
}
